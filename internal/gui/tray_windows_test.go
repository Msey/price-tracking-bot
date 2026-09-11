//go:build windows

package gui

import (
	"syscall"
	"testing"
)

func TestForgetDeadTrayIconsDoesNotPanic(t *testing.T) {
	forgetDeadTrayIcons()
	purgeOurNotifyIconSettings()
	deleteGUIDIcon()
}

func TestPutUTF16FitsDestination(t *testing.T) {
	var dst [8]uint16
	putUTF16(dst[:], "hello")
	if got := syscall.UTF16ToString(dst[:]); got != "hello" {
		t.Fatalf("got %q", got)
	}
	putUTF16(dst[:], "1234567890")
	if dst[len(dst)-1] != 0 {
		t.Fatal("нет завершающего нуля")
	}
}
