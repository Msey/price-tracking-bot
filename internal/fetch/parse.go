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
	Blocked   bool     `json:"blocked"`
	LDJSON    []string `json:"ldjson"`
	CSSPrice  string   `json:"cssPrice"`
	Name      string   `json:"name"`
	Title     string   `json:"title"`
	// SoldOut — на карточке явный блок «товар закончился», а не поле
	// availability в разметке. Разметка Ozon часто врёт.
	SoldOut bool `json:"soldOut"`
	// skipLDJSON — решение бота, а не страницы: со страницы приходит только
	// то, что на ней написано, а правило «не верить разметке» задаётся
	// настройками магазина.
	skipLDJSON bool
}

var (
	// Цена в узле страницы: одна группа цифр, разряды могут быть разделены
	// пробелами, в хвосте — необязательные копейки.
	priceRunRe        = regexp.MustCompile(`[0-9][0-9 ]*(?:[.,][0-9]{2})?`)
	shopTitleCutovers = []string{" — купить", " – купить", " | ", " — Яндекс", " – Яндекс", " — OZON", " – OZON", " на OZON", " на Ozon", " — Wildberries", " – Wildberries", " - WILDBERRIES"}
	priceSpaceRepl    = strings.NewReplacer(
		"\u00a0", " ",
		"\u202f", " ",
		"\u2007", " ",
		"\u2009", " ",
		"\u200a", " ",
		"\u2060", " ",
		"\ufeff", " ",
	)
)

func normalizePriceSpaces(s string) string {
	return priceSpaceRepl.Replace(s)
}

func botWall(p pageBits) bool {
	return p.QRATOR || p.Challenge
}

func parseBits(p pageBits) (Snapshot, error) {
	if hardBlocked(p) {
		return Snapshot{}, ErrChallenge
	}
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

func hardBlocked(p pageBits) bool {
	return titleLooksLikeHTTPBan(p.Title)
}

// titleLooksLikeHTTPBan — заголовок страницы бана, а не артикул вроде
// «Honor 403» или «401 серия».
func titleLooksLikeHTTPBan(title string) bool {
	t := strings.ToLower(strings.TrimSpace(title))
	if t == "" {
		return false
	}
	if t == "403" || t == "401" || t == "forbidden" {
		return true
	}
	for _, n := range []string{
		"403 forbidden", "401 unauthorized",
		"error 403", "error 401",
		"http 403", "http 401",
		"403 error", "401 error",
		"access forbidden",
	} {
		if strings.Contains(t, n) {
			return true
		}
	}
	return httpCodeTitlePrefix(t)
}

// httpCodeTitlePrefix — «403 - Access Denied», но не «401 серия».
func httpCodeTitlePrefix(t string) bool {
	for _, code := range []string{"403", "401"} {
		if !strings.HasPrefix(t, code) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(t, code))
		if rest == "" {
			return true
		}
		for _, sep := range []string{"-", "–", "|", ":", "/"} {
			if strings.HasPrefix(rest, sep) {
				return true
			}
		}
	}
	return false
}

// antibotWall — заглушка антибота: «Похоже, нет соединения» у Ozon
// или «Подозрительная активность» у Wildberries. Это не обрыв сети:
// в обычном Chrome та же ссылка открывается.
func antibotWall(p pageBits) bool {
	if p.Blocked {
		return true
	}
	t := strings.ToLower(strings.TrimSpace(p.Title))
	return strings.Contains(t, "antibot challenge") ||
		strings.Contains(t, "нет соединения")
}

