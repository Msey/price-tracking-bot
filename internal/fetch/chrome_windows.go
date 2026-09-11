//go:build windows

package fetch

import (
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

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
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Run()
}
