package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SnapshotRow — одна запись истории цены.
type SnapshotRow struct {
	PriceKopecks int64
	Available    bool
	CheckedAt    string
}

func formatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

// ProductsDue возвращает товары сайта, которые пора проверить.
// Это товары с хотя бы одной активной подпиской, у которых нет свежего замера.
func (s *Store) ProductsDue(ctx context.Context, site string, olderThan time.Time, limit int) ([]Product, error) {
	if limit < 1 {
		limit = 1
	}
	cutoff := formatTime(olderThan)
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.site, p.external_key, p.url, p.name, p.city
		FROM products p
		JOIN subscriptions s ON s.product_id = p.id AND s.active = 1
		LEFT JOIN price_history h ON h.product_id = p.id
		WHERE p.site = ?
		GROUP BY p.id
		HAVING MAX(h.checked_at) IS NULL OR MAX(h.checked_at) <= ?
		ORDER BY MAX(h.checked_at) IS NOT NULL, MAX(h.checked_at) ASC
		LIMIT ?`, site, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: товары к проверке (%s): %w", site, err)
	}
	defer rows.Close()

	var out []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Site, &p.ExternalKey, &p.URL, &p.Name, &p.City); err != nil {
			return nil, fmt.Errorf("storage: чтение товара к проверке: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: обход товаров к проверке: %w", err)
	}
	return out, nil
}

// RecordSnapshot пишет замер и при необходимости обновляет имя товара.
func (s *Store) RecordSnapshot(ctx context.Context, productID int64, name string, kopecks int64, currency string, available bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: начало транзакции замера: %w", err)
	}
	defer tx.Rollback()

	avail := 0
	if available {
		avail = 1
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO price_history (product_id, price_kopecks, currency, available, checked_at)
		VALUES (?, ?, ?, ?, ?)`, productID, kopecks, currency, avail, formatTime(time.Now())); err != nil {
		return fmt.Errorf("storage: запись цены товара %d: %w", productID, err)
	}
	if name != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE products SET name = ? WHERE id = ? AND (name = '' OR name != ?)`, name, productID, name); err != nil {
			return fmt.Errorf("storage: имя товара %d: %w", productID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: фиксация замера: %w", err)
	}
	return nil
}

// LastSnapshots возвращает последние n замеров, сначала самые новые.
func (s *Store) LastSnapshots(ctx context.Context, productID int64, n int) ([]SnapshotRow, error) {
	if n < 1 {
		n = 1
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT price_kopecks, available, checked_at
		FROM price_history
		WHERE product_id = ?
		ORDER BY checked_at DESC, id DESC
		LIMIT ?`, productID, n)
	if err != nil {
		return nil, fmt.Errorf("storage: история товара %d: %w", productID, err)
	}
	defer rows.Close()

	var out []SnapshotRow
	for rows.Next() {
		var r SnapshotRow
		var avail int
		if err := rows.Scan(&r.PriceKopecks, &avail, &r.CheckedAt); err != nil {
			return nil, fmt.Errorf("storage: чтение истории: %w", err)
		}
		r.Available = avail != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// NotifiedState — последняя цена, о которой уже сообщили подписчикам.
type NotifiedState struct {
	Kopecks   int64
	Available bool
	Set       bool
}

// Notified возвращает базу сравнения для товара.
func (s *Store) Notified(ctx context.Context, productID int64) (NotifiedState, error) {
	var kopecks, avail sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT last_notified_kopecks, last_notified_available FROM products WHERE id = ?`, productID).Scan(&kopecks, &avail)
	if err != nil {
		return NotifiedState{}, fmt.Errorf("storage: last_notified товара %d: %w", productID, err)
	}
	if !kopecks.Valid {
		return NotifiedState{}, nil
	}
	return NotifiedState{Kopecks: kopecks.Int64, Available: avail.Int64 != 0, Set: true}, nil
}

// MarkNotified запоминает, что об этом замере уже написали.
func (s *Store) MarkNotified(ctx context.Context, productID, kopecks int64, available bool) error {
	avail := 0
	if available {
		avail = 1
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE products SET last_notified_kopecks = ?, last_notified_available = ? WHERE id = ?`,
		kopecks, avail, productID)
	if err != nil {
		return fmt.Errorf("storage: mark notified товара %d: %w", productID, err)
	}
	return nil
}

// SubscriberChats — чаты, которым нужно сообщить о товаре.
func (s *Store) SubscriberChats(ctx context.Context, productID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.tg_chat_id
		FROM subscriptions s
		JOIN users u ON u.id = s.user_id
		WHERE s.product_id = ? AND s.active = 1`, productID)
	if err != nil {
		return nil, fmt.Errorf("storage: подписчики товара %d: %w", productID, err)
	}
	defer rows.Close()

	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("storage: чтение подписчика: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// RecordFetchError пишет сбой, чтобы потом разбирать бан/челлендж.
func (s *Store) RecordFetchError(ctx context.Context, productID int64, site, kind, message string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO fetch_errors (product_id, site, kind, message) VALUES (?, ?, ?, ?)`,
		productID, site, kind, message)
	if err != nil {
		return fmt.Errorf("storage: запись ошибки загрузки: %w", err)
	}
	return nil
}

// ProductByID нужен, чтобы после замера подставить свежее имя в уведомление.
func (s *Store) ProductByID(ctx context.Context, id int64) (Product, error) {
	var p Product
	err := s.db.QueryRowContext(ctx, `
		SELECT id, site, external_key, url, name, city FROM products WHERE id = ?`, id).
		Scan(&p.ID, &p.Site, &p.ExternalKey, &p.URL, &p.Name, &p.City)
	if err != nil {
		return Product{}, fmt.Errorf("storage: товар %d: %w", id, err)
	}
	return p, nil
}
