package gui

import (
	"database/sql"
	"image"
	"testing"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

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
