// Command guipreview открывает окно списка без Telegram и Chrome.
package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Msey/price-tracking-bot/internal/gui"
	"github.com/Msey/price-tracking-bot/internal/storage"
)

func main() {
	runtime.LockOSThread()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	dir, err := os.MkdirTemp("", "price-gui")
	if err != nil {
		log.Error("temp", "error", err)
		os.Exit(1)
	}
	store, err := storage.Open(filepath.Join(dir, "preview.db"))
	if err != nil {
		log.Error("база", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	ctx := context.Background()
	p, _, err := store.AddSubscription(ctx, 1001, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow")
	if err != nil {
		log.Error("заявка", "error", err)
		os.Exit(1)
	}
	for _, price := range []int64{15999900, 15499900, 14999900, 15200000} {
		if err := store.RecordSnapshot(ctx, p.ID, "Honor MagicBook", price, "RUB", true); err != nil {
			log.Error("цена", "error", err)
			os.Exit(1)
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if err := gui.Run(runCtx, gui.Options{Store: store, DataPath: dir, Log: log}); err != nil {
		log.Error("окно", "error", err)
		os.Exit(1)
	}
}
