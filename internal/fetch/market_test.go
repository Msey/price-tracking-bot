package fetch

import (
	"errors"
	"testing"
)

func TestParseMarketHTMLCurrentPrice(t *testing.T) {
	html := readFixture(t, "market_card.html")
	snap, err := parseMarketHTML(html)
	if err != nil {
		t.Fatalf("parseMarketHTML: %v", err)
	}
	if snap.PriceKopecks != 3858300 {
		t.Errorf("цена %d, ожидалось 3858300 (текущая, не зачёркнутая 116 900)", snap.PriceKopecks)
	}
	if snap.Currency != "RUB" {
		t.Errorf("валюта %q", snap.Currency)
	}
	if !snap.Available {
		t.Error("ожидался InStock")
	}
	if snap.Name != "SPORTFLAG Беговая дорожка SportFlag Glow-Run A" {
		t.Errorf("имя %q", snap.Name)
	}
}

func TestParseMarketHTMLIgnoresOldPriceWithoutCurrent(t *testing.T) {
	html := `
		<h1>Товар</h1>
		<span data-auto="snippet-price-old"><span>116 900</span></span>`
	_, err := parseMarketHTML(html)
	if !errors.Is(err, ErrNoPrice) {
		t.Fatalf("без текущей цены ожидался ErrNoPrice, получено %v", err)
	}
}

func TestParseMarketHTMLJSONLDFallback(t *testing.T) {
	html := `<script type="application/ld+json">{"@type":"Product","name":"Дорожка","offers":{"price":"19990","priceCurrency":"RUB"}}</script>`
	snap, err := parseMarketHTML(html)
	if err != nil {
		t.Fatalf("parseMarketHTML: %v", err)
	}
	if snap.PriceKopecks != 1999000 {
		t.Errorf("цена %d", snap.PriceKopecks)
	}
	if snap.Name != "Дорожка" {
		t.Errorf("имя %q", snap.Name)
	}
}

func TestParseMarketHTMLChallenge(t *testing.T) {
	html := `<html><title>Are you not a robot?</title><div class="SmartCaptcha"></div></html>`
	_, err := parseMarketHTML(html)
	if !errors.Is(err, ErrChallenge) {
		t.Fatalf("ожидался ErrChallenge, получено %v", err)
	}
}

func TestParseMarketHTMLNoPrice(t *testing.T) {
	_, err := parseMarketHTML(`<html><h1>Товар</h1></html>`)
	if !errors.Is(err, ErrNoPrice) {
		t.Fatalf("ожидался ErrNoPrice, получено %v", err)
	}
}

func TestCleanMarketName(t *testing.T) {
	got := cleanShopName("", "SPORTFLAG Glow-Run A — купить по выгодной цене на Яндекс Маркете")
	if got != "SPORTFLAG Glow-Run A" {
		t.Errorf("обрезанный title: %q", got)
	}
}
