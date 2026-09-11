package fetch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

const (
	pageWait    = 45 * time.Second
	pollEvery   = time.Second
	captchaWait = 4 * time.Minute
	chromeUA    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36"
	extractJS   = `(function(){
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

// DNS читает карточки через общий Chrome.
type DNS struct {
	browser *Browser
	owned   bool
	log     *slog.Logger
	breaker *Breaker
}

type DNSOptions struct {
	Browser         *Browser
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
	br := opt.Browser
	owned := false
	if br == nil {
		br = NewBrowser(BrowserOptions{
			ProfileDir: opt.ProfileDir,
			ChromePath: opt.ChromePath,
			Headless:   opt.Headless,
			Log:        opt.Log,
		})
		owned = true
	}
	return &DNS{
		browser: br,
		owned:   owned,
		log:     opt.Log,
		breaker: NewBreaker(opt.CircuitCooldown),
	}
}

func (d *DNS) Close() {
	if d.owned && d.browser != nil {
		d.browser.Close()
	}
}

func (d *DNS) Breaker() *Breaker { return d.breaker }

// Fetch открывает карточку в уже запущенном Chrome. Повторных попыток нет:
// при челлендже сразу открываем предохранитель.
func (d *DNS) Fetch(ctx context.Context, p storage.Product) (Snapshot, error) {
	if !d.breaker.Allow() {
		return Snapshot{}, fmt.Errorf("%w: пауза до %s (%s)",
			ErrChallenge, d.breaker.RetryAt().Format(time.RFC3339), d.breaker.Reason())
	}

	actions := append(pageSetup(), setCityCookie(p.City), chromedp.Navigate(p.URL))
	snap, err := d.browser.do(pageWait, actions, extractJS, parseBits)
	if err != nil {
		d.maybeTrip(err)
		if errors.Is(err, ErrChallenge) || errors.Is(err, ErrNoPrice) || errors.Is(err, context.DeadlineExceeded) {
			return Snapshot{}, err
		}
		return Snapshot{}, fmt.Errorf("dns: навигация %s: %w", p.URL, err)
	}
	return snap, nil
}

func (d *DNS) maybeTrip(err error) {
	if err == nil || isChromeStartError(err) {
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

func waitForExtract(ctx context.Context, timeout time.Duration, js string, parse func(pageBits) (Snapshot, error), ui *chromeUI) (Snapshot, error) {
	if timeout <= 0 {
		timeout = pageWait
	}
	deadline := time.Now().Add(timeout)
	shown := false
	var last pageBits
	var lastErr error = ErrNoPrice
	for {
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &last)); err != nil {
			return Snapshot{}, err
		}
		snap, err := parse(last)
		if err == nil {
			if shown && ui != nil {
				_ = ui.hide(ctx)
			}
			return snap, nil
		}
		lastErr = err
		if needsHuman(last, lastErr) && ui != nil && !shown {
			shown = true
			_ = ui.reveal(ctx)
			if ui.notify != nil {
				ui.notify(last)
			}
			if extra := time.Until(deadline); extra < captchaWait {
				deadline = time.Now().Add(captchaWait)
			}
		}

		select {
		case <-ctx.Done():
			if needsHuman(last, lastErr) {
				return Snapshot{}, ErrChallenge
			}
			return Snapshot{}, ctx.Err()
		case <-time.After(pollEvery):
		}
		if time.Now().After(deadline) {
			if needsHuman(last, lastErr) {
				return Snapshot{}, ErrChallenge
			}
			if last.Title != "" {
				return Snapshot{}, fmt.Errorf("%w (%s)", lastErr, last.Title)
			}
			return Snapshot{}, lastErr
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
