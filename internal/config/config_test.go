package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadValidatesTokenAndCity(t *testing.T) {
	t.Setenv("BOT_TOKEN", "12345:ABCDEFGHIJKLMNOPQRST")
	t.Setenv("DATABASE_PATH", "test.db")
	t.Setenv("CHECK_INTERVAL", "20m")
	t.Setenv("DEFAULT_CITY", "moscow")
	t.Setenv("ALLOWED_USERS", "")
	t.Setenv("GUI", "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load вернул ошибку: %v", err)
	}
	if cfg.DefaultCity != "moscow" {
		t.Errorf("city = %q", cfg.DefaultCity)
	}
	if cfg.CheckInterval != 20*time.Minute {
		t.Errorf("interval = %s", cfg.CheckInterval)
	}
	if cfg.UIAddr != "127.0.0.1:8080" {
		t.Errorf("UIAddr = %q", cfg.UIAddr)
	}
	if !cfg.GUI {
		t.Error("GUI по умолчанию должен быть включён")
	}
}

func TestLoadRejectsHTMLCity(t *testing.T) {
	t.Setenv("BOT_TOKEN", "12345:ABCDEFGHIJKLMNOPQRST")
	t.Setenv("DEFAULT_CITY", "<b>msk</b>")
	t.Setenv("CHECK_INTERVAL", "20m")
	t.Setenv("ALLOWED_USERS", "")

	_, err := Load()
	if err == nil {
		t.Fatal("ожидалась ошибка на DEFAULT_CITY с HTML")
	}
	if !strings.Contains(err.Error(), "DEFAULT_CITY") {
		t.Errorf("неожиданная ошибка: %v", err)
	}
}

func TestLoadRejectsMalformedToken(t *testing.T) {
	t.Setenv("BOT_TOKEN", "not-a-token")
	t.Setenv("DEFAULT_CITY", "moscow")
	t.Setenv("CHECK_INTERVAL", "20m")

	_, err := Load()
	if err == nil {
		t.Fatal("ожидалась ошибка на кривой токен")
	}
}

func TestLoadRejectsShortInterval(t *testing.T) {
	t.Setenv("BOT_TOKEN", "12345:ABCDEFGHIJKLMNOPQRST")
	t.Setenv("DEFAULT_CITY", "moscow")
	t.Setenv("CHECK_INTERVAL", "5s")

	_, err := Load()
	if err == nil {
		t.Fatal("ожидалась ошибка на слишком коротком интервале")
	}
}

func TestAllowed(t *testing.T) {
	open := Config{AllowedUsers: map[int64]bool{}}
	if !open.Allowed(1) {
		t.Error("пустой список должен пускать всех")
	}

	closed := Config{AllowedUsers: map[int64]bool{42: true}}
	if !closed.Allowed(42) {
		t.Error("свой id должен проходить")
	}
	if closed.Allowed(1) {
		t.Error("чужой id не должен проходить")
	}
}

func TestLoadDisablesUI(t *testing.T) {
	t.Setenv("BOT_TOKEN", "12345:ABCDEFGHIJKLMNOPQRST")
	t.Setenv("DEFAULT_CITY", "moscow")
	t.Setenv("CHECK_INTERVAL", "20m")
	t.Setenv("UI_ADDR", "off")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.UIAddr != "" {
		t.Errorf("UIAddr = %q, ожидалась пустая строка", cfg.UIAddr)
	}
}

func TestLoadDisablesGUI(t *testing.T) {
	t.Setenv("BOT_TOKEN", "12345:ABCDEFGHIJKLMNOPQRST")
	t.Setenv("DEFAULT_CITY", "moscow")
	t.Setenv("CHECK_INTERVAL", "20m")
	t.Setenv("GUI", "off")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GUI {
		t.Error("GUI должен быть выключен")
	}
}
