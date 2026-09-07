// Command bot запускает Telegram-бота отслеживания цен.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Msey/price-tracking-bot/internal/config"
	"github.com/Msey/price-tracking-bot/internal/fetch"
	"github.com/Msey/price-tracking-bot/internal/storage"
	"github.com/Msey/price-tracking-bot/internal/telegram"
	"github.com/Msey/price-tracking-bot/internal/tracker"
	"github.com/Msey/price-tracking-bot/internal/web"
)

func main() {
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

	store, err := storage.Open(cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	bot, err := telegram.New(cfg, store, log)
	if err != nil {
		return err
	}

	dns := fetch.NewDNS(fetch.DNSOptions{
		ProfileDir:      cfg.ChromeProfile,
		ChromePath:      cfg.ChromePath,
		Headless:        cfg.ChromeHeadless,
		CircuitCooldown: cfg.CircuitCooldown,
		Log:             log,
	})
	defer dns.Close()

	tr := tracker.New(store, dns, bot, tracker.Config{
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
		"ui", cfg.UIAddr)

	bot.Start(ctx)
	log.Info("бот остановлен")
	return nil
}
