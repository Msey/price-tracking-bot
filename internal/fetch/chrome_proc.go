package fetch

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// chromeProc — процесс Chrome с профилем бота. Профиль отдельный: в личном
// профиле пользователя бот открывал бы вкладки поверх его работы, а
// признаки падения и режим восстановления сессии приходится править.
type chromeProc struct {
	chromePath string
	profileDir string
	cmd        *exec.Cmd
	// chromeDead — процесс уже вышел. Ставится из горутины ожидания.
	chromeDead atomic.Bool
	// wantFocus — окно нужно человеку (щелчок по строке или капча).
	// Тогда фоновый сдвиг за экран останавливается.
	wantFocus atomic.Bool
}

func (c *chromeProc) alive() bool {
	return c.cmd != nil && c.cmd.Process != nil && !c.chromeDead.Load()
}

// start чистит следы прошлого запуска и поднимает Chrome с расширением.
// Адреса карточки среди флагов нет: её открывает расширение.
func (c *chromeProc) start(extDir, exceptID string, background bool) error {
	c.wantFocus.Store(!background)
	clearStaleProfileLocks(c.profileDir)
	markChromeExitedCleanly(c.profileDir)
	bustExtensionCache(c.profileDir)
	if background {
		stashOffscreenPlacement(c.profileDir)
	}

	cmd, err := startChrome(c.chromePath, c.profileDir, extDir, exceptID, background)
	if err != nil {
		return fmt.Errorf("chrome: запуск: %w", err)
	}
	c.cmd = cmd
	c.chromeDead.Store(false)
	go func() {
		_ = cmd.Wait()
		c.chromeDead.Store(true)
	}()
	if background && cmd.Process != nil {
		pid := uint32(cmd.Process.Pid)
		go c.holdBackground(pid)
	}
	return nil
}

func (c *chromeProc) holdBackground(pid uint32) {
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if c.wantFocus.Load() {
			return
		}
		demoteChrome(pid, c.profileDir, 0)
		time.Sleep(150 * time.Millisecond)
	}
}

func (c *chromeProc) reveal() {
	c.wantFocus.Store(true)
	revealChromeWithProfile(c.profileDir)
}

func (c *chromeProc) demote() {
	if c.wantFocus.Load() {
		return
	}
	demoteChromeWithProfile(c.profileDir)
}

// kill гасит все процессы Chrome этого профиля. Своего pid недостаточно:
// у Chrome он часто сразу выходит, а окно живёт в другом процессе профиля —
// тогда вкладки копятся в уже открытом окне.
func (c *chromeProc) kill() {
	if c.profileDir != "" {
		killChromeWithProfile(c.profileDir)
	}
	c.cmd = nil
}

func clearStaleProfileLocks(dir string) {
	for _, name := range []string{
		"SingletonLock", "SingletonSocket", "SingletonCookie",
		"lockfile", "DevToolsActivePort",
	} {
		_ = os.Remove(filepath.Join(dir, name))
	}
}

// bustExtensionCache — иначе Chrome поднимает прежний service worker
// расширения, со старым адресом локального сервера.
func bustExtensionCache(profile string) {
	if profile == "" {
		return
	}
	for _, rel := range []string{
		filepath.Join("Default", "Service Worker"),
		filepath.Join("Default", "Extension State"),
	} {
		_ = os.RemoveAll(filepath.Join(profile, rel))
	}
}

// markChromeExitedCleanly убирает у профиля признаки аварийного выхода,
// иначе Chrome при каждом запуске показывает «Восстановить страницы?».
// Правятся только нужные ключи разобранного JSON: замена по всему тексту
// файла могла попасть в чужое значение и испортить профиль.
func markChromeExitedCleanly(dir string) {
	if dir == "" {
		return
	}
	for _, f := range []struct {
		rel     string
		session bool
	}{
		{rel: "Local State"},
		// restore_on_startup живёт в настройках профиля: без него
		// «продолжить с того места» поднимает все старые вкладки магазинов.
		{rel: filepath.Join("Default", "Preferences"), session: true},
	} {
		p := filepath.Join(dir, f.rel)
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		out, changed := cleanProfileJSON(raw, f.session)
		if !changed {
			continue
		}
		_ = writeFileAtomic(p, out)
	}
}

