// Package fetch достаёт цену со страницы DNS.
package fetch

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/Msey/price-tracking-bot/internal/money"
)

var (
	// ErrChallenge — QRATOR не пустил, дальше по этому IP лучше не стучаться.
	ErrChallenge = errors.New("dns: qrator-челлендж")
	// ErrNoPrice — страница открылась, но цены на ней нет.
	ErrNoPrice = errors.New("dns: цена не найдена")
)

// Snapshot — то, что удалось прочитать с карточки.
type Snapshot struct {
	Name         string
	PriceKopecks int64
	Currency     string
	Available    bool
}

type pageBits struct {
	QRATOR   bool     `json:"qrator"`
	LDJSON   []string `json:"ldjson"`
	CSSPrice string   `json:"cssPrice"`
	Title    string   `json:"title"`
}

var (
	ldJSONRe = regexp.MustCompile(`(?is)<script[^>]*type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	divRe    = regexp.MustCompile(`(?is)<div\s+([^>]+)>([^<]*)</div>`)
	classRe  = regexp.MustCompile(`(?i)class=["']([^"']+)["']`)
)

// ParseHTML разбирает HTML карточки. Сети нет.
func ParseHTML(html string) (Snapshot, error) {
	return parseBits(pageBits{
		QRATOR:   isChallenge(html),
		LDJSON:   extractLDJSON(html),
		CSSPrice: extractClassText(html, "product-buy__price"),
		Title:    "",
	})
}

func parseBits(p pageBits) (Snapshot, error) {
	if p.QRATOR && strings.TrimSpace(p.CSSPrice) == "" && len(p.LDJSON) == 0 {
		return Snapshot{}, ErrChallenge
	}

	snap, ok := parseLDJSON(p.LDJSON)
	if !ok {
		if kopecks, cssOK := parseDisplayedPrice(p.CSSPrice); cssOK {
			snap.PriceKopecks = kopecks
			snap.Currency = "RUB"
			snap.Available = true
			ok = true
		}
	}
	if !ok {
		if p.QRATOR {
			return Snapshot{}, ErrChallenge
		}
		return Snapshot{}, ErrNoPrice
	}
	if snap.Currency == "" {
		snap.Currency = "RUB"
	}
	return snap, nil
}

func isChallenge(html string) bool {
	return strings.Contains(html, "/__qrator/") ||
		strings.Contains(html, "qauth_handle_validate") ||
		strings.Contains(html, "qrator_jsr")
}

func hardBlocked(p pageBits) bool {
	t := strings.ToLower(strings.TrimSpace(p.Title))
	return strings.Contains(t, "403") || strings.Contains(t, "401")
}

func extractLDJSON(html string) []string {
	matches := ldJSONRe.FindAllStringSubmatch(html, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		body := strings.TrimSpace(m[1])
		body = strings.TrimPrefix(body, "<!--")
		body = strings.TrimSuffix(body, "-->")
		out = append(out, strings.TrimSpace(body))
	}
	return out
}

func extractClassText(html, className string) string {
	for _, m := range divRe.FindAllStringSubmatch(html, -1) {
		classAttr := ""
		if cm := classRe.FindStringSubmatch(m[1]); len(cm) == 2 {
			classAttr = cm[1]
		}
		if hasClass(classAttr, className) {
			return strings.TrimSpace(m[2])
		}
	}
	return ""
}

func hasClass(attr, name string) bool {
	for _, c := range strings.Fields(attr) {
		if c == name {
			return true
		}
	}
	return false
}

type ldNode struct {
	Type          json.RawMessage `json:"@type"`
	Name          string          `json:"name"`
	Offers        json.RawMessage `json:"offers"`
	Availability  string          `json:"availability"`
	Price         json.Number     `json:"price"`
	PriceCurrency string          `json:"priceCurrency"`
}

func parseLDJSON(blobs []string) (Snapshot, bool) {
	for _, blob := range blobs {
		nodes := flattenJSON(blob)
		for _, n := range nodes {
			if !isType(n.Type, "Product") {
				continue
			}
			offer, ok := offerOf(n)
			if !ok {
				continue
			}
			kopecks, err := priceToKopecks(offer.Price)
			if err != nil {
				continue
			}
			return Snapshot{
				Name:         strings.TrimSpace(n.Name),
				PriceKopecks: kopecks,
				Currency:     defaultCurrency(offer.PriceCurrency),
				Available:    isInStock(offer.Availability),
			}, true
		}
	}
	return Snapshot{}, false
}

func flattenJSON(blob string) []ldNode {
	var one ldNode
	if err := json.Unmarshal([]byte(blob), &one); err == nil && (len(one.Type) > 0 || one.Name != "" || len(one.Offers) > 0) {
		return []ldNode{one}
	}
	var many []ldNode
	if err := json.Unmarshal([]byte(blob), &many); err == nil {
		return many
	}
	var wrap struct {
		Graph []ldNode `json:"@graph"`
	}
	if err := json.Unmarshal([]byte(blob), &wrap); err == nil && len(wrap.Graph) > 0 {
		return wrap.Graph
	}
	return nil
}

func offerOf(n ldNode) (ldNode, bool) {
	if len(n.Offers) == 0 {
		if n.Price != "" {
			return n, true
		}
		return ldNode{}, false
	}
	var one ldNode
	if err := json.Unmarshal(n.Offers, &one); err == nil && (one.Price != "" || one.Availability != "") {
		return one, true
	}
	var many []ldNode
	if err := json.Unmarshal(n.Offers, &many); err == nil {
		for _, o := range many {
			if o.Price != "" {
				return o, true
			}
		}
	}
	return ldNode{}, false
}

func isType(raw json.RawMessage, want string) bool {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.EqualFold(s, want)
	}
	var arr []string
	if json.Unmarshal(raw, &arr) == nil {
		for _, x := range arr {
			if strings.EqualFold(x, want) {
				return true
			}
		}
	}
	return false
}

func priceToKopecks(n json.Number) (int64, error) {
	s := strings.TrimSpace(n.String())
	if s == "" {
		return 0, fmt.Errorf("пустая цена")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if f <= 0 || f > 1e9 {
		return 0, fmt.Errorf("цена вне диапазона: %v", f)
	}
	return int64(math.Round(f * 100)), nil
}

func parseDisplayedPrice(s string) (int64, bool) {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	var digits strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	if digits.Len() == 0 {
		return 0, false
	}
	rub, err := strconv.ParseInt(digits.String(), 10, 64)
	if err != nil || rub <= 0 || rub > 1e9 {
		return 0, false
	}
	return money.RubToKopecks(rub), true
}

func isInStock(availability string) bool {
	a := strings.ToLower(availability)
	switch {
	case strings.Contains(a, "outofstock"), strings.Contains(a, "soldout"), strings.Contains(a, "discontinued"):
		return false
	default:
		// Нет поля — на DNS обычно значит «в наличии», раз цена на месте.
		return true
	}
}

func defaultCurrency(c string) string {
	if strings.TrimSpace(c) == "" {
		return "RUB"
	}
	return strings.ToUpper(c)
}
