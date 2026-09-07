package telegram

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Msey/price-tracking-bot/internal/sites"
	"github.com/Msey/price-tracking-bot/internal/storage"
)

func TestLinkToEscapesHTML(t *testing.T) {
	p := storage.Product{
		URL:  `https://www.dns-shop.ru/product/abc/"onclick="alert(1)`,
		Name: `<img src=x onerror=alert(1)>`,
	}
	got := linkTo(p)
	if strings.Contains(got, `<img src`) {
		t.Errorf("тег в заголовке не экранирован: %s", got)
	}
	if !strings.Contains(got, "&lt;img") {
		t.Errorf("ожидался экранированный заголовок: %s", got)
	}
	if strings.Contains(got, `href="https://www.dns-shop.ru/product/abc/"`) {
		t.Errorf("кавычка в URL разорвала href: %s", got)
	}
	if !strings.Contains(got, "&#34;") && !strings.Contains(got, "&quot;") {
		t.Errorf("кавычки в href должны быть экранированы: %s", got)
	}
}

func TestExplainParseErrorEscapes(t *testing.T) {
	err := fmt.Errorf("%w: %s", sites.ErrNotSupported, "<b>XSS</b>")
	got := explainParseError(err)
	if strings.Contains(got, "<b>") {
		t.Errorf("HTML не экранирован: %s", got)
	}
}

func TestFormatKopecks(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0\u00a0₽"},
		{99, "0,99\u00a0₽"},
		{100, "1\u00a0₽"},
		{15999900, "159\u00a0999\u00a0₽"},
		{-500, "0\u00a0₽"},
	}
	for _, tt := range tests {
		if got := formatKopecks(tt.in); got != tt.want {
			t.Errorf("formatKopecks(%d) = %q, ожидалось %q", tt.in, got, tt.want)
		}
	}
}
