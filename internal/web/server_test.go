package web

import (
	"context"
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

// localRequest — запрос от браузера на локальной машине. Handler смотрит на
// Host, поэтому в тестах он тоже должен быть loopback.
func localRequest(method, target string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.Host = "127.0.0.1:8080"
	return r
}

func TestIndexEmpty(t *testing.T) {
	srv := New(testStore(t), "127.0.0.1:0", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/"))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Заявок пока нет") {
		t.Fatal("ожидался пустой экран")
	}
	if !strings.Contains(rec.Body.String(), "заявок") {
		t.Fatal("для нуля ожидалась форма «заявок»")
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("нет nosniff")
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("нет X-Frame-Options")
	}
}

func TestLoopbackAddr(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080"} {
		if err := loopbackAddr(addr); err != nil {
			t.Fatalf("%s: %v", addr, err)
		}
	}
	for _, addr := range []string{":8080", "0.0.0.0:8080", "192.168.1.10:8080", "example.com:8080"} {
		if err := loopbackAddr(addr); err == nil {
			t.Fatalf("%s должен быть запрещён", addr)
		}
	}
}

func TestIndexListsRequests(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	p, _, err := store.AddSubscription(ctx, 1001, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordSnapshot(ctx, p.ID, "Honor MagicBook", 15999900, "RUB", true); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	New(store, "127.0.0.1:0", nil).Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/"))
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
	New(store, "127.0.0.1:0", nil).Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/"))
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
	New(testStore(t), "127.0.0.1:0", nil).Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/nope"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d", rec.Code)
	}
}

// DNS rebinding: чужое имя, разрешённое в 127.0.0.1, читать заявки не должно.
func TestIndexRejectsForeignHost(t *testing.T) {
	for _, host := range []string{"example.com", "attacker.test:8080", "192.168.1.10:8080"} {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Host = host
		New(testStore(t), "127.0.0.1:0", nil).Handler().ServeHTTP(rec, r)
		if rec.Code != http.StatusForbidden {
			t.Errorf("Host %q: код %d, ожидался 403", host, rec.Code)
		}
	}
}

func TestLoopbackHost(t *testing.T) {
	for _, host := range []string{"127.0.0.1:8080", "localhost:8080", "localhost", "[::1]:8080", "127.0.0.1"} {
		if !loopbackHost(host) {
			t.Errorf("%s должен приниматься", host)
		}
	}
	for _, host := range []string{"", "example.com", "example.com:8080", "10.0.0.5:8080", "bot.local"} {
		if loopbackHost(host) {
			t.Errorf("%s должен отклоняться", host)
		}
	}
}

// Подписи статусов и формы слов проверяются в internal/view: правила общие
// для этой страницы и для окна.
