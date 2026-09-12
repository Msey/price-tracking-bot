package tracker

import (
	"github.com/Msey/price-tracking-bot/internal/storage"
)

// Decision — что делать после нового замера.
type Decision struct {
	Notify   bool
	Baseline bool // два одинаковых чтения, ещё не было базы
	Previous storage.SnapshotRow
	Current  storage.SnapshotRow
}

// Decide смотрит историю newest-first и флаг repeated от RecordSnapshot.
//
// Каждая проверка пишет строку в историю. repeated=true значит «та же
// цена, что в прошлый раз» — это подтверждение A/B, а не смена цены.
// Уведомляем, когда одно и то же значение пришло два раза. Первая
// устойчивая цена становится базой и в чат не пишется.
//
// Если база ещё не отмечена, ищем в более старых строках другую цену
// или наличие: иначе плато из двух новых значений (90, 90, 100)
// ошибочно становилось бы базой и молча проглатывало снижение.
func Decide(history []storage.SnapshotRow, notified storage.NotifiedState, repeated bool) Decision {
	if len(history) == 0 {
		return Decision{}
	}
	cur := history[0]
	if !repeated {
		if len(history) >= 2 {
			return Decision{Current: cur, Previous: history[1]}
		}
		return Decision{Current: cur}
	}
	prev, ok := confirmedPrevious(history, cur, notified)
	if !ok {
		return Decision{Baseline: true, Current: cur}
	}
	if cur.PriceKopecks == prev.PriceKopecks && cur.Available == prev.Available {
		return Decision{Current: cur}
	}
	return Decision{Notify: true, Current: cur, Previous: prev}
}

func confirmedPrevious(history []storage.SnapshotRow, cur storage.SnapshotRow, notified storage.NotifiedState) (storage.SnapshotRow, bool) {
	if notified.Set {
		return storage.SnapshotRow{PriceKopecks: notified.Kopecks, Available: notified.Available}, true
	}
	for _, row := range history[1:] {
		if row.PriceKopecks != cur.PriceKopecks || row.Available != cur.Available {
			return row, true
		}
	}
	return storage.SnapshotRow{}, false
}
