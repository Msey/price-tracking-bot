package fetch

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseHTMLJSONLD(t *testing.T) {
	html := readFixture(t, "product.html")
	snap, err := parseDNSHTML(html)
	if err != nil {
		t.Fatalf("parseDNSHTML: %v", err)
	}
	if snap.PriceKopecks != 15999900 {
		t.Errorf("цена %d, ожидалось 15999900 (не кредитный «от 15 597»)", snap.PriceKopecks)
	}
	if snap.Currency != "RUB" {
		t.Errorf("валюта %q", snap.Currency)
	}
	if !snap.Available {
		t.Error("ожидался InStock")
	}
	if snap.Name == "" {
		t.Error("пустое имя")
	}
}

func TestParseHTMLIgnoresInstallment(t *testing.T) {
	html := `
		<div class="product-buy__price-wrap">
			<div class="product-buy__price">12 345&nbsp;₽</div>
			<div class="product-buy__sub">от 15 597&nbsp;₽/ мес.</div>
		</div>`
	snap, err := parseDNSHTML(html)
	if err != nil {
		t.Fatalf("parseDNSHTML: %v", err)
	}
	if snap.PriceKopecks != 1234500 {
		t.Errorf("взяли не ту сумму: %d", snap.PriceKopecks)
	}
}

func TestParseHTMLChallenge(t *testing.T) {
	html := `<html><head><title>HTTP 403</title></head><body>Доступ к сайту www.dns-shop.ru запрещен</body></html>`
	_, err := parseDNSHTML(html)
	if !errors.Is(err, ErrChallenge) {
		t.Fatalf("ожидался ErrChallenge, получено %v", err)
	}
}

func TestParseHTMLQratorScriptIsNotBan(t *testing.T) {
	html := `<html><script src="/__qrator/qauth_utm_v2d_v9118.js"></script><div class="product-card">нет цены</div></html>`
	_, err := parseDNSHTML(html)
	if !errors.Is(err, ErrNoPrice) {
		t.Fatalf("скрипт QRATOR на карточке — не бан, получено %v", err)
	}
}

func TestParseHTMLNoPrice(t *testing.T) {
	_, err := parseDNSHTML(`<html><div class="product-card">нет цены</div></html>`)
	if !errors.Is(err, ErrNoPrice) {
		t.Fatalf("ожидался ErrNoPrice, получено %v", err)
	}
}

func TestParseHTMLOutOfStock(t *testing.T) {
	html := `<script type="application/ld+json">{"@type":"Product","name":"X","offers":{"@type":"Offer","price":1000,"priceCurrency":"RUB","availability":"https://schema.org/OutOfStock"}}</script>`
	snap, err := parseDNSHTML(html)
	if err != nil {
		t.Fatalf("parseDNSHTML: %v", err)
	}
	if snap.Available {
		t.Error("ожидался OutOfStock")
	}
	if snap.PriceKopecks != 100000 {
		t.Errorf("цена %d", snap.PriceKopecks)
	}
}

func TestParseHTMLPriceAsStringAndArrayOffers(t *testing.T) {
	html := `<script type="application/ld+json">{"@type":"Product","name":"Y","offers":[{"price":"2499.00","priceCurrency":"RUB"}]}</script>`
	snap, err := parseDNSHTML(html)
	if err != nil {
		t.Fatalf("parseDNSHTML: %v", err)
	}
	if snap.PriceKopecks != 249900 {
		t.Errorf("цена %d", snap.PriceKopecks)
	}
}

