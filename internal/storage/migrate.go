package storage

import (
	"context"
	"fmt"
)

// migrations применяются по порядку; номер шага равен его позиции в срезе.
// Уже применённые шаги менять нельзя — только добавлять новые в конец.
var migrations = []string{
	// 1. Базовая схема.
	`
	CREATE TABLE users (
	    id         INTEGER PRIMARY KEY AUTOINCREMENT,
	    tg_chat_id INTEGER NOT NULL UNIQUE,
	    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
	);

	-- Товар не привязан к пользователю: цена у него одна на всех.
	-- Город входит в ключ, потому что на DNS цена зависит от региона.
	CREATE TABLE products (
	    id           INTEGER PRIMARY KEY AUTOINCREMENT,
	    site         TEXT    NOT NULL,
	    external_key TEXT    NOT NULL,
	    url          TEXT    NOT NULL,
	    name         TEXT    NOT NULL DEFAULT '',
	    city         TEXT    NOT NULL,
	    created_at   TEXT    NOT NULL DEFAULT (datetime('now')),
	    UNIQUE (site, external_key, city)
	);

	CREATE TABLE subscriptions (
	    id            INTEGER PRIMARY KEY AUTOINCREMENT,
	    user_id       INTEGER NOT NULL REFERENCES users(id)    ON DELETE CASCADE,
	    product_id    INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,
	    threshold_pct REAL    NOT NULL DEFAULT 0,
	    active        INTEGER NOT NULL DEFAULT 1,
	    created_at    TEXT    NOT NULL DEFAULT (datetime('now')),
	    UNIQUE (user_id, product_id)
	);

	-- Цена в копейках, чтобы не связываться с плавающей точкой:
	-- у DNS цены целые в рублях, а API Wildberries отдаёт копейки.
	CREATE TABLE price_history (
	    id             INTEGER PRIMARY KEY AUTOINCREMENT,
	    product_id     INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,
	    price_kopecks  INTEGER NOT NULL,
	    currency       TEXT    NOT NULL DEFAULT 'RUB',
	    available      INTEGER NOT NULL DEFAULT 1,
	    checked_at     TEXT    NOT NULL DEFAULT (datetime('now'))
	);

	CREATE INDEX idx_price_history_product ON price_history (product_id, checked_at DESC);

	CREATE TABLE fetch_errors (
	    id          INTEGER PRIMARY KEY AUTOINCREMENT,
	    product_id  INTEGER REFERENCES products(id) ON DELETE CASCADE,
	    site        TEXT NOT NULL DEFAULT '',
	    kind        TEXT NOT NULL,
	    message     TEXT NOT NULL DEFAULT '',
	    occurred_at TEXT NOT NULL DEFAULT (datetime('now'))
	);

	CREATE INDEX idx_fetch_errors_site ON fetch_errors (site, occurred_at DESC);
	`,
}

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("storage: чтение версии схемы: %w", err)
	}
	if version > len(migrations) {
		return fmt.Errorf("storage: база создана более новой версией бота (схема %d, известно %d)", version, len(migrations))
	}

	for i := version; i < len(migrations); i++ {
		step := i + 1
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("storage: миграция %d, начало транзакции: %w", step, err)
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("storage: миграция %d: %w", step, err)
		}
		// PRAGMA не принимает параметры, поэтому число подставляется в текст.
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", step)); err != nil {
			tx.Rollback()
			return fmt.Errorf("storage: миграция %d, запись версии: %w", step, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("storage: миграция %d, фиксация: %w", step, err)
		}
	}
	return nil
}
