//go:build windows

package gui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Msey/price-tracking-bot/internal/sites"
	"github.com/Msey/price-tracking-bot/internal/storage"
	"github.com/Msey/price-tracking-bot/internal/view"
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

type app struct {
	store         *storage.Store
	dataPath      string
	log           *slog.Logger
	mw            *walk.MainWindow
	board         *board
	status        *walk.Label
	ni            *trayIcon
	items         []Item
	allowQuit     bool
	loaded        bool
	checkNow      func()
	checkBusy     func() bool
	checkStatus   func() string
	checkBtn      *themeButton
	logBtn        *themeButton
	logCmd        uint16
	logEnabled    func() bool
	setLogEnabled func(bool)
	captchaTold   bool
	// closed — цикл сообщений уже вышел, слать в него работу больше нельзя.
	closed atomic.Bool
	// watching — сторож проверки уже запущен; второй не нужен, иначе каждый
	// щелчок по пункту в трее добавлял бы ещё один опрос базы каждые 750 мс.
	watching atomic.Bool
	// loading — чтение базы уже идёт, второе в очередь не ставим.
	loading  atomic.Bool
	checkCtx context.Context
}

func Run(ctx context.Context, opt Options) error {
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	setAppUserModelID()
	if err := enableCommonControlsV6(); err != nil {
		opt.Log.Warn("не удалось включить Common Controls 6", "error", err)
	}

	// Кисти, шрифты, перья и иконка живут ровно столько, сколько окно.
	// Их нужно освободить и на успешном выходе, и на любом раннем return,
	// иначе каждая неудачная попытка запуска оставляет объекты GDI.
	var owned []walk.Disposable
	defer func() {
		for i := len(owned) - 1; i >= 0; i-- {
			owned[i].Dispose()
		}
	}()
	keep := func(d walk.Disposable) { owned = append(owned, d) }
	defer releaseInstance()

	titleFont, err := walk.NewFont("Segoe UI", 10, walk.FontBold)
	if err != nil {
		return err
	}
	keep(titleFont)
	metaFont, err := walk.NewFont("Segoe UI", 8, 0)
	if err != nil {
		return err
	}
	keep(metaFont)
	priceFont, err := walk.NewFont("Segoe UI", 11, walk.FontBold)
	if err != nil {
		return err
	}
	keep(priceFont)

	bg, err := walk.NewSolidColorBrush(walk.RGB(22, 20, 16))
	if err != nil {
		return err
	}
	keep(bg)
	row, err := walk.NewSolidColorBrush(walk.RGB(33, 28, 22))
	if err != nil {
		return err
	}
	keep(row)
	rowSel, err := walk.NewSolidColorBrush(walk.RGB(48, 40, 30))
	if err != nil {
		return err
	}
	keep(rowSel)
	accent, err := walk.NewSolidColorBrush(walk.RGB(226, 182, 87))
	if err != nil {
		return err
	}
	keep(accent)
	gridPen, err := walk.NewCosmeticPen(walk.PenSolid, walk.RGB(58, 50, 40))
	if err != nil {
		return err
	}
	keep(gridPen)
	goldHot, err := walk.NewSolidColorBrush(walk.RGB(236, 196, 104))
	if err != nil {
		return err
	}
	keep(goldHot)
	goldPress, err := walk.NewSolidColorBrush(walk.RGB(196, 154, 64))
	if err != nil {
		return err
	}
	keep(goldPress)
	mutedFill, err := walk.NewSolidColorBrush(walk.RGB(90, 76, 52))
	if err != nil {
		return err
	}
	keep(mutedFill)
	frame, err := walk.NewSolidColorBrush(walk.RGB(58, 50, 40))
	if err != nil {
		return err
	}
	keep(frame)
	goldBrush, err := walk.NewSolidColorBrush(walk.RGB(226, 182, 87))
	if err != nil {
		return err
	}
	keep(goldBrush)
	goldPen, err := walk.NewGeometricPen(walk.PenSolid|walk.PenCapRound|walk.PenJoinRound, 2, goldBrush)
	if err != nil {
		return err
	}
	keep(goldPen)
	// Для покупателя рост цены — плохо (красный), падение — хорошо (зелёный).
	downBrush, err := walk.NewSolidColorBrush(walk.RGB(160, 222, 140))
	if err != nil {
		return err
	}
	keep(downBrush)
	downPen, err := walk.NewGeometricPen(walk.PenSolid|walk.PenCapRound|walk.PenJoinRound, 2, downBrush)
	if err != nil {
		return err
	}
	keep(downPen)
	upBrush, err := walk.NewSolidColorBrush(walk.RGB(232, 86, 74))
	if err != nil {
		return err
	}
	keep(upBrush)
	upPen, err := walk.NewGeometricPen(walk.PenSolid|walk.PenCapRound|walk.PenJoinRound, 2, upBrush)
	if err != nil {
		return err
	}
	keep(upPen)
	missBrush, err := walk.NewSolidColorBrush(walk.RGB(148, 140, 128))
	if err != nil {
		return err
	}
	keep(missBrush)
	missPen, err := walk.NewGeometricPen(walk.PenSolid|walk.PenCapRound|walk.PenJoinRound, 2, missBrush)
	if err != nil {
		return err
	}
	keep(missPen)

	icon, err := walk.NewIconFromImage(trayImage())
	if err != nil {
		return err
	}
	keep(icon)

	a := &app{
		store:         opt.Store,
		dataPath:      opt.DataPath,
		log:           opt.Log,
		checkNow:      opt.CheckNow,
		checkBusy:     opt.CheckBusy,
		checkStatus:   opt.CheckStatus,
		logEnabled:    opt.LogEnabled,
		setLogEnabled: opt.SetLogEnabled,
		board: &board{
			titleFont: titleFont,
			metaFont:  metaFont,
			priceFont: priceFont,
			bg:        bg,
			row:       row,
			rowHot:    rowSel,
			accent:    accent,
			upBrush:   upBrush,
			downBrush: downBrush,
			missBrush: missBrush,
			goldPen:   goldPen,
			upPen:     upPen,
			downPen:   downPen,
			missPen:   missPen,
			gridPen:   gridPen,
			hover:     -1,
			tipItem:   -1,
			tipNode:   -1,
		},
	}
	a.board.onOpen = func(it Item) { openURL(it.URL) }
	a.board.onDelete = a.deleteItem
	idleTrash, err := walk.NewBitmapFromImage(trashImage(64, trashMuted))
	if err != nil {
		opt.Log.Warn("иконка корзины", "error", err)
	} else {
		keep(idleTrash)
		a.board.trash = idleTrash
	}
	hotTrash, err := walk.NewBitmapFromImage(trashImage(64, iconGold))
	if err != nil {
		opt.Log.Warn("иконка корзины", "error", err)
	} else {
		keep(hotTrash)
		a.board.trashHot = hotTrash
	}
	a.board.icons = map[string]walk.Image{}
	for _, site := range sites.SitesWithIcons() {
		img := siteImage(site)
		if img == nil {
			continue
		}
		bmp, err := walk.NewBitmapFromImage(img)
		if err != nil {
			opt.Log.Warn("иконка магазина", "site", site, "error", err)
			continue
		}
		a.board.icons[string(site)] = bmp
		keep(bmp)
	}
	keep(disposeFunc(func() { a.board.disposeMeasure() }))

	muted := walk.RGB(154, 141, 122)
	gold := walk.RGB(226, 182, 87)
	var canvas *walk.CustomWidget
	chrome := &buttonChrome{
		font:      titleFont,
		row:       row,
		rowHot:    rowSel,
		accent:    accent,
		goldHot:   goldHot,
		goldPress: goldPress,
		mutedFill: mutedFill,
		frame:     frame,
	}
	a.checkBtn = newThemeButton("Проверить цены", true, chrome, a.requestCheck)
	a.logBtn = newThemeButton("Логи: выкл", false, chrome, a.toggleDiagLog)
	refreshBtn := newThemeButton("Обновить", false, chrome, a.refreshClicked)
	folderBtn := newThemeButton("Папка с данными", false, chrome, a.openDataFolder)

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
				MinSize:    ui.Size{Height: 52},
				MaxSize:    ui.Size{Height: buttonMaxH},
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
					a.checkBtn.cell(168),
					a.logBtn.cell(118),
					refreshBtn.cell(108),
					folderBtn.cell(156),
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
		return err
	}
	a.board.attach(canvas)
	keep(disposeFunc(func() { a.board.disposeTip() }))
	a.checkBtn.attach()
	a.logBtn.attach()
	refreshBtn.attach()
	folderBtn.attach()
	a.updateCheckUI()
	if a.setLogEnabled == nil && a.logBtn != nil {
		a.logBtn.SetVisible(false)
	}
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

	ni, err := newTrayIcon(a.mw.Handle(), windowTrayIcon(a.mw.Handle()), a.showWindow)
	if err != nil {
		return err
	}
	// Иначе сбой при сборке меню оставил бы иконку висеть в трее.
	// Dispose идемпотентен, поэтому явный вызов в quit остаётся рабочим.
	keep(disposeFunc(func() { ni.Dispose() }))
	a.ni = ni
	_ = ni.setToolTip("Трекинг цен")
	_ = ni.setVisible(true)
	if _, err := ni.addAction("Открыть окно", a.showWindow); err != nil {
		return err
	}
	if link := telegramBotURL(opt.BotUsername); link != "" {
		if _, err := ni.addAction("Открыть в Telegram", func() { openURL(link) }); err != nil {
			return err
		}
	}
	if a.checkNow != nil {
		if _, err := ni.addAction("Проверить цены", a.requestCheck); err != nil {
			return err
		}
	}
	if _, err := ni.addAction("Обновить список", a.refreshClicked); err != nil {
		return err
	}
	if _, err := ni.addAction("Папка с данными", a.openDataFolder); err != nil {
		return err
	}
	if a.setLogEnabled != nil {
		id, err := ni.addAction("Логи: выкл", a.toggleDiagLog)
		if err != nil {
			return err
		}
		a.logCmd = id
	}
	if err := ni.addSeparator(); err != nil {
		return err
	}
	if _, err := ni.addAction("Выход", a.quit); err != nil {
		return err
	}

	go a.watchShowRequests()
	go a.watchCancel(ctx)
	go a.poll(ctx)

	a.checkCtx = ctx
	a.refresh(false)
	a.syncDiagLogUI()
	a.log.Info("графический интерфейс", "tray", true, "hidden", opt.StartHidden)
	if opt.StartHidden {
		a.hideToTray()
		_ = ni.showInfo("Трекинг цен", "Бот в трее. Щелчок по иконке открывает окно.")
	} else {
		a.mw.Show()
	}
	a.mw.Run()
	a.closed.Store(true)
	return nil
}

