//go:build !windows

package fetch

import (
	"fmt"
	"os/exec"
	"time"
)

func killChromeWithProfile(string) {}

func startChrome(string, string, string, string, bool) (*exec.Cmd, error) {
	return nil, fmt.Errorf("запуск Chrome только на Windows")
}

func installUnpackedToProfile(string, string, string) (string, error) {
	return "", fmt.Errorf("установка расширения только на Windows")
}

func offscreenOriginOS() (int, int) { return -2400, -2400 }

func demoteChromeWithProfile(string) {}

func revealChromeWithProfile(string) {}

func demoteChrome(uint32, string, time.Duration) {}
