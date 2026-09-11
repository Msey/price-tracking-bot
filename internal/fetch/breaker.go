package fetch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Msey/price-tracking-bot/internal/sites"
)

// Breaker на весь сайт: после челленджа или обрыва TLS не ходим на этот
// магазин, пока не истечёт пауза. Иначе антибот банит IP целиком.
type Breaker struct {
	mu       sync.Mutex
	until    time.Time
	cooldown time.Duration
	reason   string
	path     string
}

type breakerState struct {
	Until  time.Time `json:"until"`
	Reason string    `json:"reason"`
}

func NewBreaker(cooldown time.Duration) *Breaker {
	return NewFileBreaker(cooldown, "")
}

func NewFileBreaker(cooldown time.Duration, path string) *Breaker {
	if cooldown < time.Minute {
		cooldown = time.Minute
	}
	b := &Breaker{cooldown: cooldown, path: path}
	b.load()
	return b
}

func circuitFile(profileDir, site string) string {
	profileDir = filepath.Clean(profileDir)
	if profileDir == "." || profileDir == "" || site == "" {
		return ""
	}
	dir := filepath.Dir(profileDir)
	path := filepath.Join(dir, "circuit-"+site+".json")
	// Раньше Маркет писал circuit-market.json. Пока новый файл не появился,
	// читаем старый, чтобы пауза не сбросилась после смены ключа.
	if site == string(sites.YandexMarket) {
		if _, err := os.Stat(path); err != nil {
			old := filepath.Join(dir, "circuit-market.json")
			if _, err := os.Stat(old); err == nil {
				return old
			}
		}
	}
	return path
}

func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return time.Now().After(b.until)
}

func (b *Breaker) Trip(reason string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.until = time.Now().Add(b.cooldown)
	b.reason = reason
	b.saveLocked()
}

func (b *Breaker) RetryAt() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.until
}

func (b *Breaker) Reason() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reason
}

func (b *Breaker) load() {
	if b.path == "" {
		return
	}
	raw, err := os.ReadFile(b.path)
	if err != nil {
		return
	}
	var st breakerState
	if json.Unmarshal(raw, &st) != nil {
		return
	}
	if st.Until.After(time.Now()) {
		b.until = st.Until
		b.reason = st.Reason
	}
}

func (b *Breaker) saveLocked() {
	if b.path == "" {
		return
	}
	raw, err := json.Marshal(breakerState{Until: b.until, Reason: b.reason})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(b.path), 0o755); err != nil {
		return
	}
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return
	}
	_ = os.Remove(b.path)
	_ = os.Rename(tmp, b.path)
}
