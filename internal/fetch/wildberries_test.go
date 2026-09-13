package fetch

import (
	"errors"
	"testing"
)

func TestParseWildberriesHTMLPrefersWallet(t *testing.T) {
	html := `<h1>Футболка</h1>
<div class="product-page__price-block">
	<del>2 000&nbsp;₽</del>
	<ins class="price-block__final-price">1 500&nbsp;₽</ins>
	<button><div><div></div><div><h2>1 350&nbsp;₽</h2></div></div></button>
	<span>с WB Кошельком</span>
</div>`
	snap, err := parseWildberriesHTML(html)
	if err != nil {
		t.Fatal(err)
	}
	if snap.PriceKopecks != 135000 {
		t.Fatalf("цена %d, ожидалось 135000 — ценник с WB Кошельком", snap.PriceKopecks)
	}
	if snap.Name != "Футболка" {
		t.Errorf("имя %q", snap.Name)
	}
	if !snap.Available {
		t.Error("ожидалось наличие")
	}
}

func TestParseWildberriesHTMLWalletClass(t *testing.T) {
	html := `<h1>Кроссовки</h1>
<span class="price-block__wallet-price">4 190 ₽</span>
<span>WB Кошелёк</span>
<ins class="price-block__final-price">4 790 ₽</ins>`
	snap, err := parseWildberriesHTML(html)
	if err != nil {
		t.Fatal(err)
	}
	if snap.PriceKopecks != 419000 {
		t.Fatalf("цена %d, ожидалось 419000", snap.PriceKopecks)
	}
}

func TestParseWildberriesHTMLFinalPriceFallback(t *testing.T) {
	html := `<h1>Носки</h1>
<div class="product-page__price-block">
	<ins class="price-block__final-price">199 ₽</ins>
</div>`
	snap, err := parseWildberriesHTML(html)
	if err != nil {
		t.Fatal(err)
	}
	if snap.PriceKopecks != 19900 {
		t.Fatalf("цена %d, ожидалось 19900", snap.PriceKopecks)
	}
}

func TestParseWildberriesHTMLAntibot(t *testing.T) {
	html := `<title>...</title><p>Что-то не так...</p><p>Подозрительная активность. Пожалуйста, подождите.</p><p>Новая попытка через 00:38</p>`
	_, err := parseWildberriesHTML(html)
	if !errors.Is(err, ErrChallenge) {
		t.Fatalf("антибот WB должен быть ErrChallenge, получено %v", err)
	}
	if !needsHuman(pageBits{Blocked: true, Title: "..."}) {
		t.Fatal("антибот WB должен ждать человека")
	}
}

func TestParseWildberriesHTMLNoPrice(t *testing.T) {
	_, err := parseWildberriesHTML(`<html><h1>Товар</h1></html>`)
	if !errors.Is(err, ErrNoPrice) {
		t.Fatalf("ожидался ErrNoPrice, получено %v", err)
	}
}

func TestNewWildberriesTripsOnChallenge(t *testing.T) {
	shop := NewWildberries(ShopOptions{ProfileDir: t.TempDir()})
	defer shop.Close()
	if shop.cfg.site != "wildberries" {
		t.Errorf("site = %q", shop.cfg.site)
	}
	if shop.cfg.pageWait != wbPageWait {
		t.Errorf("pageWait = %v", shop.cfg.pageWait)
	}
	if !shop.cfg.tripOnChallenge {
		t.Fatal("антибот WB должен гасить магазин")
	}
	if shop.cfg.policy.skipLDJSON {
		t.Fatal("у WB нет разметки с ценой — skipLDJSON не нужен")
	}
}
