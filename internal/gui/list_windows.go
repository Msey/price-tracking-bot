//go:build windows

package gui

import (
	"time"

	"github.com/Msey/price-tracking-bot/internal/money"
	"github.com/Msey/price-tracking-bot/internal/view"
	"github.com/lxn/walk"
	"github.com/lxn/win"
)

const rowHeight96 = 74 // компактная строка: в окне видно примерно вдвое больше товаров

type board struct {
	widget       *walk.CustomWidget
	items        []Item
	scroll       int
	hover        int
	tipItem      int
	tipNode      int
	lastClick    time.Time
	lastIdx      int
	titleFont    *walk.Font
	metaFont     *walk.Font
	priceFont    *walk.Font
	tipFont      *walk.Font
	tipPriceFont *walk.Font
	bg           *walk.SolidColorBrush
	row          *walk.SolidColorBrush
	rowHot       *walk.SolidColorBrush
	accent       *walk.SolidColorBrush
	upBrush      *walk.SolidColorBrush
	downBrush    *walk.SolidColorBrush
	missBrush    *walk.SolidColorBrush
	goldPen      walk.Pen
	upPen        walk.Pen
	downPen      walk.Pen
	missPen      walk.Pen
	gridPen      walk.Pen
	icons        map[string]walk.Image
	trash        walk.Image
	trashHot     walk.Image
	onOpen       func(Item)
	onDelete     func(Item)
	hoverTrash   bool
	// spark/wpts/curve живут между кадрами: Invalidate при движении мыши
	// иначе выделял бы новый срез на каждую видимую строку.
	spark  []point
	wpts   []walk.Point
	curve  []point
	wcurve []walk.Point
	// measureDC — обычный memory DC для GetTextExtentPoint32. MeasureTextPixels
	// у walk рисует в CreateEnhMetaFile и не закрывает его до Dispose: каждый
	// замер подписи и тултипа дописывал записи в EMF, и Working Set рос.
	measureDC   win.HDC
	measureDPI  int
	measureHF   map[*walk.Font]win.HFONT
	measureSize map[textSizeKey]walk.Rectangle
	// tipHost — один контейнер на всё окно: двигаем его к узлу и подставляем
	// дату с ценой, без новой рамки на каждый кадр списка.
	tipHost  *walk.Composite
	tipFace  *walk.CustomWidget
	tipDate  string
	tipPrice string
	tipW     int
	tipH     int
	tipDPI   int
	tipMiss  bool
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
		b.hoverTrash = false
	}
	b.clampScroll()
	b.syncTip()
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

	// Заголовок теплее белого, но светлее золота цены и мета-строки,
	// иначе имя сливается с остальным текстом.
	title := walk.RGB(236, 214, 176)
	muted := walk.RGB(154, 141, 122)
	gold := walk.RGB(226, 182, 87)

	if len(b.items) == 0 {
		msg := walk.Rectangle{X: bounds.X + pad, Y: bounds.Y + bounds.Height/3, Width: bounds.Width - pad*2, Height: titleH * 2}
		_ = canvas.DrawTextPixels("Пока нет ссылок.\nПришлите товар боту в Telegram.", b.titleFont, muted, msg, walk.TextCenter|walk.TextWordbreak|walk.TextNoPrefix)
		return nil
	}

	var first int
	if first = b.scroll / rowH; first < 0 {
		first = 0
	}
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
			iconSize := walk.IntFrom96DPI(16, dpi)
			iconGap := walk.IntFrom96DPI(6, dpi)
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

		_ = canvas.DrawTextPixels(item.Title, b.titleFont, title, titleBox, walk.TextLeft|walk.TextVCenter|walk.TextEndEllipsis|walk.TextSingleLine|walk.TextNoPrefix)
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
		pts := sparklineInto(b.spark, chart.Width, chart.Height, item.Points)
		b.spark = pts
		if len(pts) == 0 {
			_ = canvas.DrawTextPixels("график появится после первой проверки", b.metaFont, muted, chart, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine|walk.TextNoPrefix)
		} else {
			b.wpts = b.wpts[:0]
			for _, p := range pts {
				b.wpts = append(b.wpts, walk.Point{X: chart.X + p.X, Y: chart.Y + p.Y})
			}
			if len(pts) == 1 {
				pen := b.goldPen
				if len(item.Samples) > 0 && !item.Samples[0].Available && b.missPen != nil {
					pen = b.missPen
				}
				if pen != nil {
					left, right := singlePriceSpan(chart.Width, pts[0])
					_ = canvas.DrawLinePixels(pen,
						walk.Point{X: chart.X + left.X, Y: chart.Y + left.Y},
						walk.Point{X: chart.X + right.X, Y: chart.Y + right.Y})
				}
			}
			for j := 1; j < len(b.wpts) && j < len(item.Points); j++ {
				if pen := b.segmentPen(item, j); pen != nil {
					b.drawSparkCurve(canvas, pen, b.wpts[j-1], b.wpts[j])
				}
			}

			nodeR := walk.IntFrom96DPI(2, dpi)
			hotR := walk.IntFrom96DPI(3, dpi)
			// Подпись — только на смене цены. Одинаковые узлы подряд без
			// ценника: иначе плато из десятков замеров забивает график одним
			// и тем же числом. Близкие разные цены по-прежнему не наезжают
			// друг на друга.
			labelEdge := chart.X
			for j, p := range b.wpts {
				r := nodeR
				hot := i == b.tipItem && j == b.tipNode
				if hot {
					r = hotR
				}
				brush := b.nodeBrush(item, j)
				fillChartNode(canvas, brush, p.X, p.Y, r)
				if !sampleChartLabel(item.Samples, j) {
					continue
				}
				price := money.FormatKopecks(item.Samples[j].Price)
				box, ok := b.nodePriceBox(chart, p, price, r, m)
				if !ok || box.X < labelEdge {
					continue
				}
				labelClr := gold
				if !item.Samples[j].Available {
					labelClr = muted
				}
				_ = canvas.DrawTextPixels(price, b.metaFont, labelClr, box,
					walk.TextLeft|walk.TextTop|walk.TextSingleLine|walk.TextNoPrefix)
				labelEdge = box.X + box.Width + walk.IntFrom96DPI(6, dpi)
			}
		}
		b.paintTrash(canvas, m.trashRect(row), i == b.hover && b.hoverTrash)
	}
	return nil
}

