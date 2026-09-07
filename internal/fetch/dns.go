package fetch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

const (
	pageWait  = 45 * time.Second
	pollEvery = time.Second
	chromeUA  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36"
	extractJS = `(function(){
		var html = document.documentElement ? document.documentElement.innerHTML : '';
		var price = document.querySelector('div.product-buy__price');
		var scripts = document.querySelectorAll('script[type="application/ld+json"]');
		var ld = [];
		for (var i = 0; i < scripts.length; i++) { ld.push(scripts[i].textContent || ''); }
		return {
			qrator: html.indexOf('/__qrator/') !== -1 || html.indexOf('qauth_handle_validate') !== -1,
			ldjson: ld,
			cssPrice: price ? (price.textContent || '') : '',
			title: document.title || ''
		};
	})()`
)

// DNS читает карточки через один долгоживущий Chrome.
type DNS struct {
	mu            sync.Mutex
	log           *slog.Logger
	profileDir    string
	chromePath    string
	headless      bool
	breaker       *Breaker
	allocCancel   context.CancelFunc
	browser       context.Context
	browserCancel context.CancelFunc
}

type DNSOptions struct {
	ProfileDir      string
	ChromePath      string
	Headless        bool
	CircuitCooldown time.Duration
	Log             *slog.Logger
}

func NewDNS(opt DNSOptions) *DNS {
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	return &DNS{
		log:        opt.Log,
		profileDir: opt.ProfileDir,
		chromePath: opt.ChromePath,
		headless:   opt.Headless,
		breaker:    NewBreaker(opt.CircuitCooldown),
	}
}

func (d *DNS) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopLocked()
}

func (d *DNS) stopLocked() {
	if d.browserCancel != nil {
		d.browserCancel()
		d.browserCancel = nil
	}
	if d.allocCancel != nil {
		d.allocCancel()
		d.allocCancel = nil
	}
	d.browser = nil
}

func (d *DNS) Breaker() *Breaker { return d.breaker }

// Fetch открывает карточку в уже запущенном Chrome. Повторных попыток нет:
// при челлендже сразу открываем предохранитель.
func (d *DNS) Fetch(ctx context.Context, p storage.Product) (Snapshot, error) {
	if !d.breaker.Allow() {
		return Snapshot{}, fmt.Errorf("%w: пауза до %s (%s)",
			ErrChallenge, d.breaker.RetryAt().Format(time.RFC3339), d.breaker.Reason())
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.ensureBrowserLocked(); err != nil {
		return Snapshot{}, err
	}

	runCtx, cancel := context.WithTimeout(d.browser, pageWait+15*time.Second)
	defer cancel()

	actions := []chromedp.Action{
		network.Enable(),
		network.SetBlockedURLs([]string{
			"*.woff", "*.woff2", "*.ttf", "*.mp4", "*.webm",
		}),
		hideWebdriver(),
		setCityCookie(p.City),
		chromedp.Navigate(p.URL),
	}
	if err := chromedp.Run(runCtx, actions...); err != nil {
		d.maybeTrip(err)
		return Snapshot{}, fmt.Errorf("dns: навигация %s: %w", p.URL, err)
	}

	snap, err := waitForPrice(runCtx)
	if err != nil {
		d.maybeTrip(err)
		return Snapshot{}, err
	}
	return snap, nil
}

func (d *DNS) maybeTrip(err error) {
	if err == nil {
		return
	}
	if errors.Is(err, ErrChallenge) || isBanError(err) {
		d.breaker.Trip(err.Error())
		d.log.Warn("dns: предохранитель включён", "until", d.breaker.RetryAt(), "reason", err)
	}
}

func isBanError(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, n := range []string{
		"forcibly closed", "connection reset", "err_connection",
		"chrome-error", "net::err_", "wsarecv",
	} {
		if strings.Contains(msg, n) {
			return true
		}
	}
	return false
}

func (d *DNS) ensureBrowserLocked() error {
	if d.browser != nil {
		return nil
	}
	if d.profileDir != "" {
		if err := os.MkdirAll(d.profileDir, 0o755); err != nil {
			return fmt.Errorf("dns: профиль chrome: %w", err)
		}
	}

	opts := []chromedp.ExecAllocatorOption{
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.UserAgent(chromeUA),
		chromedp.Flag("headless", d.headless),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("start-minimized", true),
		chromedp.WindowSize(1280, 900),
	}
	if d.profileDir != "" {
		opts = append(opts, chromedp.UserDataDir(d.profileDir))
	}
	if d.chromePath != "" {
		opts = append(opts, chromedp.ExecPath(d.chromePath))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	browser, browserCancel := chromedp.NewContext(allocCtx, chromedp.WithLogf(func(string, ...any) {}))
	if err := chromedp.Run(browser); err != nil {
		allocCancel()
		browserCancel()
		return fmt.Errorf("dns: запуск chrome: %w", err)
	}

	d.allocCancel = allocCancel
	d.browser = browser
	d.browserCancel = browserCancel
	d.log.Info("dns: chrome запущен", "headless", d.headless, "profile", d.profileDir)
	return nil
}

func hideWebdriver() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(
			`Object.defineProperty(navigator, 'webdriver', {get: () => undefined});`,
		).Do(ctx)
		return err
	})
}

func setCityCookie(city string) chromedp.Action {
	if city == "" {
		city = "moscow"
	}
	return chromedp.ActionFunc(func(ctx context.Context) error {
		return network.SetCookie("city_path", city).
			WithDomain(".dns-shop.ru").
			WithPath("/").
			WithSecure(true).
			Do(ctx)
	})
}

func waitForPrice(ctx context.Context) (Snapshot, error) {
	deadline := time.Now().Add(pageWait)
	var last pageBits
	for {
		if err := chromedp.Run(ctx, chromedp.Evaluate(extractJS, &last)); err != nil {
			return Snapshot{}, err
		}
		if hardBlocked(last) {
			return Snapshot{}, ErrChallenge
		}
		snap, err := parseBits(last)
		if err == nil {
			return snap, nil
		}

		select {
		case <-ctx.Done():
			if last.QRATOR {
				return Snapshot{}, ErrChallenge
			}
			return Snapshot{}, ctx.Err()
		case <-time.After(pollEvery):
		}
		if time.Now().After(deadline) {
			if last.QRATOR {
				return Snapshot{}, ErrChallenge
			}
			if last.Title != "" {
				return Snapshot{}, fmt.Errorf("%w (%s)", err, last.Title)
			}
			return Snapshot{}, err
		}
	}
}

// Paused сообщает, что ходить на DNS сейчас нельзя.
func (d *DNS) Paused() (until time.Time, reason string, paused bool) {
	if d.breaker.Allow() {
		return time.Time{}, "", false
	}
	return d.breaker.RetryAt(), d.breaker.Reason(), true
}
