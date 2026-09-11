package fetch

import "github.com/Msey/price-tracking-bot/internal/sites"

// NewMarket собирает загрузчик карточек market.yandex.ru.
func NewMarket(opt ShopOptions) *Shop {
	return newShop(shopConfig{
		site:            string(sites.YandexMarket),
		pageWait:        pageWait,
		parse:           parseVisiblePriceBits,
		tripOnChallenge: true,
	}, opt)
}
