package telegram

import (
	"strings"
	"testing"

	"github.com/Msey/price-tracking-bot/internal/money"
	"github.com/Msey/price-tracking-bot/internal/sites"
	"github.com/Msey/price-tracking-bot/internal/storage"
)

const watchURL = "https://www.ozon.ru/product/2190214590"

func TestParseAlert(t *testing.T) {
	got, err := parseAlert(watchURL)
	if err != nil || got != 0 {
		t.Fatalf("только ссылка: %d %v", got, err)
	}
	got, err = parseAlert("смотри " + watchURL + " норм?")
	if err != nil || got != 0 {
		t.Fatalf("подпись без числа не порог: %d %v", got, err)
	}
	got, err = parseAlert(watchURL + " 15000")
	if err != nil || got != 1_500_000 {
		t.Fatalf("ссылка и порог: %d %v", got, err)
	}
	got, err = parseAlert("15000 " + watchURL)
	if err != nil || got != 1_500_000 {
		t.Fatalf("порог перед ссылкой: %d %v", got, err)
	}
	got, err = parseAlert(watchURL + " 15 000 ₽")
	if err != nil || got != 1_500_000 {
		t.Fatalf("пробелы и знак: %d %v", got, err)
	}
	if _, err := parseAlert(watchURL + " цена 15000"); err == nil {
		t.Fatal("лишний текст с цифрами должен быть ошибкой")
	}
	if _, err := parseAlert(watchURL + " 0"); err == nil {
		t.Fatal("нулевой порог")
	}
}

func TestAddReplyMentionsAlert(t *testing.T) {
	p := storage.Product{URL: watchURL, Name: "Товар"}
	ref := sites.Ref{Site: sites.Ozon, URL: watchURL}
	got := addReply(p, ref, "moscow", true, 1_500_000)
	if !strings.Contains(got, "Порог") || !strings.Contains(got, money.FormatKopecks(1_500_000)) {
		t.Fatalf("новая подписка с порогом: %s", got)
	}
	got = addReply(p, ref, "moscow", false, 1_500_000)
	if !strings.Contains(got, "уже в списке") || !strings.Contains(got, "Порог обновил") {
		t.Fatalf("повтор с порогом: %s", got)
	}
	got = addReply(p, ref, "moscow", true, 0)
	if strings.Contains(got, "Порог") {
		t.Fatalf("без порога не должно быть строки: %s", got)
	}
}

func TestDescribePriceShowsAlert(t *testing.T) {
	got := describePrice(storage.Tracked{AlertKopecks: 1_500_000})
	if !strings.Contains(got, "ещё не проверялась") || !strings.Contains(got, "порог") {
		t.Fatalf("порог без цены: %q", got)
	}
}
