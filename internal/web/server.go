// Package web отдаёт локальную страницу со всеми заявками на трекинг.
package web

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/Msey/price-tracking-bot/internal/money"
	"github.com/Msey/price-tracking-bot/internal/sites"
	"github.com/Msey/price-tracking-bot/internal/storage"
	"github.com/Msey/price-tracking-bot/internal/view"
)

//go:embed templates/index.html
var indexHTML string

var pageTmpl = template.Must(template.New("index").Parse(indexHTML))

type Server struct {
	store *storage.Store
	addr  string
	log   *slog.Logger
}

func New(store *storage.Store, addr string, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{store: store, addr: addr, log: log}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

func loopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("web: адрес %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("web: адрес %q должен быть только на localhost", addr)
	}
	return nil
}

// Start поднимает HTTP и гасит его вместе с контекстом бота.
// Ошибка привязки к порту не должна ронять Telegram: её только логируем.
func (s *Server) Start(ctx context.Context) {
	if err := loopbackAddr(s.addr); err != nil {
		s.log.Error("веб-интерфейс не запущен", "error", err)
		return
	}
	srv := &http.Server{
		Addr:              s.addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	go func() {
		s.log.Info("веб-интерфейс заявок", "addr", "http://"+s.addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("веб-интерфейс остановился", "error", err)
		}
	}()
}

type pageData struct {
	Total         int
	TotalLabel    string
	Products      int
	ProductsLabel string
	Users         int
	UsersLabel    string
	Rows          []rowView
}

type rowView struct {
	Title       string
	URL         string
	ExternalKey string
	Site        string
	SiteIcon    template.URL
	City        string
	Price       string
	Status      string
	StatusClass string
	Checked     string
	ChatID      int64
	Created     string
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}
	s.log.Info("открыта страница заявок", "remote", r.RemoteAddr)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	reqs, err := s.store.ListAllRequests(ctx)
	if err != nil {
		s.log.Error("список заявок", "error", err)
		http.Error(w, "не удалось прочитать заявки", http.StatusInternalServerError)
		return
	}

	// Шаблон собирается в буфер: иначе сбой на середине отдал бы обрезанную
	// страницу под кодом 200, а поправить заголовки было бы уже поздно.
	var buf bytes.Buffer
	if err := pageTmpl.Execute(&buf, buildPage(reqs)); err != nil {
		s.log.Error("шаблон заявок", "error", err)
		http.Error(w, "не удалось собрать страницу", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

func buildPage(reqs []storage.Request) pageData {
	products := map[int64]struct{}{}
	users := map[int64]struct{}{}
	rows := make([]rowView, 0, len(reqs))
	for _, req := range reqs {
		products[req.Product.ID] = struct{}{}
		users[req.ChatID] = struct{}{}
		rows = append(rows, viewOf(req))
	}
	return pageData{
		Total:         len(reqs),
		TotalLabel:    view.RuPlural(len(reqs), "заявка", "заявки", "заявок"),
		Products:      len(products),
		ProductsLabel: view.RuPlural(len(products), "товар", "товара", "товаров"),
		Users:         len(users),
		UsersLabel:    view.RuPlural(len(users), "пользователь", "пользователя", "пользователей"),
		Rows:          rows,
	}
}

func viewOf(req storage.Request) rowView {
	status, class := view.Status(req)
	price := "—"
	if req.LastPriceKopecks.Valid {
		price = money.FormatKopecks(req.LastPriceKopecks.Int64)
	}
	checked := "ещё не было"
	if req.LastCheckedAt.Valid && req.LastCheckedAt.String != "" {
		checked = view.FormatWhen(req.LastCheckedAt.String)
	}
	return rowView{
		Title:       req.Product.Title(),
		URL:         req.Product.URL,
		ExternalKey: req.Product.ExternalKey,
		Site:        sites.Site(req.Product.Site).Title(),
		SiteIcon:    siteIconDataURI(req.Product.Site),
		City:        view.CityTitle(req.Product.City),
		Price:       price,
		Status:      status,
		StatusClass: class,
		Checked:     checked,
		ChatID:      req.ChatID,
		Created:     view.FormatWhen(req.CreatedAt),
	}
}

func siteIconDataURI(site string) template.URL {
	raw := sites.IconPNG(sites.Site(site))
	if len(raw) == 0 {
		return ""
	}
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(raw))
}
