//go:build windows

package gui

import (
	"time"

	"github.com/Msey/price-tracking-bot/internal/money"
	"github.com/Msey/price-tracking-bot/internal/view"
	"github.com/lxn/walk"
)

const rowHeight96 = 148

type board struct {
	widget    *walk.CustomWidget
	items     []Item
	scroll    int
	hover     int
	tipItem   int
	tipNode   int
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
	icons     map[string]walk.Image
	onOpen    func(Item)
}

func (b *board) setItems(next []Item) {
	if sameItems(b.items, next) {
		return
	}
	b.items = next
	if b.tipItem >= len(b.items) {
		b.tipItem, b.tipNode = -1, -1
	}
	if b.hover >= len(b.items) {
		b.hover = -1
	}
	b.clampScroll()
	if b.widget != nil {
		b.widget.Invalidate()
	}
}

// dpi — плотность экрана виджета. До создания виджета и на старых системах
// GetDpiForWindow отдаёт ноль, поэтому ниже 96 не опускаемся.
func (b *board) dpi() int {
	if b.widget != nil {
		if d := b.widget.DPI(); d >= 96 {
			return d
		}
	}
	return 96
}

func (b *board) rowH() int {
	return walk.IntFrom96DPI(rowHeight96, b.dpi())
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
	m := b.metrics()
	pad, titleH, metaH := m.pad, m.titleH, m.metaH
	accentW, priceW, rowH := m.accentW, m.priceW, m.rowH
	dpi := m.dpi

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
	var tip *tipGeom
	for i := first; i < len(b.items); i++ {
		y := i*rowH - b.scroll
		if y >= bounds.Height {
			break
		}
		item := b.items[i]
		row := m.rowRect(bounds.Width, y)
		fill := b.row
		if i == b.hover {
			fill = b.rowHot
		}
		_ = canvas.FillRectanglePixels(fill, row)
		_ = canvas.FillRectanglePixels(b.accent, walk.Rectangle{X: row.X, Y: row.Y, Width: accentW, Height: row.Height})

		textX := row.X + pad
		if icon := b.icons[item.SiteKey]; icon != nil {
			iconSize := walk.IntFrom96DPI(28, dpi)
			iconGap := walk.IntFrom96DPI(10, dpi)
			iconY := row.Y + pad + (titleH-iconSize)/2
			if iconY < row.Y+pad {
				iconY = row.Y + pad
			}
			_ = canvas.DrawImageStretchedPixels(icon, walk.Rectangle{
				X: textX, Y: iconY, Width: iconSize, Height: iconSize,
			})
			textX += iconSize + iconGap
		}

		titleBox := walk.Rectangle{X: textX, Y: row.Y + pad, Width: row.X + row.Width - textX - pad - priceW, Height: titleH}
		priceBox := walk.Rectangle{X: row.X + row.Width - pad - priceW, Y: row.Y + pad, Width: priceW, Height: titleH}
		metaBox := walk.Rectangle{X: textX, Y: row.Y + pad + titleH, Width: row.X + row.Width - textX - pad, Height: metaH}

		_ = canvas.DrawTextPixels(item.Title, b.titleFont, text, titleBox, walk.TextLeft|walk.TextVCenter|walk.TextEndEllipsis|walk.TextSingleLine|walk.TextNoPrefix)
		_ = canvas.DrawTextPixels(item.Price, b.priceFont, gold, priceBox, walk.TextRight|walk.TextVCenter|walk.TextSingleLine|walk.TextNoPrefix)

		meta := item.Site + " · " + item.City + " · " + item.Status
		if item.Watchers > 1 {
			meta += " · " + view.RuPlural(item.Watchers, "подписчик", "подписчика", "подписчиков")
		}
		if item.Checked != "" {
			meta += " · " + item.Checked
		}
		_ = canvas.DrawTextPixels(meta, b.metaFont, muted, metaBox, walk.TextLeft|walk.TextVCenter|walk.TextEndEllipsis|walk.TextSingleLine|walk.TextNoPrefix)

		chart := m.chartRect(row)
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
		if i == b.tipItem {
			tip = &tipGeom{chart: chart, pts: wpts}
		}

		nodeR := walk.IntFrom96DPI(3, dpi)
		hotR := walk.IntFrom96DPI(5, dpi)
		// Подписи идут слева направо, и следующая пропускается, если легла бы
		// на предыдущую: при девяноста замерах на строку все ценники слиплись
		// бы в кашу. Узел под курсором подписывается всегда.
		labelEdge := chart.X
		for j, p := range wpts {
			r := nodeR
			hot := i == b.tipItem && j == b.tipNode
			if hot {
				r = hotR
			}
			if b.accent != nil {
				_ = canvas.FillEllipsePixels(b.accent, walk.Rectangle{
					X: p.X - r, Y: p.Y - r, Width: r*2 + 1, Height: r*2 + 1,
				})
			}
			if j >= len(item.Samples) {
				continue
			}
			price := money.FormatKopecks(item.Samples[j].Price)
			box, ok := b.nodePriceBox(canvas, chart, p, price, r, m)
			if !ok || (box.X < labelEdge && !hot) {
				continue
			}
			_ = canvas.DrawTextPixels(price, b.metaFont, gold, box,
				walk.TextLeft|walk.TextTop|walk.TextSingleLine|walk.TextNoPrefix)
			labelEdge = box.X + box.Width + walk.IntFrom96DPI(6, dpi)
		}
	}
	b.paintTip(canvas, bounds, m, tip)
	return nil
}

