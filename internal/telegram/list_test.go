package telegram

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Msey/price-tracking-bot/internal/config"
	"github.com/Msey/price-tracking-bot/internal/storage"
)

func botStore(t *testing.T) (*Bot, *storage.Store, context.Context) {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return New(config.Config{}, store, nil), store, context.Background()
}

func TestListContentShowsOnlyOwnLinks(t *testing.T) {
	b, store, ctx := botStore(t)
	const (
		alice   = int64(1001)
		bob     = int64(1002)
		dnsURL  = "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/noutbuk/"
		ozonURL = "https://www.ozon.ru/product/2190214590"
	)
	aliceProduct, _, err := store.AddSubscription(ctx, alice, "dns", "9ee3a4f41358d9cb", dnsURL, "moscow")
	if err != nil {
		t.Fatalf("подписка Алисы: %v", err)
	}
	bobProduct, _, err := store.AddSubscription(ctx, bob, "ozon", "2190214590", ozonURL, "moscow")
	if err != nil {
		t.Fatalf("подписка Боба: %v", err)
	}

	aliceText, aliceMarkup, err := b.listContent(ctx, alice)
	if err != nil {
		t.Fatalf("listContent Алисы: %v", err)
	}
	if !strings.Contains(aliceText, "dns-shop.ru") {
		t.Errorf("у Алисы нет своей ссылки: %s", aliceText)
	}
	if strings.Contains(aliceText, "ozon.ru") {
		t.Errorf("Алиса увидела чужую ссылку: %s", aliceText)
	}
	if aliceMarkup == nil || len(aliceMarkup.InlineKeyboard) != 1 {
		t.Fatalf("кнопок Алисы: %+v", aliceMarkup)
	}
	data := aliceMarkup.InlineKeyboard[0][0].Data
	if data != strconv.FormatInt(aliceProduct.ID, 10) {
		t.Errorf("кнопка Алисы data=%q, ожидался id %d", data, aliceProduct.ID)
	}
	if data == strconv.FormatInt(bobProduct.ID, 10) {
		t.Error("кнопка Алисы указывает на товар Боба")
	}

	bobText, _, err := b.listContent(ctx, bob)
	if err != nil {
		t.Fatalf("listContent Боба: %v", err)
	}
	if !strings.Contains(bobText, "ozon.ru") {
		t.Errorf("у Боба нет своей ссылки: %s", bobText)
	}
	if strings.Contains(bobText, "dns-shop.ru") {
		t.Errorf("Боб увидел чужую ссылку: %s", bobText)
	}

	empty, markup, err := b.listContent(ctx, 0)
	if err != nil {
		t.Fatalf("listContent(0): %v", err)
	}
	if markup != nil {
		t.Error("без пользователя не должно быть кнопок")
	}
	if !strings.Contains(empty, "пуст") {
		t.Errorf("без пользователя ожидался пустой список: %s", empty)
	}

	unknown, markup, err := b.listContent(ctx, 9999)
	if err != nil {
		t.Fatalf("незнакомый пользователь: %v", err)
	}
	if markup != nil || !strings.Contains(unknown, "пуст") {
		t.Errorf("незнакомый пользователь: text=%q markup=%v", unknown, markup)
	}
}

func TestListContentEmptyWhenOthersHaveLinks(t *testing.T) {
	b, store, ctx := botStore(t)
	if _, _, err := store.AddSubscription(ctx, 1002, "ozon", "2190214590", "https://www.ozon.ru/product/2190214590", "moscow"); err != nil {
		t.Fatal(err)
	}
	text, markup, err := b.listContent(ctx, 1001)
	if err != nil {
		t.Fatal(err)
	}
	if markup != nil || !strings.Contains(text, "пуст") || strings.Contains(text, "ozon.ru") {
		t.Errorf("Алиса без подписок увидела: %s markup=%v", text, markup)
	}
}

func TestTelegramUserIDNilSafe(t *testing.T) {
	if got := telegramUserID(nil); got != 0 {
		t.Errorf("nil context: %d", got)
	}
}

