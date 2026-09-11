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
	ozonKey   = "2190214590"
	ozonURL   = "https://www.ozon.ru/product/2190214590"
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

// /list каждого пользователя видит только свои ссылки, даже если в базе есть чужие.
func TestListSubscriptionsHidesOtherUsers(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	if _, _, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "moscow"); err != nil {
		t.Fatalf("подписка Алисы: %v", err)
	}
	bobProduct, _, err := s.AddSubscription(ctx, chatBob, "ozon", ozonKey, ozonURL, "moscow")
	if err != nil {
		t.Fatalf("подписка Боба: %v", err)
	}

	aliceItems, err := s.ListSubscriptions(ctx, chatAlice)
	if err != nil {
		t.Fatalf("ListSubscriptions Алисы: %v", err)
	}
	if len(aliceItems) != 1 {
		t.Fatalf("у Алисы подписок %d, ожидалась одна", len(aliceItems))
	}
	if aliceItems[0].Product.URL != dnsURL {
		t.Errorf("Алиса увидела %q, ожидалась своя DNS-ссылка", aliceItems[0].Product.URL)
	}

	bobItems, err := s.ListSubscriptions(ctx, chatBob)
	if err != nil {
		t.Fatalf("ListSubscriptions Боба: %v", err)
	}
	if len(bobItems) != 1 {
		t.Fatalf("у Боба подписок %d, ожидалась одна", len(bobItems))
	}
	if bobItems[0].Product.URL != ozonURL {
		t.Errorf("Боб увидел %q, ожидалась своя Ozon-ссылка", bobItems[0].Product.URL)
	}

	empty, err := s.ListSubscriptions(ctx, 0)
	if err != nil {
		t.Fatalf("ListSubscriptions(0): %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("без пользователя вернулось %d подписок", len(empty))
	}

	removed, err := s.DeleteSubscription(ctx, chatAlice, bobProduct.ID)
	if err != nil {
		t.Fatalf("DeleteSubscription чужого товара: %v", err)
	}
	if removed {
		t.Error("Алиса сняла ссылку Боба")
	}
	bobAgain, err := s.ListSubscriptions(ctx, chatBob)
	if err != nil {
		t.Fatalf("повторный список Боба: %v", err)
	}
	if len(bobAgain) != 1 {
		t.Errorf("после чужого /del у Боба подписок %d", len(bobAgain))
	}
}

func TestRejectsInvalidUserID(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	if _, err := s.EnsureUser(ctx, 0); !errors.Is(err, ErrNoUser) {
		t.Errorf("EnsureUser(0): %v", err)
	}
	if _, err := s.EnsureUser(ctx, -7); !errors.Is(err, ErrNoUser) {
		t.Errorf("EnsureUser(-7): %v", err)
	}
	if _, _, err := s.AddSubscription(ctx, 0, "dns", dnsKey, dnsURL, "moscow"); !errors.Is(err, ErrNoUser) {
		t.Errorf("AddSubscription(0): %v", err)
	}
	if items, err := s.ListSubscriptions(ctx, -7); err != nil || len(items) != 0 {
		t.Errorf("ListSubscriptions(-7): n=%d err=%v", len(items), err)
	}
	if removed, err := s.DeleteSubscription(ctx, 0, 1); err != nil || removed {
		t.Errorf("DeleteSubscription(0): removed=%v err=%v", removed, err)
	}
	if removed, err := s.DeleteSubscription(ctx, chatAlice, 0); err != nil || removed {
		t.Errorf("DeleteSubscription(product 0): removed=%v err=%v", removed, err)
	}
	if _, total, removed, err := s.DeleteOwnAt(ctx, 0, 1); err != nil || removed || total != 0 {
		t.Errorf("DeleteOwnAt(0): total=%d removed=%v err=%v", total, removed, err)
	}
}