type boardMetrics struct {
	dpi, pad, titleH, metaH, chartH, accentW, priceW, gap, rowH, trash int
}

func (b *board) metrics() boardMetrics {
	dpi := b.dpi()
	return boardMetrics{
		dpi:     dpi,
		pad:     walk.IntFrom96DPI(8, dpi),
		titleH:  walk.IntFrom96DPI(16, dpi),
		metaH:   walk.IntFrom96DPI(12, dpi),
		chartH:  walk.IntFrom96DPI(32, dpi),
		accentW: walk.IntFrom96DPI(3, dpi),
		priceW:  walk.IntFrom96DPI(140, dpi),
		gap:     walk.IntFrom96DPI(4, dpi),
		rowH:    walk.IntFrom96DPI(rowHeight96, dpi),
		trash:   walk.IntFrom96DPI(20, dpi),
	}
}

func (m boardMetrics) rowRect(width, y int) walk.Rectangle {
	return walk.Rectangle{X: 0, Y: y, Width: width, Height: m.rowH - walk.IntFrom96DPI(4, m.dpi)}
}

func (m boardMetrics) chartRect(row walk.Rectangle) walk.Rectangle {
	chartW, _ := trashLayout(row.Width, m.pad, m.gap, m.trash)
	return walk.Rectangle{
		X:      row.X + m.pad,
		Y:      row.Y + m.pad + m.titleH + m.metaH + m.gap/2,
		Width:  chartW,
		Height: m.chartH,
	}
}

func (m boardMetrics) trashRect(row walk.Rectangle) walk.Rectangle {
	_, trashX := trashLayout(row.Width, m.pad, m.gap, m.trash)
	chart := m.chartRect(row)
	return walk.Rectangle{
		X:      row.X + trashX,
		Y:      chart.Y + (chart.Height-m.trash)/2,
		Width:  m.trash,
		Height: m.trash,
	}
}

func rectContains(r walk.Rectangle, x, y int) bool {
	return x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height
}

func fillChartNode(canvas *walk.Canvas, brush *walk.SolidColorBrush, cx, cy, r int) {
	if canvas == nil || brush == nil || r < 1 {
		return
	}
	for dy := -r; dy <= r; dy++ {
		half := nodeHalfWidth(r, dy)
		if half < 1 && r > 0 {
			continue
		}
		_ = canvas.FillRectanglePixels(brush, walk.Rectangle{
			X: cx - half, Y: cy + dy, Width: half*2 + 1, Height: 1,
		})
	}
}

func (b *board) drawSparkCurve(canvas *walk.Canvas, pen walk.Pen, from, to walk.Point) {
	if canvas == nil || pen == nil {
		return
	}
	b.curve = appendCubic(b.curve[:0], point{X: from.X, Y: from.Y}, point{X: to.X, Y: to.Y})
	b.wcurve = b.wcurve[:0]
	for _, p := range b.curve {
		b.wcurve = append(b.wcurve, walk.Point{X: p.X, Y: p.Y})
	}
	if len(b.wcurve) < 2 {
		return
	}
	_ = canvas.DrawPolylinePixels(pen, b.wcurve)
}

func (b *board) sampleMissing(it Item, i int) bool {
	return i >= 0 && i < len(it.Samples) && !it.Samples[i].Available
}

func (b *board) segmentPen(it Item, j int) walk.Pen {
	if b.sampleMissing(it, j) && b.missPen != nil {
		return b.missPen
	}
	if j < 1 || j >= len(it.Points) {
		return b.goldPen
	}
	return b.sparkPen(it.Points[j-1], it.Points[j])
}

