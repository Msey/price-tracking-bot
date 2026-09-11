package gui

import (
	"path/filepath"
	"strings"
)

// trayExeName — имя собранного бота. Старые копии из других папок
// после пересборки тоже считаются нашими иконками в трее.
const trayExeName = "price-tracking-bot.exe"

func normalizeExePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, `"`)
	p = strings.ReplaceAll(p, `/`, `\`)
	return strings.ToLower(filepath.Clean(p))
}

// isOurNotifyIconPath — запись оболочки про иконку трея относится
// к этому боту: тот же exe или другая копия price-tracking-bot.exe.
func isOurNotifyIconPath(stored, self string) bool {
	stored = normalizeExePath(stored)
	self = normalizeExePath(self)
	if stored == "" || self == "" || stored == "." || self == "." {
		return false
	}
	if stored == self {
		return true
	}
	return filepath.Base(stored) == trayExeName && filepath.Base(self) == trayExeName
}
