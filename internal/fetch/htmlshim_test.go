package fetch

import (
	"regexp"
	"strings"
	"testing"
)

// Тестовая обвязка: превращает сохранённый HTML карточки в pageBits —
// ровно в тот вид, в котором их отдаёт extract.js. В бою страницу читает
// Chrome, поэтому этих регулярок в продакшене нет: иначе рядом жила бы
// вторая реализация разбора.
//
// Обвязка остаётся потому, что даёт разбору цены проверку на настоящих
// сохранённых карточках. Плата за это — селекторы в двух местах, и
// TestShimSelectorsMatchExtension следит, чтобы они не разъехались молча.
var (
	ldJSONRe           = regexp.MustCompile(`(?is)<script[^>]*type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	divRe              = regexp.MustCompile(`(?is)<div\s+([^>]+)>([^<]*)</div>`)
	classRe            = regexp.MustCompile(`(?i)class=["']([^"']+)["']`)
	marketPriceInnerRe = regexp.MustCompile(`(?is)data-auto=["']snippet-price-current["'][^>]*>\s*<span[^>]*>\s*([^<]+?)\s*<`)
	ozonHeadlineRe     = regexp.MustCompile(`(?is)class=["'][^"']*\btsHeadline600Large\b[^"']*["'][^>]*>\s*([^<]+?)\s*<`)
	h1Re               = regexp.MustCompile(`(?is)<h1\b[^>]*>(.*?)</h1>`)
	titleRe            = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title>`)
	tagRe              = regexp.MustCompile(`(?s)<[^>]+>`)
)

// TestShimSelectorsMatchExtension ловит расхождение обвязки с расширением:
// если в extract.js поменяли селектор, тесты на сохранённых карточках
// продолжали бы проходить по старому — и настоящая карточка ломалась бы
// только в бою.
func TestShimSelectorsMatchExtension(t *testing.T) {
	raw, err := extFS.ReadFile("ext/extract.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(raw)
	for _, sel := range []string{
		"product-buy__price",     // DNS
		"snippet-price-current",  // Яндекс Маркет
		`data-widget="webPrice"`, // Ozon
		"tsHeadline",             // Ozon: класс крупного ценника
		"ozon банк",              // Ozon: подпись цены с банком
		"кошельк",                // Wildberries: подпись цены с кошельком
		"price-block__wallet-price",
		"price-block__final-price",
	} {
		if !strings.Contains(js, sel) {
			t.Errorf("обвязка ищет %q, а extract.js — уже нет", sel)
		}
	}
}

func parseDNSHTML(html string) (Snapshot, error) {
	return parseBits(pageBits{
		QRATOR:   dnsChallengeHTML(html),
		LDJSON:   extractLDJSON(html),
		CSSPrice: extractClassText(html, "product-buy__price"),
	})
}

func parseMarketHTML(html string) (Snapshot, error) {
	return parseVisiblePriceBits(pageBits{
		Challenge: marketChallengeHTML(html),
		LDJSON:    extractLDJSON(html),
		CSSPrice:  extractMarketPriceText(html),
		Name:      extractH1(html),
	})
}

func parseOzonHTML(html string) (Snapshot, error) {
	blocked := ozonInterstitialHTML(html)
	return parseVisiblePriceBits(pageBits{
		Challenge: ozonChallengeHTML(html) && !blocked,
		Blocked:   blocked,
		LDJSON:    extractLDJSON(html),
		CSSPrice:  extractOzonPriceText(html),
		Name:      extractH1(html),
		Title:     extractTitle(html),
	})
}

func parseWildberriesHTML(html string) (Snapshot, error) {
	return parseVisiblePriceBits(pageBits{
		Blocked:  wbAntibotHTML(html),
		LDJSON:   extractLDJSON(html),
		CSSPrice: extractWBPriceText(html),
		Name:     extractH1(html),
		Title:    extractTitle(html),
	})
}

func dnsChallengeHTML(html string) bool {
	h := strings.ToLower(html)
	if titleLooksLikeHTTPBan(extractTitle(html)) {
		return true
	}
	return strings.Contains(html, "Доступ к сайту") ||
		strings.Contains(h, "доступ запрещен")
}

func marketChallengeHTML(html string) bool {
	h := strings.ToLower(html)
	return strings.Contains(h, "smartcaptcha") ||
		strings.Contains(h, "showcaptcha") ||
		strings.Contains(h, "checkboxcaptcha") ||
		strings.Contains(h, "are you not a robot") ||
		strings.Contains(h, "confirm that you are not a robot")
}

func ozonChallengeHTML(html string) bool {
	return strings.Contains(strings.ToLower(html), "px-captcha")
}

func ozonInterstitialHTML(html string) bool {
	h := strings.ToLower(html)
	return strings.Contains(h, "нет соединения") ||
		strings.Contains(h, "antibot challenge") ||
		strings.Contains(h, "fab_chig")
}

func wbAntibotHTML(html string) bool {
	h := strings.ToLower(html)
	return strings.Contains(h, "подозрительная активность") ||
		strings.Contains(h, "новая попытка через") ||
		strings.Contains(h, "проверяем браузер")
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

func extractMarketPriceText(html string) string {
	if m := marketPriceInnerRe.FindStringSubmatch(html); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

var ozonBankLabelRe = regexp.MustCompile(`(?i)с\s+(ozon\s*карт|ozon\s*банк|банком\s+ozon|банками\s+ozon)`)

func extractOzonPriceText(html string) string {
	low := strings.ToLower(html)
	if loc := ozonBankLabelRe.FindStringIndex(low); loc != nil {
		start := loc[0] - 2500
		if start < 0 {
			start = 0
		}
		window := html[start:loc[1]]
		matches := ozonHeadlineRe.FindAllStringSubmatch(window, -1)
		if n := len(matches); n > 0 {
			return strings.TrimSpace(matches[n-1][1])
		}
	}
	for _, marker := range []string{`data-widget="webPrice"`, `data-widget='webPrice'`} {
		i := strings.Index(html, marker)
		if i < 0 {
			continue
		}
		if m := ozonHeadlineRe.FindStringSubmatch(priceWindow(html[i:])); len(m) == 2 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

var (
	wbH2PriceRe     = regexp.MustCompile(`(?is)<h2\b[^>]*>\s*([^<]+?)\s*<`)
	wbWalletClassRe = regexp.MustCompile(`(?is)class=["'][^"']*\bprice-block__wallet-price\b[^"']*["'][^>]*>\s*([^<]+?)\s*<`)
	wbFinalPriceRe  = regexp.MustCompile(`(?is)<ins\b[^>]*class=["'][^"']*\bprice-block__final-price\b[^"']*["'][^>]*>\s*([^<]+?)\s*<`)
)

func extractWBPriceText(html string) string {
	low := strings.ToLower(html)
	low = strings.ReplaceAll(low, "ё", "е")
	if loc := strings.Index(low, "кошельк"); loc >= 0 {
		start := loc - 2500
		if start < 0 {
			start = 0
		}
		window := html[start:loc]
		if m := wbH2PriceRe.FindAllStringSubmatch(window, -1); len(m) > 0 {
			if p := strings.TrimSpace(m[len(m)-1][1]); p != "" {
				return p
			}
		}
		if m := wbWalletClassRe.FindAllStringSubmatch(window, -1); len(m) > 0 {
			if p := strings.TrimSpace(m[len(m)-1][1]); p != "" {
				return p
			}
		}
	}
	if m := wbWalletClassRe.FindStringSubmatch(html); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	if i := strings.Index(low, "product-page__price-block"); i >= 0 {
		if m := wbFinalPriceRe.FindStringSubmatch(priceWindow(html[i:])); len(m) == 2 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// priceWindow обрезает хвост по границе тега, чтобы не разрубить символ UTF-8.
func priceWindow(s string) string {
	const limit = 4000
	if len(s) <= limit {
		return s
	}
	if cut := strings.LastIndex(s[:limit], "<"); cut > 0 {
		return s[:cut]
	}
	return s[:limit]
}

func extractH1(html string) string {
	m := h1Re.FindStringSubmatch(html)
	if m == nil {
		return ""
	}
	return strings.Join(strings.Fields(tagRe.ReplaceAllString(m[1], " ")), " ")
}

func extractTitle(html string) string {
	m := titleRe.FindStringSubmatch(html)
	if m == nil {
		return ""
	}
	return strings.Join(strings.Fields(tagRe.ReplaceAllString(m[1], " ")), " ")
}
