package fetch

// NewMarket собирает загрузчик карточек market.yandex.ru.
func NewMarket(opt ShopOptions) *Shop {
	return newShop(shopConfig{
		site:            "market",
		pageWait:        pageWait,
		parse:           parseVisiblePriceBits,
		tripOnChallenge: true,
	}, opt)
}
