//go:build windows

package gui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Msey/price-tracking-bot/internal/view"
	"github.com/lxn/win"
)

// poll держит окно в курсе: раз в секунду обновляет надписи, а базу
// перечитывает реже — и чаще, когда окно открыто и его видно.
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
				every := 10 * time.Second
				if a.mw != nil && a.mw.Visible() && !win.IsIconic(a.mw.Handle()) {
					every = 4 * time.Second
				}
				if lastRefresh.IsZero() || time.Since(lastRefresh) >= every {
					lastRefresh = time.Now()
					a.refresh(true)
				}
			})
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
		if a.unchanged(ctx) {
			return
		}
		items, err := loadItems(ctx, a.store)
		a.onUI(func() { a.apply(items, err, notify) })
	}()
}

// unchanged — в базе с прошлого чтения ничего не менялось. Список и истории
// собираются оконными запросами по всей price_history, а опрос идёт раз в
// несколько секунд: без этой отсечки стоимость обновления окна росла бы
// вместе с историей цен.
func (a *app) unchanged(ctx context.Context) bool {
	mark, err := a.store.Fingerprint(ctx)
	if err != nil {
		a.log.Debug("отпечаток данных не прочитан", "error", err)
		return false
	}
	if a.haveMark && mark == a.mark {
		return true
	}
	a.mark, a.haveMark = mark, true
	return false
}

func (a *app) apply(items []Item, err error, notify bool) {
	if a.closed.Load() {
		return
	}
	if err != nil {
		a.log.Error("список товаров для окна", "error", err)
		a.setStatusText("Не удалось прочитать базу")
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
	if a.header == nil {
		return
	}
	if msg := a.checkMessage(); msg != "" {
		a.setStatusText(msg)
		a.noteCaptcha(msg)
		if a.ni != nil {
			_ = a.ni.setToolTip(a.tooltipText())
		}
		return
	}
	n := len(a.items)
	if a.checkRunning() {
		a.setStatusText("Идёт проверка цен · " +
			fmt.Sprintf("%d %s", n, view.RuPlural(n, "товар", "товара", "товаров")) +
			" · автоцикл начнётся заново после неё")
	} else {
		a.setStatusText("Работает в фоне · " +
			fmt.Sprintf("%d %s", n, view.RuPlural(n, "товар", "товара", "товаров")) +
			" в списке · закрытие окна прячет в трей")
	}
	if a.ni != nil {
		_ = a.ni.setToolTip(a.tooltipText())
	}
}

// statusMaxRunes — потолок подсказки в трее. Шапка рисует статус сама
// и обрезает многоточием по ширине колонки, без пересчёта рамы.
const statusMaxRunes = 70

func (a *app) setStatusText(s string) {
	if a.header == nil {
		return
	}
	a.header.setStatus(s)
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
