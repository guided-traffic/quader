package bad

import (
	"log/slog"
	"net/url"

	"example.test/fix/internal/observability"
)

// Log writes the field by hand.
func Log(l *slog.Logger, u *url.URL) {
	l.Warn("a", "url", u.String())
	l.Info("b", slog.String("url", observability.Other(u)))
	l.With("url", u).Info("c")
}
