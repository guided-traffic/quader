package good

import "log/slog"

func fail(l *slog.Logger) {
	l.Error("request failed")
	_ = slog.New(slog.DiscardHandler)
}
