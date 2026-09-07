package storage

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

const (
	chatAlice = int64(1001)
	chatBob   = int64(1002)
	dnsKey    = "9ee3a4f41358d9cb"
	dnsURL    = "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/noutbuk/"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open вернул ошибку: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	for i := 1; i <= 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open #%d вернул ошибку: %v", i, err)
		}
		var version int
		if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
			t.Fatalf("чтение user_version: %v", err)
		}
		if version != len(migrations) {
			t.Errorf("user_version = %d, ожидалось %d", version, len(migrations))
		}
		s.Close()
	}
}

func TestAddSubscriptionIsIdempotent(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	product, created, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "moscow")
	if err != nil {
		t.Fatalf("первый AddSubscription вернул ошибку: %v", err)
	}
	if !created {
		t.Error("первый AddSubscription вернул created = false, ожидалось true")
	}

	_, created, err = s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "moscow")
	if err != nil {
		t.Fatalf("повторный AddSubscription вернул ошибку: %v", err)
	}
	if created {
		t.Error("повторный AddSubscription вернул created = true, ожидалось false")
	}

	items, err := s.ListSubscriptions(ctx, chatAlice)
	if err != nil {
		t.Fatalf("ListSubscriptions вернул ошибку: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("подписок %d, ожидалась одна", len(items))
	}
	if items[0].Product.ID != product.ID {
		t.Errorf("Product.ID = %d, ожидался %d", items[0].Product.ID, product.ID)
	}
	if items[0].LastPriceKopecks.Valid {
		t.Error("LastPriceKopecks заполнен, хотя цену ещё не проверяли")
	}
}

// Один товар на двух подписчиков должен остаться одной записью в products,
// иначе планировщик будет дёргать магазин по разу на каждого пользователя.
func TestProductIsSharedBetweenUsers(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	alice, _, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "moscow")
	if err != nil {
		t.Fatalf("подписка Алисы: %v", err)
	}
	bob, created, err := s.AddSubscription(ctx, chatBob, "dns", dnsKey, dnsURL, "moscow")
	if err != nil {
		t.Fatalf("подписка Боба: %v", err)
	}
	if !created {
		t.Error("подписка Боба вернула created = false, ожидалось true")
	}
	if alice.ID != bob.ID {
		t.Errorf("товар размножился: id %d и %d", alice.ID, bob.ID)
	}

	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM products").Scan(&count); err != nil {
		t.Fatalf("подсчёт товаров: %v", err)
	}
	if count != 1 {
		t.Errorf("записей в products %d, ожидалась одна", count)
	}
}

// Города различают цены, поэтому один и тот же товар в разных городах —
// это разные записи со своей историей.
func TestCityMakesDistinctProducts(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	moscow, _, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "moscow")
	if err != nil {
		t.Fatalf("подписка для Москвы: %v", err)
	}
	spb, _, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "sankt-peterburg")
	if err != nil {
		t.Fatalf("подписка для Петербурга: %v", err)
	}
	if moscow.ID == spb.ID {
		t.Error("товар для разных городов получил один id, история цен смешается")
	}
}

func TestDeleteSubscription(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	product, _, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "moscow")
	if err != nil {
		t.Fatalf("AddSubscription вернул ошибку: %v", err)
	}
	if _, _, err := s.AddSubscription(ctx, chatBob, "dns", dnsKey, dnsURL, "moscow"); err != nil {
		t.Fatalf("подписка Боба: %v", err)
	}

	removed, err := s.DeleteSubscription(ctx, chatAlice, product.ID)
	if err != nil {
		t.Fatalf("DeleteSubscription вернул ошибку: %v", err)
	}
	if !removed {
		t.Error("DeleteSubscription вернул false, ожидалось true")
	}

	if items, err := s.ListSubscriptions(ctx, chatAlice); err != nil {
		t.Fatalf("ListSubscriptions вернул ошибку: %v", err)
	} else if len(items) != 0 {
		t.Errorf("у Алисы осталось подписок: %d", len(items))
	}

	// Удаление у одного подписчика не должно затрагивать остальных.
	if items, err := s.ListSubscriptions(ctx, chatBob); err != nil {
		t.Fatalf("ListSubscriptions для Боба вернул ошибку: %v", err)
	} else if len(items) != 1 {
		t.Errorf("у Боба подписок %d, ожидалась одна", len(items))
	}

	removed, err = s.DeleteSubscription(ctx, chatAlice, product.ID)
	if err != nil {
		t.Fatalf("повторный DeleteSubscription вернул ошибку: %v", err)
	}
	if removed {
		t.Error("повторный DeleteSubscription вернул true, ожидалось false")
	}
}

