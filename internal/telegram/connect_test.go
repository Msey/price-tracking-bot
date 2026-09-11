package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Msey/price-tracking-bot/internal/config"
)

func TestNewDoesNotNeedNetwork(t *testing.T) {
	b := New(config.Config{BotToken: "12345:ABCDEFGHIJKLMNOPQRST"}, nil, nil)
	if b.Username() != "" {
		t.Fatalf("username = %q", b.Username())
	}
	if got := b.StatusText(); got != "Подключаюсь к Telegram…" {
		t.Fatalf("статус до Start: %q", got)
	}
	if err := b.Notify(context.Background(), 1, "hi"); !errors.Is(err, errOffline) {
		t.Fatalf("Notify без связи: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Notify(ctx, 1, "hi"); !errors.Is(err, context.Canceled) {
		t.Fatalf("отменённый контекст: %v", err)
	}
}

func TestStatusTextWhileRetrying(t *testing.T) {
	b := New(config.Config{}, nil, nil)
	b.setRetry(errors.New(`telebot: Post "https://api.telegram.org/bot12345:TESTTOKENVALUE/getMe": timeout`), 8*time.Second)
	got := b.StatusText()
	if !strings.Contains(got, "Нет связи с Telegram") || !strings.Contains(got, "повтор через") {
		t.Fatalf("статус: %q", got)
	}
	if strings.Contains(got, "12345") || strings.Contains(got, "TESTTOKENVALUE") {
		t.Fatalf("в статусе не должно быть токена: %q", got)
	}
}

func TestStartReturnsWhenContextDone(t *testing.T) {
	b := New(config.Config{BotToken: "12345:ABCDEFGHIJKLMNOPQRST"}, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	b.Start(ctx)
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("Start не должен ходить в сеть, если контекст уже отменён")
	}
}

func TestRedactTelegram(t *testing.T) {
	in := `telebot: Post "https://api.telegram.org/bot12345:TESTTOKENVALUE/getMe": timeout`
	got := redactTelegram(in)
	if strings.Contains(got, "TESTTOKENVALUE") || strings.Contains(got, "12345") {
		t.Fatalf("токен не убран: %q", got)
	}
	if !strings.Contains(got, "bot***") {
		t.Fatalf("ожидалась замена: %q", got)
	}
}

func TestFormatRetry(t *testing.T) {
	if got := formatRetry(5 * time.Second); got != "5 с" {
		t.Fatalf("5 с: %q", got)
	}
	if got := formatRetry(65 * time.Second); got != "1 мин 5 с" {
		t.Fatalf("1 мин 5 с: %q", got)
	}
}
