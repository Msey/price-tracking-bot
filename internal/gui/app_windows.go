//go:build windows

package gui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Msey/price-tracking-bot/internal/storage"
	"github.com/lxn/walk"
	ui "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

const (
	mutexName = `Local\PriceTrackingBotGUI`
	eventName = `Local\PriceTrackingBotGUIShow`
)

var (
	instanceMu windows.Handle
	showEvent  windows.Handle
)

func Available() bool { return true }

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

func releaseInstance() {
	if showEvent != 0 {
		windows.CloseHandle(showEvent)
		showEvent = 0
	}
	if instanceMu != 0 {
		windows.CloseHandle(instanceMu)
		instanceMu = 0
	}
}

type app struct {
	store     *storage.Store
	dataPath  string
	log       *slog.Logger
	mw        *walk.MainWindow
	board     *board
	status    *walk.Label
	ni        *walk.NotifyIcon
	items     []Item
	allowQuit bool
	loaded    bool
}

func Run(ctx context.Context, opt Options) error {
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	if err := enableCommonControlsV6(); err != nil {
		opt.Log.Warn("не удалось включить Common Controls 6", "error", err)
	}
	titleFont, err := walk.NewFont("Segoe UI", 12, walk.FontBold)
	if err != nil {
		return err
	}
	metaFont, err := walk.NewFont("Segoe UI", 9, 0)
	if err != nil {
		return err
	}
	priceFont, err := walk.NewFont("Segoe UI", 14, walk.FontBold)
	if err != nil {
		return err
	}

	bg, err := walk.NewSolidColorBrush(walk.RGB(22, 20, 16))
	if err != nil {
		return err
	}
	row, err := walk.NewSolidColorBrush(walk.RGB(33, 28, 22))
	if err != nil {
		return err
	}
	rowSel, err := walk.NewSolidColorBrush(walk.RGB(48, 40, 30))
	if err != nil {
		return err
	}
	accent, err := walk.NewSolidColorBrush(walk.RGB(226, 182, 87))
	if err != nil {
		return err
	}
	gridPen, err := walk.NewCosmeticPen(walk.PenSolid, walk.RGB(58, 50, 40))
	if err != nil {
		return err
	}
	goldBrush, err := walk.NewSolidColorBrush(walk.RGB(226, 182, 87))
	if err != nil {
		return err
	}
	goldPen, err := walk.NewGeometricPen(walk.PenSolid|walk.PenCapRound|walk.PenJoinRound, 2, goldBrush)
	if err != nil {
		return err
	}

	icon, err := walk.NewIconFromImage(trayImage())
	if err != nil {
		return err
	}

	a := &app{
		store:    opt.Store,
		dataPath: opt.DataPath,
		log:      opt.Log,
		board: &board{
			titleFont: titleFont,
			metaFont:  metaFont,
			priceFont: priceFont,
			bg:        bg,
			row:       row,
			rowHot:    rowSel,
			accent:    accent,
			goldPen:   goldPen,
			gridPen:   gridPen,
		},
	}
	a.board.onOpen = func(it Item) { openURL(it.URL) }

	muted := walk.RGB(154, 141, 122)
	gold := walk.RGB(226, 182, 87)
	var canvas *walk.CustomWidget

	if err := (ui.MainWindow{
		AssignTo:   &a.mw,
		Title:      "Трекинг цен",
		Icon:       icon,
		MinSize:    ui.Size{Width: 860, Height: 560},
		Size:       ui.Size{Width: 1040, Height: 760},
		Font:       ui.Font{Family: "Segoe UI", PointSize: 10},
		Background: ui.SolidColorBrush{Color: walk.RGB(22, 20, 16)},
		Layout:     ui.VBox{Margins: ui.Margins{Left: 16, Top: 14, Right: 16, Bottom: 14}, Spacing: 10},
		Children: []ui.Widget{
			ui.Composite{
				Background: ui.SolidColorBrush{Color: walk.RGB(22, 20, 16)},
				Layout:     ui.HBox{MarginsZero: true, Spacing: 12},
				Children: []ui.Widget{
					ui.Composite{
						Background: ui.SolidColorBrush{Color: walk.RGB(22, 20, 16)},
						Layout:     ui.VBox{MarginsZero: true, Spacing: 2},
						Children: []ui.Widget{
							ui.Label{Text: "Ссылки и графики цен", Font: ui.Font{Family: "Segoe UI", PointSize: 16, Bold: true}, TextColor: gold},
							ui.Label{AssignTo: &a.status, Text: "Загрузка…", TextColor: muted},
						},
					},
					ui.HSpacer{},
					ui.PushButton{Text: "Обновить", MinSize: ui.Size{Width: 110, Height: 32}, OnClicked: func() { a.refresh(false) }},
					ui.PushButton{Text: "Папка с данными", MinSize: ui.Size{Width: 140, Height: 32}, OnClicked: a.openDataFolder},
				},
			},
			ui.CustomWidget{
				AssignTo:            &canvas,
				StretchFactor:       1,
				InvalidatesOnResize: true,
				PaintMode:           ui.PaintBuffered,
				PaintPixels:         a.board.paint,
			},
		},
	}).Create(); err != nil {
		releaseInstance()
		return err
	}
	a.board.attach(canvas)
	win.SetMenu(a.mw.Handle(), 0)
	if tb := a.mw.ToolBar(); tb != nil {
		tb.SetVisible(false)
	}

	a.mw.Closing().Attach(func(canceled *bool, _ walk.CloseReason) {
		if a.allowQuit {
			return
		}
		*canceled = true
		a.hideToTray()
	})
	a.mw.SizeChanged().Attach(func() {
		if win.IsIconic(a.mw.Handle()) {
			a.hideToTray()
		}
	})

	ni, err := walk.NewNotifyIcon(a.mw)
	if err != nil {
		releaseInstance()
		return err
	}
	a.ni = ni
	_ = ni.SetIcon(icon)
	_ = ni.SetToolTip("Трекинг цен")
	_ = ni.SetVisible(true)
	ni.MouseUp().Attach(func(_, _ int, btn walk.MouseButton) {
		if btn == walk.LeftButton {
			a.showWindow()
		}
	})
	if err := addTrayAction(ni, "Открыть окно", a.showWindow); err != nil {
		return err
	}
	if err := addTrayAction(ni, "Обновить список", func() { a.refresh(false) }); err != nil {
		return err
	}
	if err := addTrayAction(ni, "Папка с данными", a.openDataFolder); err != nil {
		return err
	}
	if err := ni.ContextMenu().Actions().Add(walk.NewSeparatorAction()); err != nil {
		return err
	}
	if err := addTrayAction(ni, "Выход", a.quit); err != nil {
		return err
	}

	go a.watchShowRequests()
	go a.watchCancel(ctx)
	go a.poll(ctx)

	a.refresh(false)
	opt.Log.Info("графический интерфейс", "tray", true)
	a.mw.Show()
	a.mw.Run()

	_ = ni.Dispose()
	releaseInstance()
	return nil
}

