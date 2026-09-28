//go:build windows

package gui

import (
	"sync"
	"syscall"

	"github.com/Msey/price-tracking-bot/internal/money"
	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// tipPad96 — зазор между текстом и рамкой.
// tipGap96 — зазор между датой и ценой в одной строке.
const (
	tipPad96 = 5
	tipGap96 = 6
)

func tipBoxSize(dateW, dateH, priceW, priceH, pad, gap int) (w, h int) {
	line := dateH
	if priceH > line {
		line = priceH
	}
	w = dateW + gap + priceW + pad*2
	h = line + pad*2
	if dateW < 1 {
		w = priceW + pad*2
	}
	if priceW < 1 {
		w = dateW + pad*2
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return
}

func (b *board) createTip(parent *walk.CustomWidget) {
	if parent == nil || b.tipHost != nil {
		return
	}
	style := win.GetWindowLongPtr(parent.Handle(), win.GWL_STYLE)
	win.SetWindowLongPtr(parent.Handle(), int(win.GWL_STYLE), style|uintptr(win.WS_CLIPCHILDREN))

	host, err := walk.NewCompositeWithStyle(parent, 0)
	if err != nil {
		b.tipLog("контейнер цены не создан", "error", err)
		return
	}
	host.SetVisible(false)
	if b.rowHot != nil {
		host.SetBackground(b.rowHot)
	}
	face, err := walk.NewCustomWidgetPixels(host, 0, b.paintTipFace)
	if err != nil {
		host.Dispose()
		b.tipLog("контейнер цены не создан", "error", err)
		return
	}
	face.SetPaintMode(walk.PaintBuffered)
	b.tipHost = host
	b.tipFace = face
	b.forwardTipInput(host)
	b.catchTipDoubleClick(face)
	face.SetCursor(walk.CursorHand())
	b.tipLog("контейнер цены создан")
}

func (b *board) disposeTip() {
	if b.tipHost != nil {
		b.tipLog("контейнер цены уничтожен", "visible", b.tipHost.Visible())
	}
	if b.tipFace != nil {
		forgetTipClicks(b.tipFace.Handle())
	}
	if b.tipHost != nil {
		b.tipHost.Dispose()
		b.tipHost = nil
		b.tipFace = nil
		b.tipTrace.visible = false
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
	// Движение с тултипа в список не переводим: координаты попадают
	// в заголовок строки, узел сбрасывается и рамка гаснет под курсором.
	host.MouseDown().Attach(func(_ int, _ int, button walk.MouseButton) {
		b.onTipMouseDown(button)
	})
	host.MouseWheel().Attach(func(_, _ int, button walk.MouseButton) {
		b.onWheel(walk.MouseWheelEventDelta(button))
	})
	if b.tipFace == nil {
		return
	}
	b.tipFace.MouseDown().Attach(func(_ int, _ int, button walk.MouseButton) {
		b.onTipMouseDown(button)
	})
	b.tipFace.MouseWheel().Attach(func(_, _ int, button walk.MouseButton) {
		b.onWheel(walk.MouseWheelEventDelta(button))
	})
}

var (
	tipBoards sync.Map
	tipPrev   sync.Map
	tipProcCB = syscall.NewCallback(tipWndProc)
)

func (b *board) catchTipDoubleClick(w *walk.CustomWidget) {
	if b == nil || w == nil {
		return
	}
	hwnd := w.Handle()
	if hwnd == 0 {
		return
	}
	tipBoards.Store(hwnd, b)
	if prev := win.SetWindowLongPtr(hwnd, win.GWLP_WNDPROC, tipProcCB); prev != 0 {
		tipPrev.Store(hwnd, prev)
	}
}

func forgetTipClicks(hwnd win.HWND) {
	if hwnd == 0 {
		return
	}
	if prev, ok := tipPrev.Load(hwnd); ok {
		win.SetWindowLongPtr(hwnd, win.GWLP_WNDPROC, prev.(uintptr))
		tipPrev.Delete(hwnd)
	}
	tipBoards.Delete(hwnd)
}

func tipWndProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	if msg == win.WM_LBUTTONDBLCLK {
		if v, ok := tipBoards.Load(hwnd); ok {
			v.(*board).openCurrent()
		}
	}
	if msg == win.WM_NCDESTROY {
		forgetTipClicks(hwnd)
	}
	if prev, ok := tipPrev.Load(hwnd); ok {
		return win.CallWindowProc(prev.(uintptr), hwnd, msg, wParam, lParam)
	}
	return win.DefWindowProc(hwnd, msg, wParam, lParam)
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
	pad := walk.IntFrom96DPI(tipPad96, dpi)
	gap := walk.IntFrom96DPI(tipGap96, dpi)
	innerH := bounds.Height - pad*2
	if innerH < 1 {
		return nil
	}
	dateFont := b.dateFont()
	priceFont := b.priceTipFont()
	textClr := walk.RGB(243, 234, 220)
	gold := walk.RGB(226, 182, 87)
	draw := walk.TextVCenter | walk.TextSingleLine | walk.TextNoPrefix
	x := bounds.X + pad
	row := walk.Rectangle{X: x, Y: bounds.Y + pad, Height: innerH}
	if dateFont != nil && b.tipDate != "" {
		dateW := b.measureLine(dateFont, b.tipDate).Width
		if dateW < 1 {
			dateW = 1
		}
		row.Width = dateW
		_ = canvas.DrawTextPixels(b.tipDate, dateFont, textClr, row, draw|walk.TextLeft)
		x += dateW + gap
	}
	if priceFont != nil && b.tipPrice != "" {
		priceClr := gold
		if b.tipMiss {
			priceClr = walk.RGB(154, 141, 122)
		}
		priceW := bounds.X + bounds.Width - pad - x
		if priceW < 1 {
			priceW = 1
		}
		row.X = x
		row.Width = priceW
		_ = canvas.DrawTextPixels(b.tipPrice, priceFont, priceClr, row, draw|walk.TextLeft|walk.TextEndEllipsis)
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
		if f, err := walk.NewFont("Segoe UI", 10, 0); err == nil {
			b.tipFont = f
		}
	}
	if b.tipPriceFont == nil {
		if f, err := walk.NewFont("Segoe UI", 10, walk.FontBold); err == nil {
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
	return b.tipNode < len(b.display(b.tipItem).samples)
}

func (b *board) syncTip() {
	if b.widget == nil {
		return
	}
	if !b.tipNeeded() {
		b.hideTip("нет узла")
		return
	}
	if !b.ensureTip() {
		return
	}
	samples := b.display(b.tipItem).samples
	if b.tipNode >= len(samples) {
		b.hideTip("узел вне ряда")
		return
	}
	sample := samples[b.tipNode]
	date := sample.When
	price := money.FormatKopecks(sample.Price)
	nx, ny, ok := b.nodePixel(b.tipItem, b.tipNode)
	if !ok {
		b.hideTip("точка не на графике")
		return
	}
	b.ensureTipSize(date, price)
	view := b.widget.ClientBoundsPixels()
	r, fits := b.placeTip(nx, ny, view)
	if !fits || b.tipWouldHide(r) {
		b.hideTip("закрыла бы точки графика")
		return
	}
	textChanged := date != b.tipDate || price != b.tipPrice || b.tipMiss != !sample.Available
	b.tipDate = date
	b.tipPrice = price
	b.tipMiss = !sample.Available
	if r != b.tipTrace.bounds || !b.tipHost.Visible() {
		_ = b.tipHost.SetBoundsPixels(r)
		if b.tipFace != nil {
			_ = b.tipFace.SetBoundsPixels(walk.Rectangle{Width: r.Width, Height: r.Height})
		}
	}
	shown := false
	if !b.tipHost.Visible() {
		b.tipHost.SetVisible(true)
		shown = true
	}
	if (shown || textChanged) && b.tipFace != nil {
		b.tipFace.Invalidate()
	}
	b.noteTipShown(r, nx, ny)
}

// placeTip держит рамку над точкой. Пустую сторону берём сразу.
// Если обе задевают точки, всё равно остаёмся над точкой: на пике
// цена должна быть видна. «Верх окна» — только когда рядом места нет.
func (b *board) placeTip(nx, ny int, view walk.Rectangle) (walk.Rectangle, bool) {
	gap := walk.IntFrom96DPI(5, b.dpi())
	above, aboveOK := tipRectSide(nx, ny, view, b.tipW, b.tipH, gap, true)
	if aboveOK {
		if score := b.coverScore(above); score == 0 {
			b.tipWhere = "над точкой"
			b.tipCovers = false
			return above, true
		}
	}
	below, belowOK := tipRectSide(nx, ny, view, b.tipW, b.tipH, gap, false)
	if belowOK {
		if score := b.coverScore(below); score == 0 {
			b.tipWhere = "под точкой"
			b.tipCovers = false
			return below, true
		}
	}
	if aboveOK {
		b.tipWhere = "над точкой"
		b.tipCovers = true
		return above, true
	}
	if belowOK {
		b.tipWhere = "под точкой"
		b.tipCovers = true
		return below, true
	}
	if top, ok := tipRectTop(nx, view, b.tipW, b.tipH); ok {
		b.tipWhere = "верх окна"
		b.tipCovers = b.coverScore(top) > 0
		return top, true
	}
	return walk.Rectangle{}, false
}

// tipRectTop — рамка у верхнего края окна, по X над точкой.
// Нужна, когда над пиком нет высоты на полную рамку.
func tipRectTop(nx int, view walk.Rectangle, w, h int) (walk.Rectangle, bool) {
	ny := view.Y + 2 + h
	return tipRectSide(nx, ny, view, w, h, 0, true)
}

func (b *board) coverScore(tip walk.Rectangle) int {
	n := 0
	if b.coversChartAbove(tip) {
		n += 1000
	}
	if b.widget == nil || b.tipNode < 0 || len(b.spark) < 2 {
		return n
	}
	m := b.metrics()
	width := b.widget.ClientBoundsPixels().Width
	chart := m.chartRect(m.rowRect(width, b.tipItem*m.rowH-b.scroll))
	pad := walk.IntFrom96DPI(2, m.dpi)
	return n + tipCoverCount(b.spark, b.tipNode, chart.X, chart.Y, tip.X, tip.Y, tip.Width, tip.Height, pad)
}

// coversChartAbove — рамка закрыла график предыдущей строки.
// У первой строки такого графика нет.
func (b *board) coversChartAbove(tip walk.Rectangle) bool {
	if b == nil || b.tipItem <= 0 {
		return false
	}
	m, row := b.rowBounds(b.tipItem - 1)
	return rectsOverlap(tip, m.chartRect(row))
}

// tipWouldHide — курсор уже внутри будущей рамки и попадает в другую точку.
// Тогда рамку не рисуем. После показа она перехватывает мышь, и прятать
// её пришлось бы отдельным проходом.
func (b *board) tipWouldHide(r walk.Rectangle) bool {
	if !rectContains(r, b.cursorX, b.cursorY) {
		return false
	}
	item, node := b.hit(b.cursorX, b.cursorY)
	return otherChartNode(b.tipItem, b.tipNode, item, node)
}

func (b *board) hideTip(reason string) {
	visible := b.tipHost != nil && b.tipHost.Visible()
	if visible {
		b.tipHost.SetVisible(false)
		b.tipLog("контейнер цены скрыт",
			"reason", reason,
			"item", b.tipItem, "node", b.tipNode,
			"cursor_x", b.cursorX, "cursor_y", b.cursorY,
		)
	}
	b.tipTrace.visible = false
	b.tipDate = ""
	b.tipPrice = ""
	b.tipMiss = false
	b.tipCovers = false
}

func (b *board) noteTipShown(r walk.Rectangle, nx, ny int) {
	if b.tipTrace.visible && b.tipTrace.item == b.tipItem && b.tipTrace.node == b.tipNode && b.tipTrace.bounds == r && b.tipTrace.covers == b.tipCovers {
		return
	}
	b.tipTrace.visible = true
	b.tipTrace.item = b.tipItem
	b.tipTrace.node = b.tipNode
	b.tipTrace.bounds = r
	b.tipTrace.covers = b.tipCovers
	b.tipLog("контейнер цены показан",
		"item", b.tipItem,
		"node", b.tipNode,
		"cursor_x", b.cursorX, "cursor_y", b.cursorY,
		"node_x", nx, "node_y", ny,
		"tip_x", r.X, "tip_y", r.Y, "tip_w", r.Width, "tip_h", r.Height,
		"where", b.tipWhere,
		"covers", b.tipCovers,
		"price", b.tipPrice,
	)
}

func (b *board) tipLog(msg string, args ...any) {
	if b == nil || b.log == nil {
		return
	}
	b.log.Info(msg, args...)
}

func (b *board) ensureTipSize(date, price string) {
	dpi := b.dpi()
	if date == "" {
		date = " "
	}
	if price == "" {
		price = " "
	}
	dateSz := b.measureLine(b.dateFont(), date)
	priceSz := b.measureLine(b.priceTipFont(), price)
	pad := walk.IntFrom96DPI(tipPad96, dpi)
	gap := walk.IntFrom96DPI(tipGap96, dpi)
	w, h := tipBoxSize(dateSz.Width, dateSz.Height, priceSz.Width, priceSz.Height, pad, gap)
	b.tipW, b.tipH, b.tipDPI = w, h, dpi
}

func (b *board) nodePixel(item, node int) (x, y int, ok bool) {
	if b.widget == nil || item < 0 || item >= len(b.items) {
		return
	}
	prices := b.display(item).prices
	if node < 0 || node >= len(prices) {
		return
	}
	m := b.metrics()
	width := b.widget.ClientBoundsPixels().Width
	chart := m.chartRect(m.rowRect(width, item*m.rowH-b.scroll))
	pts := sparklineInto(b.spark, chart.Width, chart.Height, prices)
	b.spark = pts
	if node >= len(pts) {
		return
	}
	return chart.X + pts[node].X, chart.Y + pts[node].Y, true
}

func tipRect(nx, ny int, view walk.Rectangle, w, h, gap int) walk.Rectangle {
	if r, ok := tipRectSide(nx, ny, view, w, h, gap, true); ok {
		return r
	}
	if r, ok := tipRectSide(nx, ny, view, w, h, gap, false); ok {
		return r
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return walk.Rectangle{X: view.X + 2, Y: view.Y + 2, Width: w, Height: h}
}

// tipRectSide ставит рамку над точкой или под ней. false — сторона не
// влезает в окно, прижимать её к краю нельзя: тогда она наезжает на график.
func tipRectSide(nx, ny int, view walk.Rectangle, w, h, gap int, above bool) (walk.Rectangle, bool) {
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
		return walk.Rectangle{}, false
	}
	tx := nx - w/2
	ty := ny + gap + 1
	if above {
		ty = ny - h - gap
	}
	if tx < view.X+2 {
		tx = view.X + 2
	}
	if tx+w > view.X+view.Width-2 {
		tx = view.X + view.Width - 2 - w
	}
	if tx < view.X+2 || ty < view.Y+2 || ty+h > view.Y+view.Height-2 {
		return walk.Rectangle{}, false
	}
	return walk.Rectangle{X: tx, Y: ty, Width: w, Height: h}, true
}
