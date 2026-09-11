package tracker

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Msey/price-tracking-bot/internal/fetch"
	"github.com/Msey/price-tracking-bot/internal/money"
	"github.com/Msey/price-tracking-bot/internal/storage"
)

// Fetcher ходит за ценой. Реализация для магазинов — Chrome, для тестов — заглушка.
type Fetcher interface {
	Fetch(ctx context.Context, p storage.Product) (fetch.Snapshot, error)
}

// Notifier шлёт HTML в Telegram.
type Notifier interface {
	Notify(ctx context.Context, chatID int64, message string) error
}

type Config struct {
	Interval     time.Duration
	FetchGap     time.Duration
	PerCycle     int
	StartupDelay time.Duration
}

type Tracker struct {
	store    *storage.Store
	fetchers map[string]Fetcher
	notify   Notifier
	cfg      Config
	log      *slog.Logger
	lastHit  time.Time
	kick     chan struct{}
	pending  atomic.Bool
	busy     atomic.Bool
	statusMu sync.Mutex
	status   string
}

func New(store *storage.Store, fetchers map[string]Fetcher, notify Notifier, cfg Config, log *slog.Logger) *Tracker {
	if cfg.Interval < 10*time.Minute {
		cfg.Interval = 10 * time.Minute
	}
	if cfg.FetchGap < 30*time.Second {
		cfg.FetchGap = 30 * time.Second
	}
	if cfg.PerCycle < 1 {
		cfg.PerCycle = 8
	}
	if cfg.StartupDelay < time.Minute {
		cfg.StartupDelay = time.Minute
	}
	if log == nil {
		log = slog.Default()
	}
	if fetchers == nil {
		fetchers = map[string]Fetcher{}
	}
	return &Tracker{
		store:    store,
		fetchers: fetchers,
		notify:   notify,
		cfg:      cfg,
		log:      log,
		kick:     make(chan struct{}, 1),
	}
}

// RequestCheck сбрасывает ожидание автоцикла и ставит полную проверку всех товаров.
func (t *Tracker) RequestCheck() {
	t.pending.Store(true)
	t.setStatus("Запущена проверка всех цен. Таймер автоцикла сброшен.")
	select {
	case t.kick <- struct{}{}:
		t.log.Info("запрошена принудительная проверка")
	default:
		t.log.Info("принудительная проверка уже стоит в очереди")
	}
}

// StatusText — что сейчас делает трекер, для строки статуса в окне.
func (t *Tracker) StatusText() string {
	t.statusMu.Lock()
	defer t.statusMu.Unlock()
	return t.status
}

func (t *Tracker) setStatus(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	t.statusMu.Lock()
	t.status = msg
	t.statusMu.Unlock()
}

// SetUserHint пишет в статус то, что должен увидеть человек — например капчу.
func (t *Tracker) SetUserHint(msg string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	t.setStatus("%s", msg)
}

// Busy — идёт проверка или она уже запрошена и вот-вот начнётся.
func (t *Tracker) Busy() bool {
	return t.busy.Load() || t.pending.Load()
}

// Run крутит циклы, пока жив контекст. Первый заход не сразу: после рестарта
// не нужно немедленно открывать все карточки.
func (t *Tracker) Run(ctx context.Context) {
	t.log.Info("трекер запущен",
		"interval", t.cfg.Interval,
		"gap", t.cfg.FetchGap,
		"per_cycle", t.cfg.PerCycle,
		"startup_delay", t.cfg.StartupDelay,
		"sites", t.siteNames())

	if !t.wait(ctx, t.cfg.StartupDelay) {
		return
	}
	for ctx.Err() == nil {
		force := t.consumeKick()
		t.busy.Store(true)
		t.pending.Store(false)
		if force {
			t.log.Info("принудительная проверка всех товаров")
			t.setStatus("Принудительная проверка всех товаров")
			t.cycleAll(ctx)
			t.finishStatus(true)
		} else {
			t.setStatus("Автоматическая проверка цен")
			t.cycle(ctx)
			t.finishStatus(false)
		}
		t.busy.Store(false)
		if !t.wait(ctx, t.cfg.Interval) {
			return
		}
	}
}

