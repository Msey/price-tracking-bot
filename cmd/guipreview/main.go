// Command guipreview открывает окно списка без Telegram и Chrome.
package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Msey/price-tracking-bot/internal/diaglog"
	"github.com/Msey/price-tracking-bot/internal/gui"
	"github.com/Msey/price-tracking-bot/internal/storage"
)

func main() {
	runtime.LockOSThread()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	// os.Exit не выполняет отложенные вызовы, поэтому вся работа идёт в run:
	// иначе временная база и её каталог остаются на диске после каждого сбоя.
	if err := run(log); err != nil {
		log.Error("предпросмотр", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	dir, err := os.MkdirTemp("", "price-gui")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	store, err := storage.Open(filepath.Join(dir, "preview.db"))
	if err != nil {
		return err
	}
	defer store.Close()

	ctx := context.Background()
	p, _, err := store.AddSubscription(ctx, 1001, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow")
	if err != nil {
		return err
	}
	for _, price := range []int64{15999900, 15499900, 14999900, 15200000} {
		if _, err := store.RecordSnapshot(ctx, p.ID, "Honor MagicBook", price, "RUB", true); err != nil {
			return err
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	logs := &diaglog.Switch{}
	return gui.Run(runCtx, gui.Options{
		Store:         store,
		DataPath:      dir,
		Log:           log,
		LogEnabled:    logs.Enabled,
		SetLogEnabled: logs.Set,
	})
}
