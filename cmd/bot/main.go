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
	_ = os.MkdirAll("data", 0o755)
	f, err := os.OpenFile(filepath.Join("data", "bot.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
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

	store, err := storage.Open(cfg.DatabasePath)
	if err != nil {
		return err
	}
	// closeStore вызывается после остановки трекера: иначе последний замер
	// цикла пишется в уже закрытую базу и молча теряется.
	closeStore := sync.OnceFunc(func() { _ = store.Close() })
	defer closeStore()

	bot, err := telegram.New(cfg, store, log)
	if err != nil {
		return err
	}

	browser := fetch.NewBrowser(fetch.BrowserOptions{
		ProfileDir: cfg.ChromeProfile,
		ChromePath: cfg.ChromePath,
		Headless:   cfg.ChromeHeadless,
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
	if cfg.UIAddr != "" {
		web.New(store, cfg.UIAddr, log).Start(ctx)
	}

	log.Info("бот запущен",
		"username", bot.Username(),
		"database", cfg.DatabasePath,
		"check_dns", sites.DNS.CheckInterval(),
		"check_ozon", sites.Ozon.CheckInterval(),
		"check_yandex", sites.YandexMarket.CheckInterval(),
		"fetch_gap", cfg.FetchGap,
		"per_cycle", cfg.FetchPerCycle,
		"city", cfg.DefaultCity,
		"ui", cfg.UIAddr,
		"gui", cfg.GUI && gui.Available())

	if cfg.GUI && gui.Available() {
		go bot.Start(ctx)
		err := gui.Run(ctx, gui.Options{
			Store:         store,
			DataPath:      cfg.DatabasePath,
			Log:           log,
			StartHidden:   startHiddenFromArgs(os.Args[1:]),
			BotUsername:   bot.Username(),
			CheckNow:      tr.RequestCheck,
			CheckBusy:     tr.Busy,
			CheckStatus:   tr.StatusText,
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

func startHiddenFromArgs(args []string) bool {
	for _, a := range args {
		if a == "-tray" || a == "--tray" {
			return true
		}
	}
	return false
}
