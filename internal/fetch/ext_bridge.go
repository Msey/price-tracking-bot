package fetch

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Msey/price-tracking-bot/internal/diaglog"
	"github.com/Msey/price-tracking-bot/internal/sites"
)

// extBridge — связь бота с расширением Chrome: HTTP на loopback, токен и
// передача задачи «прочитай эту карточку». Расширение само приходит за
// задачей и приносит снимок страницы, потому что вкладку магазина нельзя
// трогать через CDP — Ozon считает такой Chrome ботом.
//
// Всё состояние здесь читают обработчики HTTP, то есть чужие горутины,
// поэтому оно либо атомарное, либо под своим мьютексом. Большой mu
// браузера для этого не годится: он держится всю проверку карточки.
type extBridge struct {
	log      *slog.Logger
	token    string
	addr     string
	srv      *http.Server
	job      atomic.Pointer[extJob]
	kick     chan struct{}
	closeReq atomic.Bool
	// human — на странице капча или заглушка: вкладку не закрываем,
	// человек должен успеть нажать кнопку.
	human atomic.Bool
	// extMu закрывает только extSeen.
	extMu   sync.Mutex
	extSeen chan struct{}
	// extOrigin — chrome-extension://<id> своего расширения, если id уже
	// известен.
	extOrigin   atomic.Pointer[string]
	onChallenge atomic.Pointer[func(string)]
}

func newExtBridge(log *slog.Logger) *extBridge {
	return &extBridge{log: log, kick: make(chan struct{}, 1)}
}

// serve поднимает локальный сервер. Порт по возможности постоянный: он
// прописан в manifest.json расширения.
func (e *extBridge) serve() error {
	if e.srv != nil {
		return nil
	}
	if err := e.ensureToken(); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:18732")
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("chrome: слушатель: %w", err)
		}
	}
	e.addr = ln.Addr().String()
	mux := http.NewServeMux()
	mux.HandleFunc("/ext/ping", e.handlePing)
	mux.HandleFunc("/ext/wait-job", e.handleWaitJob)
	mux.HandleFunc("/ext/result", e.handleResult)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	e.srv = srv
	go func() { _ = srv.Serve(ln) }()
	return nil
}

func (e *extBridge) stop() {
	e.job.Store(nil)
	srv := e.srv
	e.srv = nil
	e.addr = ""
	e.setExtSeen(nil)
	if srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_ = srv.Shutdown(ctx)
	cancel()
}

