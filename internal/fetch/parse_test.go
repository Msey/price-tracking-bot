package fetch

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseHTMLJSONLD(t *testing.T) {
	html := readFixture(t, "product.html")
	snap, err := ParseHTML(html)
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
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
	snap, err := ParseHTML(html)
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}
	if snap.PriceKopecks != 1234500 {
		t.Errorf("взяли не ту сумму: %d", snap.PriceKopecks)
	}
}

func TestParseHTMLChallenge(t *testing.T) {
	html := `<html><script src="/__qrator/qauth_utm_v2d_v9118.js"></script></html>`
	_, err := ParseHTML(html)
	if !errors.Is(err, ErrChallenge) {
		t.Fatalf("ожидался ErrChallenge, получено %v", err)
	}
}

func TestParseHTMLNoPrice(t *testing.T) {
	_, err := ParseHTML(`<html><div class="product-card">нет цены</div></html>`)
	if !errors.Is(err, ErrNoPrice) {
		t.Fatalf("ожидался ErrNoPrice, получено %v", err)
	}
}

func TestParseHTMLOutOfStock(t *testing.T) {
	html := `<script type="application/ld+json">{"@type":"Product","name":"X","offers":{"@type":"Offer","price":1000,"priceCurrency":"RUB","availability":"https://schema.org/OutOfStock"}}</script>`
	snap, err := ParseHTML(html)
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
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
	snap, err := ParseHTML(html)
	if err != nil {
		t.Fatalf("ParseHTML: %v", err)
	}
	if snap.PriceKopecks != 249900 {
		t.Errorf("цена %d", snap.PriceKopecks)
	}
}

func TestHardBlockedTitle(t *testing.T) {
	if !hardBlocked(pageBits{Title: "HTTP 403"}) {
		t.Fatal("403 должен считаться баном")
	}
	if hardBlocked(pageBits{Title: `Купить 14.6" Ноутбук HONOR`}) {
		t.Fatal("обычный title не бан")
	}
}

func TestNeedsHuman(t *testing.T) {
	if !needsHuman(pageBits{Challenge: true}, ErrNoPrice) {
		t.Fatal("капча должна звать человека")
	}
	if !needsHuman(pageBits{}, ErrChallenge) {
		t.Fatal("ErrChallenge должен звать человека")
	}
	if needsHuman(pageBits{Title: "Товар"}, ErrNoPrice) {
		t.Fatal("обычная страница без цены — не капча")
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
