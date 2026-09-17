//go:build windows

package fetch

import (
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	swShowNoActivate       = 4
	swRestore              = 9
	swpNoActivate          = 0x0010
	swpShowWindow          = 0x0040
	hwndBottom             = 1
	lsfwLock               = 1
	lsfwUnlock             = 2
	smXVirtualScreen       = 76
	smYVirtualScreen       = 77
	chromeMainClass        = "Chrome_WidgetWin_1"
	processCommandLineInfo = 60
	statusInfoLenMismatch  = 0xC0000004
)

var (
	user32                        = windows.NewLazySystemDLL("user32.dll")
	ntdll                         = windows.NewLazySystemDLL("ntdll.dll")
	procEnumWindows               = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessID  = user32.NewProc("GetWindowThreadProcessId")
	procShowWindow                = user32.NewProc("ShowWindow")
	procSetWindowPos              = user32.NewProc("SetWindowPos")
	procIsWindowVisible           = user32.NewProc("IsWindowVisible")
	procGetClassName              = user32.NewProc("GetClassNameW")
	procGetAncestor               = user32.NewProc("GetAncestor")
	procLockSetForegroundWindow   = user32.NewProc("LockSetForegroundWindow")
	procGetSystemMetrics          = user32.NewProc("GetSystemMetrics")
	procSetForegroundWindow       = user32.NewProc("SetForegroundWindow")
	procNtQueryInformationProcess = ntdll.NewProc("NtQueryInformationProcess")
	enumWindowsCallback           = syscall.NewCallback(enumChromeWindow)
)

const gaRoot = 2

type chromeWindowEnum struct {
	pids  map[uint32]struct{}
	hwnds []windows.HWND
}

var chromeEnum struct {
	mu sync.Mutex
	s  *chromeWindowEnum
}

func offscreenOriginOS() (int, int) {
	left, _, _ := procGetSystemMetrics.Call(smXVirtualScreen)
	top, _, _ := procGetSystemMetrics.Call(smYVirtualScreen)
	return int(left) - chromeWindowW - 80, int(top) - chromeWindowH - 80
}

func lockForeground() func() {
	_, _, _ = procLockSetForegroundWindow.Call(lsfwLock)
	return func() {
		_, _, _ = procLockSetForegroundWindow.Call(lsfwUnlock)
	}
}

func demoteChromeWithProfile(profile string) {
	demoteChrome(0, profile, 0)
}

func revealChromeWithProfile(profile string) {
	x, y := 80, 80
	for _, hwnd := range chromeWindowsForProfile(0, profile) {
		_, _, _ = procShowWindow.Call(uintptr(hwnd), swRestore)
		_, _, _ = procSetWindowPos.Call(
			uintptr(hwnd), 0,
			uintptr(x), uintptr(y),
			uintptr(chromeWindowW), uintptr(chromeWindowH),
			swpShowWindow,
		)
		_, _, _ = procSetForegroundWindow.Call(uintptr(hwnd))
	}
}

func demoteChrome(rootPID uint32, profile string, hold time.Duration) {
	deadline := time.Now().Add(hold)
	if hold <= 0 {
		deadline = time.Now()
	}
	x, y := offscreenOrigin()
	for {
		for _, hwnd := range chromeWindowsForProfile(rootPID, profile) {
			_, _, _ = procSetWindowPos.Call(
				uintptr(hwnd), hwndBottom,
				uintptr(x), uintptr(y),
				uintptr(chromeWindowW), uintptr(chromeWindowH),
				swpNoActivate|swpShowWindow,
			)
			_, _, _ = procShowWindow.Call(uintptr(hwnd), swShowNoActivate)
		}
		if !time.Now().Before(deadline) {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func chromeWindowsForProfile(rootPID uint32, profile string) []windows.HWND {
	pids := chromePIDsForProfile(rootPID, profile)
	if len(pids) == 0 {
		return nil
	}
	state := &chromeWindowEnum{pids: pids}
	chromeEnum.mu.Lock()
	chromeEnum.s = state
	_, _, _ = procEnumWindows.Call(enumWindowsCallback, 0)
	chromeEnum.s = nil
	hwnds := state.hwnds
	chromeEnum.mu.Unlock()
	return hwnds
}

func enumChromeWindow(hwnd, _ uintptr) uintptr {
	s := chromeEnum.s
	if s == nil {
		return 1
	}
	var pid uint32
	_, _, _ = procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if _, ok := s.pids[pid]; !ok {
		return 1
	}
	vis, _, _ := procIsWindowVisible.Call(hwnd)
	if vis == 0 {
		return 1
	}
	root, _, _ := procGetAncestor.Call(hwnd, gaRoot)
	if root != 0 && root != hwnd {
		return 1
	}
	var buf [64]uint16
	n, _, _ := procGetClassName.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return 1
	}
	if syscall.UTF16ToString(buf[:n]) != chromeMainClass {
		return 1
	}
	s.hwnds = append(s.hwnds, windows.HWND(hwnd))
	return 1
}

func chromePIDsForProfile(rootPID uint32, profile string) map[uint32]struct{} {
	out := map[uint32]struct{}{}
	if rootPID != 0 {
		out[rootPID] = struct{}{}
		addDescendants(rootPID, out)
	}
	needle := strings.ToLower(filepath.Clean(profile))
	if needle == "" {
		return out
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snap, &pe); err != nil {
		return out
	}
	for {
		name := windows.UTF16ToString(pe.ExeFile[:])
		if strings.EqualFold(name, "chrome.exe") && profileInCommandLine(processCommandLine(pe.ProcessID), needle) {
			out[pe.ProcessID] = struct{}{}
		}
		if err := windows.Process32Next(snap, &pe); err != nil {
			break
		}
	}
	return out
}

func addDescendants(root uint32, into map[uint32]struct{}) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return
	}
	defer windows.CloseHandle(snap)
	type row struct{ pid, parent uint32 }
	var rows []row
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snap, &pe); err != nil {
		return
	}
	for {
		rows = append(rows, row{pe.ProcessID, pe.ParentProcessID})
		if err := windows.Process32Next(snap, &pe); err != nil {
			break
		}
	}
	changed := true
	for changed {
		changed = false
		for _, r := range rows {
			if _, have := into[r.pid]; have {
				continue
			}
			if _, ok := into[r.parent]; ok {
				into[r.pid] = struct{}{}
				changed = true
			}
		}
	}
}

func processCommandLine(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)

	var retLen uint32
	r1, _, _ := procNtQueryInformationProcess.Call(uintptr(h), processCommandLineInfo, 0, 0, uintptr(unsafe.Pointer(&retLen)))
	if retLen == 0 {
		if r1 != 0 && r1 != statusInfoLenMismatch {
			return ""
		}
		return ""
	}
	buf := make([]byte, retLen)
	r1, _, _ = procNtQueryInformationProcess.Call(
		uintptr(h), processCommandLineInfo,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
		uintptr(unsafe.Pointer(&retLen)),
	)
	if r1 != 0 || retLen < 8 {
		return ""
	}
	type unicodeString struct {
		Length        uint16
		MaximumLength uint16
		_             uint32
		Buffer        *uint16
	}
	us := (*unicodeString)(unsafe.Pointer(&buf[0]))
	if us.Length < 2 || us.Buffer == nil {
		return ""
	}
	n := int(us.Length / 2)
	return windows.UTF16ToString(unsafe.Slice(us.Buffer, n))
}
