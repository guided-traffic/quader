package observability

import (
	"context"
	"log/slog"
)

// loggerKey is the context key of the logger of the request or the job that runs.
type loggerKey struct{}

// ContextWithLogger returns ctx carrying l, the logger of the replica's component that serves the request or runs the job: the request
// chain puts it there (httpserver), and so does the job worker. Code that has no logger handed to it, a format's error writer among it,
// takes it with [LoggerFrom], so its lines carry the fields an operator filters by (the replica, the role, the log, the component).
func ContextWithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}

// LoggerFrom returns the logger ctx carries, or one that discards outside a request or a job: never the process's default logger, whose
// lines carry none of the fields of a replica.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.New(slog.DiscardHandler)
}
