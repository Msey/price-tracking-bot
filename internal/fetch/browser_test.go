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

func TestMarkChromeExitedCleanlyDisablesSessionRestore(t *testing.T) {
	dir := t.TempDir()
	pref := filepath.Join(dir, "Default")
	if err := os.MkdirAll(pref, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"session":{"restore_on_startup":1},"profile":{"exited_cleanly":true}}`)
	if err := os.WriteFile(filepath.Join(pref, "Preferences"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	markChromeExitedCleanly(dir)
	got, err := os.ReadFile(filepath.Join(pref, "Preferences"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), `"restore_on_startup":1`) {
		t.Fatalf("сессия всё ещё восстанавливается: %s", got)
	}
	if !strings.Contains(string(got), `"restore_on_startup":5`) {
		t.Fatalf("нужен restore_on_startup=5: %s", got)
	}
}

func TestEnsureRestoreNewTabInsertsKey(t *testing.T) {
	got := ensureRestoreNewTab(`{"profile":{"name":"bot"}}`)
	if !strings.Contains(got, `"restore_on_startup":5`) && !strings.Contains(got, `"restore_on_startup": 5`) {
		t.Fatalf("не вставили restore_on_startup: %s", got)
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
	if !sameShopURL("https://www.ozon.ru/t/WcmKNaP", "https://www.ozon.ru/product/germetik-2422341064") {
		t.Fatal("короткая ozon после редиректа")
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

func TestManifestBlocksHeavyMedia(t *testing.T) {
	raw, err := extFS.ReadFile("ext/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"declarativeNetRequest"`) {
		t.Fatal("нужен declarativeNetRequest, чтобы резать картинки без CDP")
	}
	if !strings.Contains(s, `"path": "rules.json"`) {
		t.Fatal("нет rules.json в манифесте")
	}
	if !strings.Contains(s, `"document_end"`) {
		t.Fatal("съём цены должен начинаться на document_end, не после idle")
	}
	if strings.Contains(s, `"document_idle"`) {
		t.Fatal("document_idle ждёт тяжёлую загрузку")
	}

	rules, err := extFS.ReadFile("ext/rules.json")
	if err != nil {
		t.Fatal(err)
	}
	rs := string(rules)
	for _, want := range []string{`"block"`, `"image"`, `"media"`, `"font"`, "smartcaptcha", "px-cdn.net"} {
		if !strings.Contains(rs, want) {
			t.Errorf("в rules.json нет %s", want)
		}
	}
}

func TestExtractTakesPriceEarly(t *testing.T) {
	raw, err := extFS.ReadFile("ext/extract.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "MutationObserver") {
		t.Fatal("нужен MutationObserver, чтобы поймать узел цены сразу")
	}
	if strings.Contains(s, "document.documentElement.innerHTML") {
		t.Fatal("полный innerHTML снова сериализует карточку")
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
	if !strings.Contains(string(got), `"path": "rules.json"`) && !strings.Contains(string(got), `"path":"rules.json"`) {
		t.Fatal("потеряли rules.json")
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

func TestPingRequiresAuth(t *testing.T) {
	b := newTestBrowser(t)
	res, err := http.Get("http://" + b.addr + "/ext/ping")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("без токена код %d, нужен 401", res.StatusCode)
	}

	req, err := http.NewRequest(http.MethodGet, "http://"+b.addr+"/ext/ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+b.token)
	ok, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	ok.Body.Close()
	if ok.StatusCode != http.StatusNoContent {
		t.Fatalf("со своим токеном код %d", ok.StatusCode)
	}
}

func TestCORSRejectsWebsiteOrigin(t *testing.T) {
	b := newTestBrowser(t)
	req, err := http.NewRequest(http.MethodOptions, "http://"+b.addr+"/ext/wait-job", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://evil.example")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("сайту отдали CORS Origin %q", got)
	}
	if got := res.Header.Get("Access-Control-Allow-Private-Network"); got != "" {
		t.Fatalf("сайту отдали Private-Network %q", got)
	}
}

func TestCORSAllowsExtensionOrigin(t *testing.T) {
	b := newTestBrowser(t)
	origin := "chrome-extension://abcdefghijklmnopqrstuvwxyzabcdef"
	req, err := http.NewRequest(http.MethodOptions, "http://"+b.addr+"/ext/wait-job", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", origin)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("Origin %q", got)
	}
	if got := res.Header.Get("Access-Control-Allow-Private-Network"); got != "true" {
		t.Fatalf("Private-Network %q", got)
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

func TestPushLatestBitsKeepsNewest(t *testing.T) {
	ch := make(chan pageBits, 2)
	pushLatestBits(ch, pageBits{Title: "1"})
	pushLatestBits(ch, pageBits{Title: "2"})
	pushLatestBits(ch, pageBits{Title: "3"})
	var got []string
drain:
	for {
		select {
		case b := <-ch:
			got = append(got, b.Title)
		default:
			break drain
		}
	}
	if len(got) != 2 || got[len(got)-1] != "3" {
		t.Fatalf("снимки %v, последний должен быть 3", got)
	}
	pushLatestBits(nil, pageBits{Title: "x"})
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
