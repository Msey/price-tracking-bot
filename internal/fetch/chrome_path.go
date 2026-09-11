package fetch

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func resolveChromePath(explicit string) string {
	if p := strings.TrimSpace(explicit); p != "" {
		if full, err := exec.LookPath(p); err == nil {
			return full
		}
		if abs, err := filepath.Abs(p); err == nil {
			if st, err := os.Stat(abs); err == nil && !st.IsDir() {
				return abs
			}
		}
		return p
	}
	for _, p := range chromeCandidates() {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func chromeCandidates() []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	local := os.Getenv("LOCALAPPDATA")
	return []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		filepath.Join(local, `Google\Chrome\Application\chrome.exe`),
	}
}