func TestDeleteOwnRemovesOnlyCaller(t *testing.T) {
	b, store, ctx := botStore(t)
	const (
		alice   = int64(1001)
		bob     = int64(1002)
		dnsURL  = "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/noutbuk/"
		ozonURL = "https://www.ozon.ru/product/2190214590"
	)
	if _, _, err := store.AddSubscription(ctx, alice, "dns", "9ee3a4f41358d9cb", dnsURL, "moscow"); err != nil {
		t.Fatalf("подписка Алисы: %v", err)
	}
	if _, _, err := store.AddSubscription(ctx, bob, "ozon", "2190214590", ozonURL, "moscow"); err != nil {
		t.Fatalf("подписка Боба: %v", err)
	}

	if _, total, removed, err := b.deleteOwn(ctx, alice, 2); err != nil {
		t.Fatalf("лишний номер: %v", err)
	} else if removed || total != 1 {
		t.Errorf("чужой номер снял товар: removed=%v total=%d", removed, total)
	}

	item, total, removed, err := b.deleteOwn(ctx, alice, 1)
	if err != nil {
		t.Fatalf("deleteOwn Алисы: %v", err)
	}
	if !removed || total != 1 {
		t.Fatalf("Алиса не сняла свой товар: removed=%v total=%d", removed, total)
	}
	if item.Product.URL != dnsURL {
		t.Errorf("снят не тот товар: %q", item.Product.URL)
	}

	aliceLeft, err := store.ListSubscriptions(ctx, alice)
	if err != nil {
		t.Fatalf("список Алисы: %v", err)
	}
	if len(aliceLeft) != 0 {
		t.Errorf("у Алисы осталось %d", len(aliceLeft))
	}

	bobLeft, err := store.ListSubscriptions(ctx, bob)
	if err != nil {
		t.Fatalf("список Боба: %v", err)
	}
	if len(bobLeft) != 1 || bobLeft[0].Product.URL != ozonURL {
		t.Errorf("чужой /del задел Боба: %+v", bobLeft)
	}
}

func TestDeleteOwnKeepsSharedProductForOthers(t *testing.T) {
	b, store, ctx := botStore(t)
	const (
		alice  = int64(1001)
		bob    = int64(1002)
		dnsURL = "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/noutbuk/"
	)
	if _, _, err := store.AddSubscription(ctx, alice, "dns", "9ee3a4f41358d9cb", dnsURL, "moscow"); err != nil {
		t.Fatalf("подписка Алисы: %v", err)
	}
	if _, _, err := store.AddSubscription(ctx, bob, "dns", "9ee3a4f41358d9cb", dnsURL, "moscow"); err != nil {
		t.Fatalf("подписка Боба: %v", err)
	}

	if _, _, removed, err := b.deleteOwn(ctx, alice, 1); err != nil || !removed {
		t.Fatalf("Алиса не сняла общий товар: removed=%v err=%v", removed, err)
	}

	bobLeft, err := store.ListSubscriptions(ctx, bob)
	if err != nil {
		t.Fatalf("список Боба: %v", err)
	}
	if len(bobLeft) != 1 || bobLeft[0].Product.URL != dnsURL {
		t.Errorf("/del Алисы снял товар у Боба: %+v", bobLeft)
	}

	if _, total, removed, err := b.deleteOwn(ctx, 0, 1); err != nil || removed || total != 0 {
		t.Errorf("без пользователя: removed=%v total=%d err=%v", removed, total, err)
	}
}

func TestDeleteOwnIndexBoundaries(t *testing.T) {
	b, store, ctx := botStore(t)
	const alice, bob = int64(1001), int64(1002)

	var urls [3]string
	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("%08x%08x", i, i)
		urls[i] = "https://www.dns-shop.ru/product/" + key + "/"
		if _, _, err := store.AddSubscription(ctx, alice, "dns", key, urls[i], "moscow"); err != nil {
			t.Fatalf("Алиса #%d: %v", i+1, err)
		}
	}
	if _, _, err := store.AddSubscription(ctx, bob, "ozon", "2190214590", "https://www.ozon.ru/product/2190214590", "moscow"); err != nil {
		t.Fatal(err)
	}

	if _, total, removed, err := b.deleteOwn(ctx, alice, 0); err != nil || removed || total != 0 {
		t.Errorf("/del 0: total=%d removed=%v err=%v", total, removed, err)
	}
	if _, total, removed, err := b.deleteOwn(ctx, alice, 4); err != nil || removed || total != 3 {
		t.Errorf("/del 4 из 3: total=%d removed=%v err=%v", total, removed, err)
	}

	item, total, removed, err := b.deleteOwn(ctx, alice, 3)
	if err != nil || !removed || total != 3 || item.Product.URL != urls[2] {
		t.Fatalf("последний: url=%q total=%d removed=%v err=%v", item.Product.URL, total, removed, err)
	}
	item, total, removed, err = b.deleteOwn(ctx, alice, 1)
	if err != nil || !removed || total != 2 || item.Product.URL != urls[0] {
		t.Fatalf("первый: url=%q total=%d removed=%v err=%v", item.Product.URL, total, removed, err)
	}
	item, total, removed, err = b.deleteOwn(ctx, alice, 1)
	if err != nil || !removed || total != 1 || item.Product.URL != urls[1] {
		t.Fatalf("повторный /del 1: url=%q total=%d removed=%v err=%v", item.Product.URL, total, removed, err)
	}

	bobLeft, err := store.ListSubscriptions(ctx, bob)
	if err != nil || len(bobLeft) != 1 {
		t.Errorf("Боб: n=%d err=%v", len(bobLeft), err)
	}
}
