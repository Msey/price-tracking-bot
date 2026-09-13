//go:build windows

package gui

import "github.com/lxn/walk"

// Палитра окна в одном месте: те же цвета нужны и списку, и кнопкам, и
// подсказке над узлом графика.
var (
	colorBg    = walk.RGB(22, 20, 16)
	colorRow   = walk.RGB(33, 28, 22)
	colorHot   = walk.RGB(48, 40, 30)
	colorFrame = walk.RGB(58, 50, 40)
	colorGold  = walk.RGB(226, 182, 87)
	// Для покупателя рост цены — плохо (красный), падение — хорошо (зелёный).
	colorUp    = walk.RGB(232, 86, 74)
	colorDown  = walk.RGB(160, 222, 140)
	colorMiss  = walk.RGB(148, 140, 128)
	colorTitle = walk.RGB(236, 214, 176)
	colorMuted = walk.RGB(154, 141, 122)
)

// theme — шрифты, кисти и перья окна. Все они объекты GDI: живут ровно
// столько, сколько окно, и освобождаются через keep конструктора. Внутри
// одни дескрипторы, поэтому theme передаётся значением.
type theme struct {
	titleFont *walk.Font
	metaFont  *walk.Font
	priceFont *walk.Font

	bg     *walk.SolidColorBrush
	row    *walk.SolidColorBrush
	rowHot *walk.SolidColorBrush
	accent *walk.SolidColorBrush
	// goldHot, goldPress, mutedFill и frame нужны только кнопкам.
	goldHot   *walk.SolidColorBrush
	goldPress *walk.SolidColorBrush
	mutedFill *walk.SolidColorBrush
	frame     *walk.SolidColorBrush
	upBrush   *walk.SolidColorBrush
	downBrush *walk.SolidColorBrush
	missBrush *walk.SolidColorBrush

	goldPen walk.Pen
	upPen   walk.Pen
	downPen walk.Pen
	missPen walk.Pen
	gridPen walk.Pen
}

// newTheme создаёт палитру и отдаёт каждый объект в keep: вызывающий гасит
// их одним списком и на успешном выходе, и на любой ранней ошибке.
func newTheme(keep func(walk.Disposable)) (theme, error) {
	g := gdiPool{keep: keep}
	var t theme
	t.titleFont = g.font(10, walk.FontBold)
	t.metaFont = g.font(8, 0)
	t.priceFont = g.font(11, walk.FontBold)

	t.bg = g.brush(colorBg)
	t.row = g.brush(colorRow)
	t.rowHot = g.brush(colorHot)
	t.accent = g.brush(colorGold)
	t.goldHot = g.brush(walk.RGB(236, 196, 104))
	t.goldPress = g.brush(walk.RGB(196, 154, 64))
	t.mutedFill = g.brush(walk.RGB(90, 76, 52))
	t.frame = g.brush(colorFrame)
	t.upBrush = g.brush(colorUp)
	t.downBrush = g.brush(colorDown)
	t.missBrush = g.brush(colorMiss)

	t.gridPen = g.linePen(colorFrame)
	t.goldPen = g.curvePen(t.accent)
	t.upPen = g.curvePen(t.upBrush)
	t.downPen = g.curvePen(t.downBrush)
	t.missPen = g.curvePen(t.missBrush)
	return t, g.err
}

func (t theme) buttonChrome() *buttonChrome {
	return &buttonChrome{
		font:      t.titleFont,
		row:       t.row,
		rowHot:    t.rowHot,
		accent:    t.accent,
		goldHot:   t.goldHot,
		goldPress: t.goldPress,
		mutedFill: t.mutedFill,
		frame:     t.frame,
	}
}

// gdiPool создаёт объекты GDI и запоминает первую ошибку: после неё все
// вызовы возвращают nil, а проверка остаётся одна на всю палитру.
type gdiPool struct {
	keep func(walk.Disposable)
	err  error
}

func (g *gdiPool) font(size int, style walk.FontStyle) *walk.Font {
	if g.err != nil {
		return nil
	}
	f, err := walk.NewFont("Segoe UI", size, style)
	if err != nil {
		g.err = err
		return nil
	}
	g.keep(f)
	return f
}

func (g *gdiPool) brush(c walk.Color) *walk.SolidColorBrush {
	if g.err != nil {
		return nil
	}
	br, err := walk.NewSolidColorBrush(c)
	if err != nil {
		g.err = err
		return nil
	}
	g.keep(br)
	return br
}

// linePen — тонкая линия сетки: косметическое перо всегда шириной в пиксель.
func (g *gdiPool) linePen(c walk.Color) walk.Pen {
	if g.err != nil {
		return nil
	}
	p, err := walk.NewCosmeticPen(walk.PenSolid, c)
	if err != nil {
		g.err = err
		return nil
	}
	g.keep(p)
	return p
}

// curvePen — кривая графика: круглые концы и стыки, иначе на изломах
// остаются зазубрины.
func (g *gdiPool) curvePen(brush walk.Brush) walk.Pen {
	if g.err != nil || brush == nil {
		return nil
	}
	p, err := walk.NewGeometricPen(walk.PenSolid|walk.PenCapRound|walk.PenJoinRound, 2, brush)
	if err != nil {
		g.err = err
		return nil
	}
	g.keep(p)
	return p
}

// disposeFunc подгоняет под walk.Disposable то, чей Dispose возвращает ошибку.
type disposeFunc func()

func (f disposeFunc) Dispose() { f() }
