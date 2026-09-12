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

func waitForBits(ctx context.Context, timeout time.Duration, bits <-chan pageBits, parse func(pageBits) (Snapshot, error), onHuman func(pageBits)) (Snapshot, error) {
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
			if last.SkipLDJSON && bankUntil.IsZero() {
				bankUntil = time.Now().Add(bankGraceOf(last))
			}
			snap, err := parsePriceBits(last, parse, bankUntil)
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
			if last.SkipLDJSON && !bankUntil.IsZero() {
				snap, err := parsePriceBits(last, parse, bankUntil)
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

func bankGraceOf(p pageBits) time.Duration {
	if p.BankGraceMs > 0 {
		return time.Duration(p.BankGraceMs) * time.Millisecond
	}
	return ozonBankGrace
}

func parsePriceBits(p pageBits, parse func(pageBits) (Snapshot, error), bankUntil time.Time) (Snapshot, error) {
	snap, err := parse(p)
	if err == nil {
		return snap, nil
	}
	if p.SkipLDJSON && !bankUntil.IsZero() && !time.Now().Before(bankUntil) {
		p.SkipLDJSON = false
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
