//go:build windows

package gui

import (
	"testing"

	"github.com/lxn/walk"
)

func TestTipBoxSizeHugsText(t *testing.T) {
	w, h := tipBoxSize(80, 16, 50, 18, 2, 1)
	if w != 84 || h != 39 {
		t.Fatalf("рамка %dx%d, ждали 84x39 под текст плюс pad", w, h)
	}
	w, h = tipBoxSize(10, 8, 40, 10, 2, 1)
	if w != 44 {
		t.Fatalf("ширина по более длинной строке: %d", w)
	}
}

func TestTipRectStaysInView(t *testing.T) {
	view := walk.Rectangle{X: 0, Y: 0, Width: 200, Height: 100}
	r := tipRect(20, 10, view, 80, 30, 5)
	if r.Y < 2 {
		t.Fatalf("у верхнего края должен уйти вниз: %+v", r)
	}
	if r.Width != 80 || r.Height != 30 {
		t.Fatalf("контейнер меняет размер: %+v", r)
	}
	r = tipRect(190, 90, view, 80, 30, 5)
	if r.X+r.Width > view.Width-2 {
		t.Fatalf("вылез справа: %+v", r)
	}
	if r.Y+r.Height > view.Height-2 {
		t.Fatalf("вылез снизу: %+v", r)
	}
}

func TestTipNeeded(t *testing.T) {
	var b board
	if b.tipNeeded() {
		t.Fatal("пустой список")
	}
	b.items = []Item{{Samples: []Sample{{Price: 100, When: "01.01.26 10:00"}}}}
	b.tipItem, b.tipNode = 0, 0
	if !b.tipNeeded() {
		t.Fatal("есть замер")
	}
	b.tipNode = 1
	if b.tipNeeded() {
		t.Fatal("узел за пределами")
	}
	b.tipItem, b.tipNode = -1, -1
	b.syncTip()
}

func TestHideTipNil(t *testing.T) {
	var b board
	b.hideTip()
	b.syncTip()
	b.disposeTip()
}

func TestDisposeMeasureNil(t *testing.T) {
	var b board
	b.disposeMeasure()
	b.disposeMeasure()
}

func TestMeasureLineEmpty(t *testing.T) {
	var b board
	if got := b.measureLine(nil, "1 990 ₽"); got.Width != 0 || got.Height != 0 {
		t.Fatalf("без шрифта: %+v", got)
	}
	if got := b.measureLine(nil, ""); got.Width != 0 {
		t.Fatalf("пустой текст: %+v", got)
	}
}

func TestMeasureLineGDI(t *testing.T) {
	font, err := walk.NewFont("Segoe UI", 8, 0)
	if err != nil {
		t.Fatal(err)
	}
	var b board
	defer b.disposeMeasure()
	a := b.measureLine(font, "12 990 ₽")
	if a.Width < 8 || a.Height < 8 {
		t.Fatalf("размер %+v", a)
	}
	if got := b.measureLine(font, "12 990 ₽"); got != a {
		t.Fatalf("кэш: %+v vs %+v", got, a)
	}
	if len(b.measureHF) != 1 {
		t.Fatalf("шрифтов GDI %d", len(b.measureHF))
	}
	_ = b.measureLine(font, "1 ₽")
	if len(b.measureHF) != 1 {
		t.Fatalf("повторный шрифт: %d", len(b.measureHF))
	}
	if len(b.measureSize) != 2 {
		t.Fatalf("кэш строк %d", len(b.measureSize))
	}
	b.disposeMeasure()
	if b.measureDC != 0 || len(b.measureHF) != 0 || b.measureSize != nil {
		t.Fatal("после Dispose DC и кэш должны быть пусты")
	}
}
