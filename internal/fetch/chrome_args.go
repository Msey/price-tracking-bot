package fetch

import (
	"os"
	"path/filepath"
	"strings"
)

// shoppingChromeArgs — флаги обычного Chrome для карточек магазинов.
// Здесь не должно быть remote-debugging-pipe/port: Ozon и DNS считают
// такой процесс ботом (заглушка «нет соединения», QRATOR 403).
func shoppingChromeArgs(profile, extDir, extID, startURL string) []string {
	if startURL == "" {
		startURL = "about:blank"
	}
	args := []string{
		"--user-data-dir=" + profile,
		"--enable-unsafe-extension-debugging",
		"--disable-features=DisableLoadExtensionCommandLineSwitch",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-popup-blocking",
		"--hide-crash-restore-bubble",
	}
	if extDir != "" {
		args = append(args, "--load-extension="+extDir)
	}
	if extID != "" {
		args = append(args, "--disable-extensions-except="+extID)
	}
	return append(args, startURL)
}

func installChromeArgs(profile string) []string {
	return []string{
		"--user-data-dir=" + profile,
		"--remote-debugging-pipe",
		"--enable-unsafe-extension-debugging",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-popup-blocking",
		"--hide-crash-restore-bubble",
		"--window-position=-2400,-2400",
		"--window-size=800,600",
		"about:blank",
	}
}

func hasRemoteDebugging(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "--remote-debugging") {
			return true
		}
	}
	return false
}

func profileMentionsExtension(profile, extDir string) bool {
	if profile == "" {
		return false
	}
	_ = extDir
	for _, rel := range []string{
		filepath.Join("Default", "Preferences"),
		filepath.Join("Default", "Secure Preferences"),
	} {
		raw, err := os.ReadFile(filepath.Join(profile, rel))
		if err != nil {
			continue
		}
		s := string(raw)
		if strings.Contains(s, "Price tracking helper") || strings.Contains(s, "chrome-ext") {
			return true
		}
	}
	return false
}

func needsPipeInstall(exe string) bool {
	low := strings.ToLower(exe)
	return !strings.Contains(low, "chrome-for-testing") && !strings.Contains(low, "chromium")
}
