package fetch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Msey/price-tracking-bot/internal/storage"
)

// Browser — один долгоживущий Chrome на все магазины. Страницы читает
// расширение (extBridge), процессом браузера занимается chromeProc,
// файлы расширения выкладывает extBundle. Сам Browser только сводит их
// вместе: одна карточка за раз, под mu.
type Browser struct {
	mu  sync.Mutex
	log *slog.Logger
	*chromeProc
	*extBridge
	// extID — id расширения в профиле, если Chrome его уже выдал.
	extID string
	// extSaved — расширение найдено в Preferences. Правится под mu.
	extSaved bool
	// done закрывается в Close: незавершённое ожидание страницы должно
	// оборваться сразу, иначе выход из бота ждёт полный таймаут карточки.
	done      chan struct{}
	closeOnce sync.Once
}

// extJob — задача расширению: одна карточка. Флаги atomic, потому что их
// правят обработчики HTTP.
type extJob struct {
	url         string
	site        string
	city        string
	bits        chan pageBits
	sent        atomic.Bool
	notedWait   atomic.Bool
	notedBadCSS atomic.Bool
}

type extResult struct {
	Href string   `json:"href"`
	Bits pageBits `json:"bits"`
}

// BrowserOptions — где лежит профиль и какой Chrome запускать. Скрытого
// режима здесь нет: карточки магазинов читаются только в обычном окне,
// headless Chrome они считают ботом.
type BrowserOptions struct {
	ProfileDir string
	ChromePath string
	Log        *slog.Logger
}

func NewBrowser(opt BrowserOptions) *Browser {
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	profile := strings.TrimSpace(opt.ProfileDir)
	if profile != "" {
		if abs, err := filepath.Abs(profile); err == nil {
			profile = abs
		}
	}
	b := &Browser{
		log: opt.Log,
		chromeProc: &chromeProc{
			chromePath: resolveChromePath(opt.ChromePath),
			profileDir: profile,
		},
		extBridge: newExtBridge(opt.Log),
		done:      make(chan struct{}),
	}
	if profile != "" {
		killChromeWithProfile(profile)
	}
	return b
}

func (b *Browser) SetOnChallenge(fn func(string)) {
	b.setOnChallenge(fn)
}

// Close гасит сервер и Chrome. Сначала обрывается ожидание страницы:
// проверка держит mu до конца таймаута, и без этого выход из бота вставал
// бы на минуту.
func (b *Browser) Close() {
	b.closeOnce.Do(func() { close(b.done) })
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopLocked()
}

func (b *Browser) stopLocked() {
	b.extBridge.stop()
	b.kill()
	b.chromeDead.Store(false)
}

func (b *Browser) do(ctx context.Context, timeout time.Duration, p storage.Product, parse func(pageBits) (Snapshot, error), pol pagePolicy) (Snapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.doLocked(ctx, timeout, p, parse, pol)
}

func (b *Browser) doLocked(ctx context.Context, timeout time.Duration, p storage.Product, parse func(pageBits) (Snapshot, error), pol pagePolicy) (Snapshot, error) {
	if timeout <= 0 {
		timeout = pageWait
	}
	j := &extJob{
		url:  p.URL,
		site: p.Site,
		city: p.City,
		bits: make(chan pageBits, 8),
	}
	b.job.Store(j)
	defer b.job.CompareAndSwap(j, nil)
	b.human.Store(false)
	b.log.Info("задача расширению", "site", p.Site, "url", p.URL, "timeout", timeout)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-b.done:
			cancel()
		case <-ctx.Done():
		}
	}()

	if err := b.ensureLocked(p.URL); err != nil {
		return Snapshot{}, err
	}

	snap, err := waitForBits(ctx, timeout, j.bits, parse, func(bits pageBits) {
		b.human.Store(true)
		b.noteChallenge(bits)
	}, pol)
	b.job.CompareAndSwap(j, nil)
	if err == nil || !b.human.Load() {
		b.requestClose()
	}
	return snap, err
}

