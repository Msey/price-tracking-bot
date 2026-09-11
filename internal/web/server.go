// Package web отдаёт локальную страницу со всеми заявками на трекинг.
package web

import (
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Msey/price-tracking-bot/internal/money"
	"github.com/Msey/price-tracking-bot/internal/sites"
	"github.com/Msey/price-tracking-bot/internal/storage"
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
	return mux
}

// Start поднимает HTTP и гасит его вместе с контекстом бота.
// Ошибка привязки к порту не должна ронять Telegram: её только логируем.
func (s *Server) Start(ctx context.Context) {
	srv := &http.Server{Addr: s.addr, Handler: s.Handler()}
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
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	reqs, err := s.store.ListAllRequests(ctx)
	if err != nil {
		s.log.Error("список заявок", "error", err)
		http.Error(w, "не удалось прочитать заявки", http.StatusInternalServerError)
		return
	}

	data := buildPage(reqs)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTmpl.Execute(w, data); err != nil {
		s.log.Error("шаблон заявок", "error", err)
	}
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
		TotalLabel:    ruPlural(len(reqs), "заявка", "заявки", "заявок"),
		Products:      len(products),
		ProductsLabel: ruPlural(len(products), "товар", "товара", "товаров"),
		Users:         len(users),
		UsersLabel:    ruPlural(len(users), "пользователь", "пользователя", "пользователей"),
		Rows:          rows,
	}
}

func ruPlural(n int, one, few, many string) string {
	if n < 0 {
		n = -n
	}
	mod100 := n % 100
	mod10 := n % 10
	if mod100 >= 11 && mod100 <= 14 {
		return many
	}
	switch mod10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	default:
		return many
	}
}

func viewOf(req storage.Request) rowView {
	status, class := statusOf(req)
	price := "—"
	if req.LastPriceKopecks.Valid {
		price = money.FormatKopecks(req.LastPriceKopecks.Int64)
	}
	checked := "ещё не было"
	if req.LastCheckedAt.Valid && req.LastCheckedAt.String != "" {
		checked = formatWhen(req.LastCheckedAt.String)
	}
	return rowView{
		Title:       req.Product.Title(),
		URL:         req.Product.URL,
		ExternalKey: req.Product.ExternalKey,
		Site:        sites.Site(req.Product.Site).Title(),
		SiteIcon:    siteIconDataURI(req.Product.Site),
		City:        cityTitle(req.Product.City),
		Price:       price,
		Status:      status,
		StatusClass: class,
		Checked:     checked,
		ChatID:      req.ChatID,
		Created:     formatWhen(req.CreatedAt),
	}
}

func statusOf(req storage.Request) (string, string) {
	if !req.LastCheckedAt.Valid {
		if req.LastErrorKind.Valid && req.LastErrorKind.String != "" {
			return "ошибка загрузки", "bad"
		}
		return "ожидает проверку", "wait"
	}
	if req.LastErrorAt.Valid && req.LastErrorAt.String > req.LastCheckedAt.String {
		return "ошибка после проверки", "bad"
	}
	if req.LastAvailable.Valid && req.LastAvailable.Int64 == 0 {
		return "нет в наличии", "bad"
	}
	return "отслеживается", "ok"
}

func siteIconDataURI(site string) template.URL {
	raw := sites.IconPNG(sites.Site(site))
	if len(raw) == 0 {
		return ""
	}
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(raw))
}

func cityTitle(city string) string {
	switch strings.ToLower(strings.TrimSpace(city)) {
	case "moscow":
		return "Москва"
	default:
		return city
	}
}

func formatWhen(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "—"
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", raw, time.UTC)
	if err != nil {
		return raw
	}
	return t.Local().Format("02.01.2006 15:04")
}
