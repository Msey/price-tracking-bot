package fetch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
}

type chromeUI struct {
	reveal func(context.Context) error
	hide   func(context.Context) error
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

func (b *Browser) do(timeout time.Duration, setup []chromedp.Action, extractJS string, parse func(pageBits) (Snapshot, error)) (Snapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	snap, err := b.doLocked(timeout, setup, extractJS, parse)
	if err != nil && isChromeStartError(err) {
		b.log.Warn("chrome перезапуск после сбоя", "error", err)
		b.stopLocked()
		return b.doLocked(timeout, setup, extractJS, parse)
	}
	return snap, err
}

func (b *Browser) doLocked(timeout time.Duration, setup []chromedp.Action, extractJS string, parse func(pageBits) (Snapshot, error)) (Snapshot, error) {
	if err := b.ensureLocked(); err != nil {
		return Snapshot{}, err
	}

	runCtx, cancel := context.WithTimeout(b.browser, timeout+captchaWait+15*time.Second)
	defer cancel()

	if err := chromedp.Run(runCtx, setup...); err != nil {
		return Snapshot{}, err
	}
	return waitForExtract(runCtx, timeout, extractJS, parse, b.ui())
}

func (b *Browser) ensureLocked() error {
	if b.browser != nil {
		return nil
	}
	if b.profileDir != "" {
		if err := os.MkdirAll(b.profileDir, 0o755); err != nil {
			return fmt.Errorf("chrome: профиль %s: %w", b.profileDir, err)
		}
		killChromeWithProfile(b.profileDir)
		clearStaleProfileLocks(b.profileDir)
	}

	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.Flag("headless", b.headless),
		chromedp.Flag("start-minimized", !b.headless),
		chromedp.UserAgent(chromeUA),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("mute-audio", true),
		chromedp.WindowSize(1280, 900),
	)
	if b.profileDir != "" {
		opts = append(opts, chromedp.UserDataDir(b.profileDir))
	}
	if b.chromePath != "" {
		opts = append(opts, chromedp.ExecPath(b.chromePath))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx, chromedp.WithLogf(func(string, ...any) {}))
	if err := chromedp.Run(browserCtx); err != nil {
		allocCancel()
		browserCancel()
		return fmt.Errorf("chrome: запуск: %w", chromeStartError(b.profileDir, err))
	}

	b.allocCancel = allocCancel
	b.browser = browserCtx
	b.browserCancel = browserCancel
	if !b.headless {
		_ = chromedp.Run(browserCtx, minimizeChromeWindow())
	}
	b.log.Info("chrome запущен", "headless", b.headless, "minimized", !b.headless, "profile", b.profileDir, "exe", b.chromePath)
	return nil
}

func (b *Browser) ui() *chromeUI {
	if b.headless {
		return nil
	}
	return &chromeUI{
		reveal: func(ctx context.Context) error {
			return chromedp.Run(ctx, showChromeWindow())
		},
		hide: func(ctx context.Context) error {
			return chromedp.Run(ctx, minimizeChromeWindow())
		},
		notify: b.noteChallenge,
	}
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