// disposeFunc подгоняет под walk.Disposable то, чей Dispose возвращает ошибку.
type disposeFunc func()

func (f disposeFunc) Dispose() { f() }

func (a *app) deleteItem(it Item) {
	a.log.Info("удаление товара из окна", "product_id", it.ProductID, "title", it.Title)
	msg := fmt.Sprintf("Снять «%s» с отслеживания?", it.Title)
	if it.Watchers > 1 {
		msg = fmt.Sprintf("«%s» отслеживают %d %s. Снять у всех?",
			it.Title, it.Watchers, view.RuPlural(it.Watchers, "человек", "человека", "человек"))
	}
	if a.mw != nil && walk.MsgBox(a.mw, "Трекинг цен", msg, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
		return
	}
	if a.store == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		n, err := a.store.DeleteProductSubscriptions(ctx, it.ProductID)
		a.onUI(func() {
			if err != nil {
				a.log.Error("удаление товара", "error", err)
				if a.status != nil {
					_ = a.status.SetText("Не удалось удалить товар")
				}
				return
			}
			a.log.Info("товар снят с отслеживания", "product_id", it.ProductID, "removed", n)
			a.refresh(false)
		})
	}()
}

func (a *app) requestCheck() {
	a.log.Info("нажата проверка цен")
	if a.checkNow == nil {
		return
	}
	a.checkNow()
	a.updateCheckUI()
	if a.status != nil {
		_ = a.status.SetText("Запущена проверка всех цен. Таймер автоцикла сброшен.")
	}
	// Пункт в трее, в отличие от кнопки, не гаснет на время проверки,
	// поэтому сторож заводится только один.
	if a.watching.CompareAndSwap(false, true) {
		go a.watchCheck()
	}
}

