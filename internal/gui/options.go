package gui

import (
	"log/slog"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

// Options — данные, с которыми поднимается окно.
type Options struct {
	Store       *storage.Store
	DataPath    string
	Log         *slog.Logger
	StartHidden bool
	// BotUsername — @имя бота без собаки, для ссылки t.me в меню трея.
	BotUsername string
	// CheckNow сбрасывает таймер автопроверки и проверяет все товары.
	CheckNow func()
	// CheckBusy сообщает, что проверка уже идёт или только что запрошена.
	CheckBusy func() bool
}
