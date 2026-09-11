package fetch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

// Живой заход на Яндекс.Маркет. По умолчанию выключен: MARKET_LIVE=1.
func TestLiveMarketFetch(t *testing.T) {
	if os.Getenv("MARKET_LIVE") != "1" {
		t.Skip("живой Маркет только с MARKET_LIVE=1")
	}

	chrome := `C:\Program Files\Google\Chrome\Application\chrome.exe`
	if _, err := os.Stat(chrome); err != nil {
		t.Skip("нет Chrome")
	}

	m := NewMarket(ShopOptions{
		ProfileDir:      filepath.Join(t.TempDir(), "profile"),
		ChromePath:      chrome,
		Headless:        false,
		CircuitCooldown: 45 * time.Minute,
	})
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()

	snap, err := m.Fetch(ctx, storage.Product{
		URL: "https://market.yandex.ru/card/begovaya-dorozhka-sportflag-glow-run-a/4638722913",
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	t.Logf("name=%q price=%d available=%v", snap.Name, snap.PriceKopecks, snap.Available)
	if snap.PriceKopecks <= 0 {
		t.Fatal("пустая цена")
	}
}
