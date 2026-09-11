package fetch

// NewDNS собирает загрузчик карточек dns-shop.ru.
func NewDNS(opt ShopOptions) *Shop {
	return newShop(shopConfig{
		site:            "dns",
		pageWait:        pageWait,
		parse:           parseBits,
		tripOnChallenge: true,
	}, opt)
}
