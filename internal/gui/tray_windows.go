//go:build windows

package gui

import (
	"syscall"

	"github.com/lxn/win"
)

// trayMessageID — сообщение, которым оболочка Windows сообщает о щелчках по
// иконке в трее: walk заводит его как WM_APP (см. walk/window.go).
const trayMessageID = win.WM_APP

// Меню трея открывалось дважды. walk показывает его и по WM_RBUTTONUP —
// обработчик сам посылает себе WM_CONTEXTMENU, — и по настоящему
// WM_CONTEXTMENU, который присылает оболочка. Второе меню всплывает ровно
// поверх первого, поэтому после выбора пункта под ним остаётся первое, и это
// выглядит как «меню открылось снова».
//
// Лишнее меню отсекается здесь. Проверяется факт того, что меню уже на
// экране, а не конкретное сообщение: тогда поведение не зависит от того,
// какой набор сообщений присылает конкретная версия Windows — меню
// открывается ровно один раз в любом случае.
var (
	trayPrevProc uintptr
	trayMenuOpen bool
)

// hookTrayMenu подменяет оконную процедуру окна, которому оболочка присылает
// сообщения иконки трея. Вызывать только из потока окна.
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
	if msg != trayMessageID || lParam != win.WM_CONTEXTMENU {
		return win.CallWindowProc(trayPrevProc, hwnd, msg, wParam, lParam)
	}
	if trayMenuOpen {
		return 0
	}

	// TrackPopupMenuEx внутри walk не отдаёт управление, пока меню не
	// закрыто, и по ходу разбирает очередь сообщений — там и приходит
	// второй WM_CONTEXTMENU.
	trayMenuOpen = true
	res := win.CallWindowProc(trayPrevProc, hwnd, msg, wParam, lParam)
	trayMenuOpen = false
	// Пустое сообщение после закрытия меню — требование документации к
	// меню иконки в трее: без него меню иногда не закрывается по щелчку
	// мимо него.
	win.PostMessage(hwnd, win.WM_NULL, 0, 0)
	return res
}
