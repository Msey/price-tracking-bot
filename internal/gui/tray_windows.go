//go:build windows

package gui

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// trayMessageID — не WM_APP: walk забирает WM_APP под свою иконку трея
// (см. walk/window.go). Свой номер, чтобы сообщения шли только сюда.
const trayMessageID = win.WM_APP + 0x20

// trayIconGUID — постоянный идентификатор иконки. Без GUID оболочка
// считает каждый HWND новым приложением: после KillProcess в трее
// остаются «завершённые» копии, пока на них не навести курсор.
var trayIconGUID = syscall.GUID{
	Data1: 0x7e4c1a90,
	Data2: 0x6b2f,
	Data3: 0x4d8e,
	Data4: [8]byte{0x9a, 0x31, 0x5c, 0x8f, 0x0e, 0x2b, 0x7d, 0x14},
}

const (
	iconSmall            = 0
	iconBig              = 1
	gclpHIcon      int32 = -14
	gclpHIconSm    int32 = -34
	appUserModelID       = "Msey.PriceTrackingBot"
)

var (
	user32              = windows.NewLazySystemDLL("user32.dll")
	shell32             = windows.NewLazySystemDLL("shell32.dll")
	procFindWindowEx    = user32.NewProc("FindWindowExW")
	procGetClassLongPtr = user32.NewProc("GetClassLongPtrW")
	procSetAUMID        = shell32.NewProc("SetCurrentProcessExplicitAppUserModelID")
	taskbarCreatedMsg   uint32
	trayPrevProc        uintptr
	trayMenuOpen        bool
	activeTray          *trayIcon
)

func init() {
	name, err := syscall.UTF16PtrFromString("TaskbarCreated")
	if err == nil {
		taskbarCreatedMsg = win.RegisterWindowMessage(name)
	}
}

type trayIcon struct {
	hwnd    win.HWND
	hicon   win.HICON
	menu    win.HMENU
	cmds    map[uint16]func()
	nextID  uint16
	visible bool
	tooltip string
	onLeft  func()
}

func newTrayIcon(hwnd win.HWND, hicon win.HICON, onLeft func()) (*trayIcon, error) {
	if hwnd == 0 {
		return nil, fmt.Errorf("нет окна для иконки трея")
	}
	forgetDeadTrayIcons()
	purgeOurNotifyIconSettings()
	deleteGUIDIcon()

	menu := win.CreatePopupMenu()
	if menu == 0 {
		return nil, fmt.Errorf("CreatePopupMenu")
	}
	t := &trayIcon{
		hwnd:   hwnd,
		hicon:  hicon,
		menu:   menu,
		cmds:   map[uint16]func(){},
		nextID: 1,
		onLeft: onLeft,
	}
	if err := t.add(); err != nil {
		win.DestroyMenu(menu)
		return nil, err
	}
	if !hookTrayMenu(hwnd) {
		t.Dispose()
		return nil, fmt.Errorf("не удалось подменить оконную процедуру трея")
	}
	activeTray = t
	return t, nil
}

func (t *trayIcon) Dispose() {
	if t == nil || t.hwnd == 0 {
		return
	}
	deleteGUIDIcon()
	if t.menu != 0 {
		win.DestroyMenu(t.menu)
		t.menu = 0
	}
	t.hwnd = 0
	if activeTray == t {
		activeTray = nil
	}
}

func (t *trayIcon) addAction(title string, fn func()) (uint16, error) {
	if t == nil || t.menu == 0 {
		return 0, fmt.Errorf("меню трея не создано")
	}
	id := t.nextID
	t.nextID++
	text, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return 0, err
	}
	mii := win.MENUITEMINFO{
		FMask:      win.MIIM_STRING | win.MIIM_ID,
		WID:        uint32(id),
		DwTypeData: text,
	}
	mii.CbSize = uint32(unsafe.Sizeof(mii))
	idx := uint32(win.GetMenuItemCount(t.menu))
	if !win.InsertMenuItem(t.menu, idx, true, &mii) {
		return 0, fmt.Errorf("InsertMenuItem %q", title)
	}
	t.cmds[id] = fn
	return id, nil
}

func (t *trayIcon) addSeparator() error {
	if t == nil || t.menu == 0 {
		return fmt.Errorf("меню трея не создано")
	}
	mii := win.MENUITEMINFO{
		FMask: win.MIIM_FTYPE,
		FType: win.MFT_SEPARATOR,
	}
	mii.CbSize = uint32(unsafe.Sizeof(mii))
	idx := uint32(win.GetMenuItemCount(t.menu))
	if !win.InsertMenuItem(t.menu, idx, true, &mii) {
		return fmt.Errorf("InsertMenuItem separator")
	}
	return nil
}

func (t *trayIcon) setItemText(id uint16, title string) {
	if t == nil || t.menu == 0 || id == 0 {
		return
	}
	text, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	mii := win.MENUITEMINFO{
		FMask:      win.MIIM_STRING,
		DwTypeData: text,
	}
	mii.CbSize = uint32(unsafe.Sizeof(mii))
	_ = win.SetMenuItemInfo(t.menu, uint32(id), false, &mii)
}

