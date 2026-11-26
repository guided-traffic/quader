package limits

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
)

// waitIn reads the wait a refusal's detail names ("Try again in 2 hours 1 minute.") back into a duration.
func waitIn(t *testing.T, detail string) time.Duration {
	t.Helper()
	m := regexp.MustCompile(`Try again in ([^.]+)\.`).FindStringSubmatch(detail)
	if m == nil {
		t.Fatalf("no wait in %q", detail)
	}
	var d time.Duration
	f := strings.Fields(m[1])
	if len(f)%2 != 0 {
		t.Fatalf("the wait %q is no list of amounts and units", m[1])
	}
	for i := 0; i < len(f); i += 2 {
		n, err := strconv.Atoi(f[i])
		if err != nil {
			t.Fatalf("the wait %q: %v", m[1], err)
		}
		unit := map[string]time.Duration{
			"second": time.Second, "seconds": time.Second, "minute": time.Minute, "minutes": time.Minute, "hour": time.Hour, "hours": time.Hour,
		}[f[i+1]]
		if unit == 0 || (n == 1) != !strings.HasSuffix(f[i+1], "s") {
			t.Fatalf("the wait %q: unit %q", m[1], f[i+1])
		}
		d += time.Duration(n) * unit
	}
	return d
}

// TestR_LIM_020_TheDetailAndRetryAfterNameOneWait: every refusal of the download limits that carries a wait names the same one in its
// detail, its Retry-After, its member retryAfterSeconds and its RateLimit policy (Q-LIM-291): whole seconds below two minutes, whole
// minutes from there, never less than the wait the counters need and at most a minute more, and for a byte or upstream figure at most
// its window. Before, an hour's refusal of 2 h 0 min 1 s said "3 hours" with a Retry-After of 7201, and one of 2 min 1 s "3 minutes"
// with 121. Unit, microseconds, no payload.
func TestR_LIM_020_TheDetailAndRetryAfterNameOneWait(t *testing.T) {
	waits := []time.Duration{
		0, 400 * time.Millisecond, time.Second, 1500 * time.Millisecond, 59*time.Second + 500*time.Millisecond, 119 * time.Second,
		119*time.Second + 200*time.Millisecond, 2 * time.Minute, 2*time.Minute + time.Second, 30 * time.Minute, 30*time.Minute + time.Millisecond,
		59*time.Minute + 59*time.Second, time.Hour, 113 * time.Minute, 2*time.Hour - time.Second, 2 * time.Hour, 2*time.Hour + time.Second,
		23*time.Hour + 59*time.Minute + 30*time.Second, 24 * time.Hour, 25 * time.Hour, 47 * time.Hour,
	}
	cases := []struct {
		code, figure string
		window       time.Duration
		capped       bool
	}{
		{errs.CodeRateLimited, RequestsPerHour.String(), hour, false},
		{errs.CodeRateLimited, RequestsPerSecond.String(), 10 * time.Second, false},
		{CodeDownloadLimitReached, CachedBytesPerHour.String(), hour, true},
		{CodeDownloadLimitReached, BytesPerDay.String(), day, true},
		{CodeUpstreamLimitReached, UpstreamRequestsPerHour.String(), hour, true},
		{CodeCallerBlocked, RefusedPerHour.String(), hour, false},
	}
	for _, c := range cases {
		for _, wait := range waits {
			ch := &Charge{}
			e := (&Engine{}).refuse(ch, refusal{code: c.code, figure: c.figure, limit: 100, window: c.window, retry: wait, signed: true, baseURL: "https://q.example"}, 100)
			retry := e.RetryAfter
			if got := waitIn(t, e.Detail); got != retry {
				t.Errorf("%s %s, wait %s: the detail %q names %s, Retry-After is %s", c.code, c.figure, wait, e.Detail, got, retry)
			}
			if s, _ := e.Extensions["retryAfterSeconds"].(int64); time.Duration(s)*time.Second != retry {
				t.Errorf("%s %s, wait %s: retryAfterSeconds %v, Retry-After %s", c.code, c.figure, wait, e.Extensions["retryAfterSeconds"], retry)
			}
			if f, _ := ParseFigure(c.figure); f != RequestsPerHour && f != RequestsPerSecond && (ch.refusal == nil || ch.refusal.Reset != retry) {
				t.Errorf("%s %s, wait %s: the refusal's RateLimit policy %+v, Retry-After %s", c.code, c.figure, wait, ch.refusal, retry)
			}
			need := max(time.Second, wait)
			if c.capped {
				need = min(need, c.window)
			}
			switch {
			case retry < need:
				t.Errorf("%s %s, wait %s: Retry-After %s is early", c.code, c.figure, wait, retry)
			case retry >= need+time.Minute:
				t.Errorf("%s %s, wait %s: Retry-After %s is a minute or more beyond the wait", c.code, c.figure, wait, retry)
			case c.capped && retry > c.window:
				t.Errorf("%s %s, wait %s: Retry-After %s beyond the window %s", c.code, c.figure, wait, retry, c.window)
			case retry < 2*time.Minute && retry%time.Second != 0, retry >= 2*time.Minute && retry%time.Minute != 0:
				t.Errorf("%s %s, wait %s: Retry-After %s is not in the detail's unit", c.code, c.figure, wait, retry)
			}
		}
	}
	t.Run("R-LIM-020 R-LIM-041 the parallel transfers' refusal names transfers, what one is, and one second", func(t *testing.T) {
		e := refusal{code: CodeTooManyParallelTransfers, figure: ParallelTransfers.String(), limit: 4, retry: time.Second}.err()
		want := "The limit of 4 parallel transfers is reached: each layer of an image and each package of an install is one transfer. " +
			"Try again in 1 second."
		if e.RetryAfter != time.Second || e.Detail != want {
			t.Errorf("%s %q", e.RetryAfter, e.Detail)
		}
	})
}
