package fetch

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Msey/price-tracking-bot/internal/sites"
	"github.com/Msey/price-tracking-bot/internal/storage"
)

//go:embed ext/manifest.json ext/background.js ext/extract.js ext/rules.json
var extFS embed.FS

// Browser — один долгоживущий Chrome на все магазины. Страницы читает
// расширение в обычном Chrome, без remote-debugging-pipe/port: CDP Ozon
// принимает за бота и показывает «Похоже, нет соединения».
type Browser struct {
	mu          sync.Mutex
	log         *slog.Logger
	profileDir  string
	chromePath  string
	token       string
	addr        string
	srv         *http.Server
	cmd         *exec.Cmd
	job         atomic.Pointer[extJob]
	kick        chan struct{}
	extSeen     chan struct{}
	chromeDead  atomic.Bool
	closeReq    atomic.Bool
	human       atomic.Bool
	extID       string
	onChallenge func(string)
}

type extJob struct {
	url, site, city string
	sent            atomic.Bool
	bits            chan pageBits
}

type extResult struct {
	Href string   `json:"href"`
	Bits pageBits `json:"bits"`
}

type BrowserOptions struct {
	ProfileDir string
	ChromePath string
	Headless   bool
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
		log:        opt.Log,
		profileDir: profile,
		chromePath: resolveChromePath(opt.ChromePath),
		kick:       make(chan struct{}, 1),
	}
	if profile != "" {
		killChromeWithProfile(profile)
	}
	_ = opt.Headless
	return b
}

func (b *Browser) SetOnChallenge(fn func(string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onChallenge = fn
}

func (b *Browser) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stopLocked()
}

func (b *Browser) stopLocked() {
	b.job.Store(nil)
	if b.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = b.srv.Shutdown(ctx)
		cancel()
		b.srv = nil
	}
	b.stopChromeLocked()
	b.addr = ""
	b.extSeen = nil
	b.chromeDead.Store(false)
}

func (b *Browser) stopChromeLocked() {
	if b.profileDir != "" {
		killChromeWithProfile(b.profileDir)
	}
	b.cmd = nil
}

func (b *Browser) chromeAlive() bool {
	return b.cmd != nil && b.cmd.Process != nil && !b.chromeDead.Load()
}

func (b *Browser) do(ctx context.Context, timeout time.Duration, p storage.Product, parse func(pageBits) (Snapshot, error)) (Snapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.doLocked(ctx, timeout, p, parse)
}

func (b *Browser) doLocked(ctx context.Context, timeout time.Duration, p storage.Product, parse func(pageBits) (Snapshot, error)) (Snapshot, error) {
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

	if err := b.ensureLocked(p.URL); err != nil {
		return Snapshot{}, err
	}

	snap, err := waitForBits(ctx, timeout, j.bits, parse, func(bits pageBits) {
		b.human.Store(true)
		b.noteChallenge(bits)
	})
	b.job.CompareAndSwap(j, nil)
	if err == nil || !b.human.Load() {
		b.requestClose()
	}
	return snap, err
}

func (b *Browser) requestClose() {
	b.closeReq.Store(true)
	b.poke()
	b.log.Info("закрываю вкладку магазина")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !b.chromeAlive() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if b.human.Load() {
		return
	}
	// chromeAlive смотрит на процесс, который мы сами запустили. У Chrome
	// он часто сразу выходит, а окно живёт в другом процессе профиля —
	// тогда без kill вкладки копятся в уже открытом окне.
	b.stopChromeLocked()
}

func (b *Browser) poke() {
	select {
	case b.kick <- struct{}{}:
	default:
	}
}

