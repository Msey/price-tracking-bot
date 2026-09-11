//go:build windows

package gui

import (
	_ "embed"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Тот же манифест уходит в ресурс exe через rsrc, поэтому он лежит файлом.
//
//go:embed app.manifest
var comctlManifest string

var (
	actOnce    sync.Once
	actErr     error
	kernel32   = windows.NewLazySystemDLL("kernel32.dll")
	createAC   = kernel32.NewProc("CreateActCtxW")
	activateAC = kernel32.NewProc("ActivateActCtx")
)

type actctx struct {
	size                  uint32
	flags                 uint32
	source                *uint16
	processorArchitecture uint16
	langID                uint16
	assemblyDirectory     *uint16
	resourceName          *uint16
	applicationName       *uint16
	module                windows.Handle
}

func enableCommonControlsV6() error {
	actOnce.Do(func() {
		dir, err := os.MkdirTemp("", "price-bot-actctx")
		if err != nil {
			actErr = err
			return
		}
		// CreateActCtxW читает манифест сразу, поэтому файл нужен только
		// на время вызова: иначе %TEMP% копит каталог на каждый запуск.
		defer os.RemoveAll(dir)
		path := filepath.Join(dir, "app.manifest")
		if err := os.WriteFile(path, []byte(comctlManifest), 0o644); err != nil {
			actErr = err
			return
		}
		src, err := windows.UTF16PtrFromString(path)
		if err != nil {
			actErr = err
			return
		}
		ac := actctx{source: src}
		ac.size = uint32(unsafe.Sizeof(ac))
		h, _, err := createAC.Call(uintptr(unsafe.Pointer(&ac)))
		if h == ^uintptr(0) {
			actErr = err
			return
		}
		var cookie uintptr
		ok, _, err := activateAC.Call(h, uintptr(unsafe.Pointer(&cookie)))
		if ok == 0 {
			actErr = err
		}
	})
	return actErr
}
