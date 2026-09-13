package fetch

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	pageWait      = 45 * time.Second
	pollEvery     = time.Second
	captchaWait   = 4 * time.Minute
	ozonBankGrace = 8 * time.Second
)

// pagePolicy — чему верить на странице магазина. Хранится в настройках
// магазина, а не приходит со страницы.
type pagePolicy struct {
	// skipLDJSON — не брать цену из разметки, только из карточки.
	skipLDJSON bool
	// bankGrace — сколько ждать нужный ценник, прежде чем всё-таки
	// согласиться на цену из разметки.
	bankGrace time.Duration
}

func (p pagePolicy) grace() time.Duration {
	if p.bankGrace > 0 {
		return p.bankGrace
	}
	return ozonBankGrace
}

func waitForBits(ctx context.Context, timeout time.Duration, bits <-chan pageBits, parse func(pageBits) (Snapshot, error), onHuman func(pageBits), pol pagePolicy) (Snapshot, error) {
	if timeout <= 0 {
		timeout = pageWait
	}
	deadline := time.Now().Add(timeout)
	told := false
	var last pageBits
	var lastErr error = ErrNoPrice
	var bankUntil time.Time
	poll := time.NewTimer(pollEvery)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			if needsHuman(last) || hardBlocked(last) {
				return Snapshot{}, challengeWithTitle(last.Title)
			}
			return Snapshot{}, ctx.Err()
		case last = <-bits:
			resetTimer(poll, pollEvery)
			if hardBlocked(last) {
				return Snapshot{}, challengeWithTitle(last.Title)
			}
			if pol.skipLDJSON && bankUntil.IsZero() {
				bankUntil = time.Now().Add(pol.grace())
			}
			snap, err := parsePriceBits(last, parse, bankUntil, pol)
			if err == nil {
				return snap, nil
			}
			lastErr = err
			if needsHuman(last) {
				if !told && onHuman != nil {
					told = true
					onHuman(last)
				}
				if extra := time.Until(deadline); extra < captchaWait {
					deadline = time.Now().Add(captchaWait)
				}
			}
		case <-poll.C:
			resetTimer(poll, pollEvery)
			if pol.skipLDJSON && !bankUntil.IsZero() {
				snap, err := parsePriceBits(last, parse, bankUntil, pol)
				if err == nil {
					return snap, nil
				}
				lastErr = err
			}
			if time.Now().After(deadline) {
				if needsHuman(last) || hardBlocked(last) {
					return Snapshot{}, challengeWithTitle(last.Title)
				}
				if last.Title != "" {
					return Snapshot{}, fmt.Errorf("%w (%s)", lastErr, last.Title)
				}
				return Snapshot{}, lastErr
			}
		}
	}
}

// parsePriceBits читает снимок по правилам магазина. Флаг ставится здесь,
// а не берётся из ответа страницы.
func parsePriceBits(p pageBits, parse func(pageBits) (Snapshot, error), bankUntil time.Time, pol pagePolicy) (Snapshot, error) {
	p.skipLDJSON = pol.skipLDJSON
	snap, err := parse(p)
	if err == nil {
		return snap, nil
	}
	if pol.skipLDJSON && !bankUntil.IsZero() && !time.Now().Before(bankUntil) {
		p.skipLDJSON = false
		return parse(p)
	}
	return snap, err
}

func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}

// isBanError — магазин оборвал соединение, а не страница оказалась пустой.
func isBanError(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, n := range []string{
		"forcibly closed", "connection reset", "err_connection",
		"chrome-error", "net::err_", "wsarecv",
	} {
		if strings.Contains(msg, n) {
			return true
		}
	}
	return false
}

// looksLikeHTTPBan — 403/401 в тексте ошибки (title страницы), когда
// расширение успело прочитать карточку, но parse вернул ErrNoPrice.
func looksLikeHTTPBan(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "403") || strings.Contains(msg, "401")
}

func challengeWithTitle(title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return ErrChallenge
	}
	return fmt.Errorf("%w (%s)", ErrChallenge, title)
}
