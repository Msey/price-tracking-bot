package telegram

import (
	"fmt"
	"strings"
	"testing"
	"time"

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

func TestHumanDuration(t *testing.T) {
	if got := humanDuration(time.Hour); got != "час" {
		t.Errorf("час: %q", got)
	}
	if got := humanDuration(24 * time.Hour); got != "сутки" {
		t.Errorf("сутки: %q", got)
	}
	if got := humanDuration(20 * time.Minute); got != "20 мин" {
		t.Errorf("минуты: %q", got)
	}
}

func TestHelpTextIsForUsers(t *testing.T) {
	for _, want := range []string{"/list", "/del", "DNS", "Ozon"} {
		if !strings.Contains(helpText, want) {
			t.Errorf("в /help нет %q:\n%s", want, helpText)
		}
	}
	for _, extra := range []string{"банит", "пауза", "Wildberries", "сутки", "час", "/help"} {
		if strings.Contains(helpText, extra) {
			t.Errorf("в /help лишнее %q:\n%s", extra, helpText)
		}
	}
}
