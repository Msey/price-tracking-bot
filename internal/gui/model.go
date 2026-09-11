package gui

import (
	"context"

	"github.com/Msey/price-tracking-bot/internal/money"
	"github.com/Msey/price-tracking-bot/internal/sites"
	"github.com/Msey/price-tracking-bot/internal/storage"
	"github.com/Msey/price-tracking-bot/internal/view"
)

const historyPoints = 90

// Item — одна строка списка: ссылка на товар и её график цены.
type Item struct {
	ProductID int64
	Title     string
	URL       string
	SiteKey   string
	Site      string
	City      string
	Price     string
	Status    string
	Checked   string
	Watchers  int
	Points    []int64
	Samples   []Sample
}

// Sample — один замер цены на графике.
type Sample struct {
	Price int64
	When  string
}

// sameItems — список не изменился и перерисовывать нечего. Сравниваются
// поля напрямую: строить строку-отпечаток на каждый опрос значило бы
// впустую собирать и выбрасывать сотни килобайт каждые четыре секунды.
func sameItems(a, b []Item) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].same(b[i]) {
			return false
		}
	}
	return true
}

func (it Item) same(other Item) bool {
	if it.ProductID != other.ProductID ||
		it.Title != other.Title ||
		it.Price != other.Price ||
		it.Status != other.Status ||
		it.Checked != other.Checked ||
		it.Watchers != other.Watchers ||
		len(it.Samples) != len(other.Samples) {
		return false
	}
	for i := range it.Samples {
		if it.Samples[i] != other.Samples[i] {
			return false
		}
	}
	return true
}

func loadItems(ctx context.Context, store *storage.Store) ([]Item, error) {
	reqs, err := store.ListAllRequests(ctx)
	if err != nil {
		return nil, err
	}
	grouped := groupRequests(reqs)
	ids := make([]int64, 0, len(grouped))
	for _, it := range grouped {
		ids = append(ids, it.ProductID)
	}
	hist, err := store.Histories(ctx, ids, historyPoints)
	if err != nil {
		return nil, err
	}
	for i := range grouped {
		rows := hist[grouped[i].ProductID]
		pts := make([]int64, 0, len(rows))
		samples := make([]Sample, 0, len(rows))
		for _, row := range rows {
			pts = append(pts, row.PriceKopecks)
			samples = append(samples, Sample{Price: row.PriceKopecks, When: view.FormatWhenShort(row.CheckedAt)})
		}
		grouped[i].Points = pts
		grouped[i].Samples = samples
	}
	return grouped, nil
}

func groupRequests(reqs []storage.Request) []Item {
	order := make([]int64, 0, len(reqs))
	byID := map[int64]*Item{}
	for _, req := range reqs {
		id := req.Product.ID
		if cur, ok := byID[id]; ok {
			cur.Watchers++
			continue
		}
		it := itemFromRequest(req)
		it.Watchers = 1
		byID[id] = &it
		order = append(order, id)
	}
	out := make([]Item, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out
}

func itemFromRequest(req storage.Request) Item {
	status, _ := view.Status(req)
	price := "—"
	if req.LastPriceKopecks.Valid {
		price = money.FormatKopecks(req.LastPriceKopecks.Int64)
	}
	checked := "ещё не проверяли"
	if req.LastCheckedAt.Valid && req.LastCheckedAt.String != "" {
		checked = view.FormatWhen(req.LastCheckedAt.String)
	}
	return Item{
		ProductID: req.Product.ID,
		Title:     req.Product.Title(),
		URL:       req.Product.URL,
		SiteKey:   req.Product.Site,
		Site:      sites.Site(req.Product.Site).Title(),
		City:      view.CityTitle(req.Product.City),
		Price:     price,
		Status:    status,
		Checked:   checked,
	}
}

func newProducts(prev, next []Item) []Item {
	seen := map[int64]struct{}{}
	for _, it := range prev {
		seen[it.ProductID] = struct{}{}
	}
	var added []Item
	for _, it := range next {
		if _, ok := seen[it.ProductID]; !ok {
			added = append(added, it)
		}
	}
	return added
}

func priceChanges(prev, next []Item) []string {
	old := map[int64]Item{}
	for _, it := range prev {
		old[it.ProductID] = it
	}
	var out []string
	for _, it := range next {
		was, ok := old[it.ProductID]
		if !ok || was.Price == it.Price || it.Price == "—" || was.Price == "—" {
			continue
		}
		out = append(out, it.Title+": "+was.Price+" → "+it.Price)
	}
	return out
}
