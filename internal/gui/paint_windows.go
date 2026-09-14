//go:build windows

package gui

import (
	"github.com/Msey/price-tracking-bot/internal/money"
	"github.com/Msey/price-tracking-bot/internal/view"
	"github.com/lxn/walk"
)

// paint рисует только видимые строки: список может быть длинным, а кадр
// приходит на каждое движение мыши.
func (b *board) paint(canvas *walk.Canvas, _ walk.Rectangle) error {
	if b.widget == nil {
		return nil
	}
	bounds := b.widget.ClientBoundsPixels()
	_ = canvas.FillRectanglePixels(b.bg, bounds)
	m := b.metrics()

	if len(b.items) == 0 {
		msg := walk.Rectangle{X: bounds.X + m.pad, Y: bounds.Y + bounds.Height/3, Width: bounds.Width - m.pad*2, Height: m.titleH * 2}
		_ = canvas.DrawTextPixels("Пока нет ссылок.\nПришлите товар боту в Telegram.", b.titleFont, colorMuted, msg, walk.TextCenter|walk.TextWordbreak|walk.TextNoPrefix)
		return nil
	}

	first := b.scroll / m.rowH
	if first < 0 {
		first = 0
	}
	for i := first; i < len(b.items); i++ {
		y := i*m.rowH - b.scroll
		if y >= bounds.Height {
			break
		}
		b.paintRow(canvas, i, m, bounds.Width, y)
	}
	return nil
}

// paintRow — одна строка: подложка, значок магазина, название с ценой,
// мета-строка, график и корзина.
func (b *board) paintRow(canvas *walk.Canvas, i int, m boardMetrics, width, y int) {
	item := b.items[i]
	row := m.rowRect(width, y)
	fill := b.row
	if i == b.hover {
		fill = b.rowHot
	}
	_ = canvas.FillRectanglePixels(fill, row)
	_ = canvas.FillRectanglePixels(b.accent, walk.Rectangle{X: row.X, Y: row.Y, Width: m.accentW, Height: row.Height})

	textX := row.X + m.pad
	if icon := b.icons[item.SiteKey]; icon != nil {
		iconSize := walk.IntFrom96DPI(16, m.dpi)
		iconGap := walk.IntFrom96DPI(6, m.dpi)
		iconY := row.Y + m.pad + (m.titleH-iconSize)/2
		if iconY < row.Y+m.pad {
			iconY = row.Y + m.pad
		}
		_ = canvas.DrawImageStretchedPixels(icon, walk.Rectangle{
			X: textX, Y: iconY, Width: iconSize, Height: iconSize,
		})
		textX += iconSize + iconGap
	}

	titleBox := walk.Rectangle{X: textX, Y: row.Y + m.pad, Width: row.X + row.Width - textX - m.pad - m.priceW, Height: m.titleH}
	priceBox := walk.Rectangle{X: row.X + row.Width - m.pad - m.priceW, Y: row.Y + m.pad, Width: m.priceW, Height: m.titleH}
	metaBox := walk.Rectangle{X: textX, Y: row.Y + m.pad + m.titleH, Width: row.X + row.Width - textX - m.pad, Height: m.metaH}
	if titleBox.Width < 0 {
		titleBox.Width = 0
	}
	if priceBox.Width < 0 {
		priceBox.Width = 0
	}
	if metaBox.Width < 0 {
		metaBox.Width = 0
	}

	// Заголовок теплее белого, но светлее золота цены и мета-строки,
	// иначе имя сливается с остальным текстом.
	if titleBox.Width > 0 {
		_ = canvas.DrawTextPixels(item.Title, b.titleFont, colorTitle, titleBox, walk.TextLeft|walk.TextVCenter|walk.TextEndEllipsis|walk.TextSingleLine|walk.TextNoPrefix)
	}
	if priceBox.Width > 0 {
		_ = canvas.DrawTextPixels(item.Price, b.priceFont, colorGold, priceBox, walk.TextRight|walk.TextVCenter|walk.TextSingleLine|walk.TextNoPrefix)
	}
	if metaBox.Width > 0 {
		_ = canvas.DrawTextPixels(rowMeta(item), b.metaFont, colorMuted, metaBox, walk.TextLeft|walk.TextVCenter|walk.TextEndEllipsis|walk.TextSingleLine|walk.TextNoPrefix)
	}

	b.paintChart(canvas, i, m.chartRect(row), m)
	b.paintTrash(canvas, m.trashRect(row), i == b.hover && b.hoverTrash)
}

