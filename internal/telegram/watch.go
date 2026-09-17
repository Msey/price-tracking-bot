package telegram

import (
	"context"
	"strings"
	"unicode"

	"github.com/Msey/price-tracking-bot/internal/money"
	"github.com/Msey/price-tracking-bot/internal/sites"
	"github.com/Msey/price-tracking-bot/internal/storage"
)

func parseAlert(text string) (int64, error) {
	rest := alertRemainder(text)
	if rest == "" {
		return 0, nil
	}
	kop, err := money.ParseRubles(rest)
	if err == nil {
		return kop, nil
	}
	if hasDigit(rest) {
		return 0, err
	}
	return 0, nil
}

func alertRemainder(text string) string {
	u := sites.ExtractURL(text)
	if u == "" {
		return strings.TrimSpace(text)
	}
	i := strings.Index(text, u)
	if i < 0 {
		return strings.TrimSpace(text)
	}
	rest := strings.TrimSpace(text[:i] + " " + text[i+len(u):])
	return strings.Trim(rest, ".,;:!?»\"'()[]")
}

func hasDigit(s string) bool {
	for _, r := range s {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func knownBelow(ctx context.Context, store *storage.Store, productID, alertKopecks int64) (int64, bool) {
	if store == nil || productID < 1 || alertKopecks <= 0 {
		return 0, false
	}
	hist, err := store.LastSnapshots(ctx, productID, 1)
	if err != nil || len(hist) == 0 {
		return 0, false
	}
	price := hist[0].PriceKopecks
	if !hist[0].Available || !storage.AlertDue(price, alertKopecks, false) {
		return 0, false
	}
	return price, true
}
