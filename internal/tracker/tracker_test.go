package tracker

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Msey/price-tracking-bot/internal/fetch"
	"github.com/Msey/price-tracking-bot/internal/storage"
)

type fakeDNS struct {
	price int64
	calls int
}

func (f *fakeDNS) Fetch(context.Context, storage.Product) (fetch.Snapshot, error) {
	f.calls++
	return fetch.Snapshot{Name: "Товар", PriceKopecks: f.price, Currency: "RUB", Available: true}, nil
}

func (f *fakeDNS) Paused() (time.Time, string, bool) { return time.Time{}, "", false }

type pausedFake struct{ fakeDNS }

func (p *pausedFake) Paused() (time.Time, string, bool) {
	return time.Now().Add(time.Hour), "test", true
}

type fakeNotify struct {
	n int
}

func (f *fakeNotify) Notify(context.Context, int64, string) error {
	f.n++
	return nil
}

func TestCheckOneConfirmsBeforeNotify(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	p, _, err := store.AddSubscription(ctx, 42, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow")
	if err != nil {
		t.Fatal(err)
	}

	dns := &fakeDNS{price: 10000}
	notes := &fakeNotify{}
	tr := New(store, map[string]Fetcher{"dns": dns}, notes, Config{
		Interval:     20 * time.Minute,
		FetchGap:     30 * time.Second,
		PerCycle:     8,
		StartupDelay: time.Minute,
	}, nil)

	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if notes.n != 0 {
		t.Fatalf("база не должна писать в чат, получено %d", notes.n)
	}

	dns.price = 9000
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if notes.n != 0 {
		t.Fatal("одно новое значение не уведомляет")
	}
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if notes.n != 1 {
		t.Fatalf("после подтверждения ожидалось 1 уведомление, получено %d", notes.n)
	}
	if dns.calls != 4 {
		t.Fatalf("вызовов fetch %d", dns.calls)
	}
	hist, err := store.LastSnapshots(ctx, p.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 {
		t.Fatalf("в истории %d строк, ожидалось 2 (повтор цены обновляет дату)", len(hist))
	}
}

func TestMinCheckInterval(t *testing.T) {
	if got := minCheckInterval(nil); got != time.Hour {
		t.Fatalf("пусто: %s", got)
	}
	if got := minCheckInterval(map[string]Fetcher{"dns": &fakeDNS{}}); got != 24*time.Hour {
		t.Fatalf("только dns: %s", got)
	}
	if got := minCheckInterval(map[string]Fetcher{"dns": &fakeDNS{}, "ozon": &fakeDNS{}}); got != time.Hour {
		t.Fatalf("dns+ozon: %s", got)
	}
	tr := New(nil, map[string]Fetcher{
		"dns":           &fakeDNS{},
		"ozon":          &fakeDNS{},
		"yandex_market": &fakeDNS{},
	}, nil, Config{Interval: 20 * time.Minute}, nil)
	if tr.cfg.Interval != time.Hour {
		t.Fatalf("пауза автоцикла = %s, ожидался час из-за Ozon", tr.cfg.Interval)
	}
	if siteCheckInterval("dns") != 24*time.Hour || siteCheckInterval("yandex_market") != 24*time.Hour {
		t.Fatal("dns и маркет должны быть раз в сутки")
	}
	if siteCheckInterval("ozon") != time.Hour {
		t.Fatal("ozon должен быть раз в час")
	}
}

func TestCycleSiteHonorsPause(t *testing.T) {
	f := &pausedFake{}
	tr := New(nil, map[string]Fetcher{"dns": f}, nil, Config{
		FetchGap:     30 * time.Second,
		PerCycle:     8,
		StartupDelay: time.Minute,
	}, nil)
	tr.cycleSite(context.Background(), "dns", true, func(context.Context, string) ([]storage.Product, error) {
		t.Fatal("при предохранителе список не спрашивают")
		return nil, nil
	})
	if f.calls != 0 {
		t.Fatalf("fetch вызван %d раз", f.calls)
	}
}

func TestWaitInterruptedByRequestCheck(t *testing.T) {
	tr := New(nil, nil, nil, Config{
		Interval:     20 * time.Minute,
		FetchGap:     30 * time.Second,
		PerCycle:     8,
		StartupDelay: time.Minute,
	}, nil)

	ctx := context.Background()
	start := time.Now()
	go func() {
		time.Sleep(30 * time.Millisecond)
		tr.RequestCheck()
	}()
	if !tr.wait(ctx, 2*time.Second, "cycle") {
		t.Fatal("wait вернул false")
	}
	if time.Since(start) > 800*time.Millisecond {
		t.Fatal("ожидание автоцикла не сбросилось")
	}
	if !tr.Busy() && !tr.consumeKick() {
		t.Fatal("после сброса должна остаться принудительная проверка")
	}
}

func TestCycleAllChecksFreshProducts(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	p, _, err := store.AddSubscription(ctx, 42, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordSnapshot(ctx, p.ID, "Товар", 10000, "RUB", true); err != nil {
		t.Fatal(err)
	}

	dns := &fakeDNS{price: 10000}
	tr := New(store, map[string]Fetcher{"dns": dns}, &fakeNotify{}, Config{
		Interval:     20 * time.Minute,
		FetchGap:     30 * time.Second,
		PerCycle:     8,
		StartupDelay: time.Minute,
	}, nil)
	tr.cfg.FetchGap = 0

	tr.cycle(ctx)
	if dns.calls != 0 {
		t.Fatalf("обычный цикл не должен трогать свежий товар, вызовов %d", dns.calls)
	}

	tr.cycleAll(ctx)
	if dns.calls != 1 {
		t.Fatalf("полная проверка должна сходить за свежим товаром, вызовов %d", dns.calls)
	}
}

func TestCycleAllDoesNotWaitInitialGap(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	if _, _, err := store.AddSubscription(ctx, 42, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow"); err != nil {
		t.Fatal(err)
	}

	dns := &fakeDNS{price: 10000}
	tr := New(store, map[string]Fetcher{"dns": dns}, &fakeNotify{}, Config{
		Interval:     20 * time.Minute,
		FetchGap:     30 * time.Second,
		PerCycle:     8,
		StartupDelay: time.Minute,
	}, nil)
	tr.lastHit = time.Now()

	done := make(chan struct{})
	go func() {
		tr.cycleAll(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(800 * time.Millisecond):
		t.Fatal("полная проверка ждала паузу перед первым товаром")
	}
	if dns.calls != 1 {
		t.Fatalf("вызовов %d", dns.calls)
	}
}

func TestCycleAllDoesNotWaitBetweenSites(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	if _, _, err := store.AddSubscription(ctx, 42, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AddSubscription(ctx, 42, "ozon", "2190214590", "https://www.ozon.ru/product/2190214590", "moscow"); err != nil {
		t.Fatal(err)
	}

	dns := &fakeDNS{price: 10000}
	ozon := &fakeDNS{price: 20000}
	tr := New(store, map[string]Fetcher{"dns": dns, "ozon": ozon}, &fakeNotify{}, Config{
		Interval:     20 * time.Minute,
		FetchGap:     30 * time.Second,
		PerCycle:     8,
		StartupDelay: time.Minute,
	}, nil)

	start := time.Now()
	tr.cycleAll(ctx)
	if time.Since(start) > 800*time.Millisecond {
		t.Fatal("принудительная проверка ждала FETCH_GAP между магазинами")
	}
	if dns.calls != 1 || ozon.calls != 1 {
		t.Fatalf("dns %d ozon %d", dns.calls, ozon.calls)
	}
}

func TestFormatCountdown(t *testing.T) {
	if got := formatCountdown(5 * time.Second); got != "5 с" {
		t.Fatalf("5 с: %q", got)
	}
	if got := formatCountdown(65 * time.Second); got != "1 мин 5 с" {
		t.Fatalf("1 мин 5 с: %q", got)
	}
	if got := formatCountdown(3723 * time.Second); got != "1 ч 2 мин 3 с" {
		t.Fatalf("1 ч 2 мин 3 с: %q", got)
	}
	if got := formatCountdown(-time.Second); got != "0 с" {
		t.Fatalf("отрицательное: %q", got)
	}
}

func TestStatusTextCountsDown(t *testing.T) {
	tr := New(nil, nil, nil, Config{
		Interval:     20 * time.Minute,
		FetchGap:     30 * time.Second,
		PerCycle:     8,
		StartupDelay: time.Minute,
	}, nil)
	tr.setUntil(time.Now().Add(90*time.Second), "cycle")
	got := tr.StatusText()
	if !strings.HasPrefix(got, "Следующая проверка через 1 мин ") {
		t.Fatalf("отсчёт автоцикла: %q", got)
	}
	tr.setUntil(time.Now().Add(12*time.Second), "startup")
	got = tr.StatusText()
	if got != "Первая проверка через 12 с" && got != "Первая проверка через 11 с" {
		t.Fatalf("отсчёт старта: %q", got)
	}
	tr.setStatus("%s", "Xiaomi")
	tr.setUntil(time.Now().Add(45*time.Second), "gap")
	got = tr.StatusText()
	if !strings.Contains(got, "Пауза ") || !strings.Contains(got, "Xiaomi") {
		t.Fatalf("пауза между товарами: %q", got)
	}
	tr.setStatus("%s", "Ошибка dns · сайт показал защиту")
	tr.setUntil(time.Now().Add(20*time.Minute), "cycle")
	got = tr.StatusText()
	if !strings.Contains(got, "Ошибка dns") || !strings.Contains(got, "следующая проверка через") {
		t.Fatalf("ошибка с отсчётом: %q", got)
	}
}
