package fetch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

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
	return &Browser{
		log:        opt.Log,
		profileDir: opt.ProfileDir,
		chromePath: opt.ChromePath,
		headless:   opt.Headless,
	}
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

	if err := b.ensureLocked(); err != nil {
		return Snapshot{}, err
	}

	runCtx, cancel := context.WithTimeout(b.browser, timeout+15*time.Second)
	defer cancel()

	if err := chromedp.Run(runCtx, setup...); err != nil {
		return Snapshot{}, err
	}
	return waitForExtract(runCtx, extractJS, parse)
}

func (b *Browser) ensureLocked() error {
	if b.browser != nil {
		return nil
	}
	if b.profileDir != "" {
		if err := os.MkdirAll(b.profileDir, 0o755); err != nil {
			return fmt.Errorf("chrome: профиль: %w", err)
		}
	}

	opts := []chromedp.ExecAllocatorOption{
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.UserAgent(chromeUA),
		chromedp.Flag("headless", b.headless),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("start-minimized", true),
		chromedp.WindowSize(1280, 900),
	}
	if b.profileDir != "" {
		opts = append(opts, chromedp.UserDataDir(b.profileDir))
	}
	if b.chromePath != "" {
		opts = append(opts, chromedp.ExecPath(b.chromePath))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	browser, browserCancel := chromedp.NewContext(allocCtx, chromedp.WithLogf(func(string, ...any) {}))
	if err := chromedp.Run(browser); err != nil {
		allocCancel()
		browserCancel()
		return fmt.Errorf("chrome: запуск: %w", err)
	}

	b.allocCancel = allocCancel
	b.browser = browser
	b.browserCancel = browserCancel
	b.log.Info("chrome запущен", "headless", b.headless, "profile", b.profileDir)
	return nil
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
