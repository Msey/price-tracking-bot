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

// Supported сообщает, умеем ли мы уже отслеживать цены в этом магазине.
func (s Site) Supported() bool {
	return s == DNS
}

// dnsProductPath вытаскивает идентификатор товара из пути вида
// /product/9ee3a4f41358d9cb/146-noutbuk-honor-magicbook-pro-14/
var dnsProductPath = regexp.MustCompile(`(?i)^/product/([0-9a-f]{8,32})(?:/|$)`)

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
	if !site.Supported() {
		return Ref{}, fmt.Errorf("%w: %s", ErrNotSupported, site.Title())
	}

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

func siteByHost(host string) (Site, bool) {
	switch host {
	case "dns-shop.ru", "www.dns-shop.ru":
		return DNS, true
	case "wildberries.ru", "www.wildberries.ru":
		return Wildberries, true
	case "ozon.ru", "www.ozon.ru":
		return Ozon, true
	case "market.yandex.ru", "www.market.yandex.ru", "market.yandex.by":
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
