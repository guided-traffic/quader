package good

import (
	"io"
	"log"
	"log/slog"
)

func fail(l *slog.Logger) {
	l.Error("request failed")
	_ = slog.New(slog.DiscardHandler)
	// log.New builds a logger of its own (an http.Server's ErrorLog) and is not the default one.
	_ = log.New(io.Discard, "", 0)
}
