package fetch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

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

type shopConfig struct {
	site     string
	pageWait time.Duration
	parse    func(pageBits) (Snapshot, error)
	// tripOnChallenge — гасить весь магазин на CIRCUIT_COOLDOWN из-за капчи.
	// Для Ozon выключено: его капча проходится в том же окне, и после
	// ручного прохождения карточка должна читаться сразу.
	tripOnChallenge bool
}

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
	dir := opt.ProfileDir
	if dir == "" && br != nil {
		dir = br.profileDir
	}
	breaker := NewFileBreaker(opt.CircuitCooldown, circuitFile(dir, cfg.site))
	if !breaker.Allow() {
		opt.Log.Warn("предохранитель ещё активен", "site", cfg.site,
			"until", breaker.RetryAt(), "reason", breaker.Reason())
	}
	return &Shop{
		cfg:     cfg,
		browser: br,
		owned:   owned,
		log:     opt.Log,
		breaker: breaker,
	}
}

func (s *Shop) Close() {
	if s.owned && s.browser != nil {
		s.browser.Close()
	}
}

func (s *Shop) Fetch(ctx context.Context, p storage.Product) (Snapshot, error) {
	s.log.Info("запрос карточки", "site", s.cfg.site, "url", p.URL, "city", p.City)
	if !s.breaker.Allow() {
		s.log.Info("карточка пропущена, предохранитель", "site", s.cfg.site, "until", s.breaker.RetryAt(), "reason", s.breaker.Reason())
		return Snapshot{}, fmt.Errorf("%w: пауза до %s (%s)",
			ErrChallenge, s.breaker.RetryAt().Format(time.RFC3339), s.breaker.Reason())
	}

	snap, err := s.browser.do(ctx, s.cfg.pageWait, p, s.cfg.parse)
	if err != nil {
		s.log.Info("карточка не прочитана", "site", s.cfg.site, "url", p.URL, "error", err)
		s.maybeTrip(err)
		if errors.Is(err, ErrChallenge) || errors.Is(err, ErrNoPrice) || errors.Is(err, context.DeadlineExceeded) {
			return Snapshot{}, err
		}
		return Snapshot{}, fmt.Errorf("%s: навигация %s: %w", s.cfg.site, p.URL, err)
	}
	s.log.Info("карточка прочитана", "site", s.cfg.site, "name", snap.Name, "kopecks", snap.PriceKopecks, "available", snap.Available)
	return snap, nil
}

func (s *Shop) Paused() (until time.Time, reason string, paused bool) {
	if s.breaker.Allow() {
		return time.Time{}, "", false
	}
	return s.breaker.RetryAt(), s.breaker.Reason(), true
}

func (s *Shop) maybeTrip(err error) {
	if err == nil || isChromeStartError(err) {
		return
	}
	if isBanError(err) || (s.cfg.tripOnChallenge && (errors.Is(err, ErrChallenge) || looksLikeHTTPBan(err))) {
		s.breaker.Trip(err.Error())
		s.log.Warn("предохранитель включён", "site", s.cfg.site,
			"until", s.breaker.RetryAt(), "reason", err)
	}
}
