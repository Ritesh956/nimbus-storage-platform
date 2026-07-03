// Package logging provides a structured (JSON) logger and request-ID
// propagation via context, per docs/03-hld.md §2 ("Correlation").
package logging

import (
	"context"
	"log/slog"
	"os"
)

type ctxKey int

const requestIDKey ctxKey = iota

// New builds the process-wide base logger. "dev" gets human-readable text
// output; anything else gets JSON, since that's what a log aggregator (or a
// human grepping container logs) actually wants.
func New(env string) *slog.Logger {
	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if env == "dev" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

// WithRequestID returns a context carrying id, and a logger pre-populated
// with a "request_id" field, for handlers to derive scoped loggers from.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// FromContext returns base annotated with the request ID in ctx, if any.
// Handlers should log through the returned logger, not the base one, so
// every line for a request carries its correlation ID.
func FromContext(ctx context.Context, base *slog.Logger) *slog.Logger {
	if id := RequestIDFromContext(ctx); id != "" {
		return base.With("request_id", id)
	}
	return base
}