func rowMeta(item Item) string {
	meta := item.Site + " · " + item.City + " · " + item.Status
	if item.Watchers > 1 {
		meta += " · " + view.RuPlural(item.Watchers, "подписчик", "подписчика", "подписчиков")
	}
	if item.Checked != "" {
		meta += " · " + item.Checked
	}
	return meta
}

func (b *board) paintChart(canvas *walk.Canvas, i int, chart walk.Rectangle, m boardMetrics) {
	_ = canvas.FillRectanglePixels(b.bg, chart)
	if b.gridPen != nil {
		mid := chart.Y + chart.Height/2
		_ = canvas.DrawLinePixels(b.gridPen, walk.Point{X: chart.X, Y: mid}, walk.Point{X: chart.X + chart.Width, Y: mid})
	}
	series := b.display(i)
	prices, samples := series.prices, series.samples
	// spark/wpts переиспользуются между кадрами: иначе каждое движение
	// мыши выделяло бы новые срезы на каждую видимую строку.
	pts := sparklineInto(b.spark, chart.Width, chart.Height, prices)
	b.spark = pts
	if len(pts) == 0 {
		_ = canvas.DrawTextPixels("график появится после первой проверки", b.metaFont, colorMuted, chart, walk.TextCenter|walk.TextVCenter|walk.TextSingleLine|walk.TextNoPrefix)
		return
	}
	b.wpts = b.wpts[:0]
	for _, p := range pts {
		b.wpts = append(b.wpts, walk.Point{X: chart.X + p.X, Y: chart.Y + p.Y})
	}
	if len(pts) == 1 {
		pen := b.goldPen
		if len(samples) > 0 && !samples[0].Available && b.missPen != nil {
			pen = b.missPen
		}
		if pen != nil {
			left, right := singlePriceSpan(chart.Width, pts[0])
			_ = canvas.DrawLinePixels(pen,
				walk.Point{X: chart.X + left.X, Y: chart.Y + left.Y},
				walk.Point{X: chart.X + right.X, Y: chart.Y + right.Y})
		}
	}
	for j := 1; j < len(b.wpts) && j < len(prices); j++ {
		if pen := b.segmentPen(prices, samples, j); pen != nil {
			b.drawSparkCurve(canvas, pen, b.wpts[j-1], b.wpts[j])
		}
	}
	b.paintNodes(canvas, i, chart, m, prices, samples)
}

// paintNodes рисует точки замеров и ценники. Подпись — только на смене
// цены: иначе плато из десятков замеров забивает график одним и тем же
// числом. Близкие разные цены при этом не наезжают друг на друга.
func (b *board) paintNodes(canvas *walk.Canvas, i int, chart walk.Rectangle, m boardMetrics, prices []int64, samples []Sample) {
	nodeR := walk.IntFrom96DPI(2, m.dpi)
	hotR := walk.IntFrom96DPI(3, m.dpi)
	labelEdge := chart.X
	for j, p := range b.wpts {
		r := nodeR
		if i == b.tipItem && j == b.tipNode {
			r = hotR
		}
		fillChartNode(canvas, b.nodeBrush(prices, samples, j), p.X, p.Y, r)
		if !sampleChartLabel(samples, j) {
			continue
		}
		price := money.FormatKopecks(samples[j].Price)
		box, ok := b.nodePriceBox(chart, p, price, r, m)
		if !ok || box.X < labelEdge {
			continue
		}
		labelClr := colorGold
		if !samples[j].Available {
			labelClr = colorMuted
		}
		_ = canvas.DrawTextPixels(price, b.metaFont, labelClr, box,
			walk.TextLeft|walk.TextTop|walk.TextSingleLine|walk.TextNoPrefix)
		labelEdge = box.X + box.Width + walk.IntFrom96DPI(6, m.dpi)
	}
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

// fillChartNode рисует круглую точку прямоугольниками в один пиксель:
// у walk нет заливки эллипса, а перо круглым узел не делает.
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

func (b *board) sampleMissing(samples []Sample, i int) bool {
	return i >= 0 && i < len(samples) && !samples[i].Available
}

func (b *board) segmentPen(prices []int64, samples []Sample, j int) walk.Pen {
	if b.sampleMissing(samples, j) && b.missPen != nil {
		return b.missPen
	}
	if j < 1 || j >= len(prices) {
		return b.goldPen
	}
	return b.sparkPen(prices[j-1], prices[j])
}

func (b *board) nodeBrush(prices []int64, samples []Sample, j int) *walk.SolidColorBrush {
	if b.sampleMissing(samples, j) && b.missBrush != nil {
		return b.missBrush
	}
	if j > 0 && j < len(prices) {
		return b.sparkNode(prices[j-1], prices[j])
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
