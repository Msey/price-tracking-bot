package fetch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// Browser — один долгоживущий Chrome на все магазины.
type Browser struct {
	mu            sync.Mutex
	log           *slog.Logger
	profileDir    string
	chromePath    string
	headless      bool
	allocCancel   context.CancelFunc
	browser       context.Context
	browserCancel context.CancelFunc
	onChallenge   func(string)
	// wantHidden — окно Chrome должно быть снято с панели задач. Снимается
	// только на время интерактивной капчи.
	wantHidden atomic.Bool
	hideStop   chan struct{}
}

type chromeUI struct {
	reveal func(context.Context) error
	// hide сворачивает окно по своему контексту: прятать Chrome нужно и после
	// того, как время на капчу вышло и контекст страницы уже погас.
	hide   func() error
	notify func(pageBits)
}

type BrowserOptions struct {
	ProfileDir string
	ChromePath string
	Headless   bool
	Log        *slog.Logger
}

func NewBrowser(opt BrowserOptions) *Browser {
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	profile := strings.TrimSpace(opt.ProfileDir)
	if profile != "" {
		if abs, err := filepath.Abs(profile); err == nil {
			profile = abs
		}
	}
	return &Browser{
		log:        opt.Log,
		profileDir: profile,
		chromePath: resolveChromePath(opt.ChromePath),
		headless:   opt.Headless,
	}
}

// SetOnChallenge вызывает fn, когда свёрнутый Chrome разворачивают из‑за капчи.
func (b *Browser) SetOnChallenge(fn func(string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onChallenge = fn
}

func (b *Browser) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopLocked()
}

func (b *Browser) stopLocked() {
	b.stopHideWatchLocked()
	if b.browserCancel != nil {
		b.browserCancel()
		b.browserCancel = nil
	}
	if b.allocCancel != nil {
		b.allocCancel()
		b.allocCancel = nil
	}
	b.browser = nil
}

func (b *Browser) do(ctx context.Context, timeout time.Duration, setup []chromedp.Action, extractJS string, parse func(pageBits) (Snapshot, error)) (Snapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	snap, err := b.doLocked(ctx, timeout, setup, extractJS, parse)
	if err != nil && isChromeStartError(err) && ctx.Err() == nil {
		b.log.Warn("chrome перезапуск после сбоя", "error", err)
		b.stopLocked()
		return b.doLocked(ctx, timeout, setup, extractJS, parse)
	}
	return snap, err
}

func (b *Browser) doLocked(ctx context.Context, timeout time.Duration, setup []chromedp.Action, extractJS string, parse func(pageBits) (Snapshot, error)) (Snapshot, error) {
	if err := b.ensureLocked(); err != nil {
		return Snapshot{}, err
	}

	runCtx, cancel := context.WithTimeout(b.browser, timeout+captchaWait+15*time.Second)
	defer cancel()
	// Контекст страницы растёт из контекста Chrome, а не из ctx вызывающего:
	// иначе выход из приложения убил бы весь браузер. Отмену пробрасываем
	// сторожем, чтобы остановка бота не ждала всю страницу с капчей.
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		select {
		case <-ctx.Done():
			cancel()
		case <-watchDone:
		}
	}()

	if err := chromedp.Run(runCtx, setup...); err != nil {
		return Snapshot{}, err
	}
	return waitForExtract(runCtx, timeout, extractJS, parse, b.ui())
}

func (b *Browser) ensureLocked() error {
	if b.browser != nil {
		if b.browser.Err() == nil {
			return nil
		}
		// Chrome упал или пользователь закрыл окно, в котором проходил капчу.
		// chromedp гасит контекст навсегда, поэтому браузер нужно поднять
		// заново — иначе все проверки молча падают до перезапуска бота.
		b.log.Warn("chrome отключился, поднимаем заново", "error", b.browser.Err())
		b.stopLocked()
	}
	if b.profileDir != "" {
		if err := os.MkdirAll(b.profileDir, 0o755); err != nil {
			return fmt.Errorf("chrome: профиль %s: %w", b.profileDir, err)
		}
		killChromeWithProfile(b.profileDir)
		clearStaleProfileLocks(b.profileDir)
		markChromeExitedCleanly(b.profileDir)
	}

	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts, chromeLaunchFlags(b.headless)...)
	if b.profileDir != "" {
		opts = append(opts, chromedp.UserDataDir(b.profileDir))
	}
	if b.chromePath != "" {
		opts = append(opts, chromedp.ExecPath(b.chromePath))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx, chromedp.WithLogf(func(string, ...any) {}))
	b.wantHidden.Store(!b.headless)
	if !b.headless {
		b.startHideWatchLocked()
	}
	if err := chromedp.Run(browserCtx); err != nil {
		b.stopHideWatchLocked()
		allocCancel()
		browserCancel()
		return fmt.Errorf("chrome: запуск: %w", chromeStartError(b.profileDir, err))
	}

	b.allocCancel = allocCancel
	b.browser = browserCtx
	b.browserCancel = browserCancel
	if !b.headless {
		_ = chromedp.Run(browserCtx, minimizeChromeWindow())
		hideChromeWindows()
	}
	b.log.Info("chrome запущен", "headless", b.headless, "hidden", !b.headless, "profile", b.profileDir, "exe", b.chromePath)
	return nil
}

