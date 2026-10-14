package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// QueuedChange — ещё не доставленное изменение цены или наличия.
// Prev — то, о чём чат уже знает (или первое недоставленное «было»).
// Cur — последнее состояние. Повторные замеры сдвигают только Cur.
type QueuedChange struct {
	ProductID     int64
	PrevKopecks   int64
	PrevAvailable bool
	CurKopecks    int64
	CurAvailable  bool
}

// QueuedAlert — ещё не доставленное письмо «ниже порога».
type QueuedAlert struct {
	SubscriptionID int64
	ProductID      int64
	PriceKopecks   int64
	AlertKopecks   int64
}

func bit(v bool) int {
	if v {
		return 1
	}
	return 0
}

// FoldChange кладёт изменение в очередь чата. Если запись уже есть,
// «было» не трогаем: в сводке останется шаг от последнего доставленного
// состояния до текущего. Совпавшие края стирают запись — писать не о чем.
func (s *Store) FoldChange(ctx context.Context, chatID, productID, prevKopecks, curKopecks int64, prevAvailable, curAvailable bool) error {
	if s == nil || chatID == 0 || productID < 1 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, sqlFoldChange,
		chatID, productID, prevKopecks, bit(prevAvailable), curKopecks, bit(curAvailable))
	if err != nil {
		return fmt.Errorf("storage: очередь изменения %d для %d: %w", productID, chatID, err)
	}
	if _, err := s.db.ExecContext(ctx, sqlDropFoldedChange, chatID, productID); err != nil {
		return fmt.Errorf("storage: схлопывание изменения %d для %d: %w", productID, chatID, err)
	}
	return nil
}

// FoldAlert ставит или обновляет недоставленный порог. Цена не ниже
// порога — запись убирается, подписка при этом ещё жива.
func (s *Store) FoldAlert(ctx context.Context, subscriptionID, chatID, productID, priceKopecks, alertKopecks int64) error {
	if s == nil || subscriptionID < 1 || chatID == 0 || productID < 1 {
		return nil
	}
	if !AlertDue(priceKopecks, alertKopecks, false) {
		if _, err := s.db.ExecContext(ctx, sqlDropAlert, subscriptionID); err != nil {
			return fmt.Errorf("storage: снятие порога %d из очереди: %w", subscriptionID, err)
		}
		return nil
	}
	_, err := s.db.ExecContext(ctx, sqlFoldAlert,
		subscriptionID, chatID, productID, priceKopecks, alertKopecks)
	if err != nil {
		return fmt.Errorf("storage: очередь порога %d: %w", subscriptionID, err)
	}
	return nil
}

// DropQueuedAlerts убирает недоставленные пороги товара, не гася их
// на подписке. Нужно, когда витрина опустела до отправки.
func (s *Store) DropQueuedAlerts(ctx context.Context, productID int64) error {
	if s == nil || productID < 1 {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, sqlDeleteProductAlerts, productID); err != nil {
		return fmt.Errorf("storage: очередь порогов товара %d: %w", productID, err)
	}
	return nil
}

// HasOutbox — есть ли хоть одно недоставленное письмо.
func (s *Store) HasOutbox(ctx context.Context) (bool, error) {
	if s == nil {
		return false, nil
	}
	var n int
	if err := s.db.QueryRowContext(ctx, sqlHasOutbox).Scan(&n); err != nil {
		return false, fmt.Errorf("storage: очередь писем: %w", err)
	}
	return n != 0, nil
}

// OutboxChats — чаты, которым ещё нужно отправить сводку.
func (s *Store) OutboxChats(ctx context.Context) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, sqlOutboxChats)
	if err != nil {
		return nil, fmt.Errorf("storage: чаты очереди: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("storage: чат очереди: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: чаты очереди: %w", err)
	}
	return out, nil
}

// OutboxChanges — недоставленные изменения одного чата, по id товара.
func (s *Store) OutboxChanges(ctx context.Context, chatID int64) ([]QueuedChange, error) {
	rows, err := s.db.QueryContext(ctx, sqlOutboxChanges, chatID)
	if err != nil {
		return nil, fmt.Errorf("storage: изменения %d: %w", chatID, err)
	}
	defer rows.Close()
	var out []QueuedChange
	for rows.Next() {
		var row QueuedChange
		var prev, cur int
		if err := rows.Scan(&row.ProductID, &row.PrevKopecks, &prev, &row.CurKopecks, &cur); err != nil {
			return nil, fmt.Errorf("storage: изменение %d: %w", chatID, err)
		}
		row.PrevAvailable = prev != 0
		row.CurAvailable = cur != 0
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: изменения %d: %w", chatID, err)
	}
	return out, nil
}

