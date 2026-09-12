package tracker

import (
	"testing"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

func TestDecideWaitsForConfirmation(t *testing.T) {
	d := Decide(rows(100), notified(false, 0, true), false)
	if d.Notify || d.Baseline {
		t.Fatalf("первый замер: %+v", d)
	}

	d = Decide(rows(100), notified(false, 0, true), true)
	if !d.Baseline || d.Notify {
		t.Fatalf("база: %+v", d)
	}

	d = Decide(rows(90, 100), notified(true, 100, true), false)
	if d.Notify {
		t.Fatalf("одно новое не должно уведомлять: %+v", d)
	}

	d = Decide(rows(90, 100), notified(true, 100, true), true)
	if !d.Notify || d.Previous.PriceKopecks != 100 || d.Current.PriceKopecks != 90 {
		t.Fatalf("подтверждённое снижение: %+v", d)
	}

	d = Decide(rows(90), notified(true, 90, true), true)
	if d.Notify {
		t.Fatalf("повтор не уведомляет: %+v", d)
	}
}

func TestDecideIgnoresFlapping(t *testing.T) {
	d := Decide(rows(90, 100), notified(true, 100, true), false)
	if d.Notify {
		t.Fatal("флап 100→90 один раз")
	}
	d = Decide(rows(100, 90, 100), notified(true, 100, true), false)
	if d.Notify {
		t.Fatal("флап обратно")
	}
}

func TestDecideConfirmedDropWithoutNotified(t *testing.T) {
	d := Decide(rows(90, 90, 100), notified(false, 0, true), true)
	if !d.Notify || d.Baseline || d.Previous.PriceKopecks != 100 || d.Current.PriceKopecks != 90 {
		t.Fatalf("два новых значения после старой цены: %+v", d)
	}

	d = Decide(rows(90, 90), notified(false, 0, true), true)
	if !d.Baseline || d.Notify {
		t.Fatalf("плато без более старой цены — база: %+v", d)
	}
}

func TestDecideEmptyHistory(t *testing.T) {
	d := Decide(nil, notified(true, 100, true), true)
	if d.Notify || d.Baseline {
		t.Fatalf("пустая история: %+v", d)
	}
}

func TestDecideAvailabilityChange(t *testing.T) {
	cur := []storage.SnapshotRow{{PriceKopecks: 100, Available: false}, {PriceKopecks: 100, Available: true}}
	d := Decide(cur, notified(true, 100, true), false)
	if d.Notify {
		t.Fatal("смена наличия без повтора")
	}
	d = Decide(cur, notified(true, 100, true), true)
	if !d.Notify || d.Current.Available || d.Previous.Available != true {
		t.Fatalf("подтверждённое отсутствие: %+v", d)
	}
}

func rows(prices ...int64) []storage.SnapshotRow {
	out := make([]storage.SnapshotRow, len(prices))
	for i, p := range prices {
		out[i] = storage.SnapshotRow{PriceKopecks: p, Available: true}
	}
	return out
}

func notified(set bool, kopecks int64, available bool) storage.NotifiedState {
	return storage.NotifiedState{Set: set, Kopecks: kopecks, Available: available}
}
