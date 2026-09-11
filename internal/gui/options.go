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
	// UserID — чей список показывать. 0 — выбрать по базе (один подписчик
	// или первый в комбобоксе, если их несколько).
	UserID int64
	// UserLocked — id задан конфигурацией, переключать пользователей нельзя.
	UserLocked bool
	// CheckNow сбрасывает таймер автопроверки и проверяет все товары.
	CheckNow func()
	// CheckBusy сообщает, что проверка уже идёт или только что запрошена.
	CheckBusy func() bool
	// CheckStatus — текст текущего шага проверки для строки статуса.
	CheckStatus func() string
	// LogEnabled / SetLogEnabled — тумблер подробных логов на панели.
	// По умолчанию выключен: Info/Debug не пишутся, Warn/Error остаются.
	LogEnabled    func() bool
	SetLogEnabled func(bool)
}
