package fetch

import (
	"sync"
	"time"
)

// Breaker на весь сайт: после челленджа или обрыва TLS не ходим на DNS,
// пока не истечёт пауза. Иначе QRATOR банит IP целиком.
type Breaker struct {
	mu       sync.Mutex
	until    time.Time
	cooldown time.Duration
	reason   string
}

func NewBreaker(cooldown time.Duration) *Breaker {
	if cooldown < time.Minute {
		cooldown = time.Minute
	}
	return &Breaker{cooldown: cooldown}
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
