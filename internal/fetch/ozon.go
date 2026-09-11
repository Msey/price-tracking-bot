package fetch

import (
	"context"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

const ozonPageWait = 60 * time.Second

// Цена на карточке Ozon: [data-widget="webPrice"] .tsHeadline600Large.
const ozonExtractJS = `(function(){
	var html = document.documentElement ? document.documentElement.innerHTML : '';
	var box = document.querySelector('[data-widget="webPrice"] .tsHeadline600Large')
		|| document.querySelector('.tsHeadline600Large');
	var h1 = document.querySelector('h1');
	var scripts = document.querySelectorAll('script[type="application/ld+json"]');
	var ld = [];
	for (var i = 0; i < scripts.length; i++) { ld.push(scripts[i].textContent || ''); }
	var title = document.title || '';
	var low = (html + ' ' + title).toLowerCase();
	var challenge = low.indexOf('px-captcha') !== -1
		|| low.indexOf('perimeterx') !== -1
		|| title.toLowerCase().indexOf('antibot challenge') !== -1;
	return {
		challenge: challenge,
		ldjson: ld,
		cssPrice: box ? (box.textContent || '') : '',
		name: h1 ? (h1.textContent || '').trim() : '',
		title: title
	};
})()`

// NewOzon собирает загрузчик карточек ozon.ru.
//
// tripOnChallenge выключен: капча Ozon проходится в том же окне Chrome,
// и после ручного прохождения карточка должна читаться сразу, а не через
// CIRCUIT_COOLDOWN.
func NewOzon(opt ShopOptions) *Shop {
	return newShop(shopConfig{
		site:      "ozon",
		pageWait:  ozonPageWait,
		extractJS: ozonExtractJS,
		parse:     parseVisiblePriceBits,
		actions: func(p storage.Product) []chromedp.Action {
			var scrolled bool
			return []chromedp.Action{
				network.Enable(),
				hideWebdriver(),
				chromedp.Navigate(p.URL),
				chromedp.WaitReady("body", chromedp.ByQuery),
				chromedp.ActionFunc(func(ctx context.Context) error {
					return chromedp.Evaluate(`window.scrollTo(0, 480); true`, &scrolled).Do(ctx)
				}),
			}
		},
	}, opt)
}
