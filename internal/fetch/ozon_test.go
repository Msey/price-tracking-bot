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

func TestParseOzonHTMLPrefersBankHeadline(t *testing.T) {
	html := `<h1>Ковер</h1>
<span class="tsHeadline600Large">5 920&nbsp;₽</span>
<div data-widget="webPrice">
	<span class="tsHeadline600Large">5 105&nbsp;₽</span>
	<span>с банками Ozon банка</span>
	<span>5 672&nbsp;₽ с другими банками</span>
</div>
<script type="application/ld+json">{"@type":"Product","name":"Ковер","offers":{"price":"5920","priceCurrency":"RUB"}}</script>`
	snap, err := parseOzonHTML(html)
	if err != nil {
		t.Fatal(err)
	}
	if snap.PriceKopecks != 510500 {
		t.Fatalf("цена %d, ожидалось 510500 — ценник с Ozon банком", snap.PriceKopecks)
	}
}

func TestParseVisiblePriceBitsSkipsLDJSON(t *testing.T) {
	_, err := parseVisiblePriceBits(pageBits{
		SkipLDJSON: true,
		LDJSON:     []string{`{"@type":"Product","name":"Ковер","offers":{"price":"5920","priceCurrency":"RUB"}}`},
	})
	if !errors.Is(err, ErrNoPrice) {
		t.Fatalf("без ценника банка JSON-LD не берём: %v", err)
	}
	snap, err := parseVisiblePriceBits(pageBits{
		SkipLDJSON: true,
		CSSPrice:   "5 105 ₽",
		LDJSON:     []string{`{"@type":"Product","name":"Ковер","offers":{"price":"5920","priceCurrency":"RUB"}}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.PriceKopecks != 510500 {
		t.Fatalf("цена %d", snap.PriceKopecks)
	}
}

func TestParseOzonHTMLIgnoresLooseHeadline(t *testing.T) {
	html := `<h1>Ковер</h1><span class="tsHeadline600Large">5 920 ₽</span>`
	_, err := parseOzonHTML(html)
	if !errors.Is(err, ErrNoPrice) {
		t.Fatalf("первый крупный ценник без подписи банка не берём: %v", err)
	}
}

func TestParseOzonHTMLHeadlineFallback(t *testing.T) {
	html := `<h1>Товар</h1><div data-widget="webPrice"><span class="tsHeadline600Large">1 990 ₽</span></div>`
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

func TestParseOzonHTMLPerimeterxIsNotChallenge(t *testing.T) {
	// Скрипт PerimeterX есть на обычной карточке. Это не виджет капчи.
	html := `<html><h1>Товар</h1><script src="https://client.perimeterx.net/px.js"></script></html>`
	_, err := parseOzonHTML(html)
	if !errors.Is(err, ErrNoPrice) {
		t.Fatalf("ожидался ErrNoPrice без виджета капчи, получено %v", err)
	}
}

func TestParseOzonHTMLOfflineInterstitial(t *testing.T) {
	html := `<title>Похоже, нет соединения</title><div id="px-captcha"></div><p>Выключите VPN</p><span>Инцидент: fab_chig_1</span>`
	_, err := parseOzonHTML(html)
	if !errors.Is(err, ErrChallenge) {
		t.Fatalf("заглушка должна быть ErrChallenge, получено %v", err)
	}
	if !needsHuman(pageBits{Challenge: true, Title: "Похоже, нет соединения"}) {
		t.Fatal("заглушка должна ждать нажатия «Обновить страницу»")
	}
}

func TestParseOzonHTMLNoPrice(t *testing.T) {
	_, err := parseOzonHTML(`<html><h1>Товар</h1></html>`)
	if !errors.Is(err, ErrNoPrice) {
		t.Fatalf("ожидался ErrNoPrice, получено %v", err)
	}
}