func (b *board) nodeBrush(it Item, j int) *walk.SolidColorBrush {
	if b.sampleMissing(it, j) && b.missBrush != nil {
		return b.missBrush
	}
	if j > 0 && j < len(it.Points) {
		return b.sparkNode(it.Points[j-1], it.Points[j])
	}
	return b.accent
}

func (b *board) sparkPen(from, to int64) walk.Pen {
	switch priceMove(from, to) {
	case -1:
		if b.downPen != nil {
			return b.downPen
		}
	case 1:
		if b.upPen != nil {
			return b.upPen
		}
	}
	return b.goldPen
}

func (b *board) sparkNode(from, to int64) *walk.SolidColorBrush {
	switch priceMove(from, to) {
	case -1:
		if b.downBrush != nil {
			return b.downBrush
		}
	case 1:
		if b.upBrush != nil {
			return b.upBrush
		}
	}
	return b.accent
}

// nodePriceBox — место для ценника узла: над точкой, а если сверху не
// влезает — под ней. Рамка подгоняется по размеру самого текста.
func (b *board) nodePriceBox(chart walk.Rectangle, p walk.Point, price string, nodeR int, m boardMetrics) (walk.Rectangle, bool) {
	if price == "" || b.metaFont == nil {
		return walk.Rectangle{}, false
	}
	sz := b.measureLine(b.metaFont, price)
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

func (b *board) paintTrash(canvas *walk.Canvas, r walk.Rectangle, hot bool) {
	icon := b.trash
	if hot && b.trashHot != nil {
		icon = b.trashHot
	}
	if icon == nil {
		return
	}
	_ = canvas.DrawImageStretchedPixels(icon, r)
}

func (b *board) rowIndex(y int) int {
	if len(b.items) == 0 {
		return -1
	}
	idx := (y + b.scroll) / b.rowH()
	if idx < 0 || idx >= len(b.items) {
		return -1
	}
	return idx
}

func (b *board) overTrash(x, y int) bool {
	idx := b.rowIndex(y)
	if idx < 0 {
		return false
	}
	width := 0
	if b.widget != nil {
		width = b.widget.ClientBoundsPixels().Width
	}
	m := b.metrics()
	row := m.rowRect(width, idx*m.rowH-b.scroll)
	hit := m.trashRect(row)
	pad := walk.IntFrom96DPI(4, m.dpi)
	hit.X -= pad
	hit.Y -= pad
	hit.Width += pad * 2
	hit.Height += pad * 2
	return rectContains(hit, x, y)
}

func (b *board) hit(x, y int) (item, node int) {
	item, node = -1, -1
	idx := b.rowIndex(y)
	if idx < 0 {
		return
	}
	item = idx
	if b.overTrash(x, y) {
		return
	}
	it := b.items[idx]
	if len(it.Points) == 0 {
		return
	}
	width := 0
	if b.widget != nil {
		width = b.widget.ClientBoundsPixels().Width
	}
	m := b.metrics()
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
		b.onWheel(walk.MouseWheelEventDelta(button))
	})
	// Иначе после растягивания окна под последней строкой остаётся пустота:
	// прокрутка упирается в старый предел до первого движения колеса.
	w.SizeChanged().Attach(func() {
		b.clampScroll()
		b.syncTip()
		w.Invalidate()
	})
	w.MouseMove().Attach(func(x, y int, _ walk.MouseButton) {
		b.onMouseMove(x, y)
	})
	w.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		b.onMouseDown(x, y, button)
	})
}

func (b *board) onWheel(delta int) {
	if delta == 0 || b.widget == nil {
		return
	}
	step := b.rowH() / 2
	if delta > 0 {
		b.scroll -= step
	} else {
		b.scroll += step
	}
	b.clampScroll()
	b.syncTip()
	b.widget.Invalidate()
}

func (b *board) onMouseMove(x, y int) {
	if b.widget == nil {
		return
	}
	idx, node := b.hit(x, y)
	trash := b.overTrash(x, y)
	if trash {
		node = -1
	}
	if idx == b.hover && node == b.tipNode && idx == b.tipItem && trash == b.hoverTrash {
		return
	}
	b.hover = idx
	b.tipItem = idx
	b.tipNode = node
	b.hoverTrash = trash
	if trash {
		b.widget.SetCursor(walk.CursorHand())
	} else {
		b.widget.SetCursor(walk.CursorArrow())
	}
	b.syncTip()
	b.widget.Invalidate()
}

func (b *board) onMouseDown(x, y int, button walk.MouseButton) {
	if button != walk.LeftButton {
		return
	}
	idx := b.rowIndex(y)
	if idx < 0 {
		return
	}
	if b.overTrash(x, y) {
		if b.onDelete != nil {
			b.onDelete(b.items[idx])
		}
		b.lastIdx = -1
		return
	}
	now := time.Now()
	if idx == b.lastIdx && now.Sub(b.lastClick) < 400*time.Millisecond && b.onOpen != nil {
		b.onOpen(b.items[idx])
	}
	b.lastClick = now
	b.lastIdx = idx
}
