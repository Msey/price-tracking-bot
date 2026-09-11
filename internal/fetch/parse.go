// Package fetch достаёт цену со страницы магазина.
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
	// ErrChallenge — сайт показал защиту (QRATOR, капча). Дальше лучше подождать.
	ErrChallenge = errors.New("сайт показал защиту от ботов")
	// ErrNoPrice — страница открылась, но цены на ней нет.
	ErrNoPrice = errors.New("цена не найдена")
)

// Snapshot — то, что удалось прочитать с карточки.
type Snapshot struct {
	Name         string
	PriceKopecks int64
	Currency     string
	Available    bool
}

type pageBits struct {
	QRATOR    bool     `json:"qrator"`
	Challenge bool     `json:"challenge"`
	LDJSON    []string `json:"ldjson"`
	CSSPrice  string   `json:"cssPrice"`
	Name      string   `json:"name"`
	Title     string   `json:"title"`
}

var (
	ldJSONRe           = regexp.MustCompile(`(?is)<script[^>]*type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	divRe              = regexp.MustCompile(`(?is)<div\s+([^>]+)>([^<]*)</div>`)
	classRe            = regexp.MustCompile(`(?i)class=["']([^"']+)["']`)
	marketPriceInnerRe = regexp.MustCompile(`(?is)data-auto=["']snippet-price-current["'][^>]*>\s*<span[^>]*>\s*([^<]+?)\s*<`)
	ozonHeadlineRe     = regexp.MustCompile(`(?is)class=["'][^"']*\btsHeadline600Large\b[^"']*["'][^>]*>\s*([^<]+?)\s*<`)
	h1Re               = regexp.MustCompile(`(?is)<h1\b[^>]*>(.*?)</h1>`)
	tagRe              = regexp.MustCompile(`(?s)<[^>]+>`)
	shopTitleCutovers  = []string{" — купить", " – купить", " | ", " — Яндекс", " – Яндекс", " — OZON", " – OZON", " на OZON", " на Ozon"}
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

func botWall(p pageBits) bool {
	return p.QRATOR || p.Challenge
}

func parseBits(p pageBits) (Snapshot, error) {
	if botWall(p) && strings.TrimSpace(p.CSSPrice) == "" && len(p.LDJSON) == 0 {
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
		if botWall(p) {
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
	return strings.Contains(t, "403") || strings.Contains(t, "401") ||
		strings.Contains(t, "not a robot")
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
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&#160;", " ")
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

// ParseMarketHTML разбирает HTML карточки Яндекс.Маркета. Сети нет.
func ParseMarketHTML(html string) (Snapshot, error) {
	return parseVisiblePriceBits(pageBits{
		Challenge: isMarketChallenge(html),
		LDJSON:    extractLDJSON(html),
		CSSPrice:  extractMarketPriceText(html),
		Name:      extractH1(html),
	})
}

// ParseOzonHTML разбирает HTML карточки Ozon. Сети нет.
func ParseOzonHTML(html string) (Snapshot, error) {
	return parseVisiblePriceBits(pageBits{
		Challenge: isOzonChallenge(html),
		LDJSON:    extractLDJSON(html),
		CSSPrice:  extractOzonPriceText(html),
		Name:      extractH1(html),
	})
}

func parseMarketBits(p pageBits) (Snapshot, error) {
	return parseVisiblePriceBits(p)
}

func parseOzonBits(p pageBits) (Snapshot, error) {
	return parseVisiblePriceBits(p)
}

func parseVisiblePriceBits(p pageBits) (Snapshot, error) {
	if hardBlocked(p) {
		return Snapshot{}, ErrChallenge
	}

	name := cleanShopName(p.Name, p.Title)
	if kopecks, ok := parseDisplayedPrice(p.CSSPrice); ok {
		avail := true
		if snap, ldOK := parseLDJSON(p.LDJSON); ldOK {
			if name == "" {
				name = snap.Name
			}
			avail = snap.Available
		}
		return Snapshot{
			Name:         name,
			PriceKopecks: kopecks,
			Currency:     "RUB",
			Available:    avail,
		}, nil
	}

	if snap, ok := parseLDJSON(p.LDJSON); ok {
		if name != "" {
			snap.Name = name
		}
		return snap, nil
	}
	if botWall(p) {
		return Snapshot{}, ErrChallenge
	}
	return Snapshot{}, ErrNoPrice
}

func isMarketChallenge(html string) bool {
	h := strings.ToLower(html)
	return strings.Contains(h, "smartcaptcha") ||
		strings.Contains(h, "showcaptcha") ||
		strings.Contains(h, "checkboxcaptcha") ||
		strings.Contains(h, "are you not a robot") ||
		strings.Contains(h, "confirm that you are not a robot")
}

func extractMarketPriceText(html string) string {
	if m := marketPriceInnerRe.FindStringSubmatch(html); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func extractOzonPriceText(html string) string {
	const marker = `data-widget="webPrice"`
	if i := strings.Index(html, marker); i >= 0 {
		window := html[i:]
		if len(window) > 4000 {
			window = window[:4000]
		}
		if m := ozonHeadlineRe.FindStringSubmatch(window); len(m) == 2 {
			return strings.TrimSpace(m[1])
		}
	}
	if i := strings.Index(html, `data-widget='webPrice'`); i >= 0 {
		window := html[i:]
		if len(window) > 4000 {
			window = window[:4000]
		}
		if m := ozonHeadlineRe.FindStringSubmatch(window); len(m) == 2 {
			return strings.TrimSpace(m[1])
		}
	}
	if m := ozonHeadlineRe.FindStringSubmatch(html); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func isOzonChallenge(html string) bool {
	h := strings.ToLower(html)
	return strings.Contains(h, "px-captcha") ||
		strings.Contains(h, "perimeterx") ||
		strings.Contains(h, "antibot challenge") ||
		strings.Contains(h, "access denied")
}

func extractH1(html string) string {
	m := h1Re.FindStringSubmatch(html)
	if m == nil {
		return ""
	}
	return strings.Join(strings.Fields(tagRe.ReplaceAllString(m[1], " ")), " ")
}

func cleanMarketName(h1, title string) string {
	return cleanShopName(h1, title)
}

func cleanShopName(h1, title string) string {
	name := strings.TrimSpace(h1)
	if name == "" {
		name = strings.TrimSpace(title)
	}
	for _, sep := range shopTitleCutovers {
		if i := strings.Index(name, sep); i > 0 {
			name = strings.TrimSpace(name[:i])
		}
	}
	return name
}
