package limits

import (
	"fmt"
	"math"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
)

// The codes of the refusals of R-LIM-020.
const (
	CodeDownloadLimitReached     = "download_limit_reached"
	CodeUpstreamLimitReached     = "upstream_limit_reached"
	CodeTooManyParallelDownloads = "too_many_parallel_downloads"
	CodeCallerBlocked            = "caller_blocked"
)

// refusal is one refusal of R-LIM-020: a 429 with Retry-After and the extension members figure, limit, windowSeconds, place
// and retryAfterSeconds, in plain words that name the figure, the limit and the wait.
type refusal struct {
	code    string
	figure  string
	limit   int64
	window  time.Duration
	place   Place
	retry   time.Duration
	ceiling CeilingClass
	signed  bool
	baseURL string
}

func (r refusal) err() *errs.Error {
	if (r.code == CodeDownloadLimitReached || r.code == CodeUpstreamLimitReached) && r.window > 0 {
		r.retry = min(r.retry, r.window) // at most the window (R-LIM-020)
	}
	// One value for every place that names the wait: the detail, Retry-After, retryAfterSeconds and the RateLimit field's t
	// (Q-LIM-291).
	retry := waitOf(r.retry)
	opts := []errs.Option{
		errs.WithRetryAfter(retry),
		errs.WithExtension("figure", r.figure),
		errs.WithExtension("place", r.place.Kind.String()),
		errs.WithExtension("retryAfterSeconds", int64(retry/time.Second)),
	}
	if r.limit > 0 {
		opts = append(opts, errs.WithExtension("limit", r.limit))
	}
	if r.window > 0 {
		opts = append(opts, errs.WithExtension("windowSeconds", int64(r.window/time.Second)))
	}
	detail := r.detail(retry)
	if r.signed && r.baseURL != "" {
		detail += " Your limits: " + r.baseURL + "/ui/profile/usage"
	}
	return errs.E(errs.RateLimited, r.code, detail, opts...)
}

func (r refusal) detail(retry time.Duration) string {
	wait := "Try again in " + humanDuration(retry) + "."
	who := ""
	if r.ceiling != "" {
		who = " of all anonymous callers of this repository"
		if r.ceiling == CeilingSignedIn {
			who = " of all signed-in callers of this repository"
		}
	}
	switch r.code {
	case errs.CodeRateLimited:
		return fmt.Sprintf("The request limit of %s is used up. %s", amount(r.figure, r.limit, r.window), wait)
	case CodeDownloadLimitReached:
		return fmt.Sprintf("The download limit of %s is used up. %s", amount(r.figure, r.limit, r.window), wait)
	case CodeUpstreamLimitReached:
		return fmt.Sprintf("The upstream limit%s of %s is used up; what is cached is still served. %s", who, amount(r.figure, r.limit, r.window), wait)
	case CodeTooManyParallelDownloads:
		return fmt.Sprintf("The limit of %d parallel downloads is reached. %s", r.limit, wait)
	case CodeCallerBlocked:
		return "Too many refused requests: requests of this caller are refused for a while. " + wait
	}
	return wait
}

// amount names a limit in words: "300 GB per hour", "2,000 requests per hour".
func amount(figure string, limit int64, w time.Duration) string {
	per := "per hour"
	switch {
	case w >= 24*time.Hour:
		per = "per day"
	case w < time.Hour:
		per = fmt.Sprintf("per %d seconds", int64(w/time.Second))
	}
	f, _ := ParseFigure(figure)
	switch f {
	case CachedBytesPerHour, UncachedBytesPerHour, BytesPerDay:
		return bytesText(limit) + " " + per
	}
	return fmt.Sprintf("%d requests %s", limit, per)
}

// bytesText is a byte count in decimal units as the UI shows them (R-LIM-004): MB and GB.
func bytesText(n int64) string {
	switch {
	case n >= gb:
		return trimFloat(float64(n)/gb) + " GB"
	case n >= mb:
		return trimFloat(float64(n)/mb) + " MB"
	}
	return fmt.Sprintf("%d bytes", n)
}

func trimFloat(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}

// waitOf is the wait a refusal names: at least a second, rounded up to the unit its text names it in, whole seconds below two
// minutes and whole minutes from there (R-LIM-020, Q-LIM-291). Rounding up keeps the wait honest: a caller that waits it is not
// early, and it waits at most 59 s more than the window needs. A wait at most a window of whole minutes stays at most that window.
func waitOf(d time.Duration) time.Duration {
	d = max(time.Second, d)
	s := (d + time.Second - 1) / time.Second * time.Second
	if s < 2*time.Minute {
		return s
	}
	return (s + time.Minute - 1) / time.Minute * time.Minute
}

// humanDuration names a wait of waitOf exactly, in the unit waitOf rounded it to: "45 seconds", "14 minutes", "113 minutes", and
// from two hours "3 hours" or "2 hours 1 minute".
func humanDuration(d time.Duration) string {
	s := int64(d / time.Second)
	if s < 120 {
		return plural(s, "second")
	}
	m := s / 60
	if m < 120 {
		return plural(m, "minute")
	}
	if m%60 == 0 {
		return plural(m/60, "hour")
	}
	return plural(m/60, "hour") + " " + plural(m%60, "minute")
}

func plural(n int64, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
