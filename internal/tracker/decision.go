package tracker

import (
	"github.com/Msey/price-tracking-bot/internal/storage"
)

// Decision — что делать после нового замера.
type Decision struct {
	Notify   bool
	Baseline bool // первая известная цена, в чат не пишем
	Previous storage.SnapshotRow
	Current  storage.SnapshotRow
}

// Decide смотрит историю newest-first.
//
// Первая известная цена — база, в Telegram не пишем. Дальше сравниваем
// с предыдущей известной ценой и сразу сообщаем, если она изменилась
// или сменилось наличие. Нулевой серый замер (цены ещё не было) ценой
// не считается.
func Decide(history []storage.SnapshotRow) Decision {
	if len(history) == 0 {
		return Decision{}
	}
	cur := history[0]
	prev, ok := lastKnownPrice(history[1:])
	if !ok {
		return Decision{Baseline: true, Current: cur}
	}
	if cur.PriceKopecks == prev.PriceKopecks && cur.Available == prev.Available {
		return Decision{Current: cur}
	}
	return Decision{Notify: true, Current: cur, Previous: prev}
}

func lastKnownPrice(history []storage.SnapshotRow) (storage.SnapshotRow, bool) {
	for _, row := range history {
		if knownPrice(row) {
			return row, true
		}
	}
	return storage.SnapshotRow{}, false
}

func knownPrice(row storage.SnapshotRow) bool {
	return row.Available || row.PriceKopecks != 0
}