func TestSharedProductURLIsNotOverwritten(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	aliceURL := dnsURL
	bobURL := "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/evil-slug/"

	alice, _, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, aliceURL, "moscow")
	if err != nil {
		t.Fatalf("подписка Алисы: %v", err)
	}
	bob, _, err := s.AddSubscription(ctx, chatBob, "dns", dnsKey, bobURL, "moscow")
	if err != nil {
		t.Fatalf("подписка Боба: %v", err)
	}
	if bob.URL != aliceURL {
		t.Errorf("Боб перезаписал URL общего товара: %q", bob.URL)
	}

	items, err := s.ListSubscriptions(ctx, chatAlice)
	if err != nil {
		t.Fatalf("ListSubscriptions: %v", err)
	}
	if items[0].Product.URL != alice.URL {
		t.Errorf("у Алисы URL стал %q", items[0].Product.URL)
	}
}

func TestSubscriptionCap(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	for i := 0; i < MaxSubscriptions; i++ {
		key := fmt.Sprintf("%08x%08x", i, i)
		url := "https://www.dns-shop.ru/product/" + key + "/"
		if _, _, err := s.AddSubscription(ctx, chatAlice, "dns", key, url, "moscow"); err != nil {
			t.Fatalf("подписка #%d: %v", i+1, err)
		}
	}

	_, _, err := s.AddSubscription(ctx, chatAlice, "dns", "deadbeefdeadbeef", "https://www.dns-shop.ru/product/deadbeefdeadbeef/", "moscow")
	if !errors.Is(err, ErrTooManySubscriptions) {
		t.Fatalf("ожидалась ErrTooManySubscriptions, получено %v", err)
	}

	// Повтор уже существующей не должен упираться в потолок.
	firstKey := fmt.Sprintf("%08x%08x", 0, 0)
	_, created, err := s.AddSubscription(ctx, chatAlice, "dns", firstKey, "https://www.dns-shop.ru/product/"+firstKey+"/", "moscow")
	if err != nil {
		t.Fatalf("повтор существующей подписки: %v", err)
	}
	if created {
		t.Error("повтор существующей подписки вернул created = true")
	}
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("Open(\"\") должен возвращать ошибку")
	}
}

func TestListSubscriptionsShowsLatestPrice(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	product, _, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "moscow")
	if err != nil {
		t.Fatalf("AddSubscription вернул ошибку: %v", err)
	}

	// Две цены с явными метками времени: в /list должна попасть свежая.
	for _, row := range []struct {
		kopecks   int64
		checkedAt string
	}{
		{16499900, "2026-09-06 10:00:00"},
		{15999900, "2026-09-07 10:00:00"},
	} {
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO price_history (product_id, price_kopecks, checked_at) VALUES (?, ?, ?)`,
			product.ID, row.kopecks, row.checkedAt); err != nil {
			t.Fatalf("вставка истории цен: %v", err)
		}
	}

	items, err := s.ListSubscriptions(ctx, chatAlice)
	if err != nil {
		t.Fatalf("ListSubscriptions вернул ошибку: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("подписок %d, ожидалась одна", len(items))
	}
	if got := items[0].LastPriceKopecks; !got.Valid || got.Int64 != 15999900 {
		t.Errorf("LastPriceKopecks = %v, ожидалось 15999900", got)
	}
}

func TestProductsDueSkipsFreshSnapshots(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	fresh, _, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "moscow")
	if err != nil {
		t.Fatal(err)
	}
	stale, _, err := s.AddSubscription(ctx, chatAlice, "dns", "aaaaaaaaaaaaaaaa", "https://www.dns-shop.ru/product/aaaaaaaaaaaaaaaa/", "moscow")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.RecordSnapshot(ctx, fresh.ID, "новый", 15999900, "RUB", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO price_history (product_id, price_kopecks, checked_at) VALUES (?, ?, ?)`,
		stale.ID, 10000, "2020-01-01 00:00:00"); err != nil {
		t.Fatal(err)
	}

	due, err := s.ProductsDue(ctx, "dns", time.Now().Add(-20*time.Minute), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != stale.ID {
		t.Fatalf("due = %+v, ожидался только старый товар", due)
	}
}

func TestRecordSnapshotUpdatesName(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	p, _, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "moscow")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSnapshot(ctx, p.ID, "Honor MagicBook", 15999900, "RUB", true); err != nil {
		t.Fatal(err)
	}
	got, err := s.ProductByID(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Honor MagicBook" {
		t.Errorf("name = %q", got.Name)
	}
	chats, err := s.SubscriberChats(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 1 || chats[0] != chatAlice {
		t.Errorf("chats = %v", chats)
	}
}
