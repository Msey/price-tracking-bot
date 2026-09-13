// Package diaglog включает подробные логи по тумблеру в окне.
// Warn и Error пишутся всегда, Info и Debug — только когда тумблер включён.
package diaglog

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"sync/atomic"
)

// tokenInText — токен бота Telegram. Он попадает в текст сетевых ошибок
// telebot, потому что лежит в пути запроса к API, а лог уходит в файл.
var tokenInText = regexp.MustCompile(`bot\d+:[A-Za-z0-9_-]+`)

// Redact убирает токен Telegram из строки. Годится и для того, что идёт
// не в лог: текста ошибки в базе, строки статуса в окне.
func Redact(s string) string {
	if !hasToken(s) {
		return s
	}
	return tokenInText.ReplaceAllString(s, "bot***")
}

// hasToken — быстрая отсечка: регулярку гоняем только по строкам, где
// вообще может быть токен.
func hasToken(s string) bool {
	return strings.Contains(s, "bot") && tokenInText.MatchString(s)
}

// Clip сжимает пробелы и обрезает строку до n символов, добавляя многоточие.
// Тексты ошибок магазинов и Telegram бывают на несколько экранов, а в логе
// и в строке статуса нужна одна строка.
func Clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if n <= 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// Switch — тумблер подробных логов. По умолчанию выключен.
type Switch struct {
	on atomic.Bool
}

// Enabled сообщает, пишутся ли Info/Debug.
func (s *Switch) Enabled() bool {
	if s == nil {
		return false
	}
	return s.on.Load()
}

// Set включает или выключает подробные логи. По умолчанию выключено.
func (s *Switch) Set(on bool) {
	if s == nil {
		return
	}
	s.on.Store(on)
}

type handler struct {
	s     *Switch
	inner slog.Handler
}

// Wrap прячет Info/Debug, пока тумблер выключен.
func Wrap(inner slog.Handler, s *Switch) slog.Handler {
	if s == nil {
		s = &Switch{}
	}
	return &handler{s: s, inner: inner}
}

func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	if !h.inner.Enabled(ctx, level) {
		return false
	}
	if level >= slog.LevelWarn {
		return true
	}
	return h.s.on.Load()
}

// Handle пропускает запись как есть, пока в ней нет токена: пересборка
// записи ради каждой строки лога обошлась бы лишними аллокациями.
func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	if !recordHasToken(r) {
		return h.inner.Handle(ctx, r)
	}
	clean := slog.NewRecord(r.Time, r.Level, Redact(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		clean.AddAttrs(redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, clean)
}

func recordHasToken(r slog.Record) bool {
	if hasToken(r.Message) {
		return true
	}
	found := false
	r.Attrs(func(a slog.Attr) bool {
		if s, ok := attrText(a); ok && hasToken(s) {
			found = true
			return false
		}
		return true
	})
	return found
}

func redactAttr(a slog.Attr) slog.Attr {
	if s, ok := attrText(a); ok && hasToken(s) {
		return slog.String(a.Key, Redact(s))
	}
	return a
}

// attrText — текст атрибута, если он вообще может содержать токен.
// Числа и время не проверяем, ошибку разворачиваем: именно так токен и
// приходит — атрибутом "error".
func attrText(a slog.Attr) (string, bool) {
	switch a.Value.Kind() {
	case slog.KindString:
		return a.Value.String(), true
	case slog.KindAny:
		if err, ok := a.Value.Any().(error); ok {
			return err.Error(), true
		}
	}
	return "", false
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{s: h.s, inner: h.inner.WithAttrs(attrs)}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{s: h.s, inner: h.inner.WithGroup(name)}
}
