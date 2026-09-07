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
	tr := New(store, dns, notes, Config{
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
