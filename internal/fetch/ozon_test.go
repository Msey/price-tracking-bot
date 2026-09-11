package fetch

import (
	"errors"
	"testing"
)

func TestParseOzonHTMLCurrentPrice(t *testing.T) {
	html := readFixture(t, "ozon_card.html")
	snap, err := parseOzonHTML(html)
	if err != nil {
		t.Fatalf("parseOzonHTML: %v", err)
	}
	if snap.PriceKopecks != 42100 {
		t.Errorf("цена %d, ожидалось 42100 (не 467 у других банков)", snap.PriceKopecks)
	}
	if snap.Currency != "RUB" {
		t.Errorf("валюта %q", snap.Currency)
	}
	if !snap.Available {
		t.Error("ожидался InStock")
	}
	if snap.Name != "Герметик акриловый Момент 420 гр белый универсальный морозостойкий" {
		t.Errorf("имя %q", snap.Name)
	}
}

func TestParseOzonHTMLHeadlineFallback(t *testing.T) {
	html := `<h1>Товар</h1><span class="tsHeadline600Large">1 990 ₽</span>`
	snap, err := parseOzonHTML(html)
	if err != nil {
		t.Fatal(err)
	}
	if snap.PriceKopecks != 199000 {
		t.Errorf("цена %d", snap.PriceKopecks)
	}
}

func TestParseOzonHTMLJSONLDFallback(t *testing.T) {
	html := `<script type="application/ld+json">{"@type":"Product","name":"Герметик","offers":{"price":"421","priceCurrency":"RUB"}}</script>`
	snap, err := parseOzonHTML(html)
	if err != nil {
		t.Fatal(err)
	}
	if snap.PriceKopecks != 42100 {
		t.Errorf("цена %d", snap.PriceKopecks)
	}
}

func TestParseOzonHTMLChallenge(t *testing.T) {
	html := `<html><div id="px-captcha">Access Denied</div></html>`
	_, err := parseOzonHTML(html)
	if !errors.Is(err, ErrChallenge) {
		t.Fatalf("ожидался ErrChallenge, получено %v", err)
	}
}

func TestParseOzonHTMLNoPrice(t *testing.T) {
	_, err := parseOzonHTML(`<html><h1>Товар</h1></html>`)
	if !errors.Is(err, ErrNoPrice) {
		t.Fatalf("ожидался ErrNoPrice, получено %v", err)
	}
}
