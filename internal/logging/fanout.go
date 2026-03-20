package logging

import (
	"context"
	"log/slog"
)

type FanoutHandler struct {
	handlers []slog.Handler
}

func NewFanoutHandler(handlers ...slog.Handler) slog.Handler {
	filtered := make([]slog.Handler, 0, len(handlers))
	for _, handler := range handlers {
		if handler != nil {
			filtered = append(filtered, handler)
		}
	}
	return &FanoutHandler{handlers: filtered}
}

func (h *FanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *FanoutHandler) Handle(ctx context.Context, rec slog.Record) error {
	var firstErr error
	for _, handler := range h.handlers {
		if !handler.Enabled(ctx, rec.Level) {
			continue
		}
		if err := handler.Handle(ctx, rec.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (h *FanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		handlers = append(handlers, handler.WithAttrs(attrs))
	}
	return &FanoutHandler{handlers: handlers}
}

func (h *FanoutHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		handlers = append(handlers, handler.WithGroup(name))
	}
	return &FanoutHandler{handlers: handlers}
}
