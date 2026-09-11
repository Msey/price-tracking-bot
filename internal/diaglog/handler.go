// Package diaglog включает подробные логи по тумблеру в окне.
// Warn и Error пишутся всегда, Info и Debug — только когда тумблер включён.
package diaglog

import (
	"context"
	"log/slog"
	"sync/atomic"
)

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

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	return h.inner.Handle(ctx, r)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{s: h.s, inner: h.inner.WithAttrs(attrs)}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{s: h.s, inner: h.inner.WithGroup(name)}
}
