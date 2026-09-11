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

// ShopOptions — как ходить в магазин. Browser задан, когда Chrome общий
// на все магазины; иначе загрузчик поднимает свой.
type ShopOptions struct {
	Browser         *Browser
	ProfileDir      string
	ChromePath      string
	Headless        bool
	CircuitCooldown time.Duration
	Log             *slog.Logger
}

// shopConfig — всё, чем магазины отличаются друг от друга.
type shopConfig struct {
	site      string
	pageWait  time.Duration
	extractJS string
	parse     func(pageBits) (Snapshot, error)
	actions   func(p storage.Product) []chromedp.Action
	// tripOnChallenge — гасить весь магазин на CIRCUIT_COOLDOWN из-за капчи.
	// Для Ozon выключено: его капча проходится в том же окне, и после
	// ручного прохождения карточка должна читаться сразу.
	tripOnChallenge bool
}

// Shop читает карточки одного магазина через Chrome.
type Shop struct {
	cfg     shopConfig
	browser *Browser
	owned   bool
	log     *slog.Logger
	breaker *Breaker
}

func newShop(cfg shopConfig, opt ShopOptions) *Shop {
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	br, owned := opt.Browser, false
	if br == nil {
		br = NewBrowser(BrowserOptions{
			ProfileDir: opt.ProfileDir,
			ChromePath: opt.ChromePath,
			Headless:   opt.Headless,
			Log:        opt.Log,
		})
		owned = true
	}
	return &Shop{
		cfg:     cfg,
		browser: br,
		owned:   owned,
		log:     opt.Log,
		breaker: NewBreaker(opt.CircuitCooldown),
	}
}

// Close гасит Chrome, если он поднимался только для этого магазина.
func (s *Shop) Close() {
	if s.owned && s.browser != nil {
		s.browser.Close()
	}
}

// Fetch открывает карточку в уже запущенном Chrome. Повторных попыток нет:
// при челлендже сразу открываем предохранитель.
func (s *Shop) Fetch(ctx context.Context, p storage.Product) (Snapshot, error) {
	if !s.breaker.Allow() {
		return Snapshot{}, fmt.Errorf("%w: пауза до %s (%s)",
			ErrChallenge, s.breaker.RetryAt().Format(time.RFC3339), s.breaker.Reason())
	}

	snap, err := s.browser.do(ctx, s.cfg.pageWait, s.cfg.actions(p), s.cfg.extractJS, s.cfg.parse)
	if err != nil {
		s.maybeTrip(err)
		if errors.Is(err, ErrChallenge) || errors.Is(err, ErrNoPrice) || errors.Is(err, context.DeadlineExceeded) {
			return Snapshot{}, err
		}
		return Snapshot{}, fmt.Errorf("%s: навигация %s: %w", s.cfg.site, p.URL, err)
	}
	return snap, nil
}

// Paused сообщает, что ходить в этот магазин сейчас нельзя.
func (s *Shop) Paused() (until time.Time, reason string, paused bool) {
	if s.breaker.Allow() {
		return time.Time{}, "", false
	}
	return s.breaker.RetryAt(), s.breaker.Reason(), true
}

func (s *Shop) maybeTrip(err error) {
	// Сбой запуска Chrome — не вина магазина, предохранитель тут ни при чём.
	if err == nil || isChromeStartError(err) {
		return
	}
	if isBanError(err) || (s.cfg.tripOnChallenge && errors.Is(err, ErrChallenge)) {
		s.breaker.Trip(err.Error())
		s.log.Warn("предохранитель включён", "site", s.cfg.site,
			"until", s.breaker.RetryAt(), "reason", err)
	}
}
