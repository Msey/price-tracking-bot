package tracker

import (
	"errors"

	"github.com/Msey/price-tracking-bot/internal/fetch"
	"github.com/Msey/price-tracking-bot/internal/storage"
)

// disappearanceRechecks — сколько раз ещё открыть карточку, прежде чем
// писать «товар пропал из наличия». Первый замер только подозрение:
// страница могла не распарситься, а сеть — моргнуть.
const disappearanceRechecks = 2

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

// suspectDisappearance — товар был в наличии, а этот замер выглядит как
// пропажа. Капча, таймаут и обрыв сети сюда не входят: их нельзя
// выдавать за отсутствие товара. Ошибку «цена не найдена» — можно,
// но только как подозрение, которое ещё надо перепроверить.
func suspectDisappearance(prevInStock bool, err error, available bool) bool {
	return prevInStock && readingLooksGone(err, available)
}

// readingLooksGone — страница не дала цену или прямо сказала, что товара нет.
// Сбой загрузки сюда не входит.
func readingLooksGone(err error, available bool) bool {
	gone, failed := classifyReading(err, available)
	return gone && !failed
}

func classifyReading(err error, available bool) (gone, failed bool) {
	if errors.Is(err, fetch.ErrNoPrice) {
		return true, false
	}
	if err != nil {
		return false, true
	}
	return !available, false
}
