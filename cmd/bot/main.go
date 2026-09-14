// Command bot запускает Telegram-бота отслеживания цен.
package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/Msey/price-tracking-bot/internal/config"
	"github.com/Msey/price-tracking-bot/internal/diaglog"
	"github.com/Msey/price-tracking-bot/internal/fetch"
	"github.com/Msey/price-tracking-bot/internal/gui"
	"github.com/Msey/price-tracking-bot/internal/sites"
	"github.com/Msey/price-tracking-bot/internal/storage"
	"github.com/Msey/price-tracking-bot/internal/telegram"
	"github.com/Msey/price-tracking-bot/internal/tracker"
	"github.com/Msey/price-tracking-bot/internal/web"
)

func main() {
	runtime.LockOSThread()
	log, logs, closeLog := newLogger()
	if closeLog != nil {
		defer closeLog()
	}

	if err := run(log, logs); err != nil {
		log.Error("бот остановлен с ошибкой", "error", err)
		os.Exit(1)
	}
}

func newLogger() (*slog.Logger, *diaglog.Switch, func()) {
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	logs := &diaglog.Switch{}
	f, err := openLogFile(filepath.Join("data", "bot.log"), logSizeLimit, logKeep)
	if err != nil {
		log := slog.New(diaglog.Wrap(slog.NewTextHandler(os.Stderr, opts), logs))
		slog.SetDefault(log)
		return log, logs, nil
	}
	log := slog.New(diaglog.Wrap(slog.NewTextHandler(io.MultiWriter(os.Stderr, f), opts), logs))
	slog.SetDefault(log)
	return log, logs, func() { _ = f.Close() }
}

func run(log *slog.Logger, logs *diaglog.Switch) error {
	cfg, err := config.Load()
	if errors.Is(err, config.ErrNoToken) {
		return errors.New("не задан BOT_TOKEN: скопируйте .env.example в .env и впишите токен от @BotFather")
	}
	if err != nil {
		return err
	}
	if cfg.GUI && gui.Available() && gui.ActivateExisting() {
		log.Info("окно уже открыто, активирую существующий процесс")
		return nil
	}
	if len(cfg.AllowedUsers) == 0 {
		log.Warn("ALLOWED_USERS пуст: боту может писать любой, кто знает его имя. " +
			"Впишите свой Telegram user id в .env, чтобы закрыть доступ")
	}

	store, err := storage.Open(cfg.DatabasePath)
	if err != nil {
		return err
	}
	store.SetUnlimitedUsers(cfg.UnlimitedUsers)
	// closeStore вызывается после остановки трекера: иначе последний замер
	// цикла пишется в уже закрытую базу и молча теряется.
	closeStore := sync.OnceFunc(func() { _ = store.Close() })
	defer closeStore()

	bot := telegram.New(cfg, store, log)

	browser := fetch.NewBrowser(fetch.BrowserOptions{
		ProfileDir: cfg.ChromeProfile,
		ChromePath: cfg.ChromePath,
		Log:        log,
	})
	defer browser.Close()

	shopOpt := fetch.ShopOptions{
		Browser:         browser,
		CircuitCooldown: cfg.CircuitCooldown,
		Log:             log,
	}
	tr := tracker.New(store, map[string]tracker.Fetcher{
		string(sites.DNS):          fetch.NewDNS(shopOpt),
		string(sites.YandexMarket): fetch.NewMarket(shopOpt),
		string(sites.Ozon):         fetch.NewOzon(shopOpt),
		string(sites.Wildberries):  fetch.NewWildberries(shopOpt),
	}, bot, tracker.Config{
		FetchGap:     cfg.FetchGap,
		PerCycle:     cfg.FetchPerCycle,
		StartupDelay: cfg.StartupDelay,
	}, log)
	browser.SetOnChallenge(tr.SetUserHint)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer func() {
		stop()
		tr.Wait(10 * time.Second)
		closeStore()
	}()

	go tr.Run(ctx)
	go pruneLoop(ctx, store, log)
	if cfg.UIAddr != "" {
		web.New(store, cfg.UIAddr, log).Start(ctx)
	}

	log.Info("бот запущен",
		"username", bot.Username(),
		"database", cfg.DatabasePath,
		"check_dns", sites.DNS.CheckInterval(),
		"check_ozon", sites.Ozon.CheckInterval(),
		"check_yandex", sites.YandexMarket.CheckInterval(),
		"check_wildberries", sites.Wildberries.CheckInterval(),
		"fetch_gap", cfg.FetchGap,
		"per_cycle", cfg.FetchPerCycle,
		"city", cfg.DefaultCity,
		"ui", cfg.UIAddr,
		"gui", cfg.GUI && gui.Available())

	if cfg.GUI && gui.Available() {
		go bot.Start(ctx)
		err := gui.Run(ctx, gui.Options{
			Store:         store,
			Log:           log,
			StartHidden:   startHiddenFromArgs(os.Args[1:]),
			BotUsername:   bot.Username(),
			OpenInChrome:  func(p storage.Product) { go browser.Show(p) },
			CheckNow:      tr.RequestCheck,
			CheckBusy:     tr.Busy,
			CheckStatus:   func() string { return statusLine(bot, tr) },
			LogEnabled:    logs.Enabled,
			SetLogEnabled: logs.Set,
		})
		stop()
		log.Info("бот остановлен")
		return err
	}

	bot.Start(ctx)
	log.Info("бот остановлен")
	return nil
}

// pruneLoop чистит историю при запуске и раз в сутки: замер пишется
// отдельной строкой на каждую проверку, и без чистки база растёт всё время
// работы бота.
func pruneLoop(ctx context.Context, store *storage.Store, log *slog.Logger) {
	const every = 24 * time.Hour
	for {
		prune(ctx, store, log)
		timer := time.NewTimer(every)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func prune(ctx context.Context, store *storage.Store, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	done, err := store.Prune(ctx, storage.HistoryKeepPerProduct, time.Now().Add(-storage.FetchErrorsKeepFor))
	if err != nil {
		log.Warn("чистка базы не удалась", "error", err)
		return
	}
	if done.Snapshots == 0 && done.Errors == 0 {
		log.Debug("чистить нечего")
		return
	}
	log.Info("база почищена", "snapshots", done.Snapshots, "errors", done.Errors,
		"keep_per_product", storage.HistoryKeepPerProduct)
}

func startHiddenFromArgs(args []string) bool {
	for _, a := range args {
		if a == "-tray" || a == "--tray" {
			return true
		}
	}
	return false
}

func statusLine(bot *telegram.Bot, tr *tracker.Tracker) string {
	if s := bot.StatusText(); s != "" {
		return s
	}
	return tr.StatusText()
}
