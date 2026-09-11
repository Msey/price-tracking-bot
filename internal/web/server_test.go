package web

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

func testStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestIndexEmpty(t *testing.T) {
	srv := New(testStore(t), "127.0.0.1:0", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Заявок пока нет") {
		t.Fatal("ожидался пустой экран")
	}
	if !strings.Contains(rec.Body.String(), "заявок") {
		t.Fatal("для нуля ожидалась форма «заявок»")
	}
}

func TestIndexListsRequests(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	p, _, err := store.AddSubscription(ctx, 1001, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSnapshot(ctx, p.ID, "Honor MagicBook", 15999900, "RUB", true); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	New(store, "127.0.0.1:0", nil).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Honor MagicBook", "DNS", "Москва", "1001", "отслеживается", "159\u00a0999\u00a0₽", ">заявка<", ">товар<", ">пользователь<", `class="shop-icon"`, "data:image/png;base64,"} {
		if !strings.Contains(body, want) {
			t.Errorf("в странице нет %q", want)
		}
	}
}

func TestIndexShowsYandexMarketIcon(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	if _, _, err := store.AddSubscription(ctx, 1001, "yandex_market", "4638722913", "https://market.yandex.ru/card/4638722913", "moscow"); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	New(store, "127.0.0.1:0", nil).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "Яндекс.Маркет") {
		t.Fatal("нет названия магазина")
	}
	if !strings.Contains(body, `class="shop-icon"`) || !strings.Contains(body, "data:image/png;base64,") {
		t.Fatal("нет иконки Яндекс.Маркета")
	}
}

func TestIndexNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	New(testStore(t), "127.0.0.1:0", nil).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d", rec.Code)
	}
}

func TestRuPlural(t *testing.T) {
	cases := map[int]string{0: "заявок", 1: "заявка", 2: "заявки", 5: "заявок", 11: "заявок", 21: "заявка", 22: "заявки"}
	for n, want := range cases {
		if got := ruPlural(n, "заявка", "заявки", "заявок"); got != want {
			t.Errorf("%d: %s, ожидалось %s", n, got, want)
		}
	}
}

func TestStatusOf(t *testing.T) {
	got, class := statusOf(storage.Request{})
	if got != "ожидает проверку" || class != "wait" {
		t.Errorf("пусто: %s / %s", got, class)
	}

	got, class = statusOf(storage.Request{LastErrorKind: sql.NullString{String: "challenge", Valid: true}})
	if got != "ошибка загрузки" || class != "bad" {
		t.Errorf("ошибка без проверки: %s / %s", got, class)
	}

	got, class = statusOf(storage.Request{
		LastCheckedAt: sql.NullString{String: "2026-01-02 03:04:05", Valid: true},
		LastAvailable: sql.NullInt64{Int64: 0, Valid: true},
	})
	if got != "нет в наличии" || class != "bad" {
		t.Errorf("oos: %s / %s", got, class)
	}

	got, class = statusOf(storage.Request{
		LastCheckedAt: sql.NullString{String: "2026-01-02 03:04:05", Valid: true},
		LastErrorAt:   sql.NullString{String: "2026-01-02 04:00:00", Valid: true},
	})
	if got != "ошибка после проверки" || class != "bad" {
		t.Errorf("ошибка после проверки: %s / %s", got, class)
	}
}
