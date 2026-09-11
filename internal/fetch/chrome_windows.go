//go:build windows

package fetch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	swHide    = 0
	swRestore = 9
)

var (
	user32            = windows.NewLazySystemDLL("user32.dll")
	procShowWindow    = user32.NewProc("ShowWindow")
	procSetForeground = user32.NewProc("SetForegroundWindow")

	chromeWinMu   sync.Mutex
	chromeWinShow bool
	chromeWinPIDs map[uint32]struct{}
	chromeEnumCB  = syscall.NewCallback(enumChromeWindow)
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

func hideChromeWindows() { setChromeWindowsVisible(false) }
func showChromeWindows() { setChromeWindowsVisible(true) }

func setChromeWindowsVisible(show bool) {
	pids := chromeDescendantPIDs()
	if len(pids) == 0 {
		return
	}
	chromeWinMu.Lock()
	defer chromeWinMu.Unlock()
	chromeWinShow = show
	chromeWinPIDs = make(map[uint32]struct{}, len(pids))
	for _, p := range pids {
		chromeWinPIDs[p] = struct{}{}
	}
	_ = windows.EnumWindows(chromeEnumCB, nil)
}

func enumChromeWindow(hwnd windows.HWND, _ uintptr) uintptr {
	var pid uint32
	_, _ = windows.GetWindowThreadProcessId(hwnd, &pid)
	if _, ok := chromeWinPIDs[pid]; !ok {
		return 1
	}
	cmd := uintptr(swHide)
	if chromeWinShow {
		cmd = uintptr(swRestore)
	}
	_, _, _ = procShowWindow.Call(uintptr(hwnd), cmd)
	if chromeWinShow {
		_, _, _ = procSetForeground.Call(uintptr(hwnd))
	}
	return 1
}

// chromeDescendantPIDs — только chrome.exe нашего процесса, не личный браузер.
func chromeDescendantPIDs() []uint32 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snap, &pe); err != nil {
		return nil
	}

	kids := map[uint32][]uint32{}
	names := map[uint32]string{}
	for {
		pid := pe.ProcessID
		kids[pe.ParentProcessID] = append(kids[pe.ParentProcessID], pid)
		names[pid] = strings.ToLower(windows.UTF16ToString(pe.ExeFile[:]))
		if err := windows.Process32Next(snap, &pe); err != nil {
			break
		}
	}

	var out []uint32
	var walk func(uint32)
	walk = func(pid uint32) {
		for _, c := range kids[pid] {
			if names[c] == "chrome.exe" {
				out = append(out, c)
			}
			walk(c)
		}
	}
	walk(uint32(os.Getpid()))
	return out
}
