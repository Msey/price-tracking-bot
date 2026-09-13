//go:build windows

package gui

import (
	"github.com/lxn/walk"
	ui "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

// buildWindow собирает главное окно: шапку с кнопками и полотно списка.
// Всё, что создаёт объекты GDI, уходит в keep — их освобождает Run.
func (a *app) buildWindow(t theme, icon *walk.Icon, keep func(walk.Disposable)) error {
	chrome := t.buttonChrome()
	a.checkBtn = newThemeButton("Проверить цены", true, chrome, a.requestCheck)
	a.logBtn = newThemeButton("Логи: выкл", false, chrome, a.toggleDiagLog)
	a.distinctBtn = newThemeButton("Distinct: выкл", false, chrome, a.toggleDistinct)
	refreshBtn := newThemeButton("Обновить", false, chrome, a.refreshClicked)
	folderBtn := newThemeButton("Папка с данными", false, chrome, a.openDataFolder)

	var canvas *walk.CustomWidget
	if err := (ui.MainWindow{
		AssignTo:   &a.mw,
		Title:      "Трекинг цен",
		Icon:       icon,
		MinSize:    ui.Size{Width: 980, Height: 560},
		Size:       ui.Size{Width: 1040, Height: 760},
		Font:       ui.Font{Family: "Segoe UI", PointSize: 10},
		Background: ui.SolidColorBrush{Color: colorBg},
		Layout:     ui.VBox{Margins: ui.Margins{Left: 16, Top: 14, Right: 16, Bottom: 14}, Spacing: 10},
		Children: []ui.Widget{
			ui.Composite{
				Background: ui.SolidColorBrush{Color: colorBg},
				MinSize:    ui.Size{Height: 52},
				MaxSize:    ui.Size{Height: buttonMaxH},
				Layout:     ui.HBox{MarginsZero: true, Spacing: 12},
				Children: []ui.Widget{
					ui.Composite{
						Background: ui.SolidColorBrush{Color: colorBg},
						Layout:     ui.VBox{MarginsZero: true, Spacing: 2},
						Children: []ui.Widget{
							ui.Label{Text: "Ссылки и графики цен", Font: ui.Font{Family: "Segoe UI", PointSize: 16, Bold: true}, TextColor: colorGold},
							ui.Label{AssignTo: &a.status, Text: "Загрузка…", TextColor: colorMuted},
						},
					},
					ui.HSpacer{},
					a.checkBtn.cell(168),
					a.logBtn.cell(118),
					a.distinctBtn.cell(128),
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
		// Крестик прячет окно: бот продолжает работать в трее, и выход
		// делается только через пункт «Выход».
		*canceled = true
		a.hideToTray()
	})
	a.mw.SizeChanged().Attach(func() {
		if win.IsIconic(a.mw.Handle()) {
			a.hideToTray()
		}
	})
	return nil
}

// buildTray собирает меню иконки в трее. Пункты повторяют кнопки окна:
// с ними бот управляется, когда окно спрятано.
func (a *app) buildTray(opt Options) error {
	ni := a.ni
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
	_, err := ni.addAction("Выход", a.quit)
	return err
}
