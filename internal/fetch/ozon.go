package fetch

import "time"

const ozonPageWait = 60 * time.Second

// NewOzon собирает загрузчик карточек ozon.ru.
//
// tripOnChallenge выключен: капча Ozon проходится в том же окне Chrome,
// и после ручного прохождения карточка должна читаться сразу.
func NewOzon(opt ShopOptions) *Shop {
	return newShop(shopConfig{
		site:     "ozon",
		pageWait: ozonPageWait,
		parse:    parseVisiblePriceBits,
	}, opt)
}
