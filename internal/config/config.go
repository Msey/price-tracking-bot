// Package config загружает настройки бота из окружения и файла .env.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// ErrNoToken возвращается, когда BOT_TOKEN не задан.
var ErrNoToken = errors.New("config: BOT_TOKEN не задан")

type Config struct {
	BotToken      string
	DatabasePath  string
	CheckInterval time.Duration
	DefaultCity   string
	// AllowedUsers пуст, если доступ открыт всем.
	AllowedUsers map[int64]bool
}

// Load читает .env, если он есть, и собирает конфигурацию.
// Отсутствие .env не ошибка: в production переменные приходят из окружения.
func Load() (Config, error) {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("config: чтение .env: %w", err)
	}

	cfg := Config{
		BotToken:     strings.TrimSpace(os.Getenv("BOT_TOKEN")),
		DatabasePath: envOr("DATABASE_PATH", "bot.db"),
		DefaultCity:  envOr("DEFAULT_CITY", "moscow"),
		AllowedUsers: map[int64]bool{},
	}
	if cfg.BotToken == "" {
		return Config{}, ErrNoToken
	}

	interval, err := time.ParseDuration(envOr("CHECK_INTERVAL", "20m"))
	if err != nil {
		return Config{}, fmt.Errorf("config: CHECK_INTERVAL: %w", err)
	}
	if interval < time.Minute {
		return Config{}, fmt.Errorf("config: CHECK_INTERVAL меньше минуты (%s), это гарантированный бан", interval)
	}
	cfg.CheckInterval = interval

	for _, raw := range strings.Split(os.Getenv("ALLOWED_USERS"), ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("config: ALLOWED_USERS содержит %q: %w", raw, err)
		}
		cfg.AllowedUsers[id] = true
	}

	return cfg, nil
}

// Allowed сообщает, разрешён ли доступ пользователю.
func (c Config) Allowed(userID int64) bool {
	if len(c.AllowedUsers) == 0 {
		return true
	}
	return c.AllowedUsers[userID]
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