func (t *Tracker) consumeKick() bool {
	select {
	case <-t.kick:
		for {
			select {
			case <-t.kick:
			default:
				return true
			}
		}
	default:
		return false
	}
}

func (t *Tracker) wait(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.kick:
		t.RequestCheck()
		return true
	case <-timer.C:
		return true
	}
}

func (t *Tracker) siteNames() []string {
	out := make([]string, 0, len(t.fetchers))
	for site := range t.fetchers {
		out = append(out, site)
	}
	sort.Strings(out)
	return out
}

func (t *Tracker) cycle(ctx context.Context) {
	for _, site := range t.siteNames() {
		if ctx.Err() != nil {
			return
		}
		t.cycleSite(ctx, site)
	}
}

func (t *Tracker) cycleSite(ctx context.Context, site string) {
	f := t.fetchers[site]
	if f == nil {
		return
	}
	if paused, ok := f.(interface {
		Paused() (time.Time, string, bool)
	}); ok {
		if until, reason, on := paused.Paused(); on {
			t.log.Warn("цикл пропущен, предохранитель", "site", site, "until", until, "reason", reason)
			t.setStatus("Пропуск %s: предохранитель до %s", site, until.Local().Format("15:04"))
			return
		}
	}

	due, err := t.store.ProductsDue(ctx, site, time.Now().Add(-t.cfg.Interval), t.cfg.PerCycle)
	if err != nil {
		t.log.Error("список товаров к проверке", "site", site, "error", err)
		return
	}
	t.fetchList(ctx, site, due)
}

func (t *Tracker) cycleAll(ctx context.Context) {
	t.lastHit = time.Time{}
	for _, site := range t.siteNames() {
		if ctx.Err() != nil {
			return
		}
		t.cycleSiteAll(ctx, site)
	}
}

func (t *Tracker) cycleSiteAll(ctx context.Context, site string) {
	f := t.fetchers[site]
	if f == nil {
		return
	}
	if paused, ok := f.(interface {
		Paused() (time.Time, string, bool)
	}); ok {
		if until, reason, on := paused.Paused(); on {
			t.log.Warn("цикл пропущен, предохранитель", "site", site, "until", until, "reason", reason)
			t.setStatus("Пропуск %s: предохранитель до %s", site, until.Local().Format("15:04"))
			return
		}
	}

	due, err := t.store.ActiveProducts(ctx, site)
	if err != nil {
		t.log.Error("список товаров к полной проверке", "site", site, "error", err)
		return
	}
	t.fetchList(ctx, site, due)
}

func (t *Tracker) fetchList(ctx context.Context, site string, due []storage.Product) {
	if len(due) == 0 {
		t.log.Info("нечего проверять", "site", site)
		t.setStatus("Нечего проверять на %s", site)
		return
	}

	t.log.Info("цикл проверки", "site", site, "count", len(due))
	for i, p := range due {
		if ctx.Err() != nil {
			return
		}
		if i > 0 || !t.lastHit.IsZero() {
			wait := t.cfg.FetchGap - time.Since(t.lastHit)
			if wait > 0 {
				t.setStatus("Пауза %s до следующего товара · дальше %s", wait.Round(time.Second), p.Title())
				if !sleepCtx(ctx, wait) {
					return
				}
			}
		}
		t.lastHit = time.Now()
		t.setStatus("Проверяю %s · %d/%d · %s", site, i+1, len(due), p.Title())
		if err := t.checkOne(ctx, p); err != nil {
			t.log.Warn("проверка не удалась", "site", site, "product", p.ID, "url", p.URL, "error", err)
			t.setStatus("Ошибка %s · %s", site, clipStatus(err.Error(), 180))
			kind := "fetch"
			if errors.Is(err, fetch.ErrChallenge) {
				kind = "challenge"
				_ = t.store.RecordFetchError(ctx, p.ID, p.Site, kind, err.Error())
				t.log.Warn("цикл сайта остановлен из-за челленджа", "site", site)
				return
			}
			_ = t.store.RecordFetchError(ctx, p.ID, p.Site, kind, err.Error())
		}
	}
}

