//go:build windows

package gui

import (
	"syscall"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

type textSizeKey struct {
	font *walk.Font
	text string
}

func (b *board) measureLine(font *walk.Font, text string) walk.Rectangle {
	if font == nil || text == "" {
		return walk.Rectangle{}
	}
	if err := b.ensureMeasure(b.dpi()); err != nil || b.measureDC == 0 {
		return walk.Rectangle{Width: len([]rune(text)) * 8, Height: 16}
	}
	key := textSizeKey{font: font, text: text}
	if sz, ok := b.measureSize[key]; ok {
		return sz
	}
	hFont, err := b.fontHandle(font)
	if err != nil || hFont == 0 {
		return walk.Rectangle{Width: len([]rune(text)) * 8, Height: 16}
	}
	old := win.SelectObject(b.measureDC, win.HGDIOBJ(hFont))
	if old == 0 {
		return walk.Rectangle{Width: len([]rune(text)) * 8, Height: 16}
	}
	defer win.SelectObject(b.measureDC, old)

	utf16, err := syscall.UTF16FromString(text)
	if err != nil || len(utf16) < 2 {
		return walk.Rectangle{Width: len([]rune(text)) * 8, Height: 16}
	}
	var ext win.SIZE
	if !win.GetTextExtentPoint32(b.measureDC, &utf16[0], int32(len(utf16)-1), &ext) {
		return walk.Rectangle{Width: len([]rune(text)) * 8, Height: 16}
	}
	sz := walk.Rectangle{Width: int(ext.CX), Height: int(ext.CY)}
	if sz.Width < 1 {
		sz.Width = 1
	}
	if sz.Height < 1 {
		sz.Height = 1
	}
	if b.measureSize == nil {
		b.measureSize = make(map[textSizeKey]walk.Rectangle)
	}
	b.measureSize[key] = sz
	return sz
}

func (b *board) ensureMeasure(dpi int) error {
	if dpi < 96 {
		dpi = 96
	}
	if b.measureDC != 0 && b.measureDPI == dpi {
		return nil
	}
	b.disposeMeasure()
	hdc := win.CreateCompatibleDC(0)
	if hdc == 0 {
		return syscall.EINVAL
	}
	b.measureDC = hdc
	b.measureDPI = dpi
	return nil
}

func (b *board) fontHandle(font *walk.Font) (win.HFONT, error) {
	if font == nil {
		return 0, syscall.EINVAL
	}
	if b.measureHF == nil {
		b.measureHF = make(map[*walk.Font]win.HFONT)
	}
	if h, ok := b.measureHF[font]; ok {
		return h, nil
	}
	h, err := createFontForDPI(font, b.measureDPI)
	if err != nil {
		return 0, err
	}
	b.measureHF[font] = h
	return h, nil
}

func (b *board) disposeMeasure() {
	for f, h := range b.measureHF {
		if h != 0 {
			win.DeleteObject(win.HGDIOBJ(h))
		}
		delete(b.measureHF, f)
	}
	if b.measureDC != 0 {
		win.DeleteDC(b.measureDC)
		b.measureDC = 0
	}
	b.measureDPI = 0
	b.measureSize = nil
}

func createFontForDPI(font *walk.Font, dpi int) (win.HFONT, error) {
	var lf win.LOGFONT
	lf.LfHeight = -win.MulDiv(int32(font.PointSize()), int32(dpi), 72)
	if font.Bold() {
		lf.LfWeight = win.FW_BOLD
	} else {
		lf.LfWeight = win.FW_NORMAL
	}
	if font.Italic() {
		lf.LfItalic = 1
	}
	if font.Underline() {
		lf.LfUnderline = 1
	}
	if font.StrikeOut() {
		lf.LfStrikeOut = 1
	}
	lf.LfCharSet = win.DEFAULT_CHARSET
	lf.LfOutPrecision = win.OUT_TT_PRECIS
	lf.LfClipPrecision = win.CLIP_DEFAULT_PRECIS
	lf.LfQuality = win.CLEARTYPE_QUALITY
	lf.LfPitchAndFamily = win.VARIABLE_PITCH | win.FF_SWISS
	name, err := syscall.UTF16FromString(font.Family())
	if err != nil {
		return 0, err
	}
	copy(lf.LfFaceName[:], name)
	h := win.CreateFontIndirect(&lf)
	if h == 0 {
		return 0, syscall.EINVAL
	}
	return h, nil
}
