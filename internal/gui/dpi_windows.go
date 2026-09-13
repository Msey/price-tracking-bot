//go:build windows

package gui

import "golang.org/x/sys/windows"

// dpiAwarenessContextPerMonitorV2 — HANDLE(-4). Без Per-Monitor V2
// Windows растягивает окно картинкой, когда рядом стартует Chrome.
const dpiAwarenessContextPerMonitorV2 = ^uintptr(3)

const processPerMonitorDPIAware = uintptr(2)

func enablePerMonitorDPI() {
	user32 := windows.NewLazySystemDLL("user32.dll")
	if setCtx := user32.NewProc("SetProcessDpiAwarenessContext"); setCtx.Find() == nil {
		ok, _, _ := setCtx.Call(dpiAwarenessContextPerMonitorV2)
		if ok != 0 {
			return
		}
	}
	shcore := windows.NewLazySystemDLL("shcore.dll")
	if set := shcore.NewProc("SetProcessDpiAwareness"); set.Find() == nil {
		_, _, _ = set.Call(processPerMonitorDPIAware)
	}
}
