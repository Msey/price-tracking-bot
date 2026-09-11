package fetch

import (
	"context"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

// Цена на карточке DNS: div.product-buy__price, рядом JSON-LD.
const dnsExtractJS = `(function(){
	var html = document.documentElement ? document.documentElement.innerHTML : '';
	var price = document.querySelector('div.product-buy__price');
	var scripts = document.querySelectorAll('script[type="application/ld+json"]');
	var ld = [];
	for (var i = 0; i < scripts.length; i++) { ld.push(scripts[i].textContent || ''); }
	return {
		qrator: html.indexOf('/__qrator/') !== -1 || html.indexOf('qauth_handle_validate') !== -1,
		ldjson: ld,
		cssPrice: price ? (price.textContent || '') : '',
		title: document.title || ''
	};
})()`

// NewDNS собирает загрузчик карточек dns-shop.ru.
func NewDNS(opt ShopOptions) *Shop {
	return newShop(shopConfig{
		site:            "dns",
		pageWait:        pageWait,
		extractJS:       dnsExtractJS,
		parse:           parseBits,
		tripOnChallenge: true,
		actions: func(p storage.Product) []chromedp.Action {
			return append(pageSetup(), setCityCookie(p.City), chromedp.Navigate(p.URL))
		},
	}, opt)
}

// setCityCookie выбирает город: от него зависит цена на DNS.
func setCityCookie(city string) chromedp.Action {
	if city == "" {
		city = "moscow"
	}
	return chromedp.ActionFunc(func(ctx context.Context) error {
		return network.SetCookie("city_path", city).
			WithDomain(".dns-shop.ru").
			WithPath("/").
			WithSecure(true).
			Do(ctx)
	})
}
