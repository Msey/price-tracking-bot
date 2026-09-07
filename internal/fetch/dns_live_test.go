package fetch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

// Один живой заход на DNS. По умолчанию выключен: DNS_LIVE=1.
func TestLiveDNSFetch(t *testing.T) {
	if os.Getenv("DNS_LIVE") != "1" {
		t.Skip("живой DNS только с DNS_LIVE=1")
	}

	chrome := `C:\Program Files\Google\Chrome\Application\chrome.exe`
	if _, err := os.Stat(chrome); err != nil {
		t.Skip("нет Chrome")
	}

	d := NewDNS(DNSOptions{
		ProfileDir:      filepath.Join(t.TempDir(), "profile"),
		ChromePath:      chrome,
		Headless:        false,
		CircuitCooldown: 45 * time.Minute,
	})
	defer d.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()

	snap, err := d.Fetch(ctx, storage.Product{
		URL:  "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/",
		City: "moscow",
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	t.Logf("name=%q price=%d available=%v", snap.Name, snap.PriceKopecks, snap.Available)
	if snap.PriceKopecks <= 0 {
		t.Fatal("пустая цена")
	}
}