func (b *Browser) ensureLocked(startURL string) error {
	if startURL == "" {
		startURL = "about:blank"
	}
	if b.chromeAlive() {
		if b.extReady() {
			b.navigateLocked(startURL)
			return nil
		}
		b.log.Warn("chrome жив, но расширение молчит — перезапускаю")
		b.stopChromeLocked()
	} else {
		b.stopChromeLocked()
	}

	if b.chromePath == "" {
		return fmt.Errorf("chrome: не найден chrome.exe")
	}
	if b.profileDir == "" {
		return fmt.Errorf("chrome: не задан профиль")
	}
	if err := os.MkdirAll(b.profileDir, 0o755); err != nil {
		return fmt.Errorf("chrome: профиль %s: %w", b.profileDir, err)
	}

	if err := b.ensureServerLocked(); err != nil {
		return err
	}

	extDir, err := b.writeExtension()
	if err != nil {
		return err
	}
	if !profileMentionsExtension(b.profileDir, extDir) {
		b.log.Info("ставлю расширение в профиль", "ext", extDir)
		id, err := installUnpackedToProfile(b.chromePath, b.profileDir, extDir)
		if err != nil {
			b.log.Warn("расширение не закрепилось в профиле", "error", err)
		} else {
			b.extID = id
			b.log.Info("расширение поставлено", "id", id, "saved", true)
		}
	}

	if err := b.launchShoppingLocked(extDir, startURL); err != nil {
		return err
	}
	if b.waitExtLocked(12 * time.Second) {
		return nil
	}
	b.log.Warn("расширение не подключилось, ставлю в профиль и пробую ещё раз")
	b.stopChromeLocked()
	id, err := installUnpackedToProfile(b.chromePath, b.profileDir, extDir)
	if err != nil {
		b.log.Warn("не удалось закрепить расширение", "error", err)
	} else {
		b.extID = id
		b.log.Info("расширение поставлено", "id", id, "saved", profileMentionsExtension(b.profileDir, extDir))
	}
	if err := b.launchShoppingLocked(extDir, startURL); err != nil {
		return err
	}
	if b.waitExtLocked(12 * time.Second) {
		return nil
	}
	return fmt.Errorf("chrome: расширение не подключилось")
}

func (b *Browser) launchShoppingLocked(extDir, startURL string) error {
	clearStaleProfileLocks(b.profileDir)
	markChromeExitedCleanly(b.profileDir)
	bustExtensionCache(b.profileDir)
	exceptID := ""
	if b.extID != "" && profileMentionsExtension(b.profileDir, extDir) {
		exceptID = b.extID
	}
	b.extSeen = make(chan struct{})
	cmd, err := startChrome(b.chromePath, b.profileDir, extDir, exceptID, startURL)
	if err != nil {
		return fmt.Errorf("chrome: запуск: %w", err)
	}
	b.cmd = cmd
	b.chromeDead.Store(false)
	go func() {
		_ = cmd.Wait()
		b.chromeDead.Store(true)
	}()
	b.log.Info("chrome запущен", "profile", b.profileDir, "exe", b.chromePath, "ext", "http://"+b.addr, "url", startURL)
	return nil
}

func (b *Browser) extReady() bool {
	if b.extSeen == nil {
		return false
	}
	select {
	case <-b.extSeen:
		return true
	default:
		return false
	}
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

func (b *Browser) ensureServerLocked() error {
	if b.srv != nil {
		return nil
	}
	if err := b.ensureToken(); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:18732")
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("chrome: слушатель: %w", err)
		}
	}
	b.addr = ln.Addr().String()
	mux := http.NewServeMux()
	mux.HandleFunc("/ext/ping", b.handlePing)
	mux.HandleFunc("/ext/wait-job", b.handleWaitJob)
	mux.HandleFunc("/ext/result", b.handleResult)
	b.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = b.srv.Serve(ln) }()
	return nil
}

func (b *Browser) navigateLocked(startURL string) {
	b.poke()
	b.log.Info("открываю карточку через расширение", "url", startURL)
}

func (b *Browser) writeExtension() (string, error) {
	dir := extensionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("chrome: каталог расширения: %w", err)
	}
	if err := copyEmbeddedExt(dir); err != nil {
		return "", err
	}
	_ = os.Remove(filepath.Join(dir, "config.js"))
	if err := stampManifest(dir); err != nil {
		return "", err
	}
	head := fmt.Sprintf("const EXT_ORIGIN = %q;\nconst EXT_TOKEN = %q;\n", "http://"+b.addr, b.token)
	for _, name := range []string{"background.js", "extract.js"} {
		p := filepath.Join(dir, name)
		raw, err := os.ReadFile(p)
		if err != nil {
			return "", fmt.Errorf("chrome: %s: %w", name, err)
		}
		if err := os.WriteFile(p, append([]byte(head), raw...), 0o644); err != nil {
			return "", fmt.Errorf("chrome: %s: %w", name, err)
		}
	}
	return dir, nil
}

