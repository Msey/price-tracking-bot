package gui

import (
	"context"
	"database/sql"
	"image"
	"path/filepath"
	"testing"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

func TestLoadItemsShowsAllUsers(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "gui.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	if _, _, err := store.AddSubscription(ctx, 1001, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AddSubscription(ctx, 1002, "ozon", "2190214590", "https://www.ozon.ru/product/2190214590", "moscow"); err != nil {
		t.Fatal(err)
	}

	got, err := loadItems(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("строк %d, ожидалось 2 — окно видит все заявки", len(got))
	}
}

func TestGroupRequestsDedupsProduct(t *testing.T) {
	p := storage.Product{ID: 7, Site: "dns", Name: "Honor", URL: "https://www.dns-shop.ru/product/aa/", City: "moscow"}
	req := storage.Request{
		Product:          p,
		LastPriceKopecks: sql.NullInt64{Int64: 15999900, Valid: true},
		LastCheckedAt:    sql.NullString{String: "2026-01-02 03:04:05", Valid: true},
		LastAvailable:    sql.NullInt64{Int64: 1, Valid: true},
	}
	got := groupRequests([]storage.Request{req, req})
	if len(got) != 1 {
		t.Fatalf("строк %d, ожидалась 1", len(got))
	}
	if got[0].Watchers != 2 {
		t.Errorf("watchers = %d", got[0].Watchers)
	}
	if got[0].Title != "Honor" || got[0].SiteKey != "dns" || got[0].Site != "DNS" || got[0].City != "Москва" {
		t.Errorf("карточка: %+v", got[0])
	}
	if got[0].Price == "—" {
		t.Error("цена должна быть заполнена")
	}
}

func TestNewProductsAndPriceChanges(t *testing.T) {
	prev := []Item{{ProductID: 1, Title: "A", Price: "10\u00a0₽"}}
	next := []Item{
		{ProductID: 1, Title: "A", Price: "9\u00a0₽"},
		{ProductID: 2, Title: "B", Price: "1\u00a0₽"},
	}
	added := newProducts(prev, next)
	if len(added) != 1 || added[0].ProductID != 2 {
		t.Fatalf("новые: %+v", added)
	}
	chg := priceChanges(prev, next)
	if len(chg) != 1 {
		t.Fatalf("изменения: %v", chg)
	}
}

func TestSameItems(t *testing.T) {
	a := []Item{{ProductID: 1, Title: "A", Price: "1", Samples: []Sample{{Price: 100, When: "01.01.26 10:00"}}}}
	b := []Item{{ProductID: 1, Title: "A", Price: "1", Samples: []Sample{{Price: 100, When: "01.01.26 10:00"}}}}
	if !sameItems(a, b) {
		t.Fatal("одинаковые списки должны совпасть")
	}
	if sameItems(a, []Item{{ProductID: 1, Title: "A", Price: "2", Samples: a[0].Samples}}) {
		t.Error("смена цены должна давать различие")
	}
	if sameItems(a, []Item{{ProductID: 1, Title: "A", Price: "1", Samples: []Sample{{Price: 100, When: "02.01.26 10:00"}}}}) {
		t.Error("новый замер должен давать различие")
	}
	if sameItems(a, nil) {
		t.Error("разная длина — различие")
	}
}

func TestSparkline(t *testing.T) {
	if sparkline(0, 10, []int64{1}) != nil {
		t.Fatal("нулевая ширина")
	}
	pts := sparkline(90, 40, []int64{100, 200, 150})
	if len(pts) != 3 {
		t.Fatalf("точек %d", len(pts))
	}
	if pts[0].X != 15 || pts[1].X != 45 || pts[2].X != 75 {
		t.Errorf("три узла должны стоять на третях: %v", pts)
	}
	if pts[1].Y >= pts[0].Y {
		t.Errorf("пик должен быть выше: %v", pts)
	}
	flat := sparkline(20, 10, []int64{5, 5, 5})
	if len(flat) != 3 || flat[0].Y != flat[2].Y {
		t.Errorf("плоский: %v", flat)
	}
}

func TestNodeHalfWidth(t *testing.T) {
	// r=2: без однопиксельной «антенны» сверху и снизу.
	if nodeHalfWidth(2, -2) != 1 || nodeHalfWidth(2, 0) != 2 || nodeHalfWidth(2, 2) != 1 {
		t.Fatalf("r=2: %d %d %d", nodeHalfWidth(2, -2), nodeHalfWidth(2, 0), nodeHalfWidth(2, 2))
	}
	if nodeHalfWidth(1, -1) != 1 || nodeHalfWidth(1, 0) != 1 {
		t.Fatal("r=1 остаётся крестом 3×3")
	}
	if nodeHalfWidth(0, 0) != 0 || nodeHalfWidth(2, 3) != 0 {
		t.Fatal("вне радиуса")
	}
}

