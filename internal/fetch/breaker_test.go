package fetch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBreakerPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "circuit-dns.json")
	b := NewFileBreaker(time.Minute, path)
	b.Trip("403")
	until := b.RetryAt()
	if b.Allow() {
		t.Fatal("после Trip ходить нельзя")
	}

	b2 := NewFileBreaker(time.Minute, path)
	if b2.Allow() {
		t.Fatal("после перезапуска предохранитель должен остаться")
	}
	if b2.Reason() != "403" {
		t.Fatalf("причина %q", b2.Reason())
	}
	got := b2.RetryAt()
	if got.Sub(until).Abs() > time.Second {
		t.Fatalf("until %v vs %v", got, until)
	}
}

func TestBreakerExpiredFileAllows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "circuit-dns.json")
	raw := []byte(`{"until":"2000-01-01T00:00:00Z","reason":"old"}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	b := NewFileBreaker(time.Minute, path)
	if !b.Allow() {
		t.Fatal("истёкшая пауза не должна блокировать")
	}
}

func TestCircuitFile(t *testing.T) {
	got := circuitFile(filepath.Join("data", "chrome-plain"), "dns")
	if filepath.Base(got) != "circuit-dns.json" {
		t.Fatalf("got %q", got)
	}
	if circuitFile("", "dns") != "" {
		t.Fatal("пустой профиль")
	}
}