func (b *Browser) auth(r *http.Request) bool {
	if b.token == "" {
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if got == "" || len(got) != len(b.token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(b.token)) == 1
}

func (b *Browser) markExtSeen() {
	if b.extSeen == nil {
		return
	}
	select {
	case <-b.extSeen:
	default:
		close(b.extSeen)
		b.log.Info("расширение на связи")
	}
}

func (b *Browser) handlePing(w http.ResponseWriter, r *http.Request) {
	cors(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !b.auth(r) {
		http.Error(w, "auth", http.StatusUnauthorized)
		return
	}
	b.markExtSeen()
	w.WriteHeader(http.StatusNoContent)
}

func cors(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if !strings.HasPrefix(origin, "chrome-extension://") {
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Private-Network", "true")
	w.Header().Set("Vary", "Origin")
}

func (b *Browser) handleWaitJob(w http.ResponseWriter, r *http.Request) {
	cors(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if !b.auth(r) {
		b.log.Warn("расширение пришло с чужим токеном")
		http.Error(w, "auth", http.StatusUnauthorized)
		return
	}
	b.markExtSeen()

	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for {
		if j := b.job.Load(); j != nil && j.sent.CompareAndSwap(false, true) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"url":  j.url,
				"site": j.site,
				"city": j.city,
			})
			b.log.Info("расширение взяло задачу", "site", j.site, "url", j.url)
			return
		}
		if b.job.Load() == nil && b.closeReq.CompareAndSwap(true, false) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"action": "close"})
			b.log.Info("расширение закрывает вкладку")
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
			b.log.Debug("расширение ждало задачу, пока пусто")
			w.WriteHeader(http.StatusNoContent)
			return
		case <-b.kick:
		}
	}
}

