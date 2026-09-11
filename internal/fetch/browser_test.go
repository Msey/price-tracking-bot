package fetch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewBrowserUsesAbsoluteProfile(t *testing.T) {
	b := NewBrowser(BrowserOptions{ProfileDir: "data/chrome-profile"})
	if !filepath.IsAbs(b.profileDir) {
		t.Fatalf("профиль должен быть абсолютным, получено %q", b.profileDir)
	}
	if !strings.Contains(filepath.ToSlash(b.profileDir), "data/chrome-profile") {
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

func TestHideChromeWindowsDoesNotPanic(t *testing.T) {
	hideChromeWindows()
	showChromeWindows()
}
