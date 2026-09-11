// Package sites распознаёт ссылки на магазины и приводит их к стабильному виду.
package sites

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Site — идентификатор магазина, он же значение колонки products.site.
type Site string

const (
	DNS          Site = "dns"
	Wildberries  Site = "wildberries"
	Ozon         Site = "ozon"
	YandexMarket Site = "yandex_market"
)

var (
	// ErrNotALink — строка вообще не похожа на ссылку.
	ErrNotALink = errors.New("это не похоже на ссылку")
	// ErrUnknownSite — домен не относится ни к одному известному магазину.
	ErrUnknownSite = errors.New("магазин не распознан")
	// ErrNotSupported — магазин известен, но трекинг для него ещё не реализован.
	ErrNotSupported = errors.New("магазин пока не поддерживается")
	// ErrNotAProduct — ссылка ведёт на магазин, но не на карточку товара.
	ErrNotAProduct = errors.New("ссылка не на карточку товара")
)

// Ref — распознанная ссылка на товар.
type Ref struct {
	Site Site
	// ExternalKey — идентификатор товара внутри магазина. Он стабилен:
	// не меняется при переименовании товара или смене slug в URL.
	ExternalKey string
	// URL приведён к каноническому виду, без query, фрагмента и slug.
	URL string
}

// Title — человекочитаемое имя магазина.
func (s Site) Title() string {
	switch s {
	case DNS:
		return "DNS"
	case Wildberries:
		return "Wildberries"
	case Ozon:
		return "Ozon"
	case YandexMarket:
		return "Яндекс.Маркет"
	default:
		return string(s)
	}
}

// CheckInterval — как часто ходить за ценой. DNS и Маркет банят за частые
// заходы, поэтому сутки; Ozon держит более частый ритм — раз в час.
func (s Site) CheckInterval() time.Duration {
	switch s {
	case Ozon:
		return time.Hour
	default:
		return 24 * time.Hour
	}
}

// dnsProductPath вытаскивает идентификатор товара из пути вида
// /product/9ee3a4f41358d9cb/146-noutbuk-honor-magicbook-pro-14/
var dnsProductPath = regexp.MustCompile(`(?i)^/product/([0-9a-f]{8,32})(?:/|$)`)

// Карточки Маркета: /card/slug/4638722913, /card/4638722913,
// /product--slug/4638722913 и старый /product/4638722913.
var (
	yandexCardPath        = regexp.MustCompile(`(?i)^/card/(?:[^/]+/)?(\d{6,})(?:/|$)`)
	yandexProductDashPath = regexp.MustCompile(`(?i)^/product--[^/]+/(\d{6,})(?:/|$)`)
	yandexProductPath     = regexp.MustCompile(`(?i)^/product/(\d{6,})(?:/|$)`)
	yandexSafeSlug        = regexp.MustCompile(`(?i)^[a-z0-9][a-z0-9-]{0,200}$`)
	// /product/slug-2422341064 — id всегда хвост пути, чтобы «420» в названии не стал ключом.
	ozonProductPath = regexp.MustCompile(`(?i)^/product/([^/]*?)(\d{6,})(?:/|$)`)
	ozonSafeSlug    = regexp.MustCompile(`(?i)^[a-z0-9][a-z0-9-]{0,240}$`)
)

