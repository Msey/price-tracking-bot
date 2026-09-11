package view

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

func TestFormatWhen(t *testing.T) {
	raw := "2026-09-11 05:40:00"
	parsed, err := time.ParseInLocation(stampLayout, raw, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := FormatWhen(raw), parsed.Local().Format("02.01.2006 15:04"); got != want {
		t.Errorf("FormatWhen = %q, ожидалось %q", got, want)
	}
	if got, want := FormatWhenShort(raw), parsed.Local().Format("02.01.06 15:04"); got != want {
		t.Errorf("FormatWhenShort = %q, ожидалось %q", got, want)
	}
	if FormatWhen("") != "—" {
		t.Error("пустая метка — прочерк")
	}
	if FormatWhen("мусор") != "мусор" {
		t.Error("неразобранная метка отдаётся как есть")
	}
}

func TestRuPlural(t *testing.T) {
	cases := map[int]string{0: "товаров", 1: "товар", 2: "товара", 5: "товаров", 11: "товаров", 21: "товар", -3: "товара"}
	for n, want := range cases {
		if got := RuPlural(n, "товар", "товара", "товаров"); got != want {
			t.Errorf("RuPlural(%d) = %q, ожидалось %q", n, got, want)
		}
	}
}

func TestStatus(t *testing.T) {
	if _, class := Status(storage.Request{}); class != "wait" {
		t.Errorf("непроверенная заявка: %q", class)
	}
	waiting := storage.Request{LastErrorKind: sql.NullString{String: "fetch", Valid: true}}
	if _, class := Status(waiting); class != "bad" {
		t.Errorf("ошибка до первой проверки: %q", class)
	}
	ok := storage.Request{
		LastCheckedAt: sql.NullString{String: "2026-01-02 03:04:05", Valid: true},
		LastAvailable: sql.NullInt64{Int64: 1, Valid: true},
	}
	if _, class := Status(ok); class != "ok" {
		t.Errorf("живая заявка: %q", class)
	}
	gone := ok
	gone.LastAvailable = sql.NullInt64{Int64: 0, Valid: true}
	if text, _ := Status(gone); text != "нет в наличии" {
		t.Errorf("пропал из наличия: %q", text)
	}
}

func TestTelegramLinkEscapes(t *testing.T) {
	p := storage.Product{URL: `https://x.ru/?a=1&b=2`, Name: `<b>злой</b> "товар"`}
	got := TelegramLink(p)
	want := `<a href="https://x.ru/?a=1&amp;b=2">&lt;b&gt;злой&lt;/b&gt; &#34;товар&#34;</a>`
	if got != want {
		t.Errorf("TelegramLink = %q, ожидалось %q", got, want)
	}
}

func TestCityTitle(t *testing.T) {
	if CityTitle(" Moscow ") != "Москва" {
		t.Error("moscow переводится")
	}
	if CityTitle("kazan") != "kazan" {
		t.Error("неизвестный город остаётся как есть")
	}
}

func TestPriceChange(t *testing.T) {
	p := storage.Product{URL: "https://www.dns-shop.ru/product/abc/", Name: "Ноутбук"}
	got := PriceChange(p,
		storage.SnapshotRow{PriceKopecks: 10000, Available: true},
		storage.SnapshotRow{PriceKopecks: 9000, Available: true},
	)
	if !strings.Contains(got, "снизилась") || !strings.Contains(got, "Ноутбук") {
		t.Fatalf("падение цены: %s", got)
	}
	if strings.Contains(got, "<") && !strings.Contains(got, "<a href=") {
		t.Fatalf("ожидалась HTML-ссылка: %s", got)
	}
	gone := PriceChange(p,
		storage.SnapshotRow{PriceKopecks: 9000, Available: true},
		storage.SnapshotRow{PriceKopecks: 9000, Available: false},
	)
	if !strings.Contains(gone, "пропал из наличия") {
		t.Fatalf("наличие: %s", gone)
	}
}