// tipGeom — геометрия строки под курсором, посчитанная во время отрисовки.
// Иначе подсказке пришлось бы заново считать весь график этой строки.
type tipGeom struct {
	chart walk.Rectangle
	pts   []walk.Point
}

type boardMetrics struct {
	dpi, pad, titleH, metaH, chartH, accentW, priceW, gap, rowH int
}

func (b *board) metrics() boardMetrics {
	dpi := b.dpi()
	return boardMetrics{
		dpi:     dpi,
		pad:     walk.IntFrom96DPI(16, dpi),
		titleH:  walk.IntFrom96DPI(24, dpi),
		metaH:   walk.IntFrom96DPI(18, dpi),
		chartH:  walk.IntFrom96DPI(72, dpi),
		accentW: walk.IntFrom96DPI(4, dpi),
		priceW:  walk.IntFrom96DPI(168, dpi),
		gap:     walk.IntFrom96DPI(8, dpi),
		rowH:    walk.IntFrom96DPI(rowHeight96, dpi),
	}
}

func (m boardMetrics) rowRect(width, y int) walk.Rectangle {
	return walk.Rectangle{X: 0, Y: y, Width: width, Height: m.rowH - walk.IntFrom96DPI(6, m.dpi)}
}

func (m boardMetrics) chartRect(row walk.Rectangle) walk.Rectangle {
	return walk.Rectangle{
		X:      row.X + m.pad,
		Y:      row.Y + m.pad + m.titleH + m.metaH + m.gap/2,
		Width:  row.Width - m.pad*2,
		Height: m.chartH,
	}
}

// nodePriceBox — место для ценника узла: над точкой, а если сверху не
// влезает — под ней. Рамка подгоняется по размеру самого текста.
func (b *board) nodePriceBox(canvas *walk.Canvas, chart walk.Rectangle, p walk.Point, price string, nodeR int, m boardMetrics) (walk.Rectangle, bool) {
	if price == "" || b.metaFont == nil {
		return walk.Rectangle{}, false
	}
	sz := measureLine(canvas, b.metaFont, price)
	if sz.Width < 1 {
		return walk.Rectangle{}, false
	}
	gap := walk.IntFrom96DPI(4, m.dpi)
	lx := p.X - sz.Width/2
	ly := p.Y - nodeR - gap - sz.Height
	if ly < chart.Y {
		ly = p.Y + nodeR + gap
	}
	if lx < chart.X {
		lx = chart.X
	}
	if lx+sz.Width > chart.X+chart.Width {
		lx = chart.X + chart.Width - sz.Width
	}
	return walk.Rectangle{X: lx, Y: ly, Width: sz.Width, Height: sz.Height}, true
}