func (a *app) refreshClicked() {
	a.log.Info("обновление списка в окне")
	a.refresh(false)
}

func (a *app) toggleDiagLog() {
	if a.setLogEnabled == nil {
		return
	}
	on := a.logEnabled == nil || !a.logEnabled()
	if on {
		a.setLogEnabled(true)
		a.log.Info("подробные логи включены")
	} else {
		a.log.Info("подробные логи выключены")
		a.setLogEnabled(false)
	}
	a.syncDiagLogUI()
}

func (a *app) syncDiagLogUI() {
	on := a.logEnabled != nil && a.logEnabled()
	text := "Логи: выкл"
	if on {
		text = "Логи: вкл"
	}
	if a.logBtn != nil {
		_ = a.logBtn.SetText(text)
	}
	if a.ni != nil && a.logCmd != 0 {
		a.ni.setItemText(a.logCmd, text)
	}
}

func (a *app) watchCheck() {
	defer a.watching.Store(false)

	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	done := a.stopSignal()
	deadline := time.NewTimer(2 * time.Hour)
	defer deadline.Stop()
	for {
		if a.closed.Load() {
			return
		}
		busy := a.checkRunning()
		a.onUI(a.updateCheckUI)
		a.refresh(true)
		if !busy {
			return
		}
		select {
		case <-ticker.C:
		case <-done:
			return
		case <-deadline.C:
			return
		}
	}
}

