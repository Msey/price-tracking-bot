//go:build windows

package fetch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// powershellPath — полный путь из %SystemRoot%, а не поиск по PATH: иначе
// запись в любой каталог из PATH выше System32 даёт запуск чужого кода.
func powershellPath() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
}

func killChromeWithProfile(profile string) {
	abs, err := filepath.Abs(profile)
	if err != nil || abs == "" {
		return
	}
	needle := strings.ReplaceAll(strings.ToLower(abs), "'", "''")
	ps := `
$n = '` + needle + `'
Get-CimInstance Win32_Process -Filter "Name = 'chrome.exe'" | ForEach-Object {
  if ($_.CommandLine -and $_.CommandLine.ToLower().Contains($n)) {
    Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
  }
}
`
	cmd := exec.Command(powershellPath(), "-NoProfile", "-NonInteractive", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Run()
}
