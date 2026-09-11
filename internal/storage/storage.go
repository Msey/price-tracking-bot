// Package storage хранит пользователей, товары и историю цен в SQLite.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	_ "modernc.org/sqlite" // драйвер SQLite на чистом Go, без CGO
)

// MaxSubscriptions — потолок активных ссылок на одного пользователя Telegram.
const MaxSubscriptions = 10

// ErrTooManySubscriptions — пользователь уже держит MaxSubscriptions товаров.
var ErrTooManySubscriptions = errors.New("слишком много подписок")

// ErrNoUser — нет валидного Telegram user id (0 или отрицательный).
var ErrNoUser = errors.New("нет пользователя")

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
	Product          Product
	LastPriceKopecks sql.NullInt64
	LastCheckedAt    sql.NullString
}

// Open открывает базу и приводит схему к актуальной версии.
func Open(path string) (*Store, error) {
	if err := validDBPath(path); err != nil {
		return nil, err
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
		pragmaJournal,
		pragmaBusyTimeout,
		pragmaForeignKeys,
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

func validDBPath(path string) error {
	if path == "" {
		return fmt.Errorf("storage: пустой путь к базе")
	}
	lower := strings.ToLower(path)
	if strings.ContainsAny(path, "?\x00") || strings.HasPrefix(lower, "file:") {
		return fmt.Errorf("storage: путь к базе не должен быть URI SQLite")
	}
	return nil
}

// EnsureUser регистрирует пользователя, если он ещё не известен.
func (s *Store) EnsureUser(ctx context.Context, chatID int64) (int64, error) {
	if chatID <= 0 {
		return 0, ErrNoUser
	}
	var id int64
	err := s.db.QueryRowContext(ctx, sqlEnsureUser, chatID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("storage: регистрация пользователя %d: %w", chatID, err)
	}
	return id, nil
}

// AddSubscription подписывает пользователя на товар, создавая товар при
// необходимости. Второй результат — false, если подписка уже была.
func (s *Store) AddSubscription(ctx context.Context, chatID int64, site, externalKey, productURL, city string) (Product, bool, error) {
	if chatID <= 0 {
		return Product{}, false, ErrNoUser
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Product{}, false, fmt.Errorf("storage: начало транзакции: %w", err)
	}
	defer tx.Rollback()

	var userID int64
	if err := tx.QueryRowContext(ctx, sqlEnsureUser, chatID).Scan(&userID); err != nil {
		return Product{}, false, fmt.Errorf("storage: регистрация пользователя %d: %w", chatID, err)
	}

	var existing Product
	err = tx.QueryRowContext(ctx, sqlFindActiveSubscription,
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
	if err := tx.QueryRowContext(ctx, sqlCountActiveSubscriptions, userID).Scan(&n); err != nil {
		return Product{}, false, fmt.Errorf("storage: подсчёт подписок: %w", err)
	}
	if n >= MaxSubscriptions {
		return Product{}, false, ErrTooManySubscriptions
	}

	product := Product{Site: site, ExternalKey: externalKey, URL: productURL, City: city}
	// id = id — намеренный no-op: SQLite не возвращает строку при DO NOTHING,
	// а URL общего товара трогать нельзя, иначе второй подписчик перезапишет
	// ссылку у всех остальных.
	if err := tx.QueryRowContext(ctx, sqlUpsertProduct, site, externalKey, productURL, city).Scan(&product.ID, &product.Name, &product.URL); err != nil {
		return Product{}, false, fmt.Errorf("storage: сохранение товара %s/%s: %w", site, externalKey, err)
	}

	if _, err := tx.ExecContext(ctx, sqlUpsertSubscription, userID, product.ID); err != nil {
		return Product{}, false, fmt.Errorf("storage: подписка на товар %d: %w", product.ID, err)
	}

	if err := tx.Commit(); err != nil {
		return Product{}, false, fmt.Errorf("storage: фиксация транзакции: %w", err)
	}
	return product, true, nil
}

// ListSubscriptions возвращает только активные подписки этого пользователя Telegram.
// Чужие ссылки сюда не попадают: это граница для команды /list.
func (s *Store) ListSubscriptions(ctx context.Context, chatID int64) ([]Tracked, error) {
	if chatID <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, sqlListSubscriptions, chatID)
	if err != nil {
		return nil, fmt.Errorf("storage: список подписок для %d: %w", chatID, err)
	}
	defer rows.Close()

	var out []Tracked
	for rows.Next() {
		var t Tracked
		if err := rows.Scan(
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

// Request — заявка на трекинг: подписка пользователя на товар.
type Request struct {
	SubscriptionID   int64
	CreatedAt        string
	ChatID           int64
	Product          Product
	LastPriceKopecks sql.NullInt64
	LastAvailable    sql.NullInt64
	LastCheckedAt    sql.NullString
	LastErrorKind    sql.NullString
	LastErrorAt      sql.NullString
}

// ListAllRequests возвращает все активные заявки, сначала новые.
func (s *Store) ListAllRequests(ctx context.Context) ([]Request, error) {
	rows, err := s.db.QueryContext(ctx, sqlListAllRequests)
	if err != nil {
		return nil, fmt.Errorf("storage: список заявок: %w", err)
	}
	defer rows.Close()

	var out []Request
	for rows.Next() {
		var r Request
		if err := rows.Scan(
			&r.SubscriptionID, &r.CreatedAt, &r.ChatID,
			&r.Product.ID, &r.Product.Site, &r.Product.ExternalKey,
			&r.Product.URL, &r.Product.Name, &r.Product.City,
			&r.LastPriceKopecks, &r.LastAvailable, &r.LastCheckedAt,
			&r.LastErrorKind, &r.LastErrorAt,
		); err != nil {
			return nil, fmt.Errorf("storage: чтение заявки: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: обход заявок: %w", err)
	}
	return out, nil
}

// DeleteSubscription убирает подписку только у этого пользователя.
// Чужую ссылку снять нельзя: чужой user_id в условие не попадает.
func (s *Store) DeleteSubscription(ctx context.Context, chatID, productID int64) (bool, error) {
	if chatID <= 0 || productID <= 0 {
		return false, nil
	}
	res, err := s.db.ExecContext(ctx, sqlDeleteSubscription, productID, chatID)
	if err != nil {
		return false, fmt.Errorf("storage: удаление подписки на товар %d: %w", productID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("storage: результат удаления: %w", err)
	}
	return affected > 0, nil
}

// DeleteOwnAt снимает n-й товар (номер как в /list, с единицы) только у этого
// пользователя. total — сколько активных ссылок было до удаления.
func (s *Store) DeleteOwnAt(ctx context.Context, chatID int64, n int) (Product, int, bool, error) {
	if chatID <= 0 || n < 1 {
		return Product{}, 0, false, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Product{}, 0, false, fmt.Errorf("storage: начало транзакции: %w", err)
	}
	defer tx.Rollback()

	var total int
	if err := tx.QueryRowContext(ctx, sqlCountOwnActive, chatID).Scan(&total); err != nil {
		return Product{}, 0, false, fmt.Errorf("storage: подсчёт подписок для %d: %w", chatID, err)
	}
	if n > total {
		return Product{}, total, false, nil
	}

	var product Product
	var subID int64
	err = tx.QueryRowContext(ctx, sqlOwnAt, chatID, n-1).Scan(
		&subID, &product.ID, &product.Site, &product.ExternalKey,
		&product.URL, &product.Name, &product.City)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Product{}, total, false, nil
		}
		return Product{}, total, false, fmt.Errorf("storage: номер %d из списка %d: %w", n, chatID, err)
	}

	res, err := tx.ExecContext(ctx, sqlDeleteOwnSub, subID, chatID)
	if err != nil {
		return Product{}, total, false, fmt.Errorf("storage: удаление подписки %d: %w", subID, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Product{}, total, false, fmt.Errorf("storage: результат удаления: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Product{}, total, false, fmt.Errorf("storage: фиксация транзакции: %w", err)
	}
	return product, total, affected > 0, nil
}

// DeleteProductSubscriptions снимает товар со всех чатов. Карточка
// остаётся в базе, чтобы повторное /add не потеряло историю цен.
func (s *Store) DeleteProductSubscriptions(ctx context.Context, productID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, sqlDeleteProductSubscriptions, productID)
	if err != nil {
		return 0, fmt.Errorf("storage: удаление подписок на товар %d: %w", productID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("storage: результат удаления подписок: %w", err)
	}
	return n, nil
}
