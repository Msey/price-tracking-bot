package main

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

const (
	// logSizeLimit — после какого размера начинать новый файл. Лог пишется
	// при каждой проверке цены и без предела растёт всё время работы бота.
	logSizeLimit = 8 << 20
	// logKeep — сколько прошлых файлов держать рядом: bot.log.1, bot.log.2.
	logKeep = 2
)

// logFile — файл лога с ротацией по размеру. Своя реализация вместо
// внешней зависимости: нужен ровно один файл и один предел.
type logFile struct {
	mu    sync.Mutex
	path  string
	limit int64
	keep  int
	f     *os.File
	size  int64
}

func openLogFile(path string, limit int64, keep int) (*logFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	l := &logFile{path: path, limit: limit, keep: keep}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *logFile) open() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	var size int64
	if st, err := f.Stat(); err == nil {
		size = st.Size()
	}
	l.f, l.size = f, size
	return nil
}

func (l *logFile) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return len(p), nil
	}
	// Ротация до записи: строку лога не разрываем между файлами.
	if l.limit > 0 && l.size > 0 && l.size+int64(len(p)) > l.limit {
		l.rotate()
	}
	if l.f == nil {
		return len(p), nil
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

// rotate сдвигает bot.log.1 в bot.log.2, а bot.log — в bot.log.1.
// Ошибки глушим: лог не та причина, по которой стоит останавливать бота.
func (l *logFile) rotate() {
	_ = l.f.Close()
	l.f, l.size = nil, 0
	for i := l.keep; i >= 1; i-- {
		src := l.path
		if i > 1 {
			src = l.path + "." + strconv.Itoa(i-1)
		}
		dst := l.path + "." + strconv.Itoa(i)
		_ = os.Remove(dst)
		_ = os.Rename(src, dst)
	}
	if l.keep < 1 {
		_ = os.Remove(l.path)
	}
	_ = l.open()
}

func (l *logFile) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}
