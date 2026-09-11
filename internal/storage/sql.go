package storage

// Запросы к SQLite — только константы и плейсхолдеры ?.
// В текст запроса нельзя подставлять пользовательские строки.
const (
	pragmaJournal        = `PRAGMA journal_mode = WAL`
	pragmaBusyTimeout    = `PRAGMA busy_timeout = 5000`
	pragmaForeignKeys    = `PRAGMA foreign_keys = ON`
	pragmaUserVersion    = `PRAGMA user_version`
	pragmaUserVersionSet = `PRAGMA user_version = %d`

	sqlEnsureUser = `
		INSERT INTO users (tg_chat_id) VALUES (?)
		ON CONFLICT (tg_chat_id) DO UPDATE SET tg_chat_id = excluded.tg_chat_id
		RETURNING id`

	sqlFindActiveSubscription = `
		SELECT p.id, p.site, p.external_key, p.url, p.name, p.city
		FROM products p
		JOIN subscriptions s ON s.product_id = p.id AND s.user_id = ? AND s.active = 1
		WHERE p.site = ? AND p.external_key = ? AND p.city = ?`

	sqlCountActiveSubscriptions = `
		SELECT COUNT(*) FROM subscriptions WHERE user_id = ? AND active = 1`

	sqlUpsertProduct = `
		INSERT INTO products (site, external_key, url, city) VALUES (?, ?, ?, ?)
		ON CONFLICT (site, external_key, city) DO UPDATE SET id = id
		RETURNING id, name, url`

	sqlUpsertSubscription = `
		INSERT INTO subscriptions (user_id, product_id) VALUES (?, ?)
		ON CONFLICT (user_id, product_id) DO UPDATE SET active = 1`

	sqlListSubscriptions = `
		SELECT p.id, p.site, p.external_key, p.url, p.name, p.city,
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
		ORDER BY s.id`

	sqlListAllRequests = `
		SELECT s.id, s.created_at, u.tg_chat_id,
		       p.id, p.site, p.external_key, p.url, p.name, p.city,
		       last.price_kopecks, last.available, last.checked_at,
		       err.kind, err.occurred_at
		FROM subscriptions s
		JOIN users u ON u.id = s.user_id
		JOIN products p ON p.id = s.product_id
		LEFT JOIN (
		    SELECT product_id, price_kopecks, available, checked_at,
		           ROW_NUMBER() OVER (PARTITION BY product_id ORDER BY checked_at DESC, id DESC) AS rn
		    FROM price_history
		) last ON last.product_id = p.id AND last.rn = 1
		LEFT JOIN (
		    SELECT product_id, kind, occurred_at,
		           ROW_NUMBER() OVER (PARTITION BY product_id ORDER BY occurred_at DESC, id DESC) AS rn
		    FROM fetch_errors
		) err ON err.product_id = p.id AND err.rn = 1
		WHERE s.active = 1
		ORDER BY s.id DESC`

	sqlDeleteSubscription = `
		DELETE FROM subscriptions
		WHERE product_id = ?
		  AND user_id = (SELECT id FROM users WHERE tg_chat_id = ?)`

	sqlCountOwnActive = `
		SELECT COUNT(*)
		FROM subscriptions s
		JOIN users u ON u.id = s.user_id
		WHERE u.tg_chat_id = ? AND s.active = 1`

	sqlOwnAt = `
		SELECT s.id, p.id, p.site, p.external_key, p.url, p.name, p.city
		FROM subscriptions s
		JOIN users u ON u.id = s.user_id
		JOIN products p ON p.id = s.product_id
		WHERE u.tg_chat_id = ? AND s.active = 1
		ORDER BY s.id
		LIMIT 1 OFFSET ?`

	sqlDeleteOwnSub = `
		DELETE FROM subscriptions
		WHERE id = ?
		  AND user_id = (SELECT id FROM users WHERE tg_chat_id = ?)`

	sqlDeleteProductSubscriptions = `DELETE FROM subscriptions WHERE product_id = ?`

	sqlProductsDue = `
		SELECT p.id, p.site, p.external_key, p.url, p.name, p.city
		FROM products p
		JOIN subscriptions s ON s.product_id = p.id AND s.active = 1
		LEFT JOIN price_history h ON h.product_id = p.id
		WHERE p.site = ?
		GROUP BY p.id
		HAVING MAX(h.checked_at) IS NULL OR MAX(h.checked_at) <= ?
		ORDER BY MAX(h.checked_at) IS NOT NULL, MAX(h.checked_at) ASC
		LIMIT ?`

	sqlActiveProducts = `
		SELECT p.id, p.site, p.external_key, p.url, p.name, p.city
		FROM products p
		JOIN subscriptions s ON s.product_id = p.id AND s.active = 1
		WHERE p.site = ?
		GROUP BY p.id
		ORDER BY p.id`

	sqlLastSnapshot = `
		SELECT id, price_kopecks, currency, available
		FROM price_history
		WHERE product_id = ?
		ORDER BY checked_at DESC, id DESC
		LIMIT 1`

	sqlTouchSnapshot = `UPDATE price_history SET checked_at = ? WHERE id = ?`

	sqlInsertSnapshot = `
		INSERT INTO price_history (product_id, price_kopecks, currency, available, checked_at)
		VALUES (?, ?, ?, ?, ?)`

	sqlUpdateProductName = `UPDATE products SET name = ? WHERE id = ? AND name != ?`

	sqlLastSnapshots = `
		SELECT price_kopecks, available, checked_at
		FROM price_history
		WHERE product_id = ?
		ORDER BY checked_at DESC, id DESC
		LIMIT ?`

	// %s — только повторяющиеся «?», см. sqlPlaceholders. Не подставлять данные.
	sqlHistories = `
		SELECT product_id, price_kopecks, available, checked_at
		FROM (
		    SELECT product_id, price_kopecks, available, checked_at,
		           ROW_NUMBER() OVER (PARTITION BY product_id ORDER BY checked_at DESC, id DESC) AS rn
		    FROM price_history
		    WHERE product_id IN (%s)
		)
		WHERE rn <= ?
		ORDER BY product_id, checked_at ASC, rn DESC`

	sqlNotified = `
		SELECT last_notified_kopecks, last_notified_available FROM products WHERE id = ?`

	sqlMarkNotified = `
		UPDATE products SET last_notified_kopecks = ?, last_notified_available = ? WHERE id = ?`

	sqlSubscriberChats = `
		SELECT u.tg_chat_id
		FROM subscriptions s
		JOIN users u ON u.id = s.user_id
		WHERE s.product_id = ? AND s.active = 1`

	sqlInsertFetchError = `
		INSERT INTO fetch_errors (product_id, site, kind, message) VALUES (?, ?, ?, ?)`

	sqlProductByID = `
		SELECT id, site, external_key, url, name, city FROM products WHERE id = ?`
)

func sqlPlaceholders(n int) string {
	if n < 1 {
		return "?"
	}
	buf := make([]byte, 0, n*2-1)
	for i := 0; i < n; i++ {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, '?')
	}
	return string(buf)
}
