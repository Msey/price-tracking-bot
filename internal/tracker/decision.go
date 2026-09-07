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

// Decide смотрит историю newest-first, включая только что записанный замер.
//
// Уведомляем только когда одно и то же значение пришло два раза подряд —
// так отсекаются одноразовые A/B-цены. Первая устойчивая цена становится
// базой и в чат не пишется.
func Decide(history []storage.SnapshotRow, notified storage.NotifiedState) Decision {
	if len(history) < 2 {
		return Decision{}
	}
	cur, prev := history[0], history[1]
	if !same(cur, prev) {
		return Decision{Current: cur, Previous: prev}
	}
	if !notified.Set {
		return Decision{Baseline: true, Current: cur}
	}
	if cur.PriceKopecks == notified.Kopecks && cur.Available == notified.Available {
		return Decision{Current: cur}
	}
	return Decision{
		Notify:   true,
		Current:  cur,
		Previous: storage.SnapshotRow{PriceKopecks: notified.Kopecks, Available: notified.Available},
	}
}

func same(a, b storage.SnapshotRow) bool {
	return a.PriceKopecks == b.PriceKopecks && a.Available == b.Available
}
