//go:build !windows

package fetch

import (
	"fmt"
	"os/exec"
)

func killChromeWithProfile(string) {}

func startChrome(string, string, string, string, string) (*exec.Cmd, error) {
	return nil, fmt.Errorf("запуск Chrome только на Windows")
}

func installUnpackedToProfile(string, string, string) (string, error) {
	return "", fmt.Errorf("установка расширения только на Windows")
}