func TestDeleteOwnAtBoundaries(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	urls := make([]string, 3)
	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("%08x%08x", i, i)
		urls[i] = "https://www.dns-shop.ru/product/" + key + "/"
		if _, _, err := s.AddSubscription(ctx, chatAlice, "dns", key, urls[i], "moscow"); err != nil {
			t.Fatalf("Алиса #%d: %v", i+1, err)
		}
	}
	if _, _, err := s.AddSubscription(ctx, chatBob, "ozon", ozonKey, ozonURL, "moscow"); err != nil {
		t.Fatal(err)
	}

	if _, total, removed, err := s.DeleteOwnAt(ctx, chatAlice, 0); err != nil || removed || total != 0 {
		t.Errorf("номер 0: total=%d removed=%v err=%v", total, removed, err)
	}
	if _, total, removed, err := s.DeleteOwnAt(ctx, chatAlice, -1); err != nil || removed || total != 0 {
		t.Errorf("номер -1: total=%d removed=%v err=%v", total, removed, err)
	}
	if _, total, removed, err := s.DeleteOwnAt(ctx, chatAlice, 4); err != nil || removed || total != 3 {
		t.Errorf("номер 4 из 3: total=%d removed=%v err=%v", total, removed, err)
	}

	p, total, removed, err := s.DeleteOwnAt(ctx, chatAlice, 3)
	if err != nil || !removed || total != 3 || p.URL != urls[2] {
		t.Fatalf("последний: url=%q total=%d removed=%v err=%v", p.URL, total, removed, err)
	}

	p, total, removed, err = s.DeleteOwnAt(ctx, chatAlice, 1)
	if err != nil || !removed || total != 2 || p.URL != urls[0] {
		t.Fatalf("первый: url=%q total=%d removed=%v err=%v", p.URL, total, removed, err)
	}

	left, err := s.ListSubscriptions(ctx, chatAlice)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].Product.URL != urls[1] {
		t.Fatalf("после первого и последнего осталось %+v", left)
	}

	p, total, removed, err = s.DeleteOwnAt(ctx, chatAlice, 1)
	if err != nil || !removed || total != 1 || p.URL != urls[1] {
		t.Fatalf("повторный /del 1: url=%q total=%d removed=%v err=%v", p.URL, total, removed, err)
	}

	bob, err := s.ListSubscriptions(ctx, chatBob)
	if err != nil {
		t.Fatal(err)
	}
	if len(bob) != 1 || bob[0].Product.URL != ozonURL {
		t.Errorf("чужие /del задели Боба: %+v", bob)
	}
}

func TestDeleteOwnAtWhenCallerHasNothing(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, _, err := s.AddSubscription(ctx, chatBob, "ozon", ozonKey, ozonURL, "moscow"); err != nil {
		t.Fatal(err)
	}

	alice, err := s.ListSubscriptions(ctx, chatAlice)
	if err != nil || len(alice) != 0 {
		t.Fatalf("у Алисы без подписок: n=%d err=%v", len(alice), err)
	}
	if _, total, removed, err := s.DeleteOwnAt(ctx, chatAlice, 1); err != nil || removed || total != 0 {
		t.Errorf("пустой /del 1: total=%d removed=%v err=%v", total, removed, err)
	}

	bob, err := s.ListSubscriptions(ctx, chatBob)
	if err != nil || len(bob) != 1 || bob[0].Product.URL != ozonURL {
		t.Errorf("Боб пострадал: %+v err=%v", bob, err)
	}
}