func TestSinglePriceSpan(t *testing.T) {
	left, right := singlePriceSpan(200, point{X: 100, Y: 12})
	if left != (point{X: 0, Y: 12}) || right != (point{X: 200, Y: 12}) {
		t.Fatalf("линия %v — %v", left, right)
	}
	left, right = singlePriceSpan(0, point{X: 5, Y: 3})
	if left != (point{X: 5, Y: 3}) || right != (point{X: 5, Y: 3}) {
		t.Fatal("нулевая ширина не должна сдвигать точку")
	}
}

func TestSparklineIntoReusesBuffer(t *testing.T) {
	buf := make([]point, 0, 8)
	got := sparklineInto(buf, 90, 40, []int64{100, 200, 150})
	if cap(got) != 8 || len(got) != 3 {
		t.Fatalf("len=%d cap=%d", len(got), cap(got))
	}
	if &got[0] != &buf[:1][0] {
		t.Fatal("должен писать в переданный буфер")
	}
	empty := sparklineInto(got, 0, 40, []int64{1})
	if empty == nil || len(empty) != 0 || cap(empty) != 8 {
		t.Fatalf("пустой вход не должен терять ёмкость: len=%d cap=%d nil=%v", len(empty), cap(empty), empty == nil)
	}
}

func TestNodeSlots(t *testing.T) {
	if nodeX(100, 3, 0) != 16 || nodeX(100, 3, 1) != 50 || nodeX(100, 3, 2) != 83 {
		t.Fatalf("трети: %d %d %d", nodeX(100, 3, 0), nodeX(100, 3, 1), nodeX(100, 3, 2))
	}
	if nodeX(100, 10, 0) != 5 || nodeX(100, 10, 9) != 95 {
		t.Fatalf("десятые: %d %d", nodeX(100, 10, 0), nodeX(100, 10, 9))
	}
	if hitSample(90, 3, 0) != 0 || hitSample(90, 3, 30) != 1 || hitSample(90, 3, 89) != 2 {
		t.Fatalf("попадание: %d %d %d", hitSample(90, 3, 0), hitSample(90, 3, 30), hitSample(90, 3, 89))
	}
}

func TestPriceMove(t *testing.T) {
	if priceMove(100, 90) != -1 {
		t.Fatal("падение")
	}
	if priceMove(90, 120) != 1 {
		t.Fatal("рост")
	}
	if priceMove(50, 50) != 0 {
		t.Fatal("без изменения")
	}
}

func TestFirstPriceLabel(t *testing.T) {
	prices := []int64{100, 100, 100, 90, 90, 120}
	want := []bool{true, false, false, true, false, true}
	for i, w := range want {
		if got := firstPriceLabel(prices, i); got != w {
			t.Errorf("i=%d: %v, нужно %v", i, got, w)
		}
	}
	if firstPriceLabel(nil, 0) || firstPriceLabel(prices, -1) || firstPriceLabel(prices, 99) {
		t.Fatal("за пределами среза не должно быть подписи")
	}
	if !firstPriceLabel([]int64{7}, 0) {
		t.Fatal("единственный узел подписывается")
	}
}

func TestTrashLayoutLeavesGap(t *testing.T) {
	chartW, trashX := trashLayout(400, 16, 8, 28)
	if trashX != 400-16-28 {
		t.Fatalf("урна X=%d", trashX)
	}
	if chartW != trashX-16-8 {
		t.Fatalf("график W=%d", chartW)
	}
	if 16+chartW+8 > trashX {
		t.Fatal("график наезжает на урну")
	}
}

func TestTrashImageOutline(t *testing.T) {
	img, ok := trashImage(64, trashMuted).(*image.RGBA)
	if !ok {
		t.Fatal("ожидался RGBA")
	}
	var painted int
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			c := img.RGBAAt(x, y)
			if c.A > 0 {
				painted++
			}
		}
	}
	if img.RGBAAt(0, 0).A != 0 {
		t.Fatal("угол должен быть прозрачным")
	}
	if painted < 80 {
		t.Fatalf("контур слишком пустой: %d пикселей", painted)
	}
}
