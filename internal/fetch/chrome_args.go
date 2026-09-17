package fetch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	chromeWindowW = 1280
	chromeWindowH = 900
)

// shoppingChromeArgs — флаги обычного Chrome для карточек магазинов.
// Здесь не должно быть remote-debugging-pipe/port: Ozon и DNS считают
// такой процесс ботом (заглушка «нет соединения», QRATOR 403).
//
// Адреса карточки среди флагов нет: её открывает расширение по задаче
// от бота. URL в argv живому Chrome этого профиля добавлял бы ещё вкладку.
//
// background — замер цены: окно за краем экрана и без активации, чтобы
// не перехватывать клавиатуру. По щелчку в списке background=false.
func shoppingChromeArgs(profile, extDir, extID string, background bool) []string {
	args := []string{
		"--user-data-dir=" + profile,
		"--enable-unsafe-extension-debugging",
		"--disable-features=DisableLoadExtensionCommandLineSwitch",
		"--no-first-run",
		"--no-default-browser-check",
		"--hide-crash-restore-bubble",
	}
	if background {
		args = append(args, offscreenChromeArgs()...)
		args = append(args,
			"--disable-backgrounding-occluded-windows",
			"--disable-renderer-backgrounding",
			"--disable-background-timer-throttling",
		)
	}
	if extDir != "" {
		args = append(args, "--load-extension="+extDir)
	}
	if extID != "" {
		args = append(args, "--disable-extensions-except="+extID)
	}
	return append(args, "about:blank")
}

func installChromeArgs(profile string) []string {
	args := []string{
		"--user-data-dir=" + profile,
		"--remote-debugging-pipe",
		"--enable-unsafe-extension-debugging",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-popup-blocking",
		"--hide-crash-restore-bubble",
	}
	args = append(args, offscreenChromeArgs()...)
	return append(args, "about:blank")
}

func offscreenChromeArgs() []string {
	x, y := offscreenOrigin()
	return []string{
		fmt.Sprintf("--window-position=%d,%d", x, y),
		fmt.Sprintf("--window-size=%d,%d", chromeWindowW, chromeWindowH),
	}
}

func offscreenOrigin() (int, int) {
	return offscreenOriginOS()
}

func hasRemoteDebugging(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "--remote-debugging") {
			return true
		}
	}
	return false
}

// profileMentionsExtension — расширение уже прописано в профиле. Имя
// каталога здесь не нужно: в Preferences лежит имя из manifest.json.
func profileMentionsExtension(profile string) bool {
	if profile == "" {
		return false
	}
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