func TestDeleteOwnAtSkipsInactive(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	first, _, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "moscow")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := s.AddSubscription(ctx, chatAlice, "ozon", ozonKey, ozonURL, "moscow")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE subscriptions SET active = 0 WHERE product_id = ?`, first.ID); err != nil {
		t.Fatal(err)
	}

	items, err := s.ListSubscriptions(ctx, chatAlice)
	if err != nil || len(items) != 1 || items[0].Product.ID != second.ID {
		t.Fatalf("в списке должна быть только активная: %+v err=%v", items, err)
	}

	p, total, removed, err := s.DeleteOwnAt(ctx, chatAlice, 1)
	if err != nil || !removed || total != 1 || p.ID != second.ID {
		t.Fatalf("/del 1 снял не активную: id=%d total=%d removed=%v err=%v", p.ID, total, removed, err)
	}
}

func TestDeleteOwnAtCapLastAndOvershoot(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	var lastURL string
	for i := 0; i < MaxSubscriptions; i++ {
		key := fmt.Sprintf("%08x%08x", i, i)
		lastURL = "https://www.dns-shop.ru/product/" + key + "/"
		if _, _, err := s.AddSubscription(ctx, chatAlice, "dns", key, lastURL, "moscow"); err != nil {
			t.Fatalf("подписка #%d: %v", i+1, err)
		}
	}
	if _, _, err := s.AddSubscription(ctx, chatBob, "ozon", ozonKey, ozonURL, "moscow"); err != nil {
		t.Fatal(err)
	}

	p, total, removed, err := s.DeleteOwnAt(ctx, chatAlice, MaxSubscriptions)
	if err != nil || !removed || total != MaxSubscriptions || p.URL != lastURL {
		t.Fatalf("/del %d: url=%q total=%d removed=%v err=%v", MaxSubscriptions, p.URL, total, removed, err)
	}
	if _, total, removed, err := s.DeleteOwnAt(ctx, chatAlice, MaxSubscriptions); err != nil || removed || total != MaxSubscriptions-1 {
		t.Errorf("номер за потолком: total=%d removed=%v err=%v", total, removed, err)
	}

	bob, err := s.ListSubscriptions(ctx, chatBob)
	if err != nil || len(bob) != 1 {
		t.Errorf("Боб: n=%d err=%v", len(bob), err)
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

func TestDeleteProductSubscriptions(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	product, _, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "moscow")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddSubscription(ctx, chatBob, "dns", dnsKey, dnsURL, "moscow"); err != nil {
		t.Fatal(err)
	}

	n, err := s.DeleteProductSubscriptions(ctx, product.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("снято подписок %d, ожидалось 2", n)
	}

	all, err := s.ListAllRequests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("в общем списке осталось %d", len(all))
	}

	if _, _, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "moscow"); err != nil {
		t.Fatal(err)
	}
	again, err := s.ListAllRequests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].Product.ID != product.ID {
		t.Fatalf("повторное добавление должно вернуть ту же карточку: %+v", again)
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

func TestOpenRejectsURI(t *testing.T) {
	for _, path := range []string{
		"file:bot.db",
		"file:bot.db?mode=ro",
		filepath.Join(t.TempDir(), "bot.db?mode=memory"),
	} {
		if _, err := Open(path); err == nil {
			t.Fatalf("Open(%q) должен отклонять URI SQLite", path)
		}
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

	if _, err := s.RecordSnapshot(ctx, fresh.ID, "новый", 15999900, "RUB", true); err != nil {
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

	all, err := s.ActiveProducts(ctx, "dns")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("активных %d, ожидалось 2 (и свежий тоже)", len(all))
	}
}

func TestRecordSnapshotUpdatesName(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	p, _, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "moscow")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordSnapshot(ctx, p.ID, "Honor MagicBook", 15999900, "RUB", true); err != nil {
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

func TestListAllRequests(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	alice, _, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "moscow")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddSubscription(ctx, chatBob, "dns", dnsKey, dnsURL, "moscow"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordSnapshot(ctx, alice.ID, "Honor", 15999900, "RUB", true); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListAllRequests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("заявок %d, ожидалось 2", len(got))
	}
	// Сначала более новая (Боб). Цена общая: оба подписаны на один товар.
	if got[0].ChatID != chatBob || got[1].ChatID != chatAlice {
		t.Errorf("порядок чатов: %d, %d", got[0].ChatID, got[1].ChatID)
	}
	if !got[0].LastPriceKopecks.Valid || got[0].LastPriceKopecks.Int64 != 15999900 {
		t.Errorf("цена = %v", got[0].LastPriceKopecks)
	}
	if got[1].LastPriceKopecks.Int64 != 15999900 {
		t.Errorf("цена Алисы = %v", got[1].LastPriceKopecks)
	}
	if got[0].Product.Name != "Honor" {
		t.Errorf("имя = %q", got[0].Product.Name)
	}

	if _, err := s.DeleteSubscription(ctx, chatBob, alice.ID); err != nil {
		t.Fatal(err)
	}
	got, err = s.ListAllRequests(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ChatID != chatAlice {
		t.Fatalf("после удаления: %+v", got)
	}
}

func TestListUserRequestsHidesOthers(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, _, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "moscow"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddSubscription(ctx, chatBob, "ozon", ozonKey, ozonURL, "moscow"); err != nil {
		t.Fatal(err)
	}

	alice, err := s.ListUserRequests(ctx, chatAlice)
	if err != nil {
		t.Fatal(err)
	}
	if len(alice) != 1 || alice[0].ChatID != chatAlice || alice[0].Product.Site != "dns" {
		t.Fatalf("Алиса: %+v", alice)
	}
	bob, err := s.ListUserRequests(ctx, chatBob)
	if err != nil {
		t.Fatal(err)
	}
	if len(bob) != 1 || bob[0].Product.Site != "ozon" {
		t.Fatalf("Боб: %+v", bob)
	}
	if got, err := s.ListUserRequests(ctx, 0); err != nil || len(got) != 0 {
		t.Fatalf("без user id: n=%d err=%v", len(got), err)
	}

	ids, err := s.ListSubscriberIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != chatAlice || ids[1] != chatBob {
		t.Fatalf("подписчики %v", ids)
	}
}

func TestHistoriesOldestFirst(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	p, _, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "moscow")
	if err != nil {
		t.Fatal(err)
	}
	for _, price := range []int64{10000, 20000, 30000} {
		if _, err := s.RecordSnapshot(ctx, p.ID, "Honor", price, "RUB", true); err != nil {
			t.Fatal(err)
		}
	}

	empty, err := s.Histories(ctx, nil, 2)
	if err != nil || len(empty) != 0 {
		t.Fatalf("пустой список: %v %#v", err, empty)
	}

	got, err := s.Histories(ctx, []int64{p.ID}, 2)
	if err != nil {
		t.Fatal(err)
	}
	pts := got[p.ID]
	if len(pts) != 2 {
		t.Fatalf("точек %d, ожидалось 2", len(pts))
	}
	if pts[0].PriceKopecks != 20000 || pts[1].PriceKopecks != 30000 {
		t.Errorf("порядок цен: %d, %d", pts[0].PriceKopecks, pts[1].PriceKopecks)
	}
}

func TestRecordSnapshotSamePriceUpdatesDate(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	p, _, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "moscow")
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.RecordSnapshot(ctx, p.ID, "Honor", 15999900, "RUB", true)
	if err != nil || first {
		t.Fatalf("первая запись: repeated=%v err=%v", first, err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE price_history SET checked_at = '2020-01-01 00:00:00' WHERE product_id = ?`, p.ID); err != nil {
		t.Fatal(err)
	}
	repeated, err := s.RecordSnapshot(ctx, p.ID, "Honor MagicBook", 15999900, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !repeated {
		t.Fatal("та же цена должна обновить дату, а не вставить строку")
	}
	var n int
	var at string
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(checked_at) FROM price_history WHERE product_id = ?`, p.ID).Scan(&n, &at); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("строк истории %d, ожидалась 1", n)
	}
	if at == "2020-01-01 00:00:00" {
		t.Fatal("checked_at не обновился")
	}
	got, err := s.ProductByID(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Honor MagicBook" {
		t.Errorf("имя при обновлении даты: %q", got.Name)
	}
}

func TestRecordSnapshotAvailabilityChangeInserts(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	p, _, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "moscow")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordSnapshot(ctx, p.ID, "Honor", 15999900, "RUB", true); err != nil {
		t.Fatal(err)
	}
	repeated, err := s.RecordSnapshot(ctx, p.ID, "Honor", 15999900, "RUB", false)
	if err != nil {
		t.Fatal(err)
	}
	if repeated {
		t.Fatal("смена наличия — новая строка")
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_history WHERE product_id = ?`, p.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("строк %d, ожидалось 2", n)
	}
	again, err := s.RecordSnapshot(ctx, p.ID, "Honor", 15999900, "RUB", false)
	if err != nil || !again {
		t.Fatalf("повтор отсутствия: repeated=%v err=%v", again, err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_history WHERE product_id = ?`, p.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("после повтора отсутствия строк %d", n)
	}
}

func TestRecordSnapshotPriceChangeInserts(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	p, _, err := s.AddSubscription(ctx, chatAlice, "dns", dnsKey, dnsURL, "moscow")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordSnapshot(ctx, p.ID, "Honor", 10000, "RUB", true); err != nil {
		t.Fatal(err)
	}
	repeated, err := s.RecordSnapshot(ctx, p.ID, "Honor", 9000, "RUB", true)
	if err != nil || repeated {
		t.Fatalf("другая цена: repeated=%v err=%v", repeated, err)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_history WHERE product_id = ?`, p.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("строк %d", n)
	}
}
