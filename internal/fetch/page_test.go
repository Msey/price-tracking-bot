package fetch

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestWaitForBitsTakesParsedPrice(t *testing.T) {
	ch := make(chan pageBits, 1)
	ch <- pageBits{CSSPrice: "1 990 ₽", Name: "Герметик"}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	snap, err := waitForBits(ctx, time.Second, ch, parseVisiblePriceBits, nil)
	if err != nil {
		t.Fatalf("waitForBits: %v", err)
	}
	if snap.PriceKopecks != 199000 {
		t.Fatalf("цена %d", snap.PriceKopecks)
	}
}

func TestWaitForBitsTimeoutNoPrice(t *testing.T) {
	ch := make(chan pageBits)
	ctx := context.Background()
	_, err := waitForBits(ctx, 20*time.Millisecond, ch, parseVisiblePriceBits, nil)
	if !errors.Is(err, ErrNoPrice) {
		t.Fatalf("ожидался ErrNoPrice, получено %v", err)
	}
}

func TestLooksLikeHTTPBan(t *testing.T) {
	err := fmt.Errorf("%w (%s)", ErrNoPrice, "403 Error")
	if !looksLikeHTTPBan(err) {
		t.Fatal("403 в title должен гасить DNS")
	}
	if looksLikeHTTPBan(ErrNoPrice) {
		t.Fatal("обычная «цена не найдена» — не бан")
	}
}

func TestWaitForBits403StopsImmediately(t *testing.T) {
	ch := make(chan pageBits, 1)
	ch <- pageBits{QRATOR: true, Title: "HTTP 403"}
	start := time.Now()
	_, err := waitForBits(context.Background(), time.Second, ch, parseBits, nil)
	if !errors.Is(err, ErrChallenge) {
		t.Fatalf("ожидался ErrChallenge, получено %v", err)
	}
	if time.Since(start) > 300*time.Millisecond {
		t.Fatal("403 не должен ждать всю паузу")
	}
}

func TestWaitForBitsQRATORScriptWaitsForPrice(t *testing.T) {
	ch := make(chan pageBits, 2)
	ch <- pageBits{QRATOR: true, Title: "DNS"}
	go func() {
		time.Sleep(40 * time.Millisecond)
		ch <- pageBits{CSSPrice: "2 799 ₽", Title: "Xiaomi"}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	snap, err := waitForBits(ctx, time.Second, ch, parseBits, nil)
	if err != nil {
		t.Fatalf("waitForBits: %v", err)
	}
	if snap.PriceKopecks != 279900 {
		t.Fatalf("цена %d", snap.PriceKopecks)
	}
}

func TestWaitForBitsManySnapshotsReuseTimer(t *testing.T) {
	ch := make(chan pageBits, 64)
	for i := 0; i < 50; i++ {
		ch <- pageBits{Title: "ещё грузится"}
	}
	ch <- pageBits{CSSPrice: "100 ₽", Title: "Товар"}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	snap, err := waitForBits(ctx, time.Second, ch, parseBits, nil)
	if err != nil {
		t.Fatalf("waitForBits: %v", err)
	}
	if snap.PriceKopecks != 10000 {
		t.Fatalf("цена %d", snap.PriceKopecks)
	}
}

func TestResetTimer(t *testing.T) {
	tm := time.NewTimer(time.Hour)
	resetTimer(tm, time.Millisecond)
	select {
	case <-tm.C:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("таймер не сбросился")
	}
	resetTimer(tm, time.Hour)
	if !tm.Stop() {
		t.Fatal("после Reset таймер должен ещё ждать")
	}
}