// needsHuman — на странице есть действие, которое может сделать человек:
// капча Ozon/Маркета, кнопка «Обновить страницу» у заглушки Ozon
// или ожидание таймера Wildberries. HTTP 403 сюда не входит:
// бан по IP кнопкой не снимается.
func needsHuman(p pageBits) bool {
	if hardBlocked(p) {
		return false
	}
	return p.Challenge || antibotWall(p)
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

// parseDisplayedPrice читает цену из текста узла. Берётся первая группа цифр,
// а если в узле их две (зачёркнутая цена рядом с текущей) — цена не читается:
// склеивать их в одно число нельзя, иначе подписчик получит выдуманную цену.
func parseDisplayedPrice(s string) (int64, bool) {
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&#160;", " ")
	s = normalizePriceSpaces(s)
	runs := priceRunRe.FindAllString(s, 2)
	if len(runs) != 1 {
		return 0, false
	}
	return parsePriceRun(runs[0])
}

// parsePriceRun разбирает одну найденную группу цифр. Разряды в ней идут
// по три, и это проверяется: две цены, разделённые только пробелом
// («5 672 5 105» — зачёркнутая рядом с текущей), регулярка видит как один
// прогон, и без проверки разрядов они склеились бы в 56 725 105 ₽.
func parsePriceRun(run string) (int64, bool) {
	run = strings.TrimSpace(run)
	var kopecks int64
	if i := strings.IndexAny(run, ".,"); i >= 0 {
		frac, err := strconv.ParseInt(run[i+1:], 10, 64)
		if err != nil {
			return 0, false
		}
		kopecks = frac
		run = strings.TrimSpace(run[:i])
	}
	groups := strings.Fields(run)
	if len(groups) == 0 {
		return 0, false
	}
	// Одна группа без пробелов неоднозначной быть не может, поэтому её
	// длину не ограничиваем.
	if len(groups) > 1 {
		if len(groups[0]) > 3 {
			return 0, false
		}
		for _, g := range groups[1:] {
			if len(g) != 3 {
				return 0, false
			}
		}
	}
	rub, err := strconv.ParseInt(strings.Join(groups, ""), 10, 64)
	if err != nil || rub <= 0 || rub > 1e9 {
		return 0, false
	}
	return money.RubToKopecks(rub) + kopecks, true
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

// parseVisiblePriceBits читает цену Маркета и Ozon: верна цена в карточке.
// JSON-LD — запасной вариант, кроме Ozon (skipLDJSON в настройках магазина):
// там в разметке часто цена «с другими банками», а нужна «с Ozon банком».
func parseVisiblePriceBits(p pageBits) (Snapshot, error) {
	if hardBlocked(p) {
		return Snapshot{}, ErrChallenge
	}

	name := cleanShopName(p.Name, p.Title)
	if kopecks, ok := parseDisplayedPrice(p.CSSPrice); ok {
		markup, markupOK := parseLDJSON(p.LDJSON)
		if markupOK && name == "" {
			name = markup.Name
		}
		return Snapshot{
			Name:         name,
			PriceKopecks: kopecks,
			Currency:     "RUB",
			Available:    offerAvailable(p, markup, markupOK),
		}, nil
	}

	if !p.skipLDJSON {
		if snap, ok := parseLDJSON(p.LDJSON); ok {
			if name != "" {
				snap.Name = name
			}
			snap.Available = offerAvailable(p, snap, true)
			return snap, nil
		}
	}
	if botWall(p) || antibotWall(p) {
		return Snapshot{}, ErrChallenge
	}
	return Snapshot{}, ErrNoPrice
}

// offerAvailable решает наличие.
// Виджет «товар закончился» важнее разметки. На Ozon, пока цену берём
// только с карточки (skipLDJSON), разметке не верим: OutOfStock стоит
// и на странице с кнопкой «В корзину». Иначе наличие берём из разметки.
// Нет разметки — товар в наличии.
func offerAvailable(p pageBits, markup Snapshot, markupOK bool) bool {
	if p.SoldOut {
		return false
	}
	if p.skipLDJSON || !markupOK {
		return true
	}
	return markup.Available
}

func cleanShopName(h1, title string) string {
	name := strings.TrimSpace(h1)
	if name == "" || genericShopTitle(name) {
		name = strings.TrimSpace(title)
	}
	if genericShopTitle(name) {
		name = ""
	}
	for _, sep := range shopTitleCutovers {
		if i := strings.Index(name, sep); i > 0 {
			name = strings.TrimSpace(name[:i])
		}
	}
	if genericShopTitle(name) {
		return ""
	}
	return name
}

// genericShopTitle — слоган витрины, а не имя карточки. У Wildberries
// document.title часто «Интернет-магазин Wildberries: …», его нельзя
// писать в список.
func genericShopTitle(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return true
	}
	if strings.HasPrefix(t, "интернет-магазин wildberries") {
		return true
	}
	return strings.Contains(t, "wildberries") && strings.Contains(t, "широкий ассортимент")
}
