//go:build windows

package gui

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Msey/price-tracking-bot/internal/storage"
	"github.com/Msey/price-tracking-bot/internal/view"
	"github.com/lxn/walk"
	"github.com/lxn/win"
)

type app struct {
	store         *storage.Store
	dataPath      string
	log           *slog.Logger
	mw            *walk.MainWindow
	board  *board
	header *headerBand
	ni     *trayIcon
	items         []Item
	allowQuit     bool
	loaded        bool
	openInChrome  func(storage.Product)
	checkNow      func()
	checkBusy     func() bool
	checkStatus   func() string
	checkBtn      *themeButton
	logBtn        *themeButton
	distinctBtn   *themeButton
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
	loading atomic.Bool
	// mark и haveMark трогает только горутина чтения, и она одна:
	// вход в неё стоит за CAS на loading, он же и синхронизирует память.
	mark     storage.Fingerprint
	haveMark bool
	checkCtx context.Context
}

func Run(ctx context.Context, opt Options) error {
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	setAppUserModelID()
	enablePerMonitorDPI()
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

	th, err := newTheme(keep)
	if err != nil {
		return err
	}
	icon, err := walk.NewIconFromImage(trayImage())
	if err != nil {
		return err
	}
	keep(icon)

	a := &app{
		store:         opt.Store,
		dataPath:      opt.DataPath,
		log:           opt.Log,
		openInChrome:  opt.OpenInChrome,
		checkNow:      opt.CheckNow,
		checkBusy:     opt.CheckBusy,
		checkStatus:   opt.CheckStatus,
		logEnabled:    opt.LogEnabled,
		setLogEnabled: opt.SetLogEnabled,
		board:         newBoard(th),
	}
	a.board.onOpen = a.openProduct
	a.board.onDelete = a.deleteItem
	a.board.loadImages(keep, opt.Log)
	keep(disposeFunc(func() { a.board.disposeMeasure() }))

	if err := a.buildWindow(th, icon, keep); err != nil {
		return err
	}

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
	if err := a.buildTray(opt); err != nil {
		return err
	}

	go a.watchShowRequests()
	go a.watchCancel(ctx)
	go a.poll(ctx)

	a.checkCtx = ctx
	a.refresh(false)
	a.syncDiagLogUI()
	a.syncDistinctUI()
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
				a.setStatusText("Не удалось удалить товар")
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
	a.setStatusText("Запущена проверка всех цен. Таймер автоцикла сброшен.")
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

func (a *app) toggleDistinct() {
	on := a.board == nil || !a.board.distinct
	if a.board != nil {
		a.board.setDistinct(on)
	}
	a.syncDistinctUI()
	a.log.Info("узлы графика", "distinct", on)
}

func (a *app) syncDistinctUI() {
	text := "Distinct: выкл"
	if a.board != nil && a.board.distinct {
		text = "Distinct: вкл"
	}
	if a.distinctBtn != nil {
		_ = a.distinctBtn.SetText(text)
	}
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

// openProduct открывает карточку тем же Chrome, что и замер. В системный
// браузер отсюда не ходим: rundll32 берёт личный профиль пользователя.
func (a *app) openProduct(it Item) {
	if a.openInChrome == nil {
		a.log.Warn("карточку открывает только Chrome бота, обработчик не задан")
		return
	}
	city := it.CityKey
	if city == "" {
		city = it.City
	}
	a.log.Info("открываю карточку в Chrome бота", "site", it.SiteKey, "url", it.URL)
	a.openInChrome(storage.Product{URL: it.URL, Site: it.SiteKey, City: city})
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
