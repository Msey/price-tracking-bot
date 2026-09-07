// Package money форматирует цены в копейках.
package money

import (
	"fmt"
	"strconv"
	"strings"
)

// RubToKopecks переводит рубли в копейки.
func RubToKopecks(rub int64) int64 { return rub * 100 }

// FormatKopecks печатает цену с разделением разрядов: 15999900 -> "159 999 ₽".
func FormatKopecks(kopecks int64) string {
	if kopecks < 0 {
		return "0\u00a0₽"
	}
	whole := kopecks / 100
	digits := strconv.FormatInt(whole, 10)

	var sb strings.Builder
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
