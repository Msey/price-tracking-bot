package gui

import (
	"log/slog"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

// Options — данные, с которыми поднимается окно.
type Options struct {
	Store    *storage.Store
	DataPath string
	Log      *slog.Logger
}
