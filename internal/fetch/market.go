package fetch

import (
	"github.com/chromedp/chromedp"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

// Цена на карточке Маркета: [data-auto="snippet-price-current"] → первый span с числом.
const marketExtractJS = `(function(){
	var html = document.documentElement ? document.documentElement.innerHTML : '';
	var box = document.querySelector('[data-auto="snippet-price-current"]')
		|| document.querySelector('[data-auto="price-value"]');
	var priceEl = box ? (box.querySelector('span') || box) : null;
	var h1 = document.querySelector('h1[data-auto="productCardTitle"]') || document.querySelector('h1');
	var scripts = document.querySelectorAll('script[type="application/ld+json"]');
	var ld = [];
	for (var i = 0; i < scripts.length; i++) { ld.push(scripts[i].textContent || ''); }
	var low = html.toLowerCase();
	var challenge = low.indexOf('smartcaptcha') !== -1
		|| low.indexOf('showcaptcha') !== -1
		|| low.indexOf('checkboxcaptcha') !== -1
		|| low.indexOf('are you not a robot') !== -1
		|| low.indexOf('confirm that you are not a robot') !== -1;
	return {
		challenge: challenge,
		ldjson: ld,
		cssPrice: priceEl ? (priceEl.textContent || '') : '',
		name: h1 ? (h1.textContent || '').trim() : '',
		title: document.title || ''
	};
})()`

// NewMarket собирает загрузчик карточек market.yandex.ru.
func NewMarket(opt ShopOptions) *Shop {
	return newShop(shopConfig{
		site:            "market",
		pageWait:        pageWait,
		extractJS:       marketExtractJS,
		parse:           parseVisiblePriceBits,
		tripOnChallenge: true,
		actions: func(p storage.Product) []chromedp.Action {
			return append(pageSetup(), chromedp.Navigate(p.URL))
		},
	}, opt)
}