// Parse распознаёт ссылку на товар. Ссылка может быть окружена текстом:
// Telegram часто присылает её вместе с подписью.
func Parse(raw string) (Ref, error) {
	raw = extractURL(raw)
	if raw == "" {
		return Ref{}, ErrNotALink
	}

	u, err := url.Parse(raw)
	if err != nil {
		return Ref{}, fmt.Errorf("%w: %v", ErrNotALink, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Ref{}, ErrNotALink
	}
	if u.User != nil {
		// https://evil@www.dns-shop.ru/... не должно выглядеть как DNS.
		return Ref{}, ErrNotALink
	}

	host := strings.ToLower(u.Hostname())
	site, ok := siteByHost(host)
	if !ok {
		return Ref{}, ErrUnknownSite
	}

	switch site {
	case DNS:
		return parseDNS(u)
	case YandexMarket:
		return parseYandexMarket(u)
	case Ozon:
		return parseOzon(u)
	default:
		return Ref{}, fmt.Errorf("%w: %s", ErrNotSupported, site.Title())
	}
}

func parseDNS(u *url.URL) (Ref, error) {
	m := dnsProductPath.FindStringSubmatch(u.EscapedPath())
	if m == nil {
		return Ref{}, ErrNotAProduct
	}
	key := strings.ToLower(m[1])

	// Канон без slug, query и userinfo: иначе второй подписчик мог бы
	// подменить отображаемую ссылку, а в href попал бы непроверенный путь.
	canonical := "https://www.dns-shop.ru/product/" + key + "/"
	return Ref{Site: DNS, ExternalKey: key, URL: canonical}, nil
}

func parseYandexMarket(u *url.URL) (Ref, error) {
	key, slug := yandexKeyAndSlug(u.EscapedPath())
	if key == "" {
		return Ref{}, ErrNotAProduct
	}
	// Канон только из id и безопасного slug: query/фрагмент отбрасываем,
	// произвольный путь в href не попадает.
	canonical := "https://market.yandex.ru/card/" + key
	if slug != "" && yandexSafeSlug.MatchString(slug) {
		canonical = "https://market.yandex.ru/card/" + strings.ToLower(slug) + "/" + key
	}
	return Ref{Site: YandexMarket, ExternalKey: key, URL: canonical}, nil
}

func yandexKeyAndSlug(path string) (key, slug string) {
	// Регулярки ниже привязаны к началу пути, поэтому префикс уже проверен
	// и остаётся только вырезать slug.
	if m := yandexCardPath.FindStringSubmatch(path); m != nil {
		key = m[1]
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) >= 3 && parts[2] == key {
			slug = parts[1]
		}
		return key, slug
	}
	if m := yandexProductDashPath.FindStringSubmatch(path); m != nil {
		key = m[1]
		after := path[len("/product--"):]
		if slash := strings.IndexByte(after, '/'); slash > 0 {
			slug = after[:slash]
		}
		return key, slug
	}
	if m := yandexProductPath.FindStringSubmatch(path); m != nil {
		return m[1], ""
	}
	return "", ""
}

func parseOzon(u *url.URL) (Ref, error) {
	m := ozonProductPath.FindStringSubmatch(u.EscapedPath())
	if m == nil {
		return Ref{}, ErrNotAProduct
	}
	key := m[2]
	slug := strings.TrimSuffix(m[1], "-")
	canonical := "https://www.ozon.ru/product/" + key
	if slug != "" && ozonSafeSlug.MatchString(slug) {
		canonical = "https://www.ozon.ru/product/" + strings.ToLower(slug) + "-" + key
	}
	return Ref{Site: Ozon, ExternalKey: key, URL: canonical}, nil
}

func siteByHost(host string) (Site, bool) {
	switch host {
	case "dns-shop.ru", "www.dns-shop.ru":
		return DNS, true
	case "wildberries.ru", "www.wildberries.ru":
		return Wildberries, true
	case "ozon.ru", "www.ozon.ru", "m.ozon.ru":
		return Ozon, true
	// market.yandex.by сознательно не принимаем: канон ведёт на .ru, и цена
	// в белорусских рублях отслеживалась бы как российская.
	case "market.yandex.ru", "www.market.yandex.ru", "m.market.yandex.ru":
		return YandexMarket, true
	default:
		return "", false
	}
}

var urlInText = regexp.MustCompile(`https?://[^\s<>"']+`)

func extractURL(s string) string {
	raw := urlInText.FindString(strings.TrimSpace(s))
	// Telegram и мессенджеры часто оборачивают ссылку в скобки или ставят точку в конце.
	return strings.TrimRight(raw, ".,);]!?»")
}
