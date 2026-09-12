//go:build windows

package gui

import (
	"github.com/Msey/price-tracking-bot/internal/money"
	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// Шаблоны ширины контейнера: дата как FormatWhenShort, цена с запасом
// под семизначные суммы. Сама рамка после этого не меняет размер.
const (
	tipDateProbe  = "00.00.00 00:00"
	tipPriceProbe = "99 999 999 ₽"
	tipGrowNum    = 5
	tipGrowDen    = 4
)

func growTip(n int) int {
	if n < 1 {
		return n
	}
	return n * tipGrowNum / tipGrowDen
}

func (b *board) createTip(parent *walk.CustomWidget) {
	if parent == nil || b.tipHost != nil {
		return
	}
	style := win.GetWindowLongPtr(parent.Handle(), win.GWL_STYLE)
	win.SetWindowLongPtr(parent.Handle(), int(win.GWL_STYLE), style|uintptr(win.WS_CLIPCHILDREN))

	host, err := walk.NewCompositeWithStyle(parent, 0)
	if err != nil {
		return
	}
	host.SetVisible(false)
	if b.rowHot != nil {
		host.SetBackground(b.rowHot)
	}
	face, err := walk.NewCustomWidgetPixels(host, 0, b.paintTipFace)
	if err != nil {
		host.Dispose()
		return
	}
	face.SetPaintMode(walk.PaintBuffered)
	b.tipHost = host
	b.tipFace = face
	b.forwardTipInput(host)
}

func (b *board) disposeTip() {
	if b.tipHost != nil {
		b.tipHost.Dispose()
		b.tipHost = nil
		b.tipFace = nil
	}
	if b.tipFont != nil {
		b.tipFont.Dispose()
		b.tipFont = nil
	}
	if b.tipPriceFont != nil {
		b.tipPriceFont.Dispose()
		b.tipPriceFont = nil
	}
}

func (b *board) forwardTipInput(host *walk.Composite) {
	host.MouseMove().Attach(func(x, y int, _ walk.MouseButton) {
		r := host.BoundsPixels()
		b.onMouseMove(r.X+x, r.Y+y)
	})
	host.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		r := host.BoundsPixels()
		b.onMouseDown(r.X+x, r.Y+y, button)
	})
	host.MouseWheel().Attach(func(_, _ int, button walk.MouseButton) {
		b.onWheel(walk.MouseWheelEventDelta(button))
	})
}

func (b *board) paintTipFace(canvas *walk.Canvas, _ walk.Rectangle) error {
	if b.tipFace == nil {
		return nil
	}
	bounds := b.tipFace.ClientBoundsPixels()
	fill := b.rowHot
	if fill == nil {
		fill = b.row
	}
	if fill != nil {
		_ = canvas.FillRectanglePixels(fill, bounds)
	}
	if b.goldPen != nil {
		_ = canvas.DrawRectanglePixels(b.goldPen, bounds)
	}
	dpi := b.dpi()
	pad := growTip(walk.IntFrom96DPI(4, dpi))
	gap := walk.IntFrom96DPI(1, dpi)
	innerW := bounds.Width - pad*2
	innerH := bounds.Height - pad*2
	if innerW < 1 || innerH < 1 {
		return nil
	}
	dateH := (innerH - gap) / 2
	if dateH < 1 {
		dateH = innerH
	}
	dateBox := walk.Rectangle{X: bounds.X + pad, Y: bounds.Y + pad, Width: innerW, Height: dateH}
	priceBox := walk.Rectangle{
		X: bounds.X + pad, Y: dateBox.Y + dateH + gap,
		Width: innerW, Height: innerH - dateH - gap,
	}
	dateFont := b.dateFont()
	priceFont := b.priceTipFont()
	textClr := walk.RGB(243, 234, 220)
	gold := walk.RGB(226, 182, 87)
	fmt := walk.TextCenter | walk.TextVCenter | walk.TextSingleLine | walk.TextEndEllipsis | walk.TextNoPrefix
	if dateFont != nil && b.tipDate != "" {
		_ = canvas.DrawTextPixels(b.tipDate, dateFont, textClr, dateBox, fmt)
	}
	if priceFont != nil && b.tipPrice != "" && priceBox.Height > 0 {
		_ = canvas.DrawTextPixels(b.tipPrice, priceFont, gold, priceBox, fmt)
	}
	return nil
}

func (b *board) dateFont() *walk.Font {
	if b.tipFont != nil {
		return b.tipFont
	}
	return b.metaFont
}

func (b *board) priceTipFont() *walk.Font {
	if b.tipPriceFont != nil {
		return b.tipPriceFont
	}
	if b.priceFont != nil {
		return b.priceFont
	}
	return b.dateFont()
}

