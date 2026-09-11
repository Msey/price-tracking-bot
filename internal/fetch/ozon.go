package fetch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

const ozonPageWait = 60 * time.Second

// Цена на карточке Ozon: [data-widget="webPrice"] .tsHeadline600Large.
const ozonExtractJS = `(function(){
	var html = document.documentElement ? document.documentElement.innerHTML : '';
	var box = document.querySelector('[data-widget="webPrice"] .tsHeadline600Large')
		|| document.querySelector('.tsHeadline600Large');
	var h1 = document.querySelector('h1');
	var scripts = document.querySelectorAll('script[type="application/ld+json"]');
	var ld = [];
	for (var i = 0; i < scripts.length; i++) { ld.push(scripts[i].textContent || ''); }
	var title = document.title || '';
	var low = (html + ' ' + title).toLowerCase();
	var challenge = low.indexOf('px-captcha') !== -1
		|| low.indexOf('perimeterx') !== -1
		|| title.toLowerCase().indexOf('antibot challenge') !== -1;
	return {
		challenge: challenge,
		ldjson: ld,
		cssPrice: box ? (box.textContent || '') : '',
		name: h1 ? (h1.textContent || '').trim() : '',
		title: title
	};
})()`

// Ozon читает карточки через общий Chrome.
type Ozon struct {
	browser *Browser
	owned   bool
	log     *slog.Logger
	breaker *Breaker
}

type OzonOptions struct {
	Browser         *Browser
	ProfileDir      string
	ChromePath      string
	Headless        bool
	CircuitCooldown time.Duration
	Log             *slog.Logger
}

func NewOzon(opt OzonOptions) *Ozon {
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
	return &Ozon{
		browser: br,
		owned:   owned,
		log:     opt.Log,
		breaker: NewBreaker(opt.CircuitCooldown),
	}
}

func (o *Ozon) Close() {
	if o.owned && o.browser != nil {
		o.browser.Close()
	}
}

func (o *Ozon) Breaker() *Breaker { return o.breaker }

func (o *Ozon) Fetch(_ context.Context, p storage.Product) (Snapshot, error) {
	if !o.breaker.Allow() {
		return Snapshot{}, fmt.Errorf("%w: пауза до %s (%s)",
			ErrChallenge, o.breaker.RetryAt().Format(time.RFC3339), o.breaker.Reason())
	}

	var scrolled bool
	actions := []chromedp.Action{
		network.Enable(),
		hideWebdriver(),
		chromedp.Navigate(p.URL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return chromedp.Evaluate(`window.scrollTo(0, 480); true`, &scrolled).Do(ctx)
		}),
	}
	snap, err := o.browser.do(ozonPageWait, actions, ozonExtractJS, parseOzonBits)
	if err != nil {
		o.maybeTrip(err)
		if errors.Is(err, ErrChallenge) || errors.Is(err, ErrNoPrice) || errors.Is(err, context.DeadlineExceeded) {
			return Snapshot{}, err
		}
		return Snapshot{}, fmt.Errorf("ozon: навигация %s: %w", p.URL, err)
	}
	return snap, nil
}

func (o *Ozon) maybeTrip(err error) {
	if err == nil {
		return
	}
	// Челлендж Ozon часто проходит в том же окне Chrome. Не глушим весь
	// магазин на CIRCUIT_COOLDOWN — иначе после ручного прохождения
	// карточка не проверится ещё 45 минут.
	if isBanError(err) {
		o.breaker.Trip(err.Error())
		o.log.Warn("ozon: предохранитель включён", "until", o.breaker.RetryAt(), "reason", err)
	}
}

func (o *Ozon) Paused() (until time.Time, reason string, paused bool) {
	if o.breaker.Allow() {
		return time.Time{}, "", false
	}
	return o.breaker.RetryAt(), o.breaker.Reason(), true
}
