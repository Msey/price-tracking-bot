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
	"github.com/Msey/price-tracking-bot/internal/storage"
	"github.com/Msey/price-tracking-bot/internal/telegram"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("бот запущен",
		"username", bot.Username(),
		"database", cfg.DatabasePath,
		"interval", cfg.CheckInterval,
		"city", cfg.DefaultCity)

	bot.Start(ctx)
	log.Info("бот остановлен")
	return nil
}
