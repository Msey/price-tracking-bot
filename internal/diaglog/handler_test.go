package diaglog

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestSwitchOffDropsInfoKeepsWarn(t *testing.T) {
	var buf bytes.Buffer
	sw := &Switch{}
	log := slog.New(Wrap(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}), sw))

	log.Debug("шаг")
	log.Info("действие")
	log.Warn("сбой")
	log.Error("ошибка")

	got := buf.String()
	if strings.Contains(got, "шаг") || strings.Contains(got, "действие") {
		t.Fatalf("при выключенных логах не должно быть Info/Debug: %s", got)
	}
	if !strings.Contains(got, "сбой") || !strings.Contains(got, "ошибка") {
		t.Fatalf("Warn/Error должны писаться всегда: %s", got)
	}
}

func TestSwitchOnWritesInfoAndDebug(t *testing.T) {
	var buf bytes.Buffer
	sw := &Switch{}
	log := slog.New(Wrap(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}), sw))

	sw.Set(true)
	log.Debug("шаг")
	log.Info("действие")

	got := buf.String()
	if !strings.Contains(got, "шаг") || !strings.Contains(got, "действие") {
		t.Fatalf("ожидались Debug и Info: %s", got)
	}

	sw.Set(false)
	buf.Reset()
	log.Info("после выключения")
	if strings.Contains(buf.String(), "после выключения") {
		t.Fatal("после выключения Info не должен писаться")
	}
}

func TestWithAttrsKeepsTheSameSwitch(t *testing.T) {
	var buf bytes.Buffer
	sw := &Switch{}
	log := slog.New(Wrap(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}), sw))
	child := log.With("site", "dns")
	child.Info("скрыто")
	if buf.Len() != 0 {
		t.Fatal("дочерний логгер не должен обходить тумблер")
	}
	sw.Set(true)
	child.Info("видно")
	if !strings.Contains(buf.String(), "видно") || !strings.Contains(buf.String(), "dns") {
		t.Fatalf("дочерний логгер после включения: %s", buf.String())
	}
}

func TestDefaultSwitchIsOff(t *testing.T) {
	if (&Switch{}).Enabled() {
		t.Fatal("по умолчанию подробные логи выключены")
	}
	if (*Switch)(nil).Enabled() {
		t.Fatal("nil-тумблер тоже выключен")
	}
}
