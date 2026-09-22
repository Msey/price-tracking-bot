package tracker

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Msey/price-tracking-bot/internal/fetch"
	"github.com/Msey/price-tracking-bot/internal/storage"
)

func TestDecideFirstPriceIsSilent(t *testing.T) {
	d := Decide(rows(100))
	if !d.Baseline || d.Notify {
		t.Fatalf("первая цена: %+v", d)
	}

	d = Decide(rows(90, 90))
	if d.Notify || d.Baseline {
		t.Fatalf("повтор первой цены: %+v", d)
	}
}

func TestDecideNotifiesOnFirstChange(t *testing.T) {
	d := Decide(rows(90, 100))
	if !d.Notify || d.Previous.PriceKopecks != 100 || d.Current.PriceKopecks != 90 {
		t.Fatalf("снижение: %+v", d)
	}

	d = Decide(rows(90, 90, 100))
	if d.Notify || d.Baseline {
		t.Fatalf("та же цена, что предыдущая: %+v", d)
	}
}

func TestDecideFlappingNotifiesEachChange(t *testing.T) {
	d := Decide(rows(90, 100))
	if !d.Notify {
		t.Fatal("100→90")
	}
	d = Decide(rows(100, 90, 100))
	if !d.Notify || d.Previous.PriceKopecks != 90 || d.Current.PriceKopecks != 100 {
		t.Fatalf("90→100: %+v", d)
	}
}

func TestDecideEmptyHistory(t *testing.T) {
	d := Decide(nil)
	if d.Notify || d.Baseline {
		t.Fatalf("пустая история: %+v", d)
	}
}

func TestDecideAvailabilityChange(t *testing.T) {
	cur := []storage.SnapshotRow{{PriceKopecks: 100, Available: false}, {PriceKopecks: 100, Available: true}}
	d := Decide(cur)
	if !d.Notify || d.Current.Available || !d.Previous.Available {
		t.Fatalf("пропал из наличия: %+v", d)
	}
}

func TestDecidePlaceholderIsNotAPrice(t *testing.T) {
	d := Decide([]storage.SnapshotRow{{PriceKopecks: 0, Available: false}})
	if !d.Baseline || d.Notify {
		t.Fatalf("нет цены: %+v", d)
	}
	d = Decide([]storage.SnapshotRow{
		{PriceKopecks: 100, Available: true},
		{PriceKopecks: 0, Available: false},
	})
	if !d.Baseline || d.Notify {
		t.Fatalf("первая настоящая цена после пустого замера: %+v", d)
	}
}

func TestSuspectDisappearance(t *testing.T) {
	if disappearanceRechecks < 2 {
		t.Fatalf("пропажу нужно перепроверить минимум дважды, сейчас %d", disappearanceRechecks)
	}
	wrapped := fmt.Errorf("карточка: %w", fetch.ErrNoPrice)
	tests := []struct {
		name      string
		prev      bool
		err       error
		available bool
		want      bool
	}{
		{"в наличии, страница живая", true, nil, true, false},
		{"в наличии, карточка сказала нет", true, nil, false, true},
		{"в наличии, цена не найдена", true, fetch.ErrNoPrice, false, true},
		{"обёрнутая цена не найдена", true, wrapped, false, true},
		{"обрыв сети — не пропажа", true, errors.New("connection reset"), false, false},
		{"таймаут — не пропажа", true, context.DeadlineExceeded, false, false},
		{"капча — не пропажа", true, fetch.ErrChallenge, false, false},
		{"уже не было в наличии", false, fetch.ErrNoPrice, false, false},
		{"первой цены ещё не было", false, nil, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := suspectDisappearance(tt.prev, tt.err, tt.available)
			if got != tt.want {
				t.Fatalf("suspect = %v, ожидалось %v", got, tt.want)
			}
		})
	}
}

func rows(prices ...int64) []storage.SnapshotRow {
	out := make([]storage.SnapshotRow, len(prices))
	for i, p := range prices {
		out[i] = storage.SnapshotRow{PriceKopecks: p, Available: true}
	}
	return out
}