func (b *board) paintTip(canvas *walk.Canvas, bounds walk.Rectangle, m boardMetrics, geom *tipGeom) {
	if geom == nil || b.tipItem < 0 || b.tipItem >= len(b.items) || b.tipNode < 0 {
		return
	}
	item := b.items[b.tipItem]
	if b.tipNode >= len(item.Samples) || b.tipNode >= len(geom.pts) {
		return
	}
	sample := item.Samples[b.tipNode]
	date := sample.When
	price := money.FormatKopecks(sample.Price)
	if date == "" && price == "" {
		return
	}
	nx := geom.pts[b.tipNode].X
	ny := geom.pts[b.tipNode].Y

	dateSz := measureLine(canvas, b.metaFont, date)
	priceSz := measureLine(canvas, b.priceFont, price)
	if b.priceFont == nil {
		priceSz = measureLine(canvas, b.metaFont, price)
	}
	lineGap := walk.IntFrom96DPI(2, m.dpi)
	boxPad := walk.IntFrom96DPI(8, m.dpi)
	innerW := dateSz.Width
	if priceSz.Width > innerW {
		innerW = priceSz.Width
	}
	innerH := dateSz.Height + lineGap + priceSz.Height
	tw := innerW + boxPad*2
	th := innerH + boxPad*2
	tx := nx - tw/2
	ty := ny - th - walk.IntFrom96DPI(10, m.dpi)
	if tx < bounds.X+2 {
		tx = bounds.X + 2
	}
	if tx+tw > bounds.X+bounds.Width-2 {
		tx = bounds.X + bounds.Width - 2 - tw
	}
	if ty < bounds.Y+2 {
		ty = ny + walk.IntFrom96DPI(12, m.dpi)
	}
	tip := walk.Rectangle{X: tx, Y: ty, Width: tw, Height: th}
	fill := b.rowHot
	if fill == nil {
		fill = b.row
	}
	if fill != nil {
		_ = canvas.FillRectanglePixels(fill, tip)
	}
	if b.goldPen != nil {
		_ = canvas.DrawRectanglePixels(b.goldPen, tip)
	}
	textClr := walk.RGB(243, 234, 220)
	gold := walk.RGB(226, 182, 87)
	dateBox := walk.Rectangle{X: tip.X + boxPad, Y: tip.Y + boxPad, Width: innerW, Height: dateSz.Height}
	priceBox := walk.Rectangle{X: tip.X + boxPad, Y: dateBox.Y + dateSz.Height + lineGap, Width: innerW, Height: priceSz.Height}
	_ = canvas.DrawTextPixels(date, b.metaFont, textClr, dateBox, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine|walk.TextNoPrefix)
	priceFont := b.priceFont
	if priceFont == nil {
		priceFont = b.metaFont
	}
	_ = canvas.DrawTextPixels(price, priceFont, gold, priceBox, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine|walk.TextNoPrefix)
}

func measureLine(canvas *walk.Canvas, font *walk.Font, text string) walk.Rectangle {
	if canvas == nil || font == nil || text == "" {
		return walk.Rectangle{}
	}
	sz, _, err := canvas.MeasureTextPixels(text, font, walk.Rectangle{Width: 2000, Height: 200}, walk.TextCalcRect|walk.TextSingleLine|walk.TextNoPrefix)
	if err != nil {
		return walk.Rectangle{Width: len([]rune(text)) * 8, Height: 16}
	}
	if sz.Width < 1 {
		sz.Width = 1
	}
	if sz.Height < 1 {
		sz.Height = 1
	}
	return sz
}

func (b *board) hit(x, y int) (item, node int) {
	item, node = -1, -1
	if len(b.items) == 0 {
		return
	}
	m := b.metrics()
	idx := (y + b.scroll) / m.rowH
	if idx < 0 || idx >= len(b.items) {
		return
	}
	item = idx
	it := b.items[idx]
	if len(it.Points) == 0 {
		return
	}
	width := 0
	if b.widget != nil {
		width = b.widget.ClientBoundsPixels().Width
	}
	rowY := idx*m.rowH - b.scroll
	chart := m.chartRect(m.rowRect(width, rowY))
	if x < chart.X || x >= chart.X+chart.Width || y < chart.Y || y >= chart.Y+chart.Height {
		return
	}
	node = hitSample(chart.Width, len(it.Points), x-chart.X)
	return
}

func (b *board) attach(w *walk.CustomWidget) {
	b.widget = w
	b.hover, b.tipItem, b.tipNode = -1, -1, -1
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
	// Иначе после растягивания окна под последней строкой остаётся пустота:
	// прокрутка упирается в старый предел до первого движения колеса.
	w.SizeChanged().Attach(func() {
		b.clampScroll()
		w.Invalidate()
	})
	w.MouseMove().Attach(func(x, y int, _ walk.MouseButton) {
		idx, node := b.hit(x, y)
		if idx != b.hover || node != b.tipNode || idx != b.tipItem {
			b.hover = idx
			b.tipItem = idx
			b.tipNode = node
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
