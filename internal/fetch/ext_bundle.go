package fetch

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed ext/manifest.json ext/background.js ext/extract.js ext/rules.json
var extFS embed.FS

// extBundle — распакованное расширение на диске. Chrome читает его из
// каталога, поэтому файлы приходится выкладывать при каждом запуске:
// адрес локального сервера и токен подставляются в код.
type extBundle struct {
	origin string
	token  string
}

// write выкладывает расширение и возвращает каталог для --load-extension.
func (e extBundle) write() (string, error) {
	dir, err := extensionDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("chrome: каталог расширения: %w", err)
	}
	if err := copyEmbeddedExt(dir); err != nil {
		return "", err
	}
	// Остаток прежней схемы с отдельным файлом настроек: если он остался
	// в каталоге, Chrome грузит старый адрес сервера.
	_ = os.Remove(filepath.Join(dir, "config.js"))
	if err := stampManifest(dir); err != nil {
		return "", err
	}
	// В этих двух файлах лежит токен локального сервера, поэтому режим 0600.
	head := fmt.Sprintf("const EXT_ORIGIN = %q;\nconst EXT_TOKEN = %q;\n", e.origin, e.token)
	for _, name := range []string{"background.js", "extract.js"} {
		p := filepath.Join(dir, name)
		raw, err := os.ReadFile(p)
		if err != nil {
			return "", fmt.Errorf("chrome: %s: %w", name, err)
		}
		if err := os.WriteFile(p, append([]byte(head), raw...), 0o600); err != nil {
			return "", fmt.Errorf("chrome: %s: %w", name, err)
		}
	}
	return dir, nil
}

// extensionDir — каталог расширения в профиле пользователя. Общий временный
// каталог не годится: рядом лежит токен локального сервера, а background.js
// с extract.js несут его в тексте.
func extensionDir() (string, error) {
	base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if base == "" {
		return "", fmt.Errorf("chrome: не задан LOCALAPPDATA, некуда положить расширение")
	}
	return filepath.Join(base, "price-tracking-bot", "chrome-ext"), nil
}

func copyEmbeddedExt(dir string) error {
	return fs.WalkDir(extFS, "ext", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(extFS, path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, filepath.Base(path)), b, 0o644)
	})
}

// stampManifest поднимает версию на каждую выкладку: иначе Chrome считает
// расширение тем же и оставляет в профиле прежний код.
func stampManifest(dir string) error {
	p := filepath.Join(dir, "manifest.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("chrome: manifest.json: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("chrome: manifest.json: %w", err)
	}
	now := time.Now().Unix()
	m["version"] = fmt.Sprintf("1.%d.%d", now/65536, now%65536)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("chrome: manifest.json: %w", err)
	}
	return os.WriteFile(p, append(out, '\n'), 0o644)
}
