package fetch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShoppingChromeArgsHaveNoDebugger(t *testing.T) {
	args := shoppingChromeArgs(`C:\data\chrome-plain`, `C:\ext\chrome-ext`, "abcdefghijklmnopabcdefghijklmnop", "https://www.ozon.ru/product/1")
	if hasRemoteDebugging(args) {
		t.Fatalf("shopping Chrome не должен быть с CDP: %v", args)
	}
	found := false
	for _, a := range args {
		if strings.HasPrefix(a, "--load-extension=") && strings.Contains(a, "chrome-ext") {
			found = true
		}
	}
	if !found {
		t.Fatalf("ожидался --load-extension: %v", args)
	}
	foundExcept := false
	for _, a := range args {
		if strings.HasPrefix(a, "--disable-extensions-except=") {
			foundExcept = true
		}
	}
	if !foundExcept {
		t.Fatalf("ожидался --disable-extensions-except: %v", args)
	}
}

func TestInstallChromeArgsStayOnBlank(t *testing.T) {
	args := installChromeArgs(`C:\data\chrome-plain`)
	if args[len(args)-1] != "about:blank" {
		t.Fatalf("установка не должна открывать магазин: %v", args)
	}
	for _, a := range args {
		if strings.Contains(a, "ozon.") || strings.Contains(a, "dns-shop") || strings.Contains(a, "market.yandex") {
			t.Fatalf("установка открыла магазин: %s", a)
		}
	}
}

func TestProfileMentionsExtension(t *testing.T) {
	dir := t.TempDir()
	pref := filepath.Join(dir, "Default")
	if err := os.MkdirAll(pref, 0o755); err != nil {
		t.Fatal(err)
	}
	ext := `C:\Users\v.s\AppData\Local\price-tracking-bot\chrome-ext`
	if profileMentionsExtension(dir, ext) {
		t.Fatal("пустой профиль не содержит расширение")
	}
	raw := []byte(`{"extensions":{"settings":{"id":{"path":"C:\\Users\\v.s\\AppData\\Local\\price-tracking-bot\\chrome-ext","manifest":{"name":"Price tracking helper"}}}}}`)
	if err := os.WriteFile(filepath.Join(pref, "Preferences"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if !profileMentionsExtension(dir, ext) {
		t.Fatal("не нашли расширение в Preferences")
	}
}

func TestProfileMentionsExtensionByPath(t *testing.T) {
	dir := t.TempDir()
	pref := filepath.Join(dir, "Default")
	if err := os.MkdirAll(pref, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"extensions":{"settings":{"nilcobfbngolcahajdaeaonajfedkmib":{"path":"C:\\Users\\me\\AppData\\Local\\price-tracking-bot\\chrome-ext"}}}}`)
	if err := os.WriteFile(filepath.Join(pref, "Secure Preferences"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if !profileMentionsExtension(dir, "") {
		t.Fatal("не нашли путь chrome-ext в Secure Preferences")
	}
}

func TestNeedsPipeInstall(t *testing.T) {
	if !needsPipeInstall(`C:\Program Files\Google\Chrome\Application\chrome.exe`) {
		t.Fatal("обычный Chrome не грузит --load-extension")
	}
	if needsPipeInstall(`C:\Users\me\AppData\Local\price-tracking-bot\chrome-for-testing\chrome-win64\chrome.exe`) {
		t.Fatal("Chrome for Testing грузит --load-extension без pipe")
	}
}