func (t *trayIcon) setToolTip(s string) error {
	if t == nil || t.hwnd == 0 {
		return nil
	}
	if s == t.tooltip {
		return nil
	}
	t.tooltip = s
	nid := t.data(win.NIF_TIP | win.NIF_SHOWTIP)
	putUTF16(nid.SzTip[:], s)
	if !win.Shell_NotifyIcon(win.NIM_MODIFY, nid) {
		return fmt.Errorf("Shell_NotifyIcon tooltip")
	}
	return nil
}

func (t *trayIcon) setVisible(visible bool) error {
	if t == nil || t.hwnd == 0 {
		return nil
	}
	if visible == t.visible {
		return nil
	}
	nid := t.data(win.NIF_STATE)
	nid.DwStateMask = win.NIS_HIDDEN
	if !visible {
		nid.DwState = win.NIS_HIDDEN
	}
	if !win.Shell_NotifyIcon(win.NIM_MODIFY, nid) {
		return fmt.Errorf("Shell_NotifyIcon visible")
	}
	t.visible = visible
	return nil
}

func (t *trayIcon) showInfo(title, info string) error {
	if t == nil || t.hwnd == 0 {
		return nil
	}
	nid := t.data(win.NIF_INFO)
	nid.DwInfoFlags = win.NIIF_INFO
	putUTF16(nid.SzInfoTitle[:], title)
	putUTF16(nid.SzInfo[:], info)
	if !win.Shell_NotifyIcon(win.NIM_MODIFY, nid) {
		return fmt.Errorf("Shell_NotifyIcon balloon")
	}
	return nil
}

func (t *trayIcon) add() error {
	nid := t.data(win.NIF_MESSAGE | win.NIF_ICON | win.NIF_TIP | win.NIF_STATE | win.NIF_SHOWTIP)
	nid.DwState = win.NIS_HIDDEN
	nid.DwStateMask = win.NIS_HIDDEN
	nid.UCallbackMessage = trayMessageID
	nid.HIcon = t.hicon
	putUTF16(nid.SzTip[:], t.tooltip)
	if !win.Shell_NotifyIcon(win.NIM_ADD, nid) {
		deleteGUIDIcon()
		if !win.Shell_NotifyIcon(win.NIM_ADD, nid) {
			return fmt.Errorf("Shell_NotifyIcon add")
		}
	}
	nid.UVersion = win.NOTIFYICON_VERSION
	if !win.Shell_NotifyIcon(win.NIM_SETVERSION, nid) {
		return fmt.Errorf("Shell_NotifyIcon version")
	}
	t.visible = false
	return nil
}

func (t *trayIcon) readd() {
	if t == nil || t.hwnd == 0 {
		return
	}
	wasVisible := t.visible
	tip := t.tooltip
	t.hicon = windowTrayIcon(t.hwnd)
	deleteGUIDIcon()
	_ = t.add()
	if tip != "" {
		t.tooltip = ""
		_ = t.setToolTip(tip)
	}
	if wasVisible {
		_ = t.setVisible(true)
	}
}

func (t *trayIcon) data(flags uint32) *win.NOTIFYICONDATA {
	nid := &win.NOTIFYICONDATA{
		HWnd:     t.hwnd,
		UFlags:   flags | win.NIF_GUID,
		GuidItem: trayIconGUID,
		HIcon:    t.hicon,
	}
	nid.CbSize = uint32(unsafe.Sizeof(*nid) - unsafe.Sizeof(win.HICON(0)))
	return nid
}

func (t *trayIcon) popupMenu() {
	if t == nil || t.menu == 0 || trayMenuOpen {
		return
	}
	var p win.POINT
	if !win.GetCursorPos(&p) {
		return
	}
	win.SetForegroundWindow(t.hwnd)
	trayMenuOpen = true
	id := uint16(win.TrackPopupMenuEx(
		t.menu,
		win.TPM_NOANIMATION|win.TPM_RETURNCMD|win.TPM_RIGHTBUTTON,
		p.X, p.Y, t.hwnd, nil))
	trayMenuOpen = false
	win.PostMessage(t.hwnd, win.WM_NULL, 0, 0)
	if fn := t.cmds[id]; fn != nil {
		fn()
	}
}

func deleteGUIDIcon() {
	nid := win.NOTIFYICONDATA{
		UFlags:   win.NIF_GUID,
		GuidItem: trayIconGUID,
	}
	nid.CbSize = uint32(unsafe.Sizeof(nid) - unsafe.Sizeof(win.HICON(0)))
	_ = win.Shell_NotifyIcon(win.NIM_DELETE, &nid)
}

func hookTrayMenu(hwnd win.HWND) bool {
	if trayPrevProc != 0 || hwnd == 0 {
		return false
	}
	prev := win.SetWindowLongPtr(hwnd, win.GWLP_WNDPROC, trayProcCallback)
	if prev == 0 {
		return false
	}
	trayPrevProc = prev
	return true
}

var trayProcCallback = syscall.NewCallback(trayProc)

func trayProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	if trayPrevProc == 0 {
		return win.DefWindowProc(hwnd, msg, wParam, lParam)
	}
	if taskbarCreatedMsg != 0 && msg == taskbarCreatedMsg {
		if activeTray != nil {
			activeTray.readd()
		}
		return win.CallWindowProc(trayPrevProc, hwnd, msg, wParam, lParam)
	}
	if msg != trayMessageID {
		return win.CallWindowProc(trayPrevProc, hwnd, msg, wParam, lParam)
	}
	switch lParam {
	case win.WM_LBUTTONUP, win.NIN_SELECT:
		if activeTray != nil && activeTray.onLeft != nil {
			activeTray.onLeft()
		}
		return 0
	case win.WM_RBUTTONUP, win.WM_CONTEXTMENU:
		if activeTray != nil {
			activeTray.popupMenu()
		}
		return 0
	}
	return 0
}

func windowTrayIcon(hwnd win.HWND) win.HICON {
	if h := win.SendMessage(hwnd, win.WM_GETICON, iconSmall, 0); h != 0 {
		return win.HICON(h)
	}
	if h := win.SendMessage(hwnd, win.WM_GETICON, iconBig, 0); h != 0 {
		return win.HICON(h)
	}
	if r, _, _ := procGetClassLongPtr.Call(uintptr(hwnd), classLongIndex(gclpHIconSm)); r != 0 {
		return win.HICON(r)
	}
	r, _, _ := procGetClassLongPtr.Call(uintptr(hwnd), classLongIndex(gclpHIcon))
	return win.HICON(r)
}

func classLongIndex(i int32) uintptr {
	return uintptr(i)
}

func setAppUserModelID() {
	p, err := windows.UTF16PtrFromString(appUserModelID)
	if err != nil {
		return
	}
	_, _, _ = procSetAUMID.Call(uintptr(unsafe.Pointer(p)))
}

func putUTF16(dst []uint16, s string) {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return
	}
	if len(u) > len(dst) {
		u = append(append([]uint16{}, u[:len(dst)-1]...), 0)
	}
	copy(dst, u)
}

// forgetDeadTrayIcons прогоняет курсор по слотам трея: Explorer сам
// снимает иконки процессов, которых уже нет. Без этого KillProcess
// оставляет «завершённые» копии, пока пользователь на них не наведёт.
func forgetDeadTrayIcons() {
	tray := findWindowClass("Shell_TrayWnd")
	if tray != 0 {
		if notify := findWindowEx(tray, 0, "TrayNotifyWnd", ""); notify != 0 {
			sweepToolbarTree(notify)
		}
	}
	sweepToolbarTree(findWindowClass("NotifyIconOverflowWindow"))
}

func sweepToolbarTree(hwnd win.HWND) {
	if hwnd == 0 {
		return
	}
	var child win.HWND
	for {
		child = findWindowEx(hwnd, child, "", "")
		if child == 0 {
			break
		}
		sweepToolbarTree(child)
	}
	if windowClass(hwnd) == "ToolbarWindow32" {
		sweepToolbar(hwnd)
	}
}

func sweepToolbar(hwnd win.HWND) {
	var rc win.RECT
	if !win.GetClientRect(hwnd, &rc) {
		return
	}
	for y := rc.Top; y <= rc.Bottom; y += 8 {
		for x := rc.Left; x <= rc.Right; x += 8 {
			win.SendMessage(hwnd, win.WM_MOUSEMOVE, 0, uintptr(win.MAKELONG(uint16(x), uint16(y))))
		}
	}
}

func findWindowClass(class string) win.HWND {
	c, err := syscall.UTF16PtrFromString(class)
	if err != nil {
		return 0
	}
	return win.FindWindow(c, nil)
}

func findWindowEx(parent, after win.HWND, class, title string) win.HWND {
	var cls, ttl uintptr
	if class != "" {
		p, err := windows.UTF16PtrFromString(class)
		if err == nil {
			cls = uintptr(unsafe.Pointer(p))
		}
	}
	if title != "" {
		p, err := windows.UTF16PtrFromString(title)
		if err == nil {
			ttl = uintptr(unsafe.Pointer(p))
		}
	}
	r, _, _ := procFindWindowEx.Call(uintptr(parent), uintptr(after), cls, ttl)
	return win.HWND(r)
}

func windowClass(hwnd win.HWND) string {
	var buf [256]uint16
	n, err := win.GetClassName(hwnd, &buf[0], len(buf))
	if err != nil || n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

func purgeOurNotifyIconSettings() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	root, err := registry.OpenKey(registry.CURRENT_USER, `Control Panel\NotifyIconSettings`, registry.ALL_ACCESS)
	if err != nil {
		return
	}
	defer root.Close()
	names, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return
	}
	for _, name := range names {
		sub, err := registry.OpenKey(root, name, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		path, _, err := sub.GetStringValue("ExecutablePath")
		sub.Close()
		if err != nil || !isOurNotifyIconPath(path, self) {
			continue
		}
		_ = registry.DeleteKey(root, name)
	}
}
