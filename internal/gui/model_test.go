package gui

import (
	"database/sql"
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

func TestSameOrderAndChangedIndexes(t *testing.T) {
	a := []Item{{ProductID: 1, Title: "A", Price: "1"}, {ProductID: 2, Title: "B", Price: "2"}}
	b := []Item{{ProductID: 1, Title: "A", Price: "1"}, {ProductID: 2, Title: "B", Price: "3"}}
	if !sameProductOrder(a, b) {
		t.Fatal("порядок тот же")
	}
	got := changedIndexes(a, b)
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("changed = %v", got)
	}
}

func TestSparkline(t *testing.T) {
	if sparkline(0, 10, []int64{1}) != nil {
		t.Fatal("нулевая ширина")
	}
	pts := sparkline(100, 40, []int64{100, 200, 150})
	if len(pts) != 3 {
		t.Fatalf("точек %d", len(pts))
	}
	if pts[0].X != 0 || pts[2].X != 99 {
		t.Errorf("x: %v", pts)
	}
	if pts[1].Y >= pts[0].Y {
		t.Errorf("пик должен быть выше: %v", pts)
	}
	flat := sparkline(20, 10, []int64{5, 5, 5})
	if len(flat) != 3 || flat[0].Y != flat[2].Y {
		t.Errorf("плоский: %v", flat)
	}
}
