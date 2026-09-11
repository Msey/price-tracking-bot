package gui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Msey/price-tracking-bot/internal/money"
	"github.com/Msey/price-tracking-bot/internal/sites"
	"github.com/Msey/price-tracking-bot/internal/storage"
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
	Kind      string
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

func (it Item) fingerprint() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d|%s|%s|%s|%s|%d", it.ProductID, it.Title, it.Price, it.Status, it.Checked, it.Watchers)
	for i, p := range it.Points {
		fmt.Fprintf(&b, "|%d", p)
		if i < len(it.Samples) {
			fmt.Fprintf(&b, "@%s", it.Samples[i].When)
		}
	}
	return b.String()
}

func fingerprints(items []Item) string {
	var b strings.Builder
	for _, it := range items {
		b.WriteString(it.fingerprint())
		b.WriteByte('\n')
	}
	return b.String()
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
			samples = append(samples, Sample{Price: row.PriceKopecks, When: formatWhenFull(row.CheckedAt)})
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
	status, kind := statusOf(req)
	price := "—"
	if req.LastPriceKopecks.Valid {
		price = money.FormatKopecks(req.LastPriceKopecks.Int64)
	}
	checked := "ещё не проверяли"
	if req.LastCheckedAt.Valid && req.LastCheckedAt.String != "" {
		checked = formatWhen(req.LastCheckedAt.String)
	}
	return Item{
		ProductID: req.Product.ID,
		Title:     req.Product.Title(),
		URL:       req.Product.URL,
		SiteKey:   req.Product.Site,
		Site:      sites.Site(req.Product.Site).Title(),
		City:      cityTitle(req.Product.City),
		Price:     price,
		Status:    status,
		Kind:      kind,
		Checked:   checked,
	}
}

func statusOf(req storage.Request) (string, string) {
	if !req.LastCheckedAt.Valid {
		if req.LastErrorKind.Valid && req.LastErrorKind.String != "" {
			return "ошибка загрузки", "bad"
		}
		return "ожидает проверку", "wait"
	}
	if req.LastErrorAt.Valid && req.LastErrorAt.String > req.LastCheckedAt.String {
		return "ошибка после проверки", "bad"
	}
	if req.LastAvailable.Valid && req.LastAvailable.Int64 == 0 {
		return "нет в наличии", "bad"
	}
	return "отслеживается", "ok"
}

func cityTitle(city string) string {
	switch strings.ToLower(strings.TrimSpace(city)) {
	case "moscow":
		return "Москва"
	default:
		return city
	}
}

func formatWhen(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "—"
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", raw, time.UTC)
	if err != nil {
		return raw
	}
	return t.Local().Format("02.01.2006 15:04")
}

func formatWhenFull(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "—"
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", raw, time.UTC)
	if err != nil {
		return raw
	}
	return t.Local().Format("02.01.06 15:04")
}

func ruPlural(n int, one, few, many string) string {
	if n < 0 {
		n = -n
	}
	mod100 := n % 100
	mod10 := n % 10
	if mod100 >= 11 && mod100 <= 14 {
		return many
	}
	switch mod10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	default:
		return many
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

func sameProductOrder(a, b []Item) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ProductID != b[i].ProductID {
			return false
		}
	}
	return true
}

func changedIndexes(prev, next []Item) []int {
	var out []int
	for i := range next {
		if prev[i].fingerprint() != next[i].fingerprint() {
			out = append(out, i)
		}
	}
	return out
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