func (b *board) ensureTipFonts() {
	if b.tipFont == nil {
		if f, err := walk.NewFont("Segoe UI", 6, 0); err == nil {
			b.tipFont = f
		}
	}
	if b.tipPriceFont == nil {
		if f, err := walk.NewFont("Segoe UI", 6, walk.FontBold); err == nil {
			b.tipPriceFont = f
		}
	}
}

func (b *board) ensureTip() bool {
	if b.widget == nil {
		return false
	}
	if b.tipHost != nil {
		return true
	}
	b.ensureTipFonts()
	b.createTip(b.widget)
	return b.tipHost != nil
}

func (b *board) tipNeeded() bool {
	if b.tipItem < 0 || b.tipNode < 0 || b.tipItem >= len(b.items) {
		return false
	}
	return b.tipNode < len(b.items[b.tipItem].Samples)
}

func (b *board) syncTip() {
	if b.widget == nil {
		return
	}
	if !b.tipNeeded() {
		b.hideTip()
		return
	}
	if !b.ensureTip() {
		return
	}
	sample := b.items[b.tipItem].Samples[b.tipNode]
	date := sample.When
	price := money.FormatKopecks(sample.Price)
	nx, ny, ok := b.nodePixel(b.tipItem, b.tipNode)
	if !ok {
		b.hideTip()
		return
	}
	b.ensureTipSize()
	view := b.widget.ClientBoundsPixels()
	r := tipRect(nx, ny, view, b.tipW, b.tipH, walk.IntFrom96DPI(5, b.dpi()))
	textChanged := date != b.tipDate || price != b.tipPrice
	b.tipDate = date
	b.tipPrice = price
	if r != b.tipHost.BoundsPixels() {
		_ = b.tipHost.SetBoundsPixels(r)
		if b.tipFace != nil {
			_ = b.tipFace.SetBoundsPixels(walk.Rectangle{Width: r.Width, Height: r.Height})
		}
	}
	if !b.tipHost.Visible() {
		b.tipHost.SetVisible(true)
		return
	}
	if textChanged && b.tipFace != nil {
		b.tipFace.Invalidate()
	}
}

func (b *board) hideTip() {
	if b.tipHost != nil && b.tipHost.Visible() {
		b.tipHost.SetVisible(false)
	}
	b.tipDate = ""
	b.tipPrice = ""
}

func (b *board) ensureTipSize() {
	dpi := b.dpi()
	if b.tipW > 0 && b.tipDPI == dpi {
		return
	}
	dateSz := b.measureLine(b.dateFont(), tipDateProbe)
	priceSz := b.measureLine(b.priceTipFont(), tipPriceProbe)
	pad := walk.IntFrom96DPI(4, dpi)
	gap := walk.IntFrom96DPI(1, dpi)
	innerW := dateSz.Width
	if priceSz.Width > innerW {
		innerW = priceSz.Width
	}
	b.tipW = growTip(innerW + pad*2 + walk.IntFrom96DPI(4, dpi))
	b.tipH = growTip(dateSz.Height + gap + priceSz.Height + pad*2)
	if b.tipW < 1 {
		b.tipW = growTip(walk.IntFrom96DPI(88, dpi))
	}
	if b.tipH < 1 {
		b.tipH = growTip(walk.IntFrom96DPI(28, dpi))
	}
	b.tipDPI = dpi
}

func (b *board) nodePixel(item, node int) (x, y int, ok bool) {
	if b.widget == nil || item < 0 || item >= len(b.items) {
		return
	}
	it := b.items[item]
	if node < 0 || node >= len(it.Points) {
		return
	}
	m := b.metrics()
	width := b.widget.ClientBoundsPixels().Width
	chart := m.chartRect(m.rowRect(width, item*m.rowH-b.scroll))
	pts := sparklineInto(b.spark, chart.Width, chart.Height, it.Points)
	b.spark = pts
	if node >= len(pts) {
		return
	}
	return chart.X + pts[node].X, chart.Y + pts[node].Y, true
}

func tipRect(nx, ny int, view walk.Rectangle, w, h, gap int) walk.Rectangle {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if w > view.Width-4 && view.Width > 4 {
		w = view.Width - 4
	}
	if h > view.Height-4 && view.Height > 4 {
		h = view.Height - 4
	}
	tx := nx - w/2
	ty := ny - h - gap
	if tx < view.X+2 {
		tx = view.X + 2
	}
	if tx+w > view.X+view.Width-2 {
		tx = view.X + view.Width - 2 - w
	}
	if ty < view.Y+2 {
		ty = ny + gap + 1
	}
	if ty+h > view.Y+view.Height-2 {
		ty = view.Y + view.Height - 2 - h
	}
	if ty < view.Y+2 {
		ty = view.Y + 2
	}
	if tx < view.X+2 {
		tx = view.X + 2
	}
	return walk.Rectangle{X: tx, Y: ty, Width: w, Height: h}
}
