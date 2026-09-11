package fetch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
