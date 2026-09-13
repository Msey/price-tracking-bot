//go:build windows

package gui

import (
	"errors"

	"golang.org/x/sys/windows"
)

// Один экземпляр окна на систему: мьютекс говорит, что окно уже запущено,
// событие просит его показаться. Имена с префиксом Local — в пределах сеанса
// пользователя, чтобы у разных пользователей были свои окна.
const (
	mutexName = `Local\PriceTrackingBotGUI`
	eventName = `Local\PriceTrackingBotGUIShow`
)

var (
	instanceMu windows.Handle
	showEvent  windows.Handle
)

func Available() bool { return true }

// ActivateExisting возвращает true, если окно уже открыто другим процессом:
// тогда мы просим его показаться и выходим.
func ActivateExisting() bool {
	evName, err := windows.UTF16PtrFromString(eventName)
	if err != nil {
		return false
	}
	ev, err := windows.CreateEvent(nil, 0, 0, evName)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return false
	}
	muName, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		windows.CloseHandle(ev)
		return false
	}
	mu, err := windows.CreateMutex(nil, false, muName)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.SetEvent(ev)
		windows.CloseHandle(mu)
		windows.CloseHandle(ev)
		return true
	}
	if err != nil {
		windows.CloseHandle(ev)
		return false
	}
	instanceMu = mu
	showEvent = ev
	return false
}

// releaseInstance отпускает мьютекс единственного экземпляра. showEvent не
// закрывается: на нём висит watchShowRequests, и закрытие дескриптора
// из-под ожидающего потока — неопределённое поведение. Дескриптор
// освободит сама система при выходе процесса, то есть ровно тогда же.
func releaseInstance() {
	if instanceMu != 0 {
		windows.CloseHandle(instanceMu)
		instanceMu = 0
	}
}

func (a *app) watchShowRequests() {
	if showEvent == 0 {
		return
	}
	for {
		s, err := windows.WaitForSingleObject(showEvent, windows.INFINITE)
		if err != nil || s != windows.WAIT_OBJECT_0 {
			return
		}
		a.showWindow()
	}
}
