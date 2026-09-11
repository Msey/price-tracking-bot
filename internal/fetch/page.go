package fetch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	pageWait    = 45 * time.Second
	pollEvery   = time.Second
	captchaWait = 4 * time.Minute
	chromeUA    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36"
)

// waitForExtract крутит extract-скрипт на открытой странице, пока не появится
// цена или не выйдет время. Окно Chrome показывается только на интерактивной
// капче (Ozon/Маркет), не на бан DNS 403 и не из‑за скрипта QRATOR в HTML.
func waitForExtract(ctx context.Context, timeout time.Duration, js string, parse func(pageBits) (Snapshot, error), ui *chromeUI) (Snapshot, error) {
	if timeout <= 0 {
		timeout = pageWait
	}
	deadline := time.Now().Add(timeout)
	shown := false
	// Окно прячется и когда капчу так и не прошли, иначе Chrome остаётся
	// развёрнутым поверх всего до следующей удачной проверки.
	defer func() {
		if shown && ui != nil {
			_ = ui.hide()
		}
	}()

	var last pageBits
	var lastErr error = ErrNoPrice
	for {
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &last)); err != nil {
			return Snapshot{}, err
		}
		if hardBlocked(last) {
			return Snapshot{}, ErrChallenge
		}
		snap, err := parse(last)
		if err == nil {
			return snap, nil
		}
		lastErr = err
		if needsHuman(last) && ui != nil && !shown {
			shown = true
			_ = ui.reveal(ctx)
			if ui.notify != nil {
				ui.notify(last)
			}
			if extra := time.Until(deadline); extra < captchaWait {
				deadline = time.Now().Add(captchaWait)
			}
		}

		select {
		case <-ctx.Done():
			if needsHuman(last) || errors.Is(lastErr, ErrChallenge) {
				return Snapshot{}, ErrChallenge
			}
			return Snapshot{}, ctx.Err()
		case <-time.After(pollEvery):
		}
		if time.Now().After(deadline) {
			if needsHuman(last) {
				return Snapshot{}, ErrChallenge
			}
			if last.Title != "" {
				return Snapshot{}, fmt.Errorf("%w (%s)", lastErr, last.Title)
			}
			return Snapshot{}, lastErr
		}
	}
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