func TestParseDisplayedPrice(t *testing.T) {
	ok := map[string]int64{
		"159 999 ₽":      15999900,
		"38\u00a0583 ₽":  3858300,
		"421&nbsp;₽":     42100,
		"от 1 990 ₽":     199000,
		"1&#160;990 руб": 199000,
		"5\u202f105 ₽":   510500,
		"5 105,00 ₽":     510500,
	}
	for in, want := range ok {
		got, fine := parseDisplayedPrice(in)
		if !fine || got != want {
			t.Errorf("parseDisplayedPrice(%q) = %d, %v; ожидалось %d", in, got, fine, want)
		}
	}

	// Две цены в одном узле склеивать нельзя: "1 999 ₽ 2 999 ₽" давало
	// 19 992 999 ₽ — правдоподобное число, которое уходило подписчикам.
	bad := []string{"1 999 ₽ 2 999 ₽", "116 900 38 583", "нет в наличии", "", "0 ₽", "2 000 000 000 ₽"}
	for _, in := range bad {
		if got, fine := parseDisplayedPrice(in); fine {
			t.Errorf("parseDisplayedPrice(%q) = %d, ожидался отказ", in, got)
		}
	}
}

func TestHardBlockedTitle(t *testing.T) {
	if !hardBlocked(pageBits{Title: "HTTP 403"}) {
		t.Fatal("403 должен считаться баном")
	}
	if hardBlocked(pageBits{Title: `Купить 14.6" Ноутбук HONOR`}) {
		t.Fatal("обычный title не бан")
	}
	if hardBlocked(pageBits{Title: "Похоже, нет соединения"}) {
		t.Fatal("заглушка Ozon — не HTTP 403, её можно пройти кнопкой «Обновить»")
	}
	if hardBlocked(pageBits{Title: "Honor 403"}) {
		t.Fatal("артикул 403 в названии — не бан")
	}
	if hardBlocked(pageBits{Title: "401 серия"}) {
		t.Fatal("401 в названии модели — не бан")
	}
	if !hardBlocked(pageBits{Title: "403 Forbidden"}) {
		t.Fatal("классический HTTP-заголовок должен быть баном")
	}
	if !hardBlocked(pageBits{Title: "403"}) {
		t.Fatal("голый 403 — бан")
	}
	if !hardBlocked(pageBits{Title: "403 - Access Denied"}) {
		t.Fatal("403 с разделителем — бан")
	}
	if !needsHuman(pageBits{Title: "Похоже, нет соединения"}) {
		t.Fatal("заглушка Ozon должна ждать нажатия «Обновить страницу»")
	}
	if !ozonInterstitial(pageBits{Blocked: true}) {
		t.Fatal("blocked — заглушка Ozon")
	}
}

func TestNeedsHuman(t *testing.T) {
	if !needsHuman(pageBits{Challenge: true}) {
		t.Fatal("капча должна звать человека")
	}
	if needsHuman(pageBits{QRATOR: true}) {
		t.Fatal("QRATOR в HTML — не интерактивная капча, окно не открываем")
	}
	if needsHuman(pageBits{Title: "HTTP 403"}) {
		t.Fatal("403 — бан, а не капча: окно не открываем")
	}
	if needsHuman(pageBits{Challenge: true, Title: "HTTP 403"}) {
		t.Fatal("403 важнее виджета капчи: окно не открываем")
	}
	if !needsHuman(pageBits{Title: "Antibot Challenge Page"}) {
		t.Fatal("заголовок Antibot Challenge — заглушка Ozon, ждём кнопку")
	}
	if needsHuman(pageBits{Title: "Товар"}) {
		t.Fatal("обычная страница без цены — не капча")
	}
}

func TestParseBitsHardBlocked(t *testing.T) {
	_, err := parseBits(pageBits{Title: "HTTP 403"})
	if !errors.Is(err, ErrChallenge) {
		t.Fatalf("403 должен быть ErrChallenge, получено %v", err)
	}
	_, err = parseVisiblePriceBits(pageBits{Title: "Antibot Challenge Page"})
	if !errors.Is(err, ErrChallenge) {
		t.Fatalf("заглушка Ozon должна быть ErrChallenge, получено %v", err)
	}
}

func TestHasClassDoesNotMatchWrap(t *testing.T) {
	if hasClass("product-buy__price-wrap product-buy__price-wrap_interactive", "product-buy__price") {
		t.Fatal("wrap не должен считаться ценой")
	}
	if !hasClass("product-buy__price", "product-buy__price") {
		t.Fatal("точный класс должен совпасть")
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
