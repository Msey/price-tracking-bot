//go:build windows

package gui

import (
	"sync"
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	ui "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

// Как выглядит кнопка: золотая главная и тёмные вторичные,
// в той же палитре, что строки списка.
type buttonFace int

const (
	facePrimary buttonFace = iota
	facePrimaryHot
	facePrimaryPress
	facePrimaryOff
	faceIdle
	faceHot
	facePress
	faceOff
)

func buttonFaceOf(primary, enabled, hover, pressed bool) buttonFace {
	if primary {
		switch {
		case !enabled:
			return facePrimaryOff
		case pressed:
			return facePrimaryPress
		case hover:
			return facePrimaryHot
		default:
			return facePrimary
		}
	}
	switch {
	case !enabled:
		return faceOff
	case pressed:
		return facePress
	case hover:
		return faceHot
	default:
		return faceIdle
	}
}

func buttonTextColor(face buttonFace) walk.Color {
	switch face {
	case facePrimary, facePrimaryHot, facePrimaryPress:
		return walk.RGB(22, 20, 16)
	case facePrimaryOff, faceOff:
		return walk.RGB(154, 141, 122)
	case facePress:
		return walk.RGB(243, 234, 220)
	default:
		return walk.RGB(226, 182, 87)
	}
}

type buttonChrome struct {
	font      *walk.Font
	row       *walk.SolidColorBrush
	rowHot    *walk.SolidColorBrush
	accent    *walk.SolidColorBrush
	goldHot   *walk.SolidColorBrush
	goldPress *walk.SolidColorBrush
	mutedFill *walk.SolidColorBrush
	frame     *walk.SolidColorBrush
}

func (c *buttonChrome) brush(face buttonFace) *walk.SolidColorBrush {
	if c == nil {
		return nil
	}
	switch face {
	case facePrimary:
		return c.accent
	case facePrimaryHot:
		return c.goldHot
	case facePrimaryPress:
		return c.goldPress
	case facePrimaryOff:
		return c.mutedFill
	case faceHot, facePress:
		return c.rowHot
	default:
		return c.row
	}
}

func (c *buttonChrome) frameBrush(face buttonFace) *walk.SolidColorBrush {
	if c == nil {
		return nil
	}
	switch face {
	case facePrimary, facePrimaryHot, facePrimaryPress, facePrimaryOff:
		return nil
	case faceHot, facePress:
		return c.accent
	default:
		return c.frame
	}
}

// buttonStripe — золотая полоска внутри рамки: 1 px сверху, снизу и слева,
// чтобы заливка не вылезала за контур.
func buttonStripe(w, h, stripe int) (x, y, sw, sh int) {
	if w < 3 || h < 3 || stripe < 1 {
		return 0, 0, 0, 0
	}
	if stripe > w-2 {
		stripe = w - 2
	}
	return 1, 1, stripe, h - 2
}

func strokeRectPixels(canvas *walk.Canvas, brush *walk.SolidColorBrush, r walk.Rectangle) {
	if canvas == nil || brush == nil || r.Width < 1 || r.Height < 1 {
		return
	}
	_ = canvas.FillRectanglePixels(brush, walk.Rectangle{X: r.X, Y: r.Y, Width: r.Width, Height: 1})
	_ = canvas.FillRectanglePixels(brush, walk.Rectangle{X: r.X, Y: r.Y + r.Height - 1, Width: r.Width, Height: 1})
	if r.Height <= 2 {
		return
	}
	_ = canvas.FillRectanglePixels(brush, walk.Rectangle{X: r.X, Y: r.Y + 1, Width: 1, Height: r.Height - 2})
	if r.Width > 1 {
		_ = canvas.FillRectanglePixels(brush, walk.Rectangle{X: r.X + r.Width - 1, Y: r.Y + 1, Width: 1, Height: r.Height - 2})
	}
}

type themeButton struct {
	widget   *walk.CustomWidget
	chrome   *buttonChrome
	text     string
	enabled  bool
	primary  bool
	hover    bool
	pressed  bool
	tracking bool
	prevProc uintptr
	onClick  func()
}

func newThemeButton(text string, primary bool, chrome *buttonChrome, onClick func()) *themeButton {
	return &themeButton{
		chrome:  chrome,
		text:    text,
		enabled: true,
		primary: primary,
		onClick: onClick,
	}
}

const (
	buttonH    = 36
	buttonMaxH = 100
)

func (b *themeButton) decl(minW int) ui.CustomWidget {
	return ui.CustomWidget{
		AssignTo:            &b.widget,
		MinSize:             ui.Size{Width: minW, Height: buttonH},
		MaxSize:             ui.Size{Width: minW, Height: buttonH},
		Alignment:           ui.AlignHNearVCenter,
		Background:          ui.SolidColorBrush{Color: walk.RGB(22, 20, 16)},
		InvalidatesOnResize: true,
		PaintMode:           ui.PaintBuffered,
		PaintPixels:         b.paint,
		OnMouseDown:         b.mouseDown,
		OnMouseUp:           b.mouseUp,
		OnMouseMove:         b.mouseMove,
	}
}

// cell сажает кнопку в столбец: CustomWidget у walk жадный по высоте,
// и в горизонтальном ряду MaxSize не удерживает её. Внутренний VBox
// режет высоту, снаружи шапка не выше buttonMaxH.
func (b *themeButton) cell(minW int) ui.Composite {
	return ui.Composite{
		Background: ui.SolidColorBrush{Color: walk.RGB(22, 20, 16)},
		MinSize:    ui.Size{Width: minW, Height: buttonH},
		MaxSize:    ui.Size{Width: minW, Height: buttonMaxH},
		Alignment:  ui.AlignHNearVCenter,
		Layout:     ui.VBox{MarginsZero: true, Alignment: ui.AlignHCenterVCenter},
		Children:   []ui.Widget{b.decl(minW)},
	}
}

func (b *themeButton) attach() {
	if b == nil || b.widget == nil || b.prevProc != 0 {
		return
	}
	hwnd := b.widget.Handle()
	if hwnd == 0 {
		return
	}
	b.widget.SetCursor(walk.CursorHand())
	themedButtons.Store(hwnd, b)
	if prev := win.SetWindowLongPtr(hwnd, win.GWLP_WNDPROC, themedButtonProcCB); prev != 0 {
		b.prevProc = prev
	}
}

func (b *themeButton) SetText(s string) error {
	if b == nil || b.text == s {
		return nil
	}
	b.text = s
	b.invalidate()
	return nil
}

func (b *themeButton) SetEnabled(v bool) {
	if b == nil {
		return
	}
	if b.enabled != v {
		b.enabled = v
		if !v {
			b.hover = false
			b.pressed = false
		}
		b.invalidate()
	}
	if b.widget != nil {
		b.widget.SetEnabled(v)
	}
}

func (b *themeButton) SetVisible(v bool) {
	if b == nil || b.widget == nil {
		return
	}
	b.widget.SetVisible(v)
}

func (b *themeButton) paint(canvas *walk.Canvas, _ walk.Rectangle) error {
	if b == nil || b.widget == nil {
		return nil
	}
	bounds := b.widget.ClientBoundsPixels()
	face := buttonFaceOf(b.primary, b.enabled, b.hover, b.pressed)
	if fill := b.chrome.brush(face); fill != nil {
		_ = canvas.FillRectanglePixels(fill, bounds)
	}
	if !b.primary && b.chrome != nil && b.chrome.accent != nil {
		dpi := b.widget.DPI()
		sx, sy, sw, sh := buttonStripe(bounds.Width, bounds.Height, walk.IntFrom96DPI(3, dpi))
		if sw > 0 && sh > 0 {
			_ = canvas.FillRectanglePixels(b.chrome.accent, walk.Rectangle{
				X: bounds.X + sx, Y: bounds.Y + sy, Width: sw, Height: sh,
			})
		}
	}
	if b.chrome != nil {
		strokeRectPixels(canvas, b.chrome.frameBrush(face), bounds)
	}
	if b.chrome == nil || b.chrome.font == nil || b.text == "" {
		return nil
	}
	dpi := b.widget.DPI()
	pad := walk.IntFrom96DPI(8, dpi)
	box := walk.Rectangle{
		X: bounds.X + pad, Y: bounds.Y,
		Width: bounds.Width - pad*2, Height: bounds.Height,
	}
	if !b.primary {
		stripe := walk.IntFrom96DPI(3, dpi)
		box.X += stripe
		box.Width -= stripe
	}
	if box.Width < 1 {
		return nil
	}
	_ = canvas.DrawTextPixels(b.text, b.chrome.font, buttonTextColor(face), box,
		walk.TextCenter|walk.TextVCenter|walk.TextSingleLine|walk.TextEndEllipsis|walk.TextNoPrefix)
	return nil
}

func (b *themeButton) mouseDown(x, y int, button walk.MouseButton) {
	if b == nil || !b.enabled || button != walk.LeftButton || b.widget == nil {
		return
	}
	b.pressed = true
	win.SetCapture(b.widget.Handle())
	b.invalidate()
}

func (b *themeButton) mouseUp(x, y int, button walk.MouseButton) {
	if b == nil || button != walk.LeftButton {
		return
	}
	win.ReleaseCapture()
	was := b.pressed
	b.pressed = false
	inside := b.contains(x, y)
	b.hover = inside
	b.invalidate()
	if was && inside && b.enabled && b.onClick != nil {
		b.onClick()
	}
}

func (b *themeButton) mouseMove(x, y int, _ walk.MouseButton) {
	if b == nil || !b.enabled {
		return
	}
	over := b.contains(x, y)
	if b.hover != over {
		b.hover = over
		b.invalidate()
	}
	b.trackLeave()
}

func (b *themeButton) contains(x, y int) bool {
	if b == nil || b.widget == nil {
		return false
	}
	r := b.widget.ClientBoundsPixels()
	return x >= 0 && y >= 0 && x < r.Width && y < r.Height
}

func (b *themeButton) trackLeave() {
	if b == nil || b.tracking || b.widget == nil {
		return
	}
	var tme win.TRACKMOUSEEVENT
	tme.CbSize = uint32(unsafe.Sizeof(tme))
	tme.DwFlags = win.TME_LEAVE
	tme.HwndTrack = b.widget.Handle()
	b.tracking = win.TrackMouseEvent(&tme)
}

func (b *themeButton) onLeave() {
	if b == nil {
		return
	}
	b.tracking = false
	if !b.hover {
		return
	}
	b.hover = false
	b.invalidate()
}

func (b *themeButton) invalidate() {
	if b != nil && b.widget != nil {
		b.widget.Invalidate()
	}
}

var (
	themedButtons      sync.Map
	themedButtonProcCB = syscall.NewCallback(themedButtonProc)
)

func themedButtonProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	v, ok := themedButtons.Load(hwnd)
	if !ok {
		return win.DefWindowProc(hwnd, msg, wParam, lParam)
	}
	b := v.(*themeButton)
	switch msg {
	case win.WM_MOUSELEAVE:
		b.onLeave()
	case win.WM_NCDESTROY:
		themedButtons.Delete(hwnd)
	}
	if b.prevProc == 0 {
		return win.DefWindowProc(hwnd, msg, wParam, lParam)
	}
	return win.CallWindowProc(b.prevProc, hwnd, msg, wParam, lParam)
}
