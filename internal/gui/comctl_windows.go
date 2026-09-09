//go:build windows

package gui

import (
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const comctlManifest = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity version="1.0.0.0" processorArchitecture="*" name="PriceTrackingBot" type="win32"/>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</dpiAware>
    </windowsSettings>
  </application>
</assembly>
`

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