func (t *Tracker) checkOne(ctx context.Context, p storage.Product) error {
	f := t.fetchers[p.Site]
	if f == nil {
		return fmt.Errorf("нет загрузчика для сайта %s", p.Site)
	}
	snap, err := f.Fetch(ctx, p)
	if err != nil {
		return err
	}
	if err := t.store.RecordSnapshot(ctx, p.ID, snap.Name, snap.PriceKopecks, snap.Currency, snap.Available); err != nil {
		return err
	}

	history, err := t.store.LastSnapshots(ctx, p.ID, 3)
	if err != nil {
		return err
	}
	notified, err := t.store.Notified(ctx, p.ID)
	if err != nil {
		return err
	}
	d := Decide(history, notified)
	switch {
	case d.Baseline:
		return t.store.MarkNotified(ctx, p.ID, d.Current.PriceKopecks, d.Current.Available)
	case d.Notify:
		product, err := t.store.ProductByID(ctx, p.ID)
		if err != nil {
			product = p
			if snap.Name != "" {
				product.Name = snap.Name
			}
		}
		if err := t.announce(ctx, product, d); err != nil {
			return err
		}
		return t.store.MarkNotified(ctx, p.ID, d.Current.PriceKopecks, d.Current.Available)
	default:
		return nil
	}
}

func (t *Tracker) announce(ctx context.Context, p storage.Product, d Decision) error {
	chats, err := t.store.SubscriberChats(ctx, p.ID)
	if err != nil {
		return err
	}
	msg := formatChange(p, d)
	var first error
	for _, chatID := range chats {
		if err := t.notify.Notify(ctx, chatID, msg); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func formatChange(p storage.Product, d Decision) string {
	oldP, newP := d.Previous.PriceKopecks, d.Current.PriceKopecks
	diff := newP - oldP
	verb := "выросла"
	arrow := "📈"
	if diff < 0 {
		verb = "снизилась"
		arrow = "📉"
	}

	oldStr := money.FormatKopecks(oldP)
	newStr := money.FormatKopecks(newP)
	delta := money.FormatKopecks(abs64(diff))
	pct := percent(oldP, diff)

	msg := fmt.Sprintf("%s Цена %s\n\n%s\nбыло %s\nстало %s\n%s %s (%s)",
		arrow, verb,
		fmt.Sprintf(`<a href="%s">%s</a>`, html.EscapeString(p.URL), html.EscapeString(p.Title())),
		html.EscapeString(oldStr), html.EscapeString(newStr),
		sign(diff), html.EscapeString(delta), html.EscapeString(pct),
	)
	if d.Previous.Available && !d.Current.Available {
		msg += "\n\nТовар пропал из наличия."
	} else if !d.Previous.Available && d.Current.Available {
		msg += "\n\nТовар снова в наличии."
	}
	return msg
}

func percent(old, diff int64) string {
	if old == 0 {
		return "—"
	}
	p := math.Abs(float64(diff) / float64(old) * 100)
	return fmt.Sprintf("%.1f%%", p)
}

func sign(diff int64) string {
	if diff < 0 {
		return "−"
	}
	return "+"
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func (t *Tracker) finishStatus(force bool) {
	cur := t.StatusText()
	if strings.HasPrefix(cur, "Ошибка") {
		return
	}
	if force {
		t.setStatus("Проверка завершена. Следующий автоцикл через %s.", t.cfg.Interval)
		return
	}
	t.setStatus("Автопроверка завершена. Следующая через %s.", t.cfg.Interval)
}

func clipStatus(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
