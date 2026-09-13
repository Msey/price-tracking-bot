package fetch

import (
	"time"

	"github.com/Msey/price-tracking-bot/internal/sites"
)

const wbPageWait = 60 * time.Second

// NewWildberries собирает загрузчик карточек wildberries.ru.
//
// tripOnChallenge включён: антибот WB привязан к IP, повторные заходы
// только продлевают паузу. Карточка всё равно проверяется раз в сутки.
func NewWildberries(opt ShopOptions) *Shop {
	return newShop(shopConfig{
		site:            string(sites.Wildberries),
		pageWait:        wbPageWait,
		parse:           parseVisiblePriceBits,
		tripOnChallenge: true,
	}, opt)
}