func (b *Browser) handleResult(w http.ResponseWriter, r *http.Request) {
	cors(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if !b.auth(r) {
		http.Error(w, "auth", http.StatusUnauthorized)
		return
	}
	defer r.Body.Close()
	var msg extResult
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&msg); err != nil {
		b.log.Debug("расширение прислало битый JSON", "error", err)
		http.Error(w, "json", http.StatusBadRequest)
		return
	}
	j := b.job.Load()
	if j == nil {
		b.log.Debug("результат без активной задачи", "href", msg.Href, "title", msg.Bits.Title)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if msg.Href != "" && !sameShopURL(j.url, msg.Href) {
		b.log.Debug("результат с чужого адреса", "job", j.url, "href", msg.Href, "title", msg.Bits.Title)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	b.log.Debug("снимок страницы",
		"site", j.site,
		"href", msg.Href,
		"title", msg.Bits.Title,
		"qrator", msg.Bits.QRATOR,
		"challenge", msg.Bits.Challenge,
		"blocked", msg.Bits.Blocked,
		"css", strings.TrimSpace(msg.Bits.CSSPrice) != "",
		"ldjson", len(msg.Bits.LDJSON),
	)
	pushLatestBits(j.bits, msg.Bits)
	w.WriteHeader(http.StatusNoContent)
}

// pushLatestBits кладёт свежий снимок, вытесняя самый старый, если
// буфер полон. Иначе расширение при быстрых мутациях DOM теряло бы
// последний (уже с ценой) кадр.
func pushLatestBits(ch chan pageBits, bits pageBits) {
	if ch == nil {
		return
	}
	for i := 0; i < cap(ch)+2; i++ {
		select {
		case ch <- bits:
			return
		default:
			select {
			case <-ch:
			default:
			}
		}
	}
}

func sameShopURL(job, href string) bool {
	ju, err1 := url.Parse(job)
	hu, err2 := url.Parse(href)
	if err1 != nil || err2 != nil {
		return false
	}
	return shopHostKey(ju.Hostname()) != "" && shopHostKey(ju.Hostname()) == shopHostKey(hu.Hostname())
}

func shopHostKey(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	switch {
	case strings.Contains(host, "dns-shop.ru"):
		return string(sites.DNS)
	case strings.Contains(host, "ozon.ru"):
		return string(sites.Ozon)
	case strings.Contains(host, "market.yandex"):
		return string(sites.YandexMarket)
	default:
		return ""
	}
}

func (b *Browser) noteChallenge(bits pageBits) {
	title := strings.TrimSpace(bits.Title)
	msg := "Нужно пройти капчу в окне Chrome"
	if ozonInterstitial(bits) {
		msg = "Нажмите «Обновить страницу» в окне Chrome"
	}
	if title != "" {
		msg += " · " + title
	}
	if b.onChallenge != nil {
		b.onChallenge(msg)
	}
	b.log.Warn("нужно действие в окне Chrome", "title", title)
}

func (b *Browser) ensureToken() error {
	if b.token != "" {
		return nil
	}
	tok, err := readOrCreateToken(tokenPath())
	if err != nil {
		return err
	}
	b.token = tok
	return nil
}

func tokenPath() string {
	return filepath.Join(filepath.Dir(extensionDir()), "ext.token")
}

func readOrCreateToken(path string) (string, error) {
	if raw, err := os.ReadFile(path); err == nil {
		t := strings.TrimSpace(string(raw))
		if len(t) >= 16 {
			return t, nil
		}
	}
	t, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("chrome: токен: %w", err)
	}
	if err := os.WriteFile(path, []byte(t+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("chrome: токен: %w", err)
	}
	return t, nil
}

func randomToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("chrome: токен: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

func stampManifest(dir string) error {
	p := filepath.Join(dir, "manifest.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("chrome: manifest.json: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("chrome: manifest.json: %w", err)
	}
	now := time.Now().Unix()
	m["version"] = fmt.Sprintf("1.%d.%d", now/65536, now%65536)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("chrome: manifest.json: %w", err)
	}
	return os.WriteFile(p, append(out, '\n'), 0o644)
}

func bustExtensionCache(profile string) {
	if profile == "" {
		return
	}
	for _, rel := range []string{
		filepath.Join("Default", "Service Worker"),
		filepath.Join("Default", "Extension State"),
	} {
		_ = os.RemoveAll(filepath.Join(profile, rel))
	}
}

func clearStaleProfileLocks(dir string) {
	for _, name := range []string{
		"SingletonLock", "SingletonSocket", "SingletonCookie",
		"lockfile", "DevToolsActivePort",
	} {
		_ = os.Remove(filepath.Join(dir, name))
	}
}

func markChromeExitedCleanly(dir string) {
	if dir == "" {
		return
	}
	repl := strings.NewReplacer(
		`"exited_cleanly":false`, `"exited_cleanly":true`,
		`"exited_cleanly": false`, `"exited_cleanly": true`,
		`"exit_type":"Crashed"`, `"exit_type":"Normal"`,
		`"exit_type": "Crashed"`, `"exit_type": "Normal"`,
		`"restore_on_startup":1`, `"restore_on_startup":5`,
		`"restore_on_startup": 1`, `"restore_on_startup": 5`,
		`"restore_on_startup":4`, `"restore_on_startup":5`,
		`"restore_on_startup": 4`, `"restore_on_startup": 5`,
	)
	for _, rel := range []string{"Local State", filepath.Join("Default", "Preferences")} {
		p := filepath.Join(dir, rel)
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		n := repl.Replace(string(b))
		n = ensureRestoreNewTab(n)
		if n != string(b) {
			_ = os.WriteFile(p, []byte(n), 0o644)
		}
	}
}

// ensureRestoreNewTab — иначе «продолжить с того места» поднимает все
// старые вкладки магазинов при каждом запуске Chrome.
func ensureRestoreNewTab(raw string) string {
	if strings.Contains(raw, `"restore_on_startup"`) {
		return raw
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return raw
	}
	sess, _ := m["session"].(map[string]any)
	if sess == nil {
		sess = map[string]any{}
		m["session"] = sess
	}
	sess["restore_on_startup"] = 5
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return string(out)
}

func chromeStartError(profile string, err error) error {
	msg := decodeProcessOutput(err.Error())
	if looksLikeExistingSession(msg) {
		return fmt.Errorf("Chrome открыл вкладку в уже запущенном браузере и не отдал управление. Нужен отдельный профиль %s: %s", profile, msg)
	}
	return fmt.Errorf("%s", msg)
}

func looksLikeExistingSession(msg string) bool {
	low := strings.ToLower(msg)
	return strings.Contains(msg, "текущем сеансе") ||
		strings.Contains(low, "current browser session") ||
		strings.Contains(low, "existing browser")
}

func isChromeStartError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "chrome failed to start") ||
		strings.Contains(msg, "chrome: запуск") ||
		strings.Contains(msg, "расширение не подключилось") ||
		strings.Contains(msg, "установка расширения") ||
		strings.Contains(msg, "текущем сеансе")
}

func copyEmbeddedExt(dir string) error {
	return fs.WalkDir(extFS, "ext", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(extFS, path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, filepath.Base(path)), b, 0o644)
	})
}

func extensionDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "price-tracking-bot", "chrome-ext")
}
