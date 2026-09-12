// Package view собирает подписи, одинаковые для окна, локальной веб-страницы
// и сообщений Telegram. Раньше эти правила лежали в трёх пакетах копиями и
// расходились при любой правке.
package view

import (
	"database/sql"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/Msey/price-tracking-bot/internal/money"
	"github.com/Msey/price-tracking-bot/internal/storage"
)

// stampLayout — как метки времени лежат в базе: UTC без зоны.
const stampLayout = "2006-01-02 15:04:05"

// Status описывает состояние заявки словами и классом для CSS.
func Status(req storage.Request) (text, class string) {
	if !req.LastCheckedAt.Valid {
		if req.LastErrorKind.Valid && req.LastErrorKind.String != "" {
			return "ошибка загрузки", "bad"
		}
		return "ожидает проверку", "wait"
	}
	if req.LastErrorAt.Valid && req.LastErrorAt.String > req.LastCheckedAt.String {
		return "ошибка после проверки", "bad"
	}
	if req.LastAvailable.Valid && req.LastAvailable.Int64 == 0 {
		return "нет в наличии", "bad"
	}
	return "отслеживается", "ok"
}

// FormatLastPrice — цена в списках. Нулевой серый замер (цены ещё не
// было) рисуем прочерком, а не «0 ₽».
func FormatLastPrice(price, available sql.NullInt64) string {
	if !price.Valid {
		return "—"
	}
	if price.Int64 == 0 && (!available.Valid || available.Int64 == 0) {
		return "—"
	}
	return money.FormatKopecks(price.Int64)
}

func CityTitle(city string) string {
	switch strings.ToLower(strings.TrimSpace(city)) {
	case "moscow":
		return "Москва"
	default:
		return city
	}
}

// FormatWhen — метка времени из базы в местное время: 02.01.2006 15:04.
func FormatWhen(raw string) string { return formatStamp(raw, "02.01.2006 15:04") }

// FormatWhenShort — то же короче, для подписей узлов графика: 02.01.06 15:04.
func FormatWhenShort(raw string) string { return formatStamp(raw, "02.01.06 15:04") }

func formatStamp(raw, layout string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "—"
	}
	t, err := time.ParseInLocation(stampLayout, raw, time.UTC)
	if err != nil {
		return raw
	}
	return t.Local().Format(layout)
}

// RuPlural выбирает форму слова по числу: 1 товар, 2 товара, 5 товаров.
func RuPlural(n int, one, few, many string) string {
	if n < 0 {
		n = -n
	}
	mod100 := n % 100
	mod10 := n % 10
	if mod100 >= 11 && mod100 <= 14 {
		return many
	}
	switch mod10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	default:
		return many
	}
}

// TelegramLink — ссылка на товар для сообщения в режиме ModeHTML. Название
// приходит из магазина, поэтому и адрес, и текст экранируются.
func TelegramLink(p storage.Product) string {
	return fmt.Sprintf(`<a href="%s">%s</a>`, html.EscapeString(p.URL), html.EscapeString(p.Title()))
}

// PriceChange — HTML для уведомления о смене цены. Сборка текста здесь,
// чтобы цикл проверки не знал про вёрстку Telegram.
func PriceChange(p storage.Product, prev, cur storage.SnapshotRow) string {
	diff := cur.PriceKopecks - prev.PriceKopecks
	verb, arrow := "выросла", "📈"
	if diff < 0 {
		verb, arrow = "снизилась", "📉"
	}
	abs := diff
	if abs < 0 {
		abs = -abs
	}
	pct := "—"
	if prev.PriceKopecks != 0 {
		ratio := float64(abs) / float64(prev.PriceKopecks) * 100
		pct = fmt.Sprintf("%.1f%%", ratio)
	}
	mark := "+"
	if diff < 0 {
		mark = "−"
	}
	msg := fmt.Sprintf("%s Цена %s\n\n%s\nбыло %s\nстало %s\n%s %s (%s)",
		arrow, verb,
		TelegramLink(p),
		html.EscapeString(money.FormatKopecks(prev.PriceKopecks)),
		html.EscapeString(money.FormatKopecks(cur.PriceKopecks)),
		mark,
		html.EscapeString(money.FormatKopecks(abs)),
		html.EscapeString(pct),
	)
	if prev.Available && !cur.Available {
		msg += "\n\nТовар пропал из наличия."
	} else if !prev.Available && cur.Available {
		msg += "\n\nТовар снова в наличии."
	}
	return msg
}