// stopSignal — канал, который закрывается при остановке приложения.
func (a *app) stopSignal() <-chan struct{} {
	if a.checkCtx == nil {
		return nil
	}
	return a.checkCtx.Done()
}

// onUI переносит работу в поток окна. После выхода из цикла сообщений
// очередь Synchronize уже никто не разбирает, поэтому туда не пишем.
func (a *app) onUI(fn func()) {
	if a.mw == nil || a.closed.Load() {
		return
	}
	a.mw.Synchronize(fn)
}

func (a *app) checkRunning() bool {
	return a.checkBusy != nil && a.checkBusy()
}

func (a *app) checkMessage() string {
	if a.checkStatus == nil {
		return ""
	}
	return strings.TrimSpace(a.checkStatus())
}

func (a *app) noteCaptcha(msg string) {
	if !strings.Contains(strings.ToLower(msg), "капч") {
		a.captchaTold = false
		return
	}
	if a.captchaTold || a.ni == nil {
		return
	}
	a.captchaTold = true
	_ = a.ni.showInfo("Нужна капча", "Откройте окно Chrome и пройдите проверку. Бот подождёт несколько минут.")
}

func (a *app) updateCheckUI() {
	if a.checkBtn == nil {
		return
	}
	if a.checkNow == nil {
		a.checkBtn.SetEnabled(false)
		return
	}
	busy := a.checkRunning()
	a.checkBtn.SetEnabled(!busy)
	if busy {
		_ = a.checkBtn.SetText("Проверка…")
	} else {
		_ = a.checkBtn.SetText("Проверить цены")
	}
}

func (a *app) hideToTray() {
	a.log.Info("окно спрятано в трей")
	a.mw.Hide()
}

func (a *app) showWindow() {
	a.log.Info("окно открыто из трея")
	a.onUI(func() {
		a.mw.Show()
		win.ShowWindow(a.mw.Handle(), win.SW_RESTORE)
		win.SetForegroundWindow(a.mw.Handle())
		a.refresh(false)
	})
}

func (a *app) quit() {
	a.log.Info("выход из приложения")
	a.allowQuit = true
	a.closed.Store(true)
	if a.ni != nil {
		a.ni.Dispose()
	}
	walk.App().Exit(0)
}

