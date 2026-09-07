// Package sites распознаёт ссылки на магазины и приводит их к стабильному виду.
package sites

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
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
	// URL приведён к каноническому виду, без query и фрагмента.
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

// Supported сообщает, умеем ли мы уже отслеживать цены в этом магазине.
func (s Site) Supported() bool {
	return s == DNS
}

// dnsProductPath вытаскивает идентификатор товара из пути вида
// /product/9ee3a4f41358d9cb/146-noutbuk-honor-magicbook-pro-14/
var dnsProductPath = regexp.MustCompile(`^/product/([0-9a-f]{8,32})(?:/|$)`)

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

	host := strings.ToLower(strings.TrimPrefix(u.Hostname(), "www."))
	site, ok := siteByHost(host)
	if !ok {
		return Ref{}, ErrUnknownSite
	}
	if !site.Supported() {
		return Ref{}, fmt.Errorf("%w: %s", ErrNotSupported, site.Title())
	}

	m := dnsProductPath.FindStringSubmatch(u.EscapedPath())
	if m == nil {
		return Ref{}, ErrNotAProduct
	}

	// Query отбрасываем: в ссылках DNS там utm-метки и параметр city,
	// который иначе размножил бы один товар на несколько записей в базе.
	canonical := url.URL{Scheme: "https", Host: "www.dns-shop.ru", Path: u.EscapedPath()}
	if !strings.HasSuffix(canonical.Path, "/") {
		canonical.Path += "/"
	}

	return Ref{Site: DNS, ExternalKey: m[1], URL: canonical.String()}, nil
}

func siteByHost(host string) (Site, bool) {
	switch {
	case host == "dns-shop.ru" || strings.HasSuffix(host, ".dns-shop.ru"):
		return DNS, true
	case host == "wildberries.ru" || strings.HasSuffix(host, ".wildberries.ru"):
		return Wildberries, true
	case host == "ozon.ru" || strings.HasSuffix(host, ".ozon.ru"):
		return Ozon, true
	case host == "market.yandex.ru" || host == "market.yandex.by":
		return YandexMarket, true
	default:
		return "", false
	}
}

var urlInText = regexp.MustCompile(`https?://[^\s<>"']+`)

func extractURL(s string) string {
	return urlInText.FindString(strings.TrimSpace(s))
}