func addTrayAction(ni *walk.NotifyIcon, title string, fn func()) error {
	act := walk.NewAction()
	if err := act.SetText(title); err != nil {
		return err
	}
	act.Triggered().Attach(fn)
	return ni.ContextMenu().Actions().Add(act)
}

func (a *app) hideToTray() {
	a.mw.Hide()
}

func (a *app) showWindow() {
	if a.mw == nil {
		return
	}
	a.mw.Synchronize(func() {
		a.mw.Show()
		win.ShowWindow(a.mw.Handle(), win.SW_RESTORE)
		win.SetForegroundWindow(a.mw.Handle())
		a.refresh(false)
	})
}

func (a *app) quit() {
	a.allowQuit = true
	if a.ni != nil {
		_ = a.ni.Dispose()
	}
	walk.App().Exit(0)
}

func (a *app) watchCancel(ctx context.Context) {
	<-ctx.Done()
	if a.mw == nil {
		return
	}
	a.mw.Synchronize(a.quit)
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

func (a *app) poll(ctx context.Context) {
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if a.mw != nil {
				a.mw.Synchronize(func() { a.refresh(true) })
			}
			d := 10 * time.Second
			if a.mw != nil && a.mw.Visible() && !win.IsIconic(a.mw.Handle()) {
				d = 4 * time.Second
			}
			timer.Reset(d)
		}
	}
}

func (a *app) refresh(notify bool) {
	if a.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	items, err := loadItems(ctx, a.store)
	if err != nil {
		a.log.Error("список товаров для окна", "error", err)
		if a.status != nil {
			_ = a.status.SetText("Не удалось прочитать базу")
		}
		return
	}
	if a.loaded && notify && a.ni != nil {
		for _, it := range newProducts(a.items, items) {
			_ = a.ni.ShowInfo("Новая ссылка", clip(it.Title, 120))
		}
		for _, msg := range priceChanges(a.items, items) {
			_ = a.ni.ShowInfo("Цена изменилась", clip(msg, 180))
		}
	}
	a.items = items
	if a.board != nil {
		a.board.setItems(items)
	}
	a.loaded = true
	if a.status != nil {
		n := len(items)
		_ = a.status.SetText("Работает в фоне · " +
			fmt.Sprintf("%d %s", n, ruPlural(n, "товар", "товара", "товаров")) +
			" в списке · закрытие окна прячет в трей")
	}
	if a.ni != nil {
		_ = a.ni.SetToolTip("Трекинг цен · " + ruPlural(len(items), "товар", "товара", "товаров"))
	}
}

func (a *app) openDataFolder() {
	path := a.dataPath
	if path == "" {
		path = "bot.db"
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	_ = exec.Command("explorer", filepath.Dir(abs)).Start()
}

func openURL(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", raw).Start()
}

func clip(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