func (a *app) watchCancel(ctx context.Context) {
	<-ctx.Done()
	a.onUI(a.quit)
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
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var lastRefresh time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			a.onUI(func() {
				a.updateCheckUI()
				a.updateStatus()
			})
			every := 10 * time.Second
			if a.mw != nil && a.mw.Visible() && !win.IsIconic(a.mw.Handle()) {
				every = 4 * time.Second
			}
			if lastRefresh.IsZero() || time.Since(lastRefresh) >= every {
				lastRefresh = time.Now()
				a.refresh(true)
			}
		}
	}
}

// refresh перечитывает базу и обновляет окно. Чтение уходит в отдельную
// горутину: запрос может занять до пяти секунд, и делать его в потоке окна
// значит подвесить интерфейс на всё это время.
func (a *app) refresh(notify bool) {
	if a.store == nil || a.closed.Load() {
		return
	}
	if !a.loading.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.loading.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		items, err := loadItems(ctx, a.store)
		a.onUI(func() { a.apply(items, err, notify) })
	}()
}

func (a *app) apply(items []Item, err error, notify bool) {
	if err != nil {
		a.log.Error("список товаров для окна", "error", err)
		if a.status != nil {
			_ = a.status.SetText("Не удалось прочитать базу")
		}
		return
	}
	if a.loaded && notify && a.ni != nil {
		a.announce("Новая ссылка", titlesOf(newProducts(a.items, items)), 120)
		a.announce("Цена изменилась", priceChanges(a.items, items), 180)
	}
	a.items = items
	if a.board != nil {
		a.board.setItems(items)
	}
	a.loaded = true
	a.updateStatus()
	if a.ni != nil {
		_ = a.ni.setToolTip(a.tooltipText())
	}
}

func (a *app) updateStatus() {
	if a.status == nil {
		return
	}
	if msg := a.checkMessage(); msg != "" {
		_ = a.status.SetText(msg)
		a.noteCaptcha(msg)
		if a.ni != nil {
			_ = a.ni.setToolTip(a.tooltipText())
		}
		return
	}
	n := len(a.items)
	if a.checkRunning() {
		_ = a.status.SetText("Идёт проверка цен · " +
			fmt.Sprintf("%d %s", n, view.RuPlural(n, "товар", "товара", "товаров")) +
			" · автоцикл начнётся заново после неё")
	} else {
		_ = a.status.SetText("Работает в фоне · " +
			fmt.Sprintf("%d %s", n, view.RuPlural(n, "товар", "товара", "товаров")) +
			" в списке · закрытие окна прячет в трей")
	}
	if a.ni != nil {
		_ = a.ni.setToolTip(a.tooltipText())
	}
}

func (a *app) tooltipText() string {
	n := view.RuPlural(len(a.items), "товар", "товара", "товаров")
	if msg := a.checkMessage(); msg != "" {
		return "Трекинг цен · " + clip(msg, 80)
	}
	return "Трекинг цен · " + n
}

// maxBalloons — сколько всплывающих подсказок показать за один заход.
// Полная проверка может сдвинуть десятки цен, и каждая подсказка висит
// около десяти секунд: без предела они забьют угол экрана на минуты.
const maxBalloons = 3

func (a *app) announce(title string, lines []string, limit int) {
	for i, line := range lines {
		if i == maxBalloons {
			rest := len(lines) - maxBalloons
			_ = a.ni.showInfo(title, fmt.Sprintf("и ещё %d %s", rest,
				view.RuPlural(rest, "изменение", "изменения", "изменений")))
			return
		}
		_ = a.ni.showInfo(title, clip(line, limit))
	}
}

func titlesOf(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

func (a *app) openDataFolder() {
	a.log.Info("открыта папка с данными")
	path := a.dataPath
	if path == "" {
		path = "bot.db"
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	_ = exec.Command(systemExe("explorer.exe"), filepath.Dir(abs)).Start()
}

func openURL(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	slog.Info("открываю ссылку", "url", raw)
	_ = exec.Command(systemExe(`System32\rundll32.exe`), "url.dll,FileProtocolHandler", raw).Start()
}

// systemExe собирает путь от %SystemRoot%, а не ищет программу в PATH:
// иначе запись в любой каталог из PATH даёт подмену запускаемой программы.
func systemExe(rel string) string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, rel)
}

func clip(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