// OutboxAlerts — недоставленные пороги одного чата.
func (s *Store) OutboxAlerts(ctx context.Context, chatID int64) ([]QueuedAlert, error) {
	rows, err := s.db.QueryContext(ctx, sqlOutboxAlerts, chatID)
	if err != nil {
		return nil, fmt.Errorf("storage: пороги %d: %w", chatID, err)
	}
	defer rows.Close()
	var out []QueuedAlert
	for rows.Next() {
		var row QueuedAlert
		if err := rows.Scan(&row.SubscriptionID, &row.ProductID, &row.PriceKopecks, &row.AlertKopecks); err != nil {
			return nil, fmt.Errorf("storage: порог %d: %w", chatID, err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: пороги %d: %w", chatID, err)
	}
	return out, nil
}

// FoldProductChange пишет одно и то же изменение всем чатам атомарно.
// Иначе сбой на втором подписчике оставил бы письмо только первому,
// а повторный замер с той же ценой уже никого бы не звал.
func (s *Store) FoldProductChange(ctx context.Context, chats []int64, productID, prevKopecks, curKopecks int64, prevAvailable, curAvailable bool) error {
	if s == nil || productID < 1 || len(chats) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: очередь товара %d: %w", productID, err)
	}
	defer tx.Rollback()
	for _, chatID := range chats {
		if chatID == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, sqlFoldChange,
			chatID, productID, prevKopecks, bit(prevAvailable), curKopecks, bit(curAvailable)); err != nil {
			return fmt.Errorf("storage: очередь изменения %d для %d: %w", productID, chatID, err)
		}
		if _, err := tx.ExecContext(ctx, sqlDropFoldedChange, chatID, productID); err != nil {
			return fmt.Errorf("storage: схлопывание изменения %d для %d: %w", productID, chatID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: очередь товара %d: %w", productID, err)
	}
	return nil
}

// AckChange — доставленное изменение, которое можно снять с очереди.
type AckChange struct {
	ProductID    int64
	CurKopecks   int64
	CurAvailable bool
}

// AckOutbox снимает доставленный кусок сводки целиком. Порог гасится
// только вместе с удалением из очереди, иначе письмо ушло бы дважды.
func (s *Store) AckOutbox(ctx context.Context, chatID int64, changes []AckChange, alertSubs []int64) error {
	if s == nil || chatID == 0 {
		return nil
	}
	if len(changes) == 0 && len(alertSubs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: подтверждение сводки %d: %w", chatID, err)
	}
	defer tx.Rollback()
	for _, c := range changes {
		if _, err := tx.ExecContext(ctx, sqlForgetChange, chatID, c.ProductID); err != nil {
			return fmt.Errorf("storage: снятие изменения %d для %d: %w", c.ProductID, chatID, err)
		}
		var left int
		if err := tx.QueryRowContext(ctx, sqlChangeStillQueued, c.ProductID).Scan(&left); err != nil {
			return fmt.Errorf("storage: остаток изменений %d: %w", c.ProductID, err)
		}
		if left == 0 {
			if _, err := tx.ExecContext(ctx, sqlMarkNotified, c.CurKopecks, bit(c.CurAvailable), c.ProductID); err != nil {
				return fmt.Errorf("storage: mark notified товара %d: %w", c.ProductID, err)
			}
		}
	}
	for _, id := range alertSubs {
		if id < 1 {
			continue
		}
		if _, err := tx.ExecContext(ctx, sqlDropAlert, id); err != nil {
			return fmt.Errorf("storage: снятие порога %d из очереди: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, sqlClearPriceAlert, id); err != nil {
			return fmt.Errorf("storage: сброс порога %d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: подтверждение сводки %d: %w", chatID, err)
	}
	return nil
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func dropChatProductQueue(ctx context.Context, db execer, chatID, productID int64) error {
	if _, err := db.ExecContext(ctx, sqlDeleteNotices, chatID, productID); err != nil {
		return fmt.Errorf("storage: очередь товара %d: %w", productID, err)
	}
	if _, err := db.ExecContext(ctx, sqlDeleteAlerts, chatID, productID); err != nil {
		return fmt.Errorf("storage: очередь порогов товара %d: %w", productID, err)
	}
	return nil
}
