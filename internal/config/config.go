// Package config загружает настройки бота из окружения и файла .env.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// ErrNoToken возвращается, когда BOT_TOKEN не задан.
var ErrNoToken = errors.New("config: BOT_TOKEN не задан")

var (
	tokenShape = regexp.MustCompile(`^\d{5,}:[A-Za-z0-9_-]{20,}$`)
	cityShape  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
)

type Config struct {
	BotToken        string
	DatabasePath    string
	CheckInterval   time.Duration // устаревший общий интервал; расписание магазинов в sites.CheckInterval
	FetchGap        time.Duration
	FetchPerCycle   int
	StartupDelay    time.Duration
	CircuitCooldown time.Duration
	ChromeProfile   string
	ChromePath      string
	ChromeHeadless  bool
	DefaultCity     string
	// AllowedUsers пуст, если доступ открыт всем.
	AllowedUsers map[int64]bool
	// UIAddr — адрес локальной страницы со всеми заявками.
	// Пусто или "off" — не поднимать HTTP.
	UIAddr string
	// GUI — нативное окно со списком ссылок и графиками. Выключается GUI=off.
	GUI bool
}

// Load читает .env, если он есть, и собирает конфигурацию.
// Отсутствие .env не ошибка: в production переменные приходят из окружения.
func Load() (Config, error) {
	loadDotEnv()

	cfg := Config{
		BotToken:     strings.TrimSpace(os.Getenv("BOT_TOKEN")),
		DatabasePath: envOr("DATABASE_PATH", "bot.db"),
		DefaultCity:  strings.ToLower(envOr("DEFAULT_CITY", "moscow")),
		AllowedUsers: map[int64]bool{},
	}
	if cfg.BotToken == "" {
		return Config{}, ErrNoToken
	}
	if !tokenShape.MatchString(cfg.BotToken) {
		return Config{}, errors.New("config: BOT_TOKEN не похож на токен Telegram")
	}

	interval, err := time.ParseDuration(envOr("CHECK_INTERVAL", "20m"))
	if err != nil {
		return Config{}, fmt.Errorf("config: CHECK_INTERVAL: %w", err)
	}
	if interval < 10*time.Minute {
		return Config{}, fmt.Errorf("config: CHECK_INTERVAL меньше 10 минут (%s): DNS банит за частые запросы", interval)
	}
	cfg.CheckInterval = interval

	gap, err := time.ParseDuration(envOr("FETCH_GAP", "45s"))
	if err != nil {
		return Config{}, fmt.Errorf("config: FETCH_GAP: %w", err)
	}
	if gap < 30*time.Second {
		return Config{}, fmt.Errorf("config: FETCH_GAP меньше 30 секунд (%s)", gap)
	}
	cfg.FetchGap = gap

	perCycle, err := strconv.Atoi(envOr("FETCH_PER_CYCLE", "8"))
	if err != nil || perCycle < 1 || perCycle > 20 {
		return Config{}, fmt.Errorf("config: FETCH_PER_CYCLE должен быть от 1 до 20")
	}
	cfg.FetchPerCycle = perCycle

	startup, err := time.ParseDuration(envOr("STARTUP_DELAY", "1m"))
	if err != nil {
		return Config{}, fmt.Errorf("config: STARTUP_DELAY: %w", err)
	}
	if startup < 30*time.Second {
		startup = 30 * time.Second
	}
	cfg.StartupDelay = startup

	cooldown, err := time.ParseDuration(envOr("CIRCUIT_COOLDOWN", "45m"))
	if err != nil {
		return Config{}, fmt.Errorf("config: CIRCUIT_COOLDOWN: %w", err)
	}
	if cooldown < 15*time.Minute {
		return Config{}, fmt.Errorf("config: CIRCUIT_COOLDOWN меньше 15 минут (%s)", cooldown)
	}
	cfg.CircuitCooldown = cooldown

	cfg.ChromeProfile = envOr("CHROME_PROFILE", "data/chrome-plain")
	cfg.ChromePath = os.Getenv("CHROME_PATH")
	cfg.ChromeHeadless = envOr("CHROME_HEADLESS", "0") == "1"
	cfg.UIAddr = strings.TrimSpace(envOr("UI_ADDR", "127.0.0.1:8080"))
	if strings.EqualFold(cfg.UIAddr, "off") || cfg.UIAddr == "-" {
		cfg.UIAddr = ""
	}
	cfg.GUI = parseOnOff(envOr("GUI", "1"))

	if !cityShape.MatchString(cfg.DefaultCity) {
		return Config{}, fmt.Errorf("config: DEFAULT_CITY %q: только латиница, цифры, дефис и подчёркивание", cfg.DefaultCity)
	}

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

func loadDotEnv() {
	// Сначала cwd — удобно при разработке. Потом каталог бинарника: при
	// автозапуске Windows рабочая папка часто System32, и .env рядом с exe
	// иначе не находится. Load не перезаписывает уже заданные переменные.
	_ = godotenv.Load()
	if exe, err := os.Executable(); err == nil {
		_ = godotenv.Load(filepath.Join(filepath.Dir(exe), ".env"))
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func parseOnOff(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "0", "off", "false", "-", "no":
		return false
	default:
		return true
	}
}
