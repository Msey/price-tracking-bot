package fetch

import "time"

const ozonPageWait = 60 * time.Second

// NewOzon собирает загрузчик карточек ozon.ru.
//
// tripOnChallenge выключен: капча Ozon проходится в том же окне Chrome,
// и после ручного прохождения карточка должна читаться сразу.
//
// skipLDJSON: в разметке Ozon лежит цена «с другими банками», а нужна
// «с Ozon банком» — она есть только в самой карточке. bankGrace — сколько
// ждать этот ценник, прежде чем согласиться на цену из разметки: иначе
// вкладка висит до общего таймаута.
func NewOzon(opt ShopOptions) *Shop {
	return newShop(shopConfig{
		site:     "ozon",
		pageWait: ozonPageWait,
		parse:    parseVisiblePriceBits,
		policy:   pagePolicy{skipLDJSON: true, bankGrace: ozonBankGrace},
	}, opt)
}
