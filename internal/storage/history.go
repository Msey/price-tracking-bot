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
// записью.
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

// historyIDBatch — сколько товаров кладём в один IN (...). У SQLite предел
// на число параметров запроса (по умолчанию 999), и на большом списке
// подписок запрос иначе просто не собрался бы.
const historyIDBatch = 500

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
	for start := 0; start < len(productIDs); start += historyIDBatch {
		end := start + historyIDBatch
		if end > len(productIDs) {
			end = len(productIDs)
		}
		if err := s.appendHistories(ctx, out, productIDs[start:end], limit); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) appendHistories(ctx context.Context, out map[int64][]SnapshotRow, productIDs []int64, limit int) error {
	args := make([]any, 0, len(productIDs)+1)
	for _, id := range productIDs {
		args = append(args, id)
	}
	args = append(args, limit)

	q := fmt.Sprintf(sqlHistories, sqlPlaceholders(len(productIDs)))
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("storage: истории цен: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var r SnapshotRow
		var avail int
		if err := rows.Scan(&id, &r.PriceKopecks, &avail, &r.CheckedAt); err != nil {
			return fmt.Errorf("storage: чтение истории: %w", err)
		}
		r.Available = avail != 0
		out[id] = append(out[id], r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: обход историй: %w", err)
	}
	return nil
}

const (
	// HistoryKeepPerProduct — сколько замеров хранить на товар. Ozon пишет
	// около 72 строк в сутки, так что этого хватает примерно на пять дней
	// подробной истории при 90 точках на графике.
	HistoryKeepPerProduct = 400
	// FetchErrorsKeepFor — сколько хранить записи о сбоях загрузки.
	FetchErrorsKeepFor = 30 * 24 * time.Hour
)

// Pruned — сколько строк убрала чистка.
type Pruned struct {
	Snapshots int64
	Errors    int64
}

// Prune убирает лишнюю историю. Без неё price_history растёт навсегда:
// каждый замер — отдельная строка, а окно и уведомления смотрят только на
// хвост. Свежие HistoryKeepPerProduct замеров на товар остаются на месте.
func (s *Store) Prune(ctx context.Context, keepPerProduct int, errorsOlderThan time.Time) (Pruned, error) {
	if keepPerProduct < 1 {
		keepPerProduct = HistoryKeepPerProduct
	}
	var out Pruned

	res, err := s.db.ExecContext(ctx, sqlPruneHistory, keepPerProduct)
	if err != nil {
		return out, fmt.Errorf("storage: чистка истории цен: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		out.Snapshots = n
	}

	if !errorsOlderThan.IsZero() {
		res, err = s.db.ExecContext(ctx, sqlPruneFetchErrors, formatTime(errorsOlderThan))
		if err != nil {
			return out, fmt.Errorf("storage: чистка ошибок загрузки: %w", err)
		}
		if n, err := res.RowsAffected(); err == nil {
			out.Errors = n
		}
	}
	return out, nil
}

// Fingerprint — отпечаток того, что показывает окно. Меняется при новой
// подписке, снятии подписки, новом замере и новой ошибке загрузки.
// Имя товара обновляется в той же транзакции, что и замер, поэтому
// отдельно его не считаем.
type Fingerprint struct {
	ActiveSubs    int64
	MaxSubID      int64
	Snapshots     int64
	MaxSnapshotID int64
	MaxErrorID    int64
}

// Fingerprint читает отпечаток одним запросом по агрегатам. Окно опрашивает
// его каждые несколько секунд и лезет за списком и историями только когда
// отпечаток изменился.
func (s *Store) Fingerprint(ctx context.Context) (Fingerprint, error) {
	var f Fingerprint
	err := s.db.QueryRowContext(ctx, sqlFingerprint).Scan(
		&f.ActiveSubs, &f.MaxSubID, &f.Snapshots, &f.MaxSnapshotID, &f.MaxErrorID)
	if err != nil {
		return Fingerprint{}, fmt.Errorf("storage: отпечаток данных: %w", err)
	}
	return f, nil
}

// MarkNotified запоминает, что об этом замере уже написали. Читателя у этих
// столбцов нет: решение об уведомлении принимается по истории цен, а запись
// остаётся следом в базе.
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

// SetPriceAlert ставит разовый порог на подписку этого чата и сбрасывает
// «уже писал», чтобы новое число могло сработать снова.
func (s *Store) SetPriceAlert(ctx context.Context, chatID, productID, alertKopecks int64) (int64, error) {
	if chatID <= 0 {
		return 0, ErrNoUser
	}
	if productID < 1 || alertKopecks <= 0 {
		return 0, fmt.Errorf("storage: порог для товара %d: нет подписки или суммы", productID)
	}
	var id int64
	err := s.db.QueryRowContext(ctx, sqlSetPriceAlert, alertKopecks, productID, chatID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("storage: порог для товара %d: нет подписки", productID)
	}
	if err != nil {
		return 0, fmt.Errorf("storage: порог для товара %d: %w", productID, err)
	}
	return id, nil
}

// PendingPriceAlerts — подписчики товара с порогом, которым ещё не писали.
func (s *Store) PendingPriceAlerts(ctx context.Context, productID int64) ([]PriceAlert, error) {
	if productID < 1 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, sqlPendingPriceAlerts, productID)
	if err != nil {
		return nil, fmt.Errorf("storage: пороги товара %d: %w", productID, err)
	}
	defer rows.Close()

	var out []PriceAlert
	for rows.Next() {
		var a PriceAlert
		if err := rows.Scan(&a.SubscriptionID, &a.ChatID, &a.Kopecks); err != nil {
			return nil, fmt.Errorf("storage: чтение порога: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: обход порогов: %w", err)
	}
	return out, nil
}

// MarkAlertFired отмечает, что разовое письмо по порогу уже ушло.
func (s *Store) MarkAlertFired(ctx context.Context, subscriptionID int64) error {
	if subscriptionID < 1 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, sqlMarkAlertFired, subscriptionID); err != nil {
		return fmt.Errorf("storage: отметка порога %d: %w", subscriptionID, err)
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
