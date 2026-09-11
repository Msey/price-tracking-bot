// Command bot запускает Telegram-бота отслеживания цен.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/Msey/price-tracking-bot/internal/config"
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
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(log); err != nil {
		log.Error("бот остановлен с ошибкой", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
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
	defer store.Close()

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

	dns := fetch.NewDNS(fetch.DNSOptions{
		Browser:         browser,
		CircuitCooldown: cfg.CircuitCooldown,
		Log:             log,
	})
	market := fetch.NewMarket(fetch.MarketOptions{
		Browser:         browser,
		CircuitCooldown: cfg.CircuitCooldown,
		Log:             log,
	})
	ozon := fetch.NewOzon(fetch.OzonOptions{
		Browser:         browser,
		CircuitCooldown: cfg.CircuitCooldown,
		Log:             log,
	})

	tr := tracker.New(store, map[string]tracker.Fetcher{
		string(sites.DNS):          dns,
		string(sites.YandexMarket): market,
		string(sites.Ozon):         ozon,
	}, bot, tracker.Config{
		Interval:     cfg.CheckInterval,
		FetchGap:     cfg.FetchGap,
		PerCycle:     cfg.FetchPerCycle,
		StartupDelay: cfg.StartupDelay,
	}, log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go tr.Run(ctx)
	if cfg.UIAddr != "" {
		web.New(store, cfg.UIAddr, log).Start(ctx)
	}

	log.Info("бот запущен",
		"username", bot.Username(),
		"database", cfg.DatabasePath,
		"interval", cfg.CheckInterval,
		"fetch_gap", cfg.FetchGap,
		"per_cycle", cfg.FetchPerCycle,
		"city", cfg.DefaultCity,
		"ui", cfg.UIAddr,
		"gui", cfg.GUI && gui.Available())

	if cfg.GUI && gui.Available() {
		go bot.Start(ctx)
		err := gui.Run(ctx, gui.Options{
			Store:       store,
			DataPath:    cfg.DatabasePath,
			Log:         log,
			StartHidden: startHiddenFromArgs(os.Args[1:]),
			BotUsername: bot.Username(),
			CheckNow:    tr.RequestCheck,
			CheckBusy:   tr.Busy,
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