// poke будит долгий запрос расширения: оно висит на /ext/wait-job и должно
// сразу узнать про новую задачу или про закрытие вкладки.
func (e *extBridge) poke() {
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

func (e *extBridge) setOnChallenge(fn func(string)) {
	if fn == nil {
		e.onChallenge.Store(nil)
		return
	}
	e.onChallenge.Store(&fn)
}

// setExtID запоминает origin, по которому CORS пускает своё расширение.
func (e *extBridge) setExtOrigin(id string) {
	if id == "" {
		e.extOrigin.Store(nil)
		return
	}
	origin := "chrome-extension://" + id
	e.extOrigin.Store(&origin)
}

func (e *extBridge) setExtSeen(ch chan struct{}) {
	e.extMu.Lock()
	e.extSeen = ch
	e.extMu.Unlock()
}

func (e *extBridge) extReady() bool {
	e.extMu.Lock()
	ch := e.extSeen
	e.extMu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// markExtSeen зовут обработчики HTTP, их может быть несколько сразу:
// проверка «не закрыт ли уже» и закрытие должны быть под одним замком,
// иначе второй вызов закрыл бы канал повторно и уронил процесс.
func (e *extBridge) markExtSeen() {
	e.extMu.Lock()
	ch := e.extSeen
	closed := false
	if ch != nil {
		select {
		case <-ch:
		default:
			close(ch)
			closed = true
		}
	}
	e.extMu.Unlock()
	if closed {
		e.log.Info("расширение на связи")
	}
}

func (e *extBridge) auth(r *http.Request) bool {
	if e.token == "" {
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if got == "" || len(got) != len(e.token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(e.token)) == 1
}

// cors отвечает только своему расширению. Пока его id неизвестен (Chrome
// выдаёт его при установке), пускаем любое chrome-extension: без токена
// оттуда всё равно ничего не выйдет.
func (e *extBridge) cors(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if !strings.HasPrefix(origin, "chrome-extension://") {
		return
	}
	if want := e.extOrigin.Load(); want != nil && *want != origin {
		e.log.Debug("CORS с чужого расширения", "origin", origin, "want", *want)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Private-Network", "true")
	w.Header().Set("Vary", "Origin")
}

func (e *extBridge) handlePing(w http.ResponseWriter, r *http.Request) {
	e.cors(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !e.auth(r) {
		http.Error(w, "auth", http.StatusUnauthorized)
		return
	}
	e.markExtSeen()
	w.WriteHeader(http.StatusNoContent)
}

func (e *extBridge) handleWaitJob(w http.ResponseWriter, r *http.Request) {
	e.cors(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if !e.auth(r) {
		e.log.Warn("расширение пришло с чужим токеном")
		http.Error(w, "auth", http.StatusUnauthorized)
		return
	}
	e.markExtSeen()

	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for {
		if j := e.job.Load(); j != nil && j.sent.CompareAndSwap(false, true) {
			left, top := offscreenOrigin()
			payload := map[string]string{
				"url":  j.url,
				"site": j.site,
				"city": j.city,
				"left": strconv.Itoa(left),
				"top":  strconv.Itoa(top),
			}
			if j.focus {
				payload["focus"] = "1"
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(payload)
			e.log.Info("расширение взяло задачу", "site", j.site, "url", j.url, "focus", j.focus)
			return
		}
		if e.job.Load() == nil && e.closeReq.CompareAndSwap(true, false) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"action": "close"})
			e.log.Info("расширение закрывает вкладку")
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
			e.log.Debug("расширение ждало задачу, пока пусто")
			w.WriteHeader(http.StatusNoContent)
			return
		case <-e.kick:
		}
	}
}

func (e *extBridge) handleResult(w http.ResponseWriter, r *http.Request) {
	e.cors(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if !e.auth(r) {
		http.Error(w, "auth", http.StatusUnauthorized)
		return
	}
	defer r.Body.Close()
	var msg extResult
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&msg); err != nil {
		e.log.Debug("расширение прислало битый JSON", "error", err)
		http.Error(w, "json", http.StatusBadRequest)
		return
	}
	j := e.job.Load()
	if j == nil {
		e.log.Debug("результат без активной задачи", "href", msg.Href, "title", msg.Bits.Title)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if msg.Href != "" && !sameShopURL(j.url, msg.Href) {
		e.log.Debug("результат с чужого адреса", "job", j.url, "href", msg.Href, "title", msg.Bits.Title)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	e.log.Debug("снимок страницы",
		"site", j.site,
		"href", msg.Href,
		"title", msg.Bits.Title,
		"qrator", msg.Bits.QRATOR,
		"challenge", msg.Bits.Challenge,
		"blocked", msg.Bits.Blocked,
		"css", strings.TrimSpace(msg.Bits.CSSPrice) != "",
		"ldjson", len(msg.Bits.LDJSON),
	)
	e.notePriceBits(j, msg)
	pushLatestBits(j.bits, msg.Bits)
	w.WriteHeader(http.StatusNoContent)
}

// notePriceBits пишет в лог две беды, которые иначе видно только по
// зависшей вкладке: ценника всё нет, или он есть, но не читается.
// Каждая — по одному разу на задачу.
func (e *extBridge) notePriceBits(j *extJob, msg extResult) {
	css := strings.TrimSpace(msg.Bits.CSSPrice)
	if css == "" {
		if j.notedWait.CompareAndSwap(false, true) {
			waitMsg := "жду ценник"
			if j.site == "ozon" {
				waitMsg = "жду ценник Ozon с банком"
			}
			e.log.Warn(waitMsg, "site", j.site, "url", j.url, "href", msg.Href, "title", diaglog.Clip(msg.Bits.Title, 120))
		}
		return
	}
	if _, ok := parseDisplayedPrice(css); !ok && j.notedBadCSS.CompareAndSwap(false, true) {
		e.log.Warn("ценник не разобрался", "site", j.site, "css", diaglog.Clip(css, 80), "url", j.url)
	}
}

func (e *extBridge) noteChallenge(bits pageBits) {
	title := strings.TrimSpace(bits.Title)
	msg := "Нужно пройти капчу в окне Chrome"
	if antibotWall(bits) {
		msg = "Нажмите «Обновить страницу» в окне Chrome"
		low := strings.ToLower(title)
		if strings.Contains(low, "подозрительная") || title == "..." {
			msg = "Подождите, пока Wildberries снимет защиту, или обновите страницу"
		}
	}
	if title != "" {
		msg += " · " + title
	}
	if fn := e.onChallenge.Load(); fn != nil {
		(*fn)(msg)
	}
	e.log.Warn("нужно действие в окне Chrome", "title", title)
}

func (e *extBridge) ensureToken() error {
	if e.token != "" {
		return nil
	}
	path, err := tokenPath()
	if err != nil {
		return err
	}
	tok, err := readOrCreateToken(path)
	if err != nil {
		return err
	}
	e.token = tok
	return nil
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

// sameShopURL — снимок пришёл с той же карточки, что и задача. Хост
// сравнивается точно, тем же списком, что и разбор ссылки от пользователя:
// подстрока пускала бы цену со страницы ozon.ru.example.com как цену Ozon.
func sameShopURL(job, href string) bool {
	ju, err1 := url.Parse(job)
	hu, err2 := url.Parse(href)
	if err1 != nil || err2 != nil {
		return false
	}
	want, ok := sites.ByHost(ju.Hostname())
	if !ok {
		return false
	}
	got, ok := sites.ByHost(hu.Hostname())
	return ok && want == got
}

func tokenPath() (string, error) {
	dir, err := extensionDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(dir), "ext.token"), nil
}

// readOrCreateToken держит токен рядом с расширением: он же прописывается
// в background.js, и после перезапуска бота вкладка не теряет доступ.
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
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