// cleanProfileJSON правит признаки падения и режим восстановления вкладок.
// Непонятный файл возвращается без изменений: лучше лишний вопрос Chrome,
// чем сломанный профиль.
func cleanProfileJSON(raw []byte, withSession bool) ([]byte, bool) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false
	}
	changed := false
	if prof, ok := m["profile"].(map[string]any); ok {
		changed = setJSONValue(prof, "exit_type", "Normal") || changed
		changed = setJSONValue(prof, "exited_cleanly", true) || changed
	}
	if withSession {
		sess, ok := m["session"].(map[string]any)
		if !ok {
			sess = map[string]any{}
			m["session"] = sess
		}
		// 5 — «новая вкладка»; 1 и 4 поднимают прошлую сессию.
		changed = setJSONValue(sess, "restore_on_startup", float64(5)) || changed
	}
	if !changed {
		return nil, false
	}
	out, err := json.Marshal(m)
	if err != nil {
		return nil, false
	}
	return out, true
}

func setJSONValue(m map[string]any, key string, want any) bool {
	if got, ok := m[key]; ok && got == want {
		return false
	}
	m[key] = want
	return true
}

// writeFileAtomic пишет через временный файл: обрыв на середине не оставит
// профиль Chrome с обрезанным JSON.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// stashOffscreenPlacement пишет в профиль позицию за краем виртуального
// экрана: иначе Chrome поднимает окно там, где его закрыли в прошлый раз.
func stashOffscreenPlacement(dir string) {
	if dir == "" {
		return
	}
	p := filepath.Join(dir, "Default", "Preferences")
	raw, err := os.ReadFile(p)
	if err != nil {
		return
	}
	x, y := offscreenOrigin()
	out, changed := applyOffscreenPlacement(raw, x, y, chromeWindowW, chromeWindowH)
	if !changed {
		return
	}
	_ = writeFileAtomic(p, out)
}

func applyOffscreenPlacement(raw []byte, left, top, width, height int) ([]byte, bool) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false
	}
	browser, ok := m["browser"].(map[string]any)
	if !ok {
		browser = map[string]any{}
		m["browser"] = browser
	}
	place := map[string]any{
		"left":          float64(left),
		"top":           float64(top),
		"right":         float64(left + width),
		"bottom":        float64(top + height),
		"maximized":     false,
		"always_on_top": false,
	}
	if cur, ok := browser["window_placement"].(map[string]any); ok && sameWindowPlacement(cur, place) {
		return nil, false
	}
	browser["window_placement"] = place
	out, err := json.Marshal(m)
	if err != nil {
		return nil, false
	}
	return out, true
}

func sameWindowPlacement(got, want map[string]any) bool {
	for _, key := range []string{"left", "top", "right", "bottom", "maximized", "always_on_top"} {
		if got[key] != want[key] {
			return false
		}
	}
	return true
}

func profileInCommandLine(cmd, profile string) bool {
	if profile == "" || cmd == "" {
		return false
	}
	c := strings.ToLower(strings.ReplaceAll(cmd, `/`, `\`))
	p := strings.ToLower(strings.ReplaceAll(filepath.Clean(profile), `/`, `\`))
	return strings.Contains(c, p)
}

func chromeStartError(profile string, err error) error {
	msg := decodeProcessOutput(err.Error())
	if looksLikeExistingSession(msg) {
		return fmt.Errorf("Chrome открыл вкладку в уже запущенном браузере и не отдал управление. Нужен отдельный профиль %s: %s", profile, msg)
	}
	return fmt.Errorf("%s", msg)
}

func looksLikeExistingSession(msg string) bool {
	low := strings.ToLower(msg)
	return strings.Contains(msg, "текущем сеансе") ||
		strings.Contains(low, "current browser session") ||
		strings.Contains(low, "existing browser")
}

func isChromeStartError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "chrome failed to start") ||
		strings.Contains(msg, "chrome: запуск") ||
		strings.Contains(msg, "расширение не подключилось") ||
		strings.Contains(msg, "установка расширения") ||
		strings.Contains(msg, "текущем сеансе")
}