// requestClose просит расширение закрыть вкладку. Если человек разбирается
// с капчей, вкладку оставляем.
func (b *Browser) requestClose() {
	b.closeReq.Store(true)
	b.poke()
	b.log.Info("закрываю вкладку магазина")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !b.alive() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if b.human.Load() {
		return
	}
	b.kill()
}

func (b *Browser) ensureLocked(startURL string) error {
	if startURL == "" {
		startURL = "about:blank"
	}
	if b.alive() {
		if b.extReady() {
			b.log.Info("открываю карточку через расширение", "url", startURL)
			b.poke()
			return nil
		}
		b.log.Warn("chrome жив, но расширение молчит — перезапускаю")
	}
	b.kill()

	if b.chromePath == "" {
		return fmt.Errorf("chrome: не найден chrome.exe")
	}
	if b.profileDir == "" {
		return fmt.Errorf("chrome: не задан профиль")
	}
	if err := os.MkdirAll(b.profileDir, 0o755); err != nil {
		return fmt.Errorf("chrome: профиль %s: %w", b.profileDir, err)
	}
	if err := b.serve(); err != nil {
		return err
	}
	extDir, err := extBundle{origin: "http://" + b.addr, token: b.token}.write()
	if err != nil {
		return err
	}

	if !b.extInProfileLocked() {
		b.log.Info("ставлю расширение в профиль", "ext", extDir)
		b.installExtLocked(extDir)
	}
	if err := b.launchLocked(extDir, startURL); err != nil {
		return err
	}
	if b.waitExtLocked(12 * time.Second) {
		return nil
	}

	// Расширение с командной строки Chrome иногда игнорирует. Второй заход:
	// прописать его в профиль и запустить ещё раз.
	b.log.Warn("расширение не подключилось, ставлю в профиль и пробую ещё раз")
	b.kill()
	b.installExtLocked(extDir)
	if err := b.launchLocked(extDir, startURL); err != nil {
		return err
	}
	if b.waitExtLocked(12 * time.Second) {
		return nil
	}
	return fmt.Errorf("chrome: расширение не подключилось")
}

func (b *Browser) launchLocked(extDir, startURL string) error {
	exceptID := ""
	if b.extID != "" && b.extInProfileLocked() {
		exceptID = b.extID
	}
	b.setExtSeen(make(chan struct{}))
	if err := b.chromeProc.start(extDir, exceptID); err != nil {
		return err
	}
	b.log.Info("chrome запущен", "profile", b.profileDir, "exe", b.chromePath,
		"ext", "http://"+b.addr, "url", startURL)
	return nil
}

func (b *Browser) installExtLocked(extDir string) {
	id, err := installUnpackedToProfile(b.chromePath, b.profileDir, extDir)
	if err != nil {
		b.log.Warn("расширение не закрепилось в профиле", "error", err)
		return
	}
	b.setExtID(id)
	b.log.Info("расширение поставлено", "id", id, "saved", b.recheckExtInProfile())
}

// setExtID запоминает id расширения и origin, по которому его пускает CORS.
func (b *Browser) setExtID(id string) {
	b.extID = id
	b.setExtOrigin(id)
}

// extInProfileLocked — расширение уже прописано в профиле. Проверка читает
// Preferences и Secure Preferences целиком, а это мегабайты, поэтому
// положительный ответ запоминаем на всё время работы процесса: из профиля
// расширение само не исчезает.
func (b *Browser) extInProfileLocked() bool {
	if b.extSaved {
		return true
	}
	b.extSaved = profileMentionsExtension(b.profileDir)
	return b.extSaved
}

// recheckExtInProfile перечитывает профиль после установки расширения.
func (b *Browser) recheckExtInProfile() bool {
	b.extSaved = false
	return b.extInProfileLocked()
}

func (b *Browser) waitExtLocked(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if b.chromeDead.Load() {
			return false
		}
		if b.extReady() {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return b.extReady()
}
