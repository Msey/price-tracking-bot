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

// Правило «не верить разметке Ozon» задаётся настройками магазина,
// а не приходит со страницы.
func TestNewOzonKeepsBankPricePolicy(t *testing.T) {
	shop := NewOzon(ShopOptions{ProfileDir: t.TempDir()})
	defer shop.Close()
	if !shop.cfg.policy.skipLDJSON {
		t.Fatal("на Ozon разметка отдаёт цену с другими банками")
	}
	if shop.cfg.policy.grace() != ozonBankGrace {
		t.Fatalf("отсрочка %v", shop.cfg.policy.grace())
	}
	market := NewMarket(ShopOptions{ProfileDir: t.TempDir()})
	defer market.Close()
	if market.cfg.policy.skipLDJSON {
		t.Fatal("на Маркете разметка — законный запасной вариант")
	}
}

func TestCardPriceAvailability(t *testing.T) {
	const schemaOut = `{"@type":"Product","name":"Набор","offers":{"@type":"Offer","price":"525","priceCurrency":"RUB","availability":"https://schema.org/OutOfStock"}}`
	const schemaIn = `{"@type":"Product","name":"Набор","offers":{"price":"481","priceCurrency":"RUB","availability":"https://schema.org/InStock"}}`
	tests := []struct {
		name      string
		bits      pageBits
		wantAvail bool
		wantPrice int64
		wantName  string
	}{
		{
			name:      "ozon: разметка OutOfStock не отменяет ценник",
			bits:      pageBits{skipLDJSON: true, CSSPrice: "481 ₽", Name: "Набор саморезов", LDJSON: []string{schemaOut}},
			wantAvail: true,
			wantPrice: 48100,
			wantName:  "Набор саморезов",
		},
		{
			name:      "ozon: имя из разметки, наличие всё равно с карточки",
			bits:      pageBits{skipLDJSON: true, CSSPrice: "481 ₽", LDJSON: []string{schemaOut}},
			wantAvail: true,
			wantPrice: 48100,
			wantName:  "Набор",
		},
		{
			name:      "ozon: нет разметки — ценник значит в наличии",
			bits:      pageBits{skipLDJSON: true, CSSPrice: "481 ₽", Name: "Набор"},
			wantAvail: true,
			wantPrice: 48100,
			wantName:  "Набор",
		},
		{
			name:      "ozon: виджет «закончился» важнее ценника и InStock",
			bits:      pageBits{skipLDJSON: true, SoldOut: true, CSSPrice: "481 ₽", LDJSON: []string{schemaIn}},
			wantAvail: false,
			wantPrice: 48100,
			wantName:  "Набор",
		},
		{
			name:      "ozon: виджет «закончился» без разметки",
			bits:      pageBits{skipLDJSON: true, SoldOut: true, CSSPrice: "481 ₽", Name: "Набор"},
			wantAvail: false,
			wantPrice: 48100,
			wantName:  "Набор",
		},
		{
			name:      "не ozon: наличие из разметки",
			bits:      pageBits{CSSPrice: "1 000 ₽", LDJSON: []string{`{"@type":"Product","name":"X","offers":{"price":"1000","priceCurrency":"RUB","availability":"https://schema.org/OutOfStock"}}`}},
			wantAvail: false,
			wantPrice: 100000,
			wantName:  "X",
		},
		{
			name:      "не ozon: виджет «закончился» важнее InStock",
			bits:      pageBits{SoldOut: true, CSSPrice: "481 ₽", Name: "Набор", LDJSON: []string{schemaIn}},
			wantAvail: false,
			wantPrice: 48100,
			wantName:  "Набор",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap, err := parseVisiblePriceBits(tt.bits)
			if err != nil {
				t.Fatal(err)
			}
			if snap.Available != tt.wantAvail {
				t.Fatalf("available %v, ожидалось %v", snap.Available, tt.wantAvail)
			}
			if snap.PriceKopecks != tt.wantPrice {
				t.Fatalf("цена %d, ожидалось %d", snap.PriceKopecks, tt.wantPrice)
			}
			if snap.Name != tt.wantName {
				t.Fatalf("имя %q, ожидалось %q", snap.Name, tt.wantName)
			}
		})
	}
}

func TestVisiblePriceFallbackKeepsSchemaStock(t *testing.T) {
	const inStock = `{"@type":"Product","name":"Набор","offers":{"price":"525","priceCurrency":"RUB","availability":"https://schema.org/InStock"}}`
	const outOfStock = `{"@type":"Product","name":"Набор","offers":{"price":"525","priceCurrency":"RUB","availability":"https://schema.org/OutOfStock"}}`
	tests := []struct {
		name      string
		bits      pageBits
		wantAvail bool
	}{
		{
			name:      "без ценника наличие из разметки",
			bits:      pageBits{LDJSON: []string{outOfStock}},
			wantAvail: false,
		},
		{
			name:      "виджет «закончился» важнее разметки и без ценника",
			bits:      pageBits{SoldOut: true, LDJSON: []string{inStock}},
			wantAvail: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap, err := parseVisiblePriceBits(tt.bits)
			if err != nil {
				t.Fatal(err)
			}
			if snap.PriceKopecks != 52500 {
				t.Fatalf("цена %d, ожидалось 52500", snap.PriceKopecks)
			}
			if snap.Available != tt.wantAvail {
				t.Fatalf("available %v, ожидалось %v", snap.Available, tt.wantAvail)
			}
		})
	}
}

func TestParseVisiblePriceBitsSkipsLDJSON(t *testing.T) {
	_, err := parseVisiblePriceBits(pageBits{
		skipLDJSON: true,
		LDJSON:     []string{`{"@type":"Product","name":"Ковер","offers":{"price":"5920","priceCurrency":"RUB"}}`},
	})
	if !errors.Is(err, ErrNoPrice) {
		t.Fatalf("без ценника банка JSON-LD не берём: %v", err)
	}
	snap, err := parseVisiblePriceBits(pageBits{
		skipLDJSON: true,
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
