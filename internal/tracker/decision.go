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
// Повтор той же цены не плодит строку в БД, поэтому подтверждение A/B —
// это repeated=true, а не две одинаковые записи подряд. Уведомляем только
// когда одно и то же значение пришло два раза. Первая устойчивая цена
// становится базой и в чат не пишется.
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
