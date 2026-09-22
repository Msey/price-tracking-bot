package tracker

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Msey/price-tracking-bot/internal/fetch"
	"github.com/Msey/price-tracking-bot/internal/storage"
)

type fakeDNS struct {
	price int64
	calls int
}

func (f *fakeDNS) Fetch(context.Context, storage.Product) (fetch.Snapshot, error) {
	f.calls++
	return fetch.Snapshot{Name: "Товар", PriceKopecks: f.price, Currency: "RUB", Available: true}, nil
}

func (f *fakeDNS) Paused() (time.Time, string, bool) { return time.Time{}, "", false }

type pausedFake struct{ fakeDNS }

func (p *pausedFake) Paused() (time.Time, string, bool) {
	return time.Now().Add(time.Hour), "test", true
}

type fakeNotify struct {
	n     int
	chats []int64
	msgs  []string
}

func (f *fakeNotify) Notify(_ context.Context, chatID int64, message string) error {
	f.n++
	f.chats = append(f.chats, chatID)
	f.msgs = append(f.msgs, message)
	return nil
}

func TestCheckOneNotifiesOnFirstChange(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	p, _, err := store.AddSubscription(ctx, 42, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow")
	if err != nil {
		t.Fatal(err)
	}

	dns := &fakeDNS{price: 10000}
	notes := &fakeNotify{}
	tr := New(store, map[string]Fetcher{"dns": dns}, notes, Config{
		Interval:     20 * time.Minute,
		FetchGap:     30 * time.Second,
		PerCycle:     8,
		StartupDelay: time.Minute,
	}, nil)

	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if notes.n != 0 {
		t.Fatalf("база не должна писать в чат, получено %d", notes.n)
	}

	dns.price = 9000
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if notes.n != 1 {
		t.Fatalf("смена относительно предыдущей цены должна сразу уведомить, получено %d", notes.n)
	}
	if dns.calls != 3 {
		t.Fatalf("вызовов fetch %d", dns.calls)
	}
	hist, err := store.LastSnapshots(ctx, p.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 3 {
		t.Fatalf("в истории %d строк, ожидалось 3 (каждый замер — точка)", len(hist))
	}
}

type missingDNS struct {
	miss  bool
	price int64
	calls int
}

func (f *missingDNS) Fetch(context.Context, storage.Product) (fetch.Snapshot, error) {
	f.calls++
	if f.miss {
		return fetch.Snapshot{}, fetch.ErrNoPrice
	}
	return fetch.Snapshot{Name: "Товар", PriceKopecks: f.price, Currency: "RUB", Available: true}, nil
}

func (f *missingDNS) Paused() (time.Time, string, bool) { return time.Time{}, "", false }

type deadNotify struct{ n int }

func (f *deadNotify) Notify(context.Context, int64, string) error {
	f.n++
	return errors.New("forbidden: bot was blocked by the user")
}

// Подписчик, заблокировавший бота, не должен превращать удачный замер
// в ошибку проверки: цена уже записана, остальным она ушла.
func TestCheckOneSurvivesUndeliveredNotification(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "blocked.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	p, _, err := store.AddSubscription(ctx, 42, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow")
	if err != nil {
		t.Fatal(err)
	}

	dns := &fakeDNS{price: 10000}
	notes := &deadNotify{}
	tr := New(store, map[string]Fetcher{"dns": dns}, notes, Config{}, nil)

	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	dns.price = 9000
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatalf("недоставленное уведомление не должно валить проверку: %v", err)
	}
	if notes.n != 1 {
		t.Fatalf("попыток отправки %d, ожидалась одна", notes.n)
	}
	hist, err := store.LastSnapshots(ctx, p.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 || hist[0].PriceKopecks != 9000 {
		t.Fatalf("замер должен остаться в истории: %+v", hist)
	}
}

func TestCheckOneRecordsMissingPrice(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "miss.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	p, _, err := store.AddSubscription(ctx, 42, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow")
	if err != nil {
		t.Fatal(err)
	}

	dns := &missingDNS{price: 15500}
	notes := &fakeNotify{}
	tr := New(store, map[string]Fetcher{"dns": dns}, notes, Config{
		Interval: 20 * time.Minute, FetchGap: 30 * time.Second, PerCycle: 8, StartupDelay: time.Minute,
	}, nil)
	tr.recheckGap = 0

	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	dns.miss = true
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if dns.calls != 4 {
		t.Fatalf("запросов %d, ожидалось 4: база и три замера пропажи", dns.calls)
	}
	hist, err := store.LastSnapshots(ctx, p.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 {
		t.Fatalf("строк %d", len(hist))
	}
	if hist[0].Available || hist[0].PriceKopecks != 15500 {
		t.Fatalf("последний замер должен быть серым с прошлой ценой: %+v", hist[0])
	}
	if !hist[1].Available || hist[1].PriceKopecks != 15500 {
		t.Fatalf("предыдущий живой замер: %+v", hist[1])
	}
	if notes.n != 1 || !strings.Contains(notes.msgs[0], "пропал") {
		t.Fatalf("подтверждённая пропажа должна уведомить один раз, получено %+v", notes.msgs)
	}
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if dns.calls != 5 {
		t.Fatalf("повторная пропажа не должна снова открывать карточку трижды, запросов %d", dns.calls)
	}
	if notes.n != 1 {
		t.Fatalf("о той же пропаже второе письмо не нужно, получено %d", notes.n)
	}

	empty, _, err := store.AddSubscription(ctx, 7, "dns", "aaaaaaaaaaaaaaaa", "https://www.dns-shop.ru/product/aaaaaaaaaaaaaaaa/", "moscow")
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.checkOne(ctx, empty); err != nil {
		t.Fatal(err)
	}
	none, err := store.LastSnapshots(ctx, empty.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 1 || none[0].Available || none[0].PriceKopecks != 0 {
		t.Fatalf("без истории пишем нулевой серый замер, чтобы сдвинуть очередь: %+v", none)
	}
	if dns.calls != 6 {
		t.Fatalf("первая проверка без цены — один запрос, не перепроверка, запросов %d", dns.calls)
	}
	if notes.n != 1 {
		t.Fatal("первая проверка без цены не должна писать в чат")
	}
	due, err := store.ProductsDue(ctx, "dns", time.Now().Add(-20*time.Minute), 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range due {
		if p.ID == empty.ID {
			t.Fatal("только что проверенный товар без цены не должен снова быть в очереди")
		}
	}
}

type scriptedStep struct {
	snap fetch.Snapshot
	err  error
}

type scriptedFetch struct {
	steps []scriptedStep
	n     int
}

func (f *scriptedFetch) Fetch(context.Context, storage.Product) (fetch.Snapshot, error) {
	if f.n >= len(f.steps) {
		return fetch.Snapshot{}, errors.New("лишний запрос карточки")
	}
	step := f.steps[f.n]
	f.n++
	return step.snap, step.err
}

func (f *scriptedFetch) Paused() (time.Time, string, bool) { return time.Time{}, "", false }

func stockSnap(kopecks int64, available bool) fetch.Snapshot {
	return fetch.Snapshot{Name: "Товар", PriceKopecks: kopecks, Currency: "RUB", Available: available}
}

func newTracked(t *testing.T, f Fetcher) (*Tracker, *storage.Store, storage.Product, *fakeNotify) {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	p, _, err := store.AddSubscription(context.Background(), 42, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow")
	if err != nil {
		t.Fatal(err)
	}
	notes := &fakeNotify{}
	tr := New(store, map[string]Fetcher{"dns": f}, notes, Config{}, nil)
	tr.recheckGap = 0
	return tr, store, p, notes
}

func TestDisappearanceRecoversBeforeAnnounce(t *testing.T) {
	ctx := context.Background()
	f := &scriptedFetch{steps: []scriptedStep{
		{snap: stockSnap(10000, true)},
		{err: fetch.ErrNoPrice},
		{snap: stockSnap(10000, true)},
	}}
	tr, store, p, notes := newTracked(t, f)
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if notes.n != 0 {
		t.Fatalf("живой повтор не должен писать о пропаже: %+v", notes.msgs)
	}
	if f.n != 3 {
		t.Fatalf("запросов %d, хватило первого повтора", f.n)
	}
	hist, err := store.LastSnapshots(ctx, p.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 || !hist[0].Available || !hist[1].Available {
		t.Fatalf("сбой не должен оставить серую точку: %+v", hist)
	}
}

func TestDisappearanceSecondRecheckCanStillRecover(t *testing.T) {
	ctx := context.Background()
	f := &scriptedFetch{steps: []scriptedStep{
		{snap: stockSnap(10000, true)},
		{err: fetch.ErrNoPrice},
		{err: fetch.ErrNoPrice},
		{snap: stockSnap(9000, true)},
	}}
	tr, store, p, notes := newTracked(t, f)
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if notes.n != 1 || strings.Contains(notes.msgs[0], "пропал") {
		t.Fatalf("нашлась новая цена, не пропажа: %+v", notes.msgs)
	}
	if f.n != 4 {
		t.Fatalf("запросов %d, нужны оба повтора", f.n)
	}
	hist, err := store.LastSnapshots(ctx, p.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 || !hist[0].Available || hist[0].PriceKopecks != 9000 {
		t.Fatalf("в истории должна остаться живая цена: %+v", hist)
	}
}

func TestDisappearanceHardErrorIsNotStockout(t *testing.T) {
	ctx := context.Background()
	f := &scriptedFetch{steps: []scriptedStep{
		{snap: stockSnap(10000, true)},
		{err: fetch.ErrNoPrice},
		{err: errors.New("connection reset")},
	}}
	tr, store, p, notes := newTracked(t, f)
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	err := tr.checkOne(ctx, p)
	if err == nil || errors.Is(err, fetch.ErrNoPrice) {
		t.Fatalf("обрыв сети нельзя превращать в пропажу: %v", err)
	}
	if notes.n != 0 {
		t.Fatalf("писем %d", notes.n)
	}
	if f.n != 3 {
		t.Fatalf("после обрыва третий замер не нужен, запросов %d", f.n)
	}
	hist, err := store.LastSnapshots(ctx, p.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || !hist[0].Available {
		t.Fatalf("неподтверждённая пропажа не пишется: %+v", hist)
	}
}

func TestDisappearanceCancelDoesNotWaitOrRecord(t *testing.T) {
	f := &scriptedFetch{steps: []scriptedStep{
		{snap: stockSnap(10000, true)},
		{err: fetch.ErrNoPrice},
	}}
	tr, store, p, notes := newTracked(t, f)
	if err := tr.checkOne(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	tr.recheckGap = 30 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := tr.checkOne(ctx, p)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("отмена: %v", err)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("отмена не должна ждать паузу перепроверки")
	}
	if notes.n != 0 || f.n != 2 {
		t.Fatalf("писем %d, запросов %d", notes.n, f.n)
	}
	hist, err := store.LastSnapshots(context.Background(), p.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || !hist[0].Available {
		t.Fatalf("отменённая перепроверка не должна писать серую точку: %+v", hist)
	}
}

func TestExplicitUnavailableNeedsTwoRechecks(t *testing.T) {
	ctx := context.Background()
	f := &scriptedFetch{steps: []scriptedStep{
		{snap: stockSnap(10000, true)},
		{snap: stockSnap(10000, false)},
		{snap: stockSnap(10000, false)},
		{snap: stockSnap(10000, false)},
	}}
	tr, store, p, notes := newTracked(t, f)
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if notes.n != 1 || !strings.Contains(notes.msgs[0], "пропал") || strings.Contains(notes.msgs[0], "выросла") {
		t.Fatalf("письмо: %+v", notes.msgs)
	}
	if f.n != 4 {
		t.Fatalf("запросов %d", f.n)
	}
	hist, err := store.LastSnapshots(ctx, p.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 || hist[0].Available || hist[0].PriceKopecks != 10000 {
		t.Fatalf("одна серая точка после трёх согласий: %+v", hist)
	}
}

func TestFirstUnavailableIsNotADisappearance(t *testing.T) {
	ctx := context.Background()
	f := &scriptedFetch{steps: []scriptedStep{
		{snap: stockSnap(10000, false)},
	}}
	tr, store, p, notes := newTracked(t, f)
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if notes.n != 0 || f.n != 1 {
		t.Fatalf("писем %d, запросов %d", notes.n, f.n)
	}
	hist, err := store.LastSnapshots(ctx, p.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].Available {
		t.Fatalf("первая карточка без наличия — база, не перепроверка: %+v", hist)
	}
}

func TestCheckOneNotifiesDropAfterFirstPrice(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "drop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	p, _, err := store.AddSubscription(ctx, 42, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow")
	if err != nil {
		t.Fatal(err)
	}

	dns := &fakeDNS{price: 10000}
	notes := &fakeNotify{}
	tr := New(store, map[string]Fetcher{"dns": dns}, notes, Config{
		Interval: 20 * time.Minute, FetchGap: 30 * time.Second, PerCycle: 8, StartupDelay: time.Minute,
	}, nil)

	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if notes.n != 0 {
		t.Fatal("первая цена не уведомляет")
	}
	dns.price = 9000
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if notes.n != 1 {
		t.Fatalf("смена после первой цены должна сразу уведомить, получено %d", notes.n)
	}
}

func TestCheckOneFiresPriceAlertOncePerSubscriber(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "alert.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	p, _, err := store.AddSubscription(ctx, 42, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AddSubscription(ctx, 43, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPriceAlert(ctx, 42, p.ID, 9500); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPriceAlert(ctx, 43, p.ID, 8500); err != nil {
		t.Fatal(err)
	}

	dns := &fakeDNS{price: 10000}
	notes := &fakeNotify{}
	tr := New(store, map[string]Fetcher{"dns": dns}, notes, Config{
		Interval: 20 * time.Minute, FetchGap: 30 * time.Second, PerCycle: 8, StartupDelay: time.Minute,
	}, nil)

	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if notes.n != 0 {
		t.Fatalf("цена выше порога не должна писать про порог, получено %d", notes.n)
	}

	dns.price = 9000
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	// смена цены — обоим, порог Алисы (42) — только ей
	if notes.n != 3 {
		t.Fatalf("писем %d, ожидалось 3 (два про смену и одно про порог)", notes.n)
	}
	alerts := 0
	for i, chat := range notes.chats {
		if strings.Contains(notes.msgs[i], "ниже порога") {
			alerts++
			if chat != 42 {
				t.Fatalf("первый порог ушёл чужому чату %d", chat)
			}
		}
	}
	if alerts != 1 {
		t.Fatalf("писем про порог %d", alerts)
	}
	alice, err := store.ListSubscriptions(ctx, 42)
	if err != nil || len(alice) != 1 || alice[0].AlertKopecks != 0 {
		t.Fatalf("после письма порог Алисы должен пропасть: %+v err=%v", alice, err)
	}
	bob, err := store.ListSubscriptions(ctx, 43)
	if err != nil || len(bob) != 1 || bob[0].AlertKopecks != 8500 {
		t.Fatalf("порог Боба задели: %+v err=%v", bob, err)
	}

	dns.price = 8000
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	alerts = 0
	for _, msg := range notes.msgs {
		if strings.Contains(msg, "ниже порога") {
			alerts++
		}
	}
	if alerts != 2 {
		t.Fatalf("на 8000 должен сработать порог Боба, писем про порог %d", alerts)
	}
	bob, err = store.ListSubscriptions(ctx, 43)
	if err != nil || len(bob) != 1 || bob[0].AlertKopecks != 0 {
		t.Fatalf("после письма порог Боба должен пропасть: %+v err=%v", bob, err)
	}
}

func TestCheckOneFiresAlertOnFirstPriceAlreadyBelow(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "alert-first.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	p, _, err := store.AddSubscription(ctx, 42, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPriceAlert(ctx, 42, p.ID, 9500); err != nil {
		t.Fatal(err)
	}

	dns := &fakeDNS{price: 9000}
	notes := &fakeNotify{}
	tr := New(store, map[string]Fetcher{"dns": dns}, notes, Config{
		Interval: 20 * time.Minute, FetchGap: 30 * time.Second, PerCycle: 8, StartupDelay: time.Minute,
	}, nil)
	if err := tr.checkOne(ctx, p); err != nil {
		t.Fatal(err)
	}
	if notes.n != 1 || !strings.Contains(notes.msgs[0], "ниже порога") {
		t.Fatalf("первая цена ниже порога — одно письмо про порог, получено %d %v", notes.n, notes.msgs)
	}
	left, err := store.ListSubscriptions(ctx, 42)
	if err != nil || len(left) != 1 || left[0].AlertKopecks != 0 {
		t.Fatalf("после письма порог должен пропасть: %+v err=%v", left, err)
	}
}

func TestMinCheckInterval(t *testing.T) {
	if got := minCheckInterval(nil); got != time.Hour {
		t.Fatalf("пусто: %s", got)
	}
	if got := minCheckInterval(map[string]Fetcher{"dns": &fakeDNS{}}); got != 24*time.Hour {
		t.Fatalf("только dns: %s", got)
	}
	if got := minCheckInterval(map[string]Fetcher{"dns": &fakeDNS{}, "ozon": &fakeDNS{}}); got != 90*time.Minute {
		t.Fatalf("dns+ozon: %s", got)
	}
	tr := New(nil, map[string]Fetcher{
		"dns":           &fakeDNS{},
		"ozon":          &fakeDNS{},
		"yandex_market": &fakeDNS{},
	}, nil, Config{Interval: time.Hour}, nil)
	if tr.cfg.Interval != 90*time.Minute {
		t.Fatalf("пауза автоцикла = %s, ожидалось 1,5 часа из-за Ozon", tr.cfg.Interval)
	}
	if siteCheckInterval("dns") != 24*time.Hour || siteCheckInterval("yandex_market") != 24*time.Hour {
		t.Fatal("dns и маркет должны быть раз в сутки")
	}
	if siteCheckInterval("ozon") != 90*time.Minute {
		t.Fatal("ozon должен быть раз в 1,5 часа")
	}
}

func TestCycleSiteHonorsPause(t *testing.T) {
	f := &pausedFake{}
	tr := New(nil, map[string]Fetcher{"dns": f}, nil, Config{
		FetchGap:     30 * time.Second,
		PerCycle:     8,
		StartupDelay: time.Minute,
	}, nil)
	tr.cycleSite(context.Background(), "dns", true, func(context.Context, string) ([]storage.Product, error) {
		t.Fatal("при предохранителе список не спрашивают")
		return nil, nil
	})
	if f.calls != 0 {
		t.Fatalf("fetch вызван %d раз", f.calls)
	}
}

func TestWaitInterruptedByRequestCheck(t *testing.T) {
	tr := New(nil, nil, nil, Config{
		Interval:     20 * time.Minute,
		FetchGap:     30 * time.Second,
		PerCycle:     8,
		StartupDelay: time.Minute,
	}, nil)

	ctx := context.Background()
	start := time.Now()
	go func() {
		time.Sleep(30 * time.Millisecond)
		tr.RequestCheck()
	}()
	if !tr.wait(ctx, 2*time.Second, "cycle") {
		t.Fatal("wait вернул false")
	}
	if time.Since(start) > 800*time.Millisecond {
		t.Fatal("ожидание автоцикла не сбросилось")
	}
	if !tr.Busy() && !tr.consumeKick() {
		t.Fatal("после сброса должна остаться принудительная проверка")
	}
}

func TestCycleAllChecksFreshProducts(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	p, _, err := store.AddSubscription(ctx, 42, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordSnapshot(ctx, p.ID, "Товар", 10000, "RUB", true); err != nil {
		t.Fatal(err)
	}

	dns := &fakeDNS{price: 10000}
	tr := New(store, map[string]Fetcher{"dns": dns}, &fakeNotify{}, Config{
		Interval:     20 * time.Minute,
		FetchGap:     30 * time.Second,
		PerCycle:     8,
		StartupDelay: time.Minute,
	}, nil)
	tr.cfg.FetchGap = 0

	tr.cycle(ctx)
	if dns.calls != 0 {
		t.Fatalf("обычный цикл не должен трогать свежий товар, вызовов %d", dns.calls)
	}

	tr.cycleAll(ctx)
	if dns.calls != 1 {
		t.Fatalf("полная проверка должна сходить за свежим товаром, вызовов %d", dns.calls)
	}
}

func TestCycleAllDoesNotWaitInitialGap(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	if _, _, err := store.AddSubscription(ctx, 42, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow"); err != nil {
		t.Fatal(err)
	}

	dns := &fakeDNS{price: 10000}
	tr := New(store, map[string]Fetcher{"dns": dns}, &fakeNotify{}, Config{
		Interval:     20 * time.Minute,
		FetchGap:     30 * time.Second,
		PerCycle:     8,
		StartupDelay: time.Minute,
	}, nil)
	tr.lastHit = time.Now()

	done := make(chan struct{})
	go func() {
		tr.cycleAll(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(800 * time.Millisecond):
		t.Fatal("полная проверка ждала паузу перед первым товаром")
	}
	if dns.calls != 1 {
		t.Fatalf("вызовов %d", dns.calls)
	}
}

func TestCycleAllDoesNotWaitBetweenSites(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	if _, _, err := store.AddSubscription(ctx, 42, "dns", "9ee3a4f41358d9cb", "https://www.dns-shop.ru/product/9ee3a4f41358d9cb/", "moscow"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AddSubscription(ctx, 42, "ozon", "2190214590", "https://www.ozon.ru/product/2190214590", "moscow"); err != nil {
		t.Fatal(err)
	}

	dns := &fakeDNS{price: 10000}
	ozon := &fakeDNS{price: 20000}
	tr := New(store, map[string]Fetcher{"dns": dns, "ozon": ozon}, &fakeNotify{}, Config{
		Interval:     20 * time.Minute,
		FetchGap:     30 * time.Second,
		PerCycle:     8,
		StartupDelay: time.Minute,
	}, nil)

	start := time.Now()
	tr.cycleAll(ctx)
	if time.Since(start) > 800*time.Millisecond {
		t.Fatal("принудительная проверка ждала FETCH_GAP между магазинами")
	}
	if dns.calls != 1 || ozon.calls != 1 {
		t.Fatalf("dns %d ozon %d", dns.calls, ozon.calls)
	}
}

func TestFormatCountdown(t *testing.T) {
	if got := formatCountdown(5 * time.Second); got != "5 с" {
		t.Fatalf("5 с: %q", got)
	}
	if got := formatCountdown(65 * time.Second); got != "1 мин 5 с" {
		t.Fatalf("1 мин 5 с: %q", got)
	}
	if got := formatCountdown(3723 * time.Second); got != "1 ч 2 мин 3 с" {
		t.Fatalf("1 ч 2 мин 3 с: %q", got)
	}
	if got := formatCountdown(-time.Second); got != "0 с" {
		t.Fatalf("отрицательное: %q", got)
	}
}

func TestStatusTextCountsDown(t *testing.T) {
	tr := New(nil, nil, nil, Config{
		Interval:     20 * time.Minute,
		FetchGap:     30 * time.Second,
		PerCycle:     8,
		StartupDelay: time.Minute,
	}, nil)
	tr.setUntil(time.Now().Add(90*time.Second), "cycle")
	got := tr.StatusText()
	if !strings.HasPrefix(got, "Следующая проверка через 1 мин ") {
		t.Fatalf("отсчёт автоцикла: %q", got)
	}
	tr.setUntil(time.Now().Add(12*time.Second), "startup")
	got = tr.StatusText()
	if got != "Первая проверка через 12 с" && got != "Первая проверка через 11 с" {
		t.Fatalf("отсчёт старта: %q", got)
	}
	tr.setStatus("%s", "Xiaomi")
	tr.setUntil(time.Now().Add(45*time.Second), "gap")
	got = tr.StatusText()
	if !strings.Contains(got, "Пауза ") || !strings.Contains(got, "Xiaomi") {
		t.Fatalf("пауза между товарами: %q", got)
	}
	tr.setStatus("%s", "Перепроверяю наличие · 1/2 · Xiaomi")
	tr.setUntil(time.Now().Add(3*time.Second), "recheck")
	got = tr.StatusText()
	if !strings.Contains(got, "Перепроверяю наличие") || (!strings.Contains(got, "3 с") && !strings.Contains(got, "2 с")) {
		t.Fatalf("пауза перепроверки: %q", got)
	}
	tr.setStatus("%s", "Ошибка dns · сайт показал защиту")
	tr.setUntil(time.Now().Add(20*time.Minute), "cycle")
	got = tr.StatusText()
	if !strings.Contains(got, "Ошибка dns") || !strings.Contains(got, "следующая проверка через") {
		t.Fatalf("ошибка с отсчётом: %q", got)
	}
}
