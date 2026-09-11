package tracker

import (
	"context"
	"path/filepath"
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
	if !tr.wait(ctx, 2*time.Second) {
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
	if err := store.RecordSnapshot(ctx, p.ID, "Товар", 10000, "RUB", true); err != nil {
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
