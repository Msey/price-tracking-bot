// Package storage хранит пользователей, товары и историю цен в SQLite.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite" // драйвер SQLite на чистом Go, без CGO
)

// MaxSubscriptions — потолок активных подписок на одного пользователя.
// Упирается не в SQLite, а в Telegram (клавиатура до 100 кнопок, сообщение до 4096
// символов) и в то, что DNS после серии быстрых запросов банит IP.
const MaxSubscriptions = 50

// ErrTooManySubscriptions — пользователь уже держит MaxSubscriptions товаров.
var ErrTooManySubscriptions = errors.New("слишком много подписок")

type Store struct {
	db *sql.DB
}

// Product — отслеживаемый товар. Один товар общий для всех подписчиков,
// поэтому десять подписок на один ноутбук стоят одного запроса к магазину.
type Product struct {
	ID          int64
	Site        string
	ExternalKey string
	URL         string
	Name        string
	City        string
}

// Title возвращает имя товара, а пока оно неизвестно — короткий вид ссылки.
func (p Product) Title() string {
	if p.Name != "" {
		return p.Name
	}
	if u, err := url.Parse(p.URL); err == nil {
		return u.Host + u.Path
	}
	return p.URL
}

// Tracked — подписка пользователя вместе с последней известной ценой.
type Tracked struct {
	SubscriptionID   int64
	ThresholdPct     float64
	Product          Product
	LastPriceKopecks sql.NullInt64
	LastCheckedAt    sql.NullString
}

// Open открывает базу и приводит схему к актуальной версии.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("storage: пустой путь к базе")
	}
	// Путь передаём как имя файла, не как SQLite URI: иначе символы ? и &
	// в DATABASE_PATH превратились бы в параметры подключения.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("storage: открытие %s: %w", path, err)
	}
	// Одно соединение на запись: пропускной способности тут хватает с запасом,
	// зато исключены гонки за блокировку файла.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("storage: соединение с %s: %w", path, err)
	}

	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("storage: %s: %w", pragma, err)
		}
	}

	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// EnsureUser регистрирует пользователя, если он ещё не известен.
func (s *Store) EnsureUser(ctx context.Context, chatID int64) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO users (tg_chat_id) VALUES (?)
		ON CONFLICT (tg_chat_id) DO UPDATE SET tg_chat_id = excluded.tg_chat_id
		RETURNING id`, chatID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("storage: регистрация пользователя %d: %w", chatID, err)
	}
	return id, nil
}

// AddSubscription подписывает пользователя на товар, создавая товар при
// необходимости. Второй результат — false, если подписка уже была.
func (s *Store) AddSubscription(ctx context.Context, chatID int64, site, externalKey, productURL, city string) (Product, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Product{}, false, fmt.Errorf("storage: начало транзакции: %w", err)
	}
	defer tx.Rollback()

	var userID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO users (tg_chat_id) VALUES (?)
		ON CONFLICT (tg_chat_id) DO UPDATE SET tg_chat_id = excluded.tg_chat_id
		RETURNING id`, chatID).Scan(&userID); err != nil {
		return Product{}, false, fmt.Errorf("storage: регистрация пользователя %d: %w", chatID, err)
	}

	var existing Product
	err = tx.QueryRowContext(ctx, `
		SELECT p.id, p.site, p.external_key, p.url, p.name, p.city
		FROM products p
		JOIN subscriptions s ON s.product_id = p.id AND s.user_id = ? AND s.active = 1
		WHERE p.site = ? AND p.external_key = ? AND p.city = ?`,
		userID, site, externalKey, city,
	).Scan(&existing.ID, &existing.Site, &existing.ExternalKey, &existing.URL, &existing.Name, &existing.City)
	switch {
	case err == nil:
		if err := tx.Commit(); err != nil {
			return Product{}, false, fmt.Errorf("storage: фиксация транзакции: %w", err)
		}
		return existing, false, nil
	case !errors.Is(err, sql.ErrNoRows):
		return Product{}, false, fmt.Errorf("storage: поиск существующей подписки: %w", err)
	}

	var n int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM subscriptions WHERE user_id = ? AND active = 1`, userID).Scan(&n); err != nil {
		return Product{}, false, fmt.Errorf("storage: подсчёт подписок: %w", err)
	}
	if n >= MaxSubscriptions {
		return Product{}, false, ErrTooManySubscriptions
	}

	product := Product{Site: site, ExternalKey: externalKey, URL: productURL, City: city}
	// id = id — намеренный no-op: SQLite не возвращает строку при DO NOTHING,
	// а URL общего товара трогать нельзя, иначе второй подписчик перезапишет
	// ссылку у всех остальных.
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO products (site, external_key, url, city) VALUES (?, ?, ?, ?)
		ON CONFLICT (site, external_key, city) DO UPDATE SET id = id
		RETURNING id, name, url`, site, externalKey, productURL, city).Scan(&product.ID, &product.Name, &product.URL); err != nil {
		return Product{}, false, fmt.Errorf("storage: сохранение товара %s/%s: %w", site, externalKey, err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO subscriptions (user_id, product_id) VALUES (?, ?)
		ON CONFLICT (user_id, product_id) DO UPDATE SET active = 1`, userID, product.ID); err != nil {
		return Product{}, false, fmt.Errorf("storage: подписка на товар %d: %w", product.ID, err)
	}

	if err := tx.Commit(); err != nil {
		return Product{}, false, fmt.Errorf("storage: фиксация транзакции: %w", err)
	}
	return product, true, nil
}

// ListSubscriptions возвращает активные подписки пользователя.
func (s *Store) ListSubscriptions(ctx context.Context, chatID int64) ([]Tracked, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.threshold_pct,
		       p.id, p.site, p.external_key, p.url, p.name, p.city,
		       last.price_kopecks, last.checked_at
		FROM subscriptions s
		JOIN users u ON u.id = s.user_id
		JOIN products p ON p.id = s.product_id
		LEFT JOIN (
		    SELECT product_id, price_kopecks, checked_at,
		           ROW_NUMBER() OVER (PARTITION BY product_id ORDER BY checked_at DESC, id DESC) AS rn
		    FROM price_history
		) last ON last.product_id = p.id AND last.rn = 1
		WHERE u.tg_chat_id = ? AND s.active = 1
		ORDER BY s.id`, chatID)
	if err != nil {
		return nil, fmt.Errorf("storage: список подписок для %d: %w", chatID, err)
	}
	defer rows.Close()

	var out []Tracked
	for rows.Next() {
		var t Tracked
		if err := rows.Scan(&t.SubscriptionID, &t.ThresholdPct,
			&t.Product.ID, &t.Product.Site, &t.Product.ExternalKey,
			&t.Product.URL, &t.Product.Name, &t.Product.City,
			&t.LastPriceKopecks, &t.LastCheckedAt); err != nil {
			return nil, fmt.Errorf("storage: чтение подписки: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: обход подписок: %w", err)
	}
	return out, nil
}

// DeleteSubscription убирает подписку. Возвращает false, если её не было.
func (s *Store) DeleteSubscription(ctx context.Context, chatID, productID int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM subscriptions
		WHERE product_id = ?
		  AND user_id = (SELECT id FROM users WHERE tg_chat_id = ?)`, productID, chatID)
	if err != nil {
		return false, fmt.Errorf("storage: удаление подписки на товар %d: %w", productID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("storage: результат удаления: %w", err)
	}
	return affected > 0, nil
}