// chromeLaunchFlags перекрывает DefaultExecAllocatorOptions: там стоит
// enable-automation=true, из‑за него жёлтая полоса «браузером управляет
// автоматизированное тестовое ПО». false в карте chromedp просто не
// передаёт флаг; exclude-switches убирает его, если Chrome добавил сам
// из‑за remote-debugging-port.
func chromeLaunchFlags(headless bool) []chromedp.ExecAllocatorOption {
	return []chromedp.ExecAllocatorOption{
		chromedp.Flag("headless", headless),
		chromedp.Flag("start-minimized", !headless),
		chromedp.UserAgent(chromeUA),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("enable-automation", false),
		chromedp.Flag("exclude-switches", "enable-automation"),
		chromedp.Flag("hide-crash-restore-bubble", true),
		chromedp.Flag("disable-session-crashed-bubble", true),
		chromedp.Flag("mute-audio", true),
		chromedp.WindowSize(1280, 900),
	}
}

func (b *Browser) ui() *chromeUI {
	if b.headless {
		return nil
	}
	return &chromeUI{
		reveal: func(ctx context.Context) error {
			b.wantHidden.Store(false)
			showChromeWindows()
			return chromedp.Run(ctx, showChromeWindow())
		},
		hide: func() error {
			b.wantHidden.Store(true)
			if b.browser == nil || b.browser.Err() != nil {
				hideChromeWindows()
				return nil
			}
			ctx, cancel := context.WithTimeout(b.browser, 5*time.Second)
			defer cancel()
			err := chromedp.Run(ctx, minimizeChromeWindow())
			hideChromeWindows()
			return err
		},
		notify: b.noteChallenge,
	}
}

func (b *Browser) startHideWatchLocked() {
	if b.hideStop != nil {
		return
	}
	stop := make(chan struct{})
	b.hideStop = stop
	go func() {
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		n := 0
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if b.wantHidden.Load() {
					hideChromeWindows()
				}
				n++
				if n == 20 {
					ticker.Reset(time.Second)
				}
			}
		}
	}()
}

func (b *Browser) stopHideWatchLocked() {
	if b.hideStop == nil {
		return
	}
	close(b.hideStop)
	b.hideStop = nil
}

func (b *Browser) noteChallenge(bits pageBits) {
	title := strings.TrimSpace(bits.Title)
	msg := "Нужно пройти капчу в окне Chrome"
	if title != "" {
		msg += " · " + title
	}
	if b.onChallenge != nil {
		b.onChallenge(msg)
	}
	b.log.Warn("показана капча, окно Chrome развёрнуто", "title", title)
}

func hideWebdriver() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(
			`Object.defineProperty(navigator, 'webdriver', {get: () => undefined});`,
		).Do(ctx)
		return err
	})
}

func pageSetup() []chromedp.Action {
	return []chromedp.Action{
		network.Enable(),
		network.SetBlockedURLs([]string{
			"*.woff", "*.woff2", "*.ttf", "*.mp4", "*.webm",
		}),
		hideWebdriver(),
	}
}

func showChromeWindow() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		_ = page.BringToFront().Do(ctx)
		id, _, err := browser.GetWindowForTarget().Do(ctx)
		if err != nil {
			return nil
		}
		return browser.SetWindowBounds(id, &browser.Bounds{
			Left:        80,
			Top:         80,
			Width:       1280,
			Height:      900,
			WindowState: browser.WindowStateNormal,
		}).Do(ctx)
	})
}

func minimizeChromeWindow() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		id, _, err := browser.GetWindowForTarget().Do(ctx)
		if err != nil {
			return nil
		}
		return browser.SetWindowBounds(id, &browser.Bounds{
			WindowState: browser.WindowStateMinimized,
		}).Do(ctx)
	})
}

func clearStaleProfileLocks(dir string) {
	for _, name := range []string{
		"SingletonLock", "SingletonSocket", "SingletonCookie",
		"lockfile", "DevToolsActivePort",
	} {
		_ = os.Remove(filepath.Join(dir, name))
	}
}

// markChromeExitedCleanly убирает «сессия завершена некорректно»: иначе после
// Stop-Process Chrome показывает диалог восстановления и вылезает на экран.
func markChromeExitedCleanly(dir string) {
	if dir == "" {
		return
	}
	repl := strings.NewReplacer(
		`"exited_cleanly":false`, `"exited_cleanly":true`,
		`"exited_cleanly": false`, `"exited_cleanly": true`,
		`"exit_type":"Crashed"`, `"exit_type":"Normal"`,
		`"exit_type": "Crashed"`, `"exit_type": "Normal"`,
	)
	for _, rel := range []string{"Local State", filepath.Join("Default", "Preferences")} {
		p := filepath.Join(dir, rel)
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		n := repl.Replace(string(b))
		if n != string(b) {
			_ = os.WriteFile(p, []byte(n), 0o644)
		}
	}
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
		strings.Contains(msg, "текущем сеансе")
}
