package fetch

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewBrowserUsesAbsoluteProfile(t *testing.T) {
	b := NewBrowser(BrowserOptions{ProfileDir: "data/chrome-plain"})
	if !filepath.IsAbs(b.profileDir) {
		t.Fatalf("профиль должен быть абсолютным, получено %q", b.profileDir)
	}
	if !strings.Contains(filepath.ToSlash(b.profileDir), "data/chrome-plain") {
		t.Fatalf("профиль %q", b.profileDir)
	}
}

func TestDecodeProcessOutputCP1251(t *testing.T) {
	raw := "chrome failed to start:\n" + string([]byte{
		0xce, 0xea, 0xed, 0xee, 0x20, 0xe8, 0xeb, 0xe8, 0x20, 0xe2, 0xea, 0xeb, 0xe0, 0xe4, 0xea, 0xe0,
		0x20, 0xee, 0xf2, 0xea, 0xf0, 0xee, 0xfe, 0xf2, 0xf1, 0xff, 0x20, 0xe2, 0x20, 0xf2, 0xe5, 0xea,
		0xf3, 0xf9, 0xe5, 0xec, 0x20, 0xf1, 0xe5, 0xe0, 0xed, 0xf1, 0xe5, 0x20, 0xe1, 0xf0, 0xe0, 0xf3,
		0xe7, 0xe5, 0xf0, 0xe0, 0x2e,
	})
	got := decodeProcessOutput(raw)
	if !strings.Contains(got, "текущем сеансе") {
		t.Fatalf("не раскодировали вывод Chrome: %q", got)
	}
	if !looksLikeExistingSession(got) {
		t.Fatal("не узнали чужой сеанс Chrome")
	}
}

func TestMarkChromeExitedCleanly(t *testing.T) {
	dir := t.TempDir()
	pref := filepath.Join(dir, "Default")
	if err := os.MkdirAll(pref, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"profile":{"exited_cleanly":false,"exit_type":"Crashed"}}`)
	if err := os.WriteFile(filepath.Join(dir, "Local State"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pref, "Preferences"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	markChromeExitedCleanly(dir)
	got, err := os.ReadFile(filepath.Join(dir, "Local State"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if strings.Contains(s, `"exited_cleanly":false`) || strings.Contains(s, `"exit_type":"Crashed"`) {
		t.Fatalf("не почистили краш: %s", s)
	}
	if !strings.Contains(s, `"exited_cleanly":true`) || !strings.Contains(s, `"exit_type":"Normal"`) {
		t.Fatalf("ожидался чистый выход: %s", s)
	}
}

func TestSameShopURL(t *testing.T) {
	ozon := "https://www.ozon.ru/product/germetik-2422341064"
	if !sameShopURL(ozon, "https://www.ozon.ru/product/germetik-2422341064/?_bctx=1") {
		t.Fatal("ozon с query")
	}
	if !sameShopURL(ozon, "https://ozon.ru/product/other-123") {
		t.Fatal("другой товар ozon в той же вкладке")
	}
	market := "https://market.yandex.ru/card/begovaya-dorozhka-sportflag-glow-run-a/4638722913"
	if !sameShopURL(market, "https://market.yandex.ru/card/begovaya-dorozhka-sportflag-glow-run-a/4638722913?nid=1") {
		t.Fatal("market с query")
	}
	if sameShopURL(ozon, "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/") {
		t.Fatal("чужой магазин")
	}
	if sameShopURL(ozon, "about:blank") {
		t.Fatal("about:blank")
	}
}

func TestManifestAllowsLocalhostAnyPort(t *testing.T) {
	raw, err := extFS.ReadFile("ext/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"http://127.0.0.1:*/*"`) {
		t.Fatalf("нужен порт * для локального HTTP, иначе Chrome не пустит :18732: %s", s)
	}
	if strings.Contains(s, `"http://127.0.0.1/*"`) {
		t.Fatal("http://127.0.0.1/* совпадает только с портом 80")
	}
}

func TestReadOrCreateTokenPersists(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ext.token")
	a, err := readOrCreateToken(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) < 16 {
		t.Fatalf("короткий токен %q", a)
	}
	b, err := readOrCreateToken(p)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("токен не сохранился: %q vs %q", a, b)
	}
}

func TestStampManifestBumpsVersion(t *testing.T) {
	dir := t.TempDir()
	src, err := extFS.ReadFile("ext/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(p, src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := stampManifest(dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), `"version": "1.0.0"`) {
		t.Fatalf("версия не сменилась: %s", got)
	}
	if !strings.Contains(string(got), `"http://127.0.0.1:*/*"`) {
		t.Fatal("потеряли host_permissions")
	}
}

func TestWaitJobSendsCloseWhenIdle(t *testing.T) {
	b := newTestBrowser(t)
	b.closeReq.Store(true)
	got := waitJobJSON(t, b, 2*time.Second)
	if got["action"] != "close" {
		t.Fatalf("ожидался action=close, получено %v", got)
	}
}

func TestWaitJobPrefersNewURLOverClose(t *testing.T) {
	b := newTestBrowser(t)
	b.closeReq.Store(true)
	b.job.Store(&extJob{
		url:  "https://www.ozon.ru/product/1",
		site: "ozon",
		bits: make(chan pageBits, 1),
	})
	got := waitJobJSON(t, b, 2*time.Second)
	if got["action"] == "close" {
		t.Fatal("новая карточка важнее закрытия")
	}
	if got["url"] != "https://www.ozon.ru/product/1" {
		t.Fatalf("url %v", got)
	}
}

func newTestBrowser(t *testing.T) *Browser {
	t.Helper()
	b := NewBrowser(BrowserOptions{ProfileDir: t.TempDir()})
	b.token = "tokentokentoken1"
	if err := b.ensureServerLocked(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	return b
}

func waitJobJSON(t *testing.T, b *Browser, timeout time.Duration) map[string]string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+b.addr+"/ext/wait-job", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+b.token)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req = req.WithContext(ctx)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	var got map[string]string
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got
}
