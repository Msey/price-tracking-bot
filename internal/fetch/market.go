package fetch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

// Цена на карточке Маркета: [data-auto="snippet-price-current"] → первый span с числом.
const marketExtractJS = `(function(){
	var html = document.documentElement ? document.documentElement.innerHTML : '';
	var box = document.querySelector('[data-auto="snippet-price-current"]')
		|| document.querySelector('[data-auto="price-value"]');
	var priceEl = box ? (box.querySelector('span') || box) : null;
	var h1 = document.querySelector('h1[data-auto="productCardTitle"]') || document.querySelector('h1');
	var scripts = document.querySelectorAll('script[type="application/ld+json"]');
	var ld = [];
	for (var i = 0; i < scripts.length; i++) { ld.push(scripts[i].textContent || ''); }
	var low = html.toLowerCase();
	var challenge = low.indexOf('smartcaptcha') !== -1
		|| low.indexOf('showcaptcha') !== -1
		|| low.indexOf('checkboxcaptcha') !== -1
		|| low.indexOf('are you not a robot') !== -1
		|| low.indexOf('confirm that you are not a robot') !== -1;
	return {
		challenge: challenge,
		ldjson: ld,
		cssPrice: priceEl ? (priceEl.textContent || '') : '',
		name: h1 ? (h1.textContent || '').trim() : '',
		title: document.title || ''
	};
})()`

// Market читает карточки Яндекс.Маркета через общий Chrome.
type Market struct {
	browser *Browser
	owned   bool
	log     *slog.Logger
	breaker *Breaker
}

type MarketOptions struct {
	Browser         *Browser
	ProfileDir      string
	ChromePath      string
	Headless        bool
	CircuitCooldown time.Duration
	Log             *slog.Logger
}

func NewMarket(opt MarketOptions) *Market {
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
	return &Market{
		browser: br,
		owned:   owned,
		log:     opt.Log,
		breaker: NewBreaker(opt.CircuitCooldown),
	}
}

func (m *Market) Close() {
	if m.owned && m.browser != nil {
		m.browser.Close()
	}
}

func (m *Market) Breaker() *Breaker { return m.breaker }

func (m *Market) Fetch(_ context.Context, p storage.Product) (Snapshot, error) {
	if !m.breaker.Allow() {
		return Snapshot{}, fmt.Errorf("%w: пауза до %s (%s)",
			ErrChallenge, m.breaker.RetryAt().Format(time.RFC3339), m.breaker.Reason())
	}

	actions := append(pageSetup(), chromedp.Navigate(p.URL))
	snap, err := m.browser.do(pageWait, actions, marketExtractJS, parseMarketBits)
	if err != nil {
		m.maybeTrip(err)
		if errors.Is(err, ErrChallenge) || errors.Is(err, ErrNoPrice) || errors.Is(err, context.DeadlineExceeded) {
			return Snapshot{}, err
		}
		return Snapshot{}, fmt.Errorf("market: навигация %s: %w", p.URL, err)
	}
	return snap, nil
}

func (m *Market) maybeTrip(err error) {
	if err == nil {
		return
	}
	if errors.Is(err, ErrChallenge) || isBanError(err) {
		m.breaker.Trip(err.Error())
		m.log.Warn("market: предохранитель включён", "until", m.breaker.RetryAt(), "reason", err)
	}
}

func (m *Market) Paused() (until time.Time, reason string, paused bool) {
	if m.breaker.Allow() {
		return time.Time{}, "", false
	}
	return m.breaker.RetryAt(), m.breaker.Reason(), true
}
