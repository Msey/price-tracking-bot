package tracker

import (
	"strings"

	"github.com/Msey/price-tracking-bot/internal/storage"
	"github.com/Msey/price-tracking-bot/internal/view"
)

// noticeLimit — сколько байт кладём в одно сообщение Telegram.
// Предел API — 4096 символов; запас нужен, потому что кириллица в UTF-8
// длиннее одного байта, а режем мы по байтам.
const noticeLimit = 3500

// noticePart — один блок сводки и то, что из очереди он закрывает.
type noticePart struct {
	text         string
	productID    int64
	curKopecks   int64
	curAvailable bool
	alertSub     int64
	change       bool
}

func changePart(p storage.Product, row storage.QueuedChange) noticePart {
	return noticePart{
		text: view.PriceChange(p, storage.SnapshotRow{
			PriceKopecks: row.PrevKopecks,
			Available:    row.PrevAvailable,
		}, storage.SnapshotRow{
			PriceKopecks: row.CurKopecks,
			Available:    row.CurAvailable,
		}),
		productID:    row.ProductID,
		curKopecks:   row.CurKopecks,
		curAvailable: row.CurAvailable,
		change:       true,
	}
}

func alertPart(p storage.Product, row storage.QueuedAlert) noticePart {
	return noticePart{
		text:      view.PriceBelow(p, row.PriceKopecks, row.AlertKopecks),
		productID: row.ProductID,
		alertSub:  row.SubscriptionID,
	}
}

// packNotices склеивает блоки одного чата. Сверх лимита режет по границе
// блока, чтобы не порвать HTML-ссылку.
func packNotices(parts []noticePart, limit int) [][]noticePart {
	if limit < 1 {
		limit = noticeLimit
	}
	var out [][]noticePart
	var cur []noticePart
	size := 0
	for _, part := range parts {
		extra := len(part.text)
		if len(cur) > 0 {
			extra += len("\n\n")
		}
		if len(cur) > 0 && size+extra > limit {
			out = append(out, cur)
			cur = nil
			size = 0
			extra = len(part.text)
		}
		cur = append(cur, part)
		size += extra
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

func renderNotices(parts []noticePart) string {
	texts := make([]string, len(parts))
	for i, part := range parts {
		texts[i] = part.text
	}
	return strings.Join(texts, "\n\n")
}

// digestParts собирает изменения и пороги одного чата: по товару сначала
// цена или наличие, сразу за ним порог, если он тоже ждал.
func digestParts(products map[int64]storage.Product, changes []storage.QueuedChange, alerts []storage.QueuedAlert) []noticePart {
	alertAt := make(map[int64][]storage.QueuedAlert, len(alerts))
	ids := make([]int64, 0, len(changes)+len(alerts))
	seen := make(map[int64]bool, len(changes)+len(alerts))
	for _, row := range changes {
		if !seen[row.ProductID] {
			seen[row.ProductID] = true
			ids = append(ids, row.ProductID)
		}
	}
	for _, row := range alerts {
		alertAt[row.ProductID] = append(alertAt[row.ProductID], row)
		if !seen[row.ProductID] {
			seen[row.ProductID] = true
			ids = append(ids, row.ProductID)
		}
	}
	var parts []noticePart
	changeAt := make(map[int64]storage.QueuedChange, len(changes))
	for _, row := range changes {
		changeAt[row.ProductID] = row
	}
	for _, id := range ids {
		p := products[id]
		if row, ok := changeAt[id]; ok {
			parts = append(parts, changePart(p, row))
		}
		for _, row := range alertAt[id] {
			parts = append(parts, alertPart(p, row))
		}
	}
	return parts
}
