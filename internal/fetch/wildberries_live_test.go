package fetch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

// Живой заход на Wildberries. По умолчанию выключен: WB_LIVE=1.
func TestLiveWildberriesFetch(t *testing.T) {
	if os.Getenv("WB_LIVE") != "1" {
		t.Skip("живой Wildberries только с WB_LIVE=1")
	}

	chrome := `C:\Program Files\Google\Chrome\Application\chrome.exe`
	if _, err := os.Stat(chrome); err != nil {
		t.Skip("нет Chrome")
	}

	profile := os.Getenv("CHROME_PROFILE")
	if profile == "" {
		profile = filepath.Join(t.TempDir(), "profile")
	}

	w := NewWildberries(ShopOptions{
		ProfileDir:      profile,
		ChromePath:      chrome,
		CircuitCooldown: 45 * time.Minute,
	})
	defer w.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	snap, err := w.Fetch(ctx, storage.Product{
		Site: "wildberries",
		URL:  "https://www.wildberries.ru/catalog/949425394/detail.aspx",
	})
	if err != nil {
		if errors.Is(err, ErrChallenge) {
			t.Logf("антибот WB: %v", err)
		}
		t.Fatalf("Fetch: %v", err)
	}
	t.Logf("name=%q price=%d available=%v", snap.Name, snap.PriceKopecks, snap.Available)
	if snap.PriceKopecks <= 0 {
		t.Fatal("пустая цена")
	}
}
