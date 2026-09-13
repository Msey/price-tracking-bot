//go:build windows

package gui

import (
	"image/color"
	"log/slog"
	"time"

	"github.com/Msey/price-tracking-bot/internal/sites"
	"github.com/lxn/walk"
	"github.com/lxn/win"
)

const rowHeight96 = 74 // компактная строка: в окне видно примерно вдвое больше товаров

type board struct {
	// theme значением: внутри только дескрипторы GDI, а нулевой board
	// с пустой палитрой нужен тестам разметки.
	theme
	widget       *walk.CustomWidget
	items        []Item
	scroll       int
	hover        int
	tipItem      int
	tipNode      int
	lastClick    time.Time
	lastIdx      int
	tipFont      *walk.Font
	tipPriceFont *walk.Font
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
	// distinct — на графике только смена цены или наличия, плато из
	// одинаковых соседних узлов схлопывается в один.
	distinct bool
	// series — готовые узлы строк, по одному на элемент items. Пересчёт
	// только на смену списка или тумблера: paint и hit зовутся на каждое
	// движение мыши, и считать серию там значило бы выделять срезы на кадр.
	series []chartSeries
}

func newBoard(t theme) *board {
	return &board{theme: t, hover: -1, tipItem: -1, tipNode: -1}
}

// loadImages готовит картинки строк: корзину в двух состояниях и значки
// магазинов. Сбой одной картинки не мешает окну: строка просто рисуется
// без неё.
func (b *board) loadImages(keep func(walk.Disposable), log *slog.Logger) {
	for _, want := range []struct {
		color color.RGBA
		dst   *walk.Image
	}{
		{trashMuted, &b.trash},
		{iconGold, &b.trashHot},
	} {
		bmp, err := walk.NewBitmapFromImage(trashImage(64, want.color))
		if err != nil {
			log.Warn("иконка корзины", "error", err)
			continue
		}
		keep(bmp)
		*want.dst = bmp
	}
	b.icons = map[string]walk.Image{}
	for _, site := range sites.SitesWithIcons() {
		img := siteImage(site)
		if img == nil {
			continue
		}
		bmp, err := walk.NewBitmapFromImage(img)
		if err != nil {
			log.Warn("иконка магазина", "site", site, "error", err)
			continue
		}
		keep(bmp)
		b.icons[string(site)] = bmp
	}
}

// display — узлы i-й строки. Индекс, а не Item: серия лежит рядом со списком.
func (b *board) display(i int) chartSeries {
	if i < 0 || i >= len(b.series) {
		return chartSeries{}
	}
	return b.series[i]
}

func (b *board) rebuildSeries() {
	if cap(b.series) < len(b.items) {
		b.series = make([]chartSeries, len(b.items))
	} else {
		b.series = b.series[:len(b.items)]
	}
	for i := range b.items {
		b.series[i] = chartSeriesOf(b.items[i].Samples, b.distinct)
	}
}

func (b *board) setDistinct(on bool) {
	if b.distinct == on {
		return
	}
	b.distinct = on
	b.rebuildSeries()
	b.tipItem, b.tipNode = -1, -1
	b.syncTip()
	if b.widget != nil {
		b.widget.Invalidate()
	}
}

func (b *board) setItems(next []Item) {
	if sameItems(b.items, next) && len(b.series) == len(next) {
		return
	}
	b.items = next
	b.rebuildSeries()
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

// boardMetrics — размеры строки под текущую плотность экрана. Считаются
// один раз на кадр: их спрашивают и рисование, и попадание мыши.
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
	prices := b.display(idx).prices
	if len(prices) == 0 {
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
	node = hitSample(chart.Width, len(prices), x-chart.X)
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
