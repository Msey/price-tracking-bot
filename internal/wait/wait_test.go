package wait

import (
	"context"
	"testing"
	"time"
)

func TestSleepWaitsOut(t *testing.T) {
	start := time.Now()
	if !Sleep(context.Background(), 20*time.Millisecond) {
		t.Fatal("пауза должна была дождаться конца")
	}
	if time.Since(start) < 15*time.Millisecond {
		t.Fatalf("пауза кончилась слишком быстро: %v", time.Since(start))
	}
}

func TestSleepStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	if Sleep(ctx, 5*time.Second) {
		t.Fatal("после отмены пауза должна вернуть false")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("отмена не прервала паузу: %v", time.Since(start))
	}
}

func TestSleepZeroChecksContext(t *testing.T) {
	if !Sleep(context.Background(), 0) {
		t.Fatal("нулевая пауза с живым контекстом — можно продолжать")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if Sleep(ctx, 0) {
		t.Fatal("нулевая пауза с отменённым контекстом — продолжать нельзя")
	}
}
