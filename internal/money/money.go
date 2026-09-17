// Package money форматирует цены в копейках.
package money

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

var (
	// ErrBadAmount — в строке не сумма в рублях.
	ErrBadAmount = errors.New("непонятная сумма")
	// ErrAmountRange — ноль, минус или слишком большое число.
	ErrAmountRange = errors.New("сумма вне диапазона")
)

// maxRubles — потолок порога: выше в карточках магазинов не бывает,
// а умножение на 100 не должно переполнить int64.
const maxRubles int64 = 99_999_999

// RubToKopecks переводит рубли в копейки.
func RubToKopecks(rub int64) int64 { return rub * 100 }

// ParseRubles разбирает сумму в рублях: "15000", "15 000", "15000,50", "15 000 ₽".
// Пустая строка — (0, nil): порог не задан.
func ParseRubles(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	s = stripMoneySuffix(s)
	s = stripSpaces(s)
	if s == "" {
		return 0, ErrBadAmount
	}
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
		return 0, ErrAmountRange
	}
	sep := -1
	for i, r := range s {
		switch {
		case r == '.' || r == ',':
			if sep >= 0 {
				return 0, ErrBadAmount
			}
			sep = i
		case r < '0' || r > '9':
			return 0, ErrBadAmount
		}
	}
	whole, frac := s, ""
	if sep >= 0 {
		whole, frac = s[:sep], s[sep+1:]
		if whole == "" || frac == "" || len(frac) > 2 {
			return 0, ErrBadAmount
		}
		if len(frac) == 1 {
			frac += "0"
		}
	}
	rub, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, ErrAmountRange
	}
	if rub > maxRubles {
		return 0, ErrAmountRange
	}
	var kop int64
	if frac != "" {
		kop, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, ErrBadAmount
		}
	}
	if rub == 0 && kop == 0 {
		return 0, ErrAmountRange
	}
	return rub*100 + kop, nil
}

func stripMoneySuffix(s string) string {
	s = strings.TrimSpace(s)
	lower := strings.ToLower(s)
	for _, suf := range []string{"рублей", "рубля", "руб.", "руб", "₽", "р.", "р"} {
		if strings.HasSuffix(lower, suf) {
			return strings.TrimSpace(s[:len(s)-len(suf)])
		}
	}
	return s
}

func stripSpaces(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// FormatKopecks печатает цену с разделением разрядов: 15999900 -> "159 999 ₽".
func FormatKopecks(kopecks int64) string {
	var sb strings.Builder
	// Отрицательная цена — признак сбоя разбора. Печатаем со знаком, а не
	// подменяем нулём: молчаливый «0 ₽» выглядит как настоящая цена.
	if kopecks < 0 {
		sb.WriteString("−")
		kopecks = -kopecks
	}
	whole := kopecks / 100
	digits := strconv.FormatInt(whole, 10)

	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			sb.WriteString("\u00a0")
		}
		sb.WriteRune(d)
	}
	if rem := kopecks % 100; rem != 0 {
		fmt.Fprintf(&sb, ",%02d", rem)
	}
	sb.WriteString("\u00a0₽")
	return sb.String()
}
