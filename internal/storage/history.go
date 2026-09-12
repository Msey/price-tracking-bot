package storage

import (
	"context"
	"database/sql"
	"errors"
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
	rows, err := s.db.QueryContext(ctx, sqlProductsDue, site, cutoff, limit)
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

// ActiveProducts — все товары сайта с хотя бы одной активной подпиской,
// независимо от того, когда их проверяли в последний раз.
func (s *Store) ActiveProducts(ctx context.Context, site string) ([]Product, error) {
	rows, err := s.db.QueryContext(ctx, sqlActiveProducts, site)
	if err != nil {
		return nil, fmt.Errorf("storage: активные товары (%s): %w", site, err)
	}
	defer rows.Close()

	var out []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Site, &p.ExternalKey, &p.URL, &p.Name, &p.City); err != nil {
			return nil, fmt.Errorf("storage: чтение активного товара: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: обход активных товаров: %w", err)
	}
	return out, nil
}

// RecordSnapshot пишет каждый замер отдельной строкой, даже если цена
// не изменилась: иначе на графике не видно, что проверка прошла.
// repeated=true, когда цена, валюта и наличие совпали с предыдущей
// записью — трекер не шлёт повторное уведомление в Telegram.
func (s *Store) RecordSnapshot(ctx context.Context, productID int64, name string, kopecks int64, currency string, available bool) (repeated bool, err error) {
	if productID < 1 {
		return false, fmt.Errorf("storage: запись цены товара %d: нет такого товара", productID)
	}
	if currency == "" {
		currency = "RUB"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("storage: начало транзакции замера: %w", err)
	}
	defer tx.Rollback()

	avail := 0
	if available {
		avail = 1
	}
	now := formatTime(time.Now())

	var lastKopecks int64
	var lastCurrency string
	var lastAvail int
	err = tx.QueryRowContext(ctx, sqlLastSnapshot, productID).Scan(&lastKopecks, &lastCurrency, &lastAvail)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return false, fmt.Errorf("storage: последняя цена товара %d: %w", productID, err)
	case lastKopecks == kopecks && lastAvail == avail && lastCurrency == currency:
		repeated = true
	}
	if _, err := tx.ExecContext(ctx, sqlInsertSnapshot, productID, kopecks, currency, avail, now); err != nil {
		return false, fmt.Errorf("storage: запись цены товара %d: %w", productID, err)
	}
	if name != "" {
		if _, err := tx.ExecContext(ctx, sqlUpdateProductName, name, productID, name); err != nil {
			return false, fmt.Errorf("storage: имя товара %d: %w", productID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("storage: фиксация замера: %w", err)
	}
	return repeated, nil
}

// LastSnapshots возвращает последние n замеров, сначала самые новые.
func (s *Store) LastSnapshots(ctx context.Context, productID int64, n int) ([]SnapshotRow, error) {
	if n < 1 {
		n = 1
	}
	rows, err := s.db.QueryContext(ctx, sqlLastSnapshots, productID, n)
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

const defaultHistoryLimit = 90

// Histories возвращает последние limit замеров по каждому товару,
// уже в порядке от старых к новым — так удобнее рисовать график.
func (s *Store) Histories(ctx context.Context, productIDs []int64, limit int) (map[int64][]SnapshotRow, error) {
	out := make(map[int64][]SnapshotRow, len(productIDs))
	if len(productIDs) == 0 {
		return out, nil
	}
	if limit < 1 {
		limit = defaultHistoryLimit
	}

	args := make([]any, 0, len(productIDs)+1)
	for _, id := range productIDs {
		args = append(args, id)
	}
	args = append(args, limit)

	q := fmt.Sprintf(sqlHistories, sqlPlaceholders(len(productIDs)))
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: истории цен: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var r SnapshotRow
		var avail int
		if err := rows.Scan(&id, &r.PriceKopecks, &avail, &r.CheckedAt); err != nil {
			return nil, fmt.Errorf("storage: чтение истории: %w", err)
		}
		r.Available = avail != 0
		out[id] = append(out[id], r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: обход историй: %w", err)
	}
	return out, nil
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
	err := s.db.QueryRowContext(ctx, sqlNotified, productID).Scan(&kopecks, &avail)
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
	_, err := s.db.ExecContext(ctx, sqlMarkNotified, kopecks, avail, productID)
	if err != nil {
		return fmt.Errorf("storage: mark notified товара %d: %w", productID, err)
	}
	return nil
}

// SubscriberChats — чаты, которым нужно сообщить о товаре.
func (s *Store) SubscriberChats(ctx context.Context, productID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, sqlSubscriberChats, productID)
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
	_, err := s.db.ExecContext(ctx, sqlInsertFetchError, productID, site, kind, message)
	if err != nil {
		return fmt.Errorf("storage: запись ошибки загрузки: %w", err)
	}
	return nil
}

// ProductByID нужен, чтобы после замера подставить свежее имя в уведомление.
func (s *Store) ProductByID(ctx context.Context, id int64) (Product, error) {
	var p Product
	err := s.db.QueryRowContext(ctx, sqlProductByID, id).
		Scan(&p.ID, &p.Site, &p.ExternalKey, &p.URL, &p.Name, &p.City)
	if err != nil {
		return Product{}, fmt.Errorf("storage: товар %d: %w", id, err)
	}
	return p, nil
}
