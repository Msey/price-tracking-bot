//go:build windows

package gui

import "github.com/lxn/walk"

const headerTitle = "Ссылки и графики цен"

// headerBand рисует заголовок и статус сама. walk.Label при SetText
// пересчитывает минимальную ширину по всей строке, и FormBase раздувает
// HWND — при запуске Chrome это ещё и сжимается как картинка.
type headerBand struct {
	widget *walk.CustomWidget
	title  *walk.Font
	meta   *walk.Font
	bg     *walk.SolidColorBrush
	status string
}

func newHeaderBand(t theme) *headerBand {
	return &headerBand{
		title:  t.headingFont,
		meta:   t.metaFont,
		bg:     t.bg,
		status: "Загрузка…",
	}
}

func (h *headerBand) setStatus(s string) {
	if h == nil || h.status == s {
		return
	}
	h.status = s
	if h.widget != nil {
		h.widget.Invalidate()
	}
}

func (h *headerBand) paint(canvas *walk.Canvas, _ walk.Rectangle) error {
	if h == nil || h.widget == nil {
		return nil
	}
	bounds := h.widget.ClientBoundsPixels()
	if h.bg != nil {
		_ = canvas.FillRectanglePixels(h.bg, bounds)
	}
	dpi := h.widget.DPI()
	if dpi < 96 {
		dpi = 96
	}
	pad := walk.IntFrom96DPI(2, dpi)
	titleH := walk.IntFrom96DPI(22, dpi)
	metaH := walk.IntFrom96DPI(16, dpi)
	flags := walk.TextLeft | walk.TextVCenter | walk.TextEndEllipsis | walk.TextSingleLine | walk.TextNoPrefix
	if h.title != nil {
		_ = canvas.DrawTextPixels(headerTitle, h.title, colorGold, walk.Rectangle{
			X: bounds.X, Y: bounds.Y + pad, Width: bounds.Width, Height: titleH,
		}, flags)
	}
	if h.meta != nil && h.status != "" {
		_ = canvas.DrawTextPixels(h.status, h.meta, colorMuted, walk.Rectangle{
			X: bounds.X, Y: bounds.Y + pad + titleH, Width: bounds.Width, Height: metaH,
		}, flags)
	}
	return nil
}
