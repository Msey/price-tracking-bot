package fetch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

// Живой заход на Ozon. По умолчанию выключен: OZON_LIVE=1.
func TestLiveOzonFetch(t *testing.T) {
	if os.Getenv("OZON_LIVE") != "1" {
		t.Skip("живой Ozon только с OZON_LIVE=1")
	}

	chrome := `C:\Program Files\Google\Chrome\Application\chrome.exe`
	if _, err := os.Stat(chrome); err != nil {
		t.Skip("нет Chrome")
	}

	profile := os.Getenv("CHROME_PROFILE")
	if profile == "" {
		profile = filepath.Join(t.TempDir(), "profile")
	}

	o := NewOzon(OzonOptions{
		ProfileDir:      profile,
		ChromePath:      chrome,
		Headless:        false,
		CircuitCooldown: 45 * time.Minute,
	})
	defer o.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()

	snap, err := o.Fetch(ctx, storage.Product{
		URL: "https://www.ozon.ru/product/germetik-akrilovyy-moment-420-gr-belyy-universalnyy-morozostoykiy-2422341064",
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	t.Logf("name=%q price=%d available=%v", snap.Name, snap.PriceKopecks, snap.Available)
	if snap.PriceKopecks <= 0 {
		t.Fatal("пустая цена")
	}
}
