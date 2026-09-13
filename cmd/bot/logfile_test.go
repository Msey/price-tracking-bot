package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogFileRotatesBySize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data", "bot.log")

	f, err := openLogFile(path, 64, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	line := strings.Repeat("x", 40) + "\n"
	for i := 0; i < 5; i++ {
		if _, err := f.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}

	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() > 64 {
		t.Fatalf("текущий файл %d байт, предел 64", st.Size())
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("прошлый файл должен остаться: %v", err)
	}
	if _, err := os.Stat(path + ".2"); err != nil {
		t.Fatalf("предпрошлый файл должен остаться: %v", err)
	}
	// Больше keep файлов не копится.
	if _, err := os.Stat(path + ".3"); err == nil {
		t.Fatal("файлов больше, чем keep")
	}
}

func TestLogFileAppendsWithinLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bot.log")
	f, err := openLogFile(path, 1<<20, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("первая\n")); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := openLogFile(path, 1<<20, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if _, err := again.Write([]byte("вторая\n")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); got != "первая\nвторая\n" {
		t.Fatalf("лог перезаписан вместо дописывания: %q", got)
	}
}
