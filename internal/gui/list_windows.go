//go:build windows

package gui

import (
	"time"

	"github.com/lxn/walk"
)

const rowHeight96 = 128

type board struct {
	widget    *walk.CustomWidget
	items     []Item
	scroll    int
	hover     int
	lastClick time.Time
	lastIdx   int
	titleFont *walk.Font
	metaFont  *walk.Font
	priceFont *walk.Font
	bg        *walk.SolidColorBrush
	row       *walk.SolidColorBrush
	rowHot    *walk.SolidColorBrush
	accent    *walk.SolidColorBrush
	goldPen   walk.Pen
	gridPen   walk.Pen
	onOpen    func(Item)
}

func (b *board) setItems(next []Item) {
	if fingerprints(b.items) == fingerprints(next) {
		return
	}
	b.items = next
	b.clampScroll()
	if b.widget != nil {
		b.widget.Invalidate()
	}
}

func (b *board) rowH() int {
	dpi := 96
	if b.widget != nil {
		if d := b.widget.DPI(); d >= 96 {
			dpi = d
		}
	}
	h := walk.IntFrom96DPI(rowHeight96, dpi)
	if h < 96 {
		return 128
	}
	return h
}

func (b *board) clampScroll() {
	max := len(b.items)*b.rowH() - b.viewH()
	if max < 0 {
		max = 0
	}
	if b.scroll < 0 {
		b.scroll = 0
	}
	if b.scroll > max {
		b.scroll = max
	}
}

func (b *board) viewH() int {
	if b.widget == nil {
		return 0
	}
	return b.widget.ClientBoundsPixels().Height
}

func (b *board) paint(canvas *walk.Canvas, _ walk.Rectangle) error {
	if b.widget == nil {
		return nil
	}
	bounds := b.widget.ClientBoundsPixels()
	_ = canvas.FillRectanglePixels(b.bg, bounds)
	dpi := b.widget.DPI()
	if dpi < 96 {
		dpi = 96
	}
	pad := walk.IntFrom96DPI(16, dpi)
	titleH := walk.IntFrom96DPI(24, dpi)
	metaH := walk.IntFrom96DPI(18, dpi)
	chartH := walk.IntFrom96DPI(58, dpi)
	accentW := walk.IntFrom96DPI(4, dpi)
	priceW := walk.IntFrom96DPI(168, dpi)
	gap := walk.IntFrom96DPI(8, dpi)
	rowH := b.rowH()

	text := walk.RGB(243, 234, 220)
	muted := walk.RGB(154, 141, 122)
	gold := walk.RGB(226, 182, 87)

	if len(b.items) == 0 {
		msg := walk.Rectangle{X: bounds.X + pad, Y: bounds.Y + bounds.Height/3, Width: bounds.Width - pad*2, Height: titleH * 2}
		_ = canvas.DrawTextPixels("Пока нет ссылок.\nПришлите товар боту в Telegram.", b.titleFont, muted, msg, walk.TextCenter|walk.TextWordbreak|walk.TextNoPrefix)
		return nil
	}

	first := b.scroll / rowH
	if first < 0 {
		first = 0
	}
	for i := first; i < len(b.items); i++ {
		y := i*rowH - b.scroll
		if y >= bounds.Height {
			break
		}
		item := b.items[i]
		row := walk.Rectangle{X: 0, Y: y, Width: bounds.Width, Height: rowH - walk.IntFrom96DPI(6, dpi)}
		fill := b.row
		if i == b.hover {
			fill = b.rowHot
		}
		_ = canvas.FillRectanglePixels(fill, row)
		_ = canvas.FillRectanglePixels(b.accent, walk.Rectangle{X: row.X, Y: row.Y, Width: accentW, Height: row.Height})

		titleBox := walk.Rectangle{X: row.X + pad, Y: row.Y + pad, Width: row.Width - pad*2 - priceW, Height: titleH}
		priceBox := walk.Rectangle{X: row.X + row.Width - pad - priceW, Y: row.Y + pad, Width: priceW, Height: titleH}
		metaBox := walk.Rectangle{X: row.X + pad, Y: row.Y + pad + titleH, Width: row.Width - pad*2, Height: metaH}

		_ = canvas.DrawTextPixels(item.Title, b.titleFont, text, titleBox, walk.TextLeft|walk.TextVCenter|walk.TextEndEllipsis|walk.TextSingleLine|walk.TextNoPrefix)
		_ = canvas.DrawTextPixels(item.Price, b.priceFont, gold, priceBox, walk.TextRight|walk.TextVCenter|walk.TextSingleLine|walk.TextNoPrefix)

		meta := item.Site + " · " + item.City + " · " + item.Status
		if item.Watchers > 1 {
			meta += " · " + ruPlural(item.Watchers, "подписчик", "подписчика", "подписчиков")
		}
		if item.Checked != "" {
			meta += " · " + item.Checked
		}
		_ = canvas.DrawTextPixels(meta, b.metaFont, muted, metaBox, walk.TextLeft|walk.TextVCenter|walk.TextEndEllipsis|walk.TextSingleLine|walk.TextNoPrefix)

		chart := walk.Rectangle{
			X:      row.X + pad,
			Y:      row.Y + pad + titleH + metaH + gap/2,
			Width:  row.Width - pad*2,
			Height: chartH,
		}
		_ = canvas.FillRectanglePixels(b.bg, chart)
		if b.gridPen != nil {
			mid := chart.Y + chart.Height/2
			_ = canvas.DrawLinePixels(b.gridPen, walk.Point{X: chart.X, Y: mid}, walk.Point{X: chart.X + chart.Width, Y: mid})
		}
		pts := sparkline(chart.Width, chart.Height, item.Points)
		if len(pts) == 0 {
			_ = canvas.DrawTextPixels("график появится после первой проверки", b.metaFont, muted, chart, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine|walk.TextNoPrefix)
			continue
		}
		wpts := make([]walk.Point, len(pts))
		for j, p := range pts {
			wpts[j] = walk.Point{X: chart.X + p.X, Y: chart.Y + p.Y}
		}
		if b.goldPen != nil {
			_ = canvas.DrawPolylinePixels(b.goldPen, wpts)
		}
	}
	return nil
}

func (b *board) attach(w *walk.CustomWidget) {
	b.widget = w
	w.MouseWheel().Attach(func(_, _ int, button walk.MouseButton) {
		delta := walk.MouseWheelEventDelta(button)
		if delta == 0 {
			return
		}
		step := b.rowH() / 2
		if delta > 0 {
			b.scroll -= step
		} else {
			b.scroll += step
		}
		b.clampScroll()
		w.Invalidate()
	})
	w.MouseMove().Attach(func(_, y int, _ walk.MouseButton) {
		idx := (y + b.scroll) / b.rowH()
		if idx < 0 || idx >= len(b.items) {
			idx = -1
		}
		if idx != b.hover {
			b.hover = idx
			w.Invalidate()
		}
	})
	w.MouseDown().Attach(func(_, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		idx := (y + b.scroll) / b.rowH()
		if idx < 0 || idx >= len(b.items) {
			return
		}
		now := time.Now()
		if idx == b.lastIdx && now.Sub(b.lastClick) < 400*time.Millisecond && b.onOpen != nil {
			b.onOpen(b.items[idx])
		}
		b.lastClick = now
		b.lastIdx = idx
	})
}
