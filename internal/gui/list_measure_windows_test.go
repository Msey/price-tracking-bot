//go:build windows

package gui

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Msey/price-tracking-bot/internal/storage"
	"github.com/lxn/walk"
)

func TestClipStatusFitsHeader(t *testing.T) {
	long := "Проверяю ozon · 1/5 · " + strings.Repeat("ковёр шерстяной ", 20)
	got := clip(long, statusMaxRunes)
	if n := len([]rune(got)); n > statusMaxRunes {
		t.Fatalf("статус %d рун, потолок %d — окно снова раздуется", n, statusMaxRunes)
	}
}

func TestClipBounds(t *testing.T) {
	if clip("abc", 0) != "" || clip("abc", -1) != "" {
		t.Fatal("n<=0 должен давать пустую строку")
	}
	if clip("abc", 3) != "abc" || clip("абв", 3) != "абв" {
		t.Fatal("строка влезает целиком")
	}
	if got := clip("abcd", 3); got != "ab…" {
		t.Fatalf("обрезка ASCII: %q", got)
	}
	if got := clip("абвг", 3); got != "аб…" {
		t.Fatalf("обрезка рун: %q", got)
	}
	if clip("я", 1) != "я" {
		t.Fatal("один символ влезает")
	}
	if clip("аб", 1) != "…" {
		t.Fatal("n=1 и длиннее — только многоточие")
	}
}

func TestPerMonitorDPIContext(t *testing.T) {
	if dpiAwarenessContextPerMonitorV2 != ^uintptr(3) {
		t.Fatalf("PerMonitorV2 должен быть HANDLE(-4), иначе Windows растянет окно")
	}
}

func TestHeadingRectStaysAboveChart(t *testing.T) {
	m := boardMetrics{dpi: 96, pad: 8, titleH: 16, metaH: 12, chartH: 32, gap: 4, rowH: 74, trash: 20, priceW: 140}
	row := m.rowRect(400, 0)
	head := m.headingRect(row)
	chart := m.chartRect(row)
	if head.Y+head.Height > chart.Y {
		t.Fatalf("заголовок наезжает на график: %+v / %+v", head, chart)
	}
}

func TestOpenProductUsesBotChrome(t *testing.T) {
	var opened storage.Product
	a := &app{
		log: slog.Default(),
		openInChrome: func(p storage.Product) {
			opened = p
		},
	}
	a.openProduct(Item{
		URL:     "https://www.ozon.ru/t/WcmKNaP",
		SiteKey: "ozon",
		City:    "Москва",
		CityKey: "moscow",
	})
	if opened.URL != "https://www.ozon.ru/t/WcmKNaP" || opened.Site != "ozon" || opened.City != "moscow" {
		t.Fatalf("в Chrome бота ушло %+v", opened)
	}
	a.openInChrome = nil
	a.openProduct(Item{URL: "https://www.ozon.ru/t/WcmKNaP"})
}

func TestTipDoubleClickOpensItem(t *testing.T) {
	var got Item
	b := &board{onOpen: func(it Item) { got = it }, hover: -1, tipItem: -1, tipNode: -1}
	b.setItems([]Item{{URL: "https://www.ozon.ru/t/WcmKNaP", Title: "TV"}})
	b.tipItem, b.tipNode = 0, 0
	b.onTipMouseDown(walk.LeftButton)
	if got.URL != "" {
		t.Fatal("один клик по тултипу не должен открывать карточку")
	}
	b.tipClick = time.Now().Add(-100 * time.Millisecond)
	b.onTipMouseDown(walk.LeftButton)
	if got.URL != "https://www.ozon.ru/t/WcmKNaP" {
		t.Fatalf("даблклик по тултипу: %+v", got)
	}
}

func TestOpenCurrentUsesHoveredRow(t *testing.T) {
	var got Item
	b := &board{onOpen: func(it Item) { got = it }, hover: 1, tipItem: -1, tipNode: -1}
	b.setItems([]Item{{URL: "https://a"}, {URL: "https://b"}})
	b.openCurrent()
	if got.URL != "https://b" {
		t.Fatalf("без узла должна открыться строка под курсором: %+v", got)
	}
}

func TestHeaderSetStatusDoesNotNeedWidget(t *testing.T) {
	var h *headerBand
	h.setStatus("x")
	h = &headerBand{}
	h.setStatus("Проверяю ozon · 1/5 · " + strings.Repeat("ковёр ", 20))
	if h.status == "" {
		t.Fatal("статус должен остаться в поле, а не уйти в метку walk")
	}
	h.setStatus(h.status)
}

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
	b.setItems([]Item{{Samples: []Sample{{Price: 100, When: "01.01.26 10:00"}}}})
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

func TestTipNeededDistinct(t *testing.T) {
	var b board
	b.distinct = true
	b.setItems([]Item{{Samples: []Sample{
		{Price: 100, When: "a"},
		{Price: 100, When: "b"},
		{Price: 90, When: "c"},
	}}})
	b.tipItem, b.tipNode = 0, 1
	if !b.tipNeeded() {
		t.Fatal("второй distinct-узел есть")
	}
	b.tipNode = 2
	if b.tipNeeded() {
		t.Fatal("плато схлопнуто, третьего узла нет")
	}
	// Тумблер пересобирает серию: плато разворачивается обратно.
	b.setDistinct(false)
	b.tipItem, b.tipNode = 0, 2
	if !b.tipNeeded() {
		t.Fatal("без distinct должны быть все три узла")
	}
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

func TestRebuildSeriesDropsOldRows(t *testing.T) {
	b := &board{}
	b.setItems([]Item{
		{Samples: []Sample{{Price: 1}, {Price: 2}}},
		{Samples: []Sample{{Price: 3}}},
	})
	if len(b.series) != 2 || cap(b.series) < 2 {
		t.Fatalf("len=%d cap=%d", len(b.series), cap(b.series))
	}
	b.setItems([]Item{{Samples: []Sample{{Price: 9}}}})
	if len(b.series) != 1 {
		t.Fatalf("после сжатия len=%d", len(b.series))
	}
	if cap(b.series) > 1 {
		tail := b.series[:cap(b.series)][1]
		if tail.prices != nil || tail.samples != nil {
			t.Fatalf("хвост series должен быть обнулён: %+v", tail)
		}
	}
}

func TestHideToTrayNilWindow(t *testing.T) {
	a := &app{log: slog.Default()}
	a.hideToTray()
}

func TestOpenURLEmpty(t *testing.T) {
	openURL("")
	openURL("   ")
	startDetached("")
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
