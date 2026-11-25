package good

import (
	"log/slog"
	"net/url"

	"example.test/fix/internal/observability"
)

// Log writes the field in the form of the rule.
func Log(l *slog.Logger, s observability.Scrubber, u *url.URL) {
	l.Info("a", "url", observability.ScrubURL(u))
	l.Info("b", slog.String("url", s.URL(u)))
	l.Info("c", "other", u.String())
}
