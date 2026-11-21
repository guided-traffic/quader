// Package poolerlog reads the log of a CloudNativePG PgBouncer for the e2e suites: how long one pooler instance had no server
// after a PostgreSQL switchover (R-HA-058, Q-HA-302, Q-HA-370). It has no build tag, so `go test ./test/e2e/poolerlog/` checks
// the reading without a cluster.
//
// A failure line is a server connection the switchover ended, a server attempt that failed (the connect, or the login), or a
// client refused because the pool's last attempt failed ("server login has been failing, cached error: …"). PgBouncer starts a
// server attempt only when a client asks for one, and not within server_login_retry of the last one; the client that starts the
// next attempt is itself refused with the pool's last error. A pool that no client asks after a failed attempt therefore makes no
// attempt at all, and the first client it gets, however much later, is refused with the old error. That refusal says nothing
// about the server. So the window ends at the pool's last failed server attempt plus server_login_retry, the earliest time the
// pool tries the server again, or at the last failure line when that comes earlier.
package poolerlog

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// failure matches the failure lines: the server connections the switchover ended, a server attempt that failed, and a client
// refused while the pool's last attempt counts as failed.
var failure = regexp.MustCompile(`closing because: server conn crashed\?|closing because: connect failed|server login failed|server login has been failing|closing because: password authentication failed`)

// Window is what one pooler instance logged around a switchover.
type Window struct {
	First      time.Time // the first failure line
	End        time.Time // the end of the window: the last failed server attempt plus server_login_retry, or the last failure line before that
	LastServer time.Time // the last failed server attempt (zero: the log shows none)
	Lines      int       // failure lines inside the window
	Refused    int       // clients refused inside the window
	// Late counts the clients refused after the window, from LateFirst to LateLast: each came when the pool was free to try
	// the server again, started that attempt and was refused with the pool's last error.
	Late                int
	LateFirst, LateLast time.Time
}

// Length is the time from the first failure line to the end of the window; 0 without a failure line.
func (w Window) Length() time.Duration {
	if w.First.IsZero() {
		return 0
	}
	return w.End.Sub(w.First)
}

// record is one line of the CloudNativePG pooler manager: PgBouncer's own log line under "record".
type record struct {
	Record struct {
		Timestamp string `json:"timestamp"`
		Msg       string `json:"msg"`
	} `json:"record"`
}

type line struct {
	at      time.Time
	server  bool // a server connection ended or a server attempt failed
	refused bool // a client refused with the pool's error
}

// Read reads the log of one pooler instance (one JSON record per line, as the pooler manager writes it, optionally behind a
// prefix) with the instance's server_login_retry.
func Read(log string, retry time.Duration) Window {
	var lines []line
	for _, l := range strings.Split(log, "\n") {
		if !failure.MatchString(l) {
			continue
		}
		var r record
		i := strings.IndexByte(l, '{')
		if i < 0 || json.Unmarshal([]byte(l[i:]), &r) != nil {
			continue
		}
		at, err := time.Parse("2006-01-02 15:04:05.000 MST", r.Record.Timestamp)
		if err != nil {
			continue
		}
		m := r.Record.Msg
		lines = append(lines, line{
			at:      at,
			server:  strings.HasPrefix(m, "S-") || strings.HasPrefix(m, "server login failed"),
			refused: strings.Contains(m, "pooler error"),
		})
	}
	var w Window
	for _, l := range lines {
		if w.First.IsZero() || l.at.Before(w.First) {
			w.First = l.at
		}
		if l.at.After(w.End) {
			w.End = l.at
		}
		if l.server && l.at.After(w.LastServer) {
			w.LastServer = l.at
		}
	}
	// Without a failed server attempt in the log a refusal cannot be put beside one: the window runs to the last line.
	if !w.LastServer.IsZero() && w.LastServer.Add(retry).Before(w.End) {
		w.End = w.LastServer.Add(retry)
	}
	for _, l := range lines {
		if l.at.After(w.End) {
			if l.refused {
				if w.Late == 0 {
					w.LateFirst = l.at
				}
				w.Late++
				w.LateLast = l.at
			}
			continue
		}
		w.Lines++
		if l.refused {
			w.Refused++
		}
	}
	return w
}

// Param reads one parameter of a rendered pgbouncer.ini; ok is false when the file does not set it.
func Param(ini, name string) (value string, ok bool) {
	m := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(name) + `\s*=\s*(\S+)`).FindStringSubmatch(ini)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// LoginRetry is the server_login_retry of a rendered pgbouncer.ini: PgBouncer's default of 15 s when it is not set or does not
// parse.
func LoginRetry(ini string) time.Duration {
	v, ok := Param(ini, "server_login_retry")
	if !ok {
		return 15 * time.Second
	}
	s, err := strconv.ParseFloat(v, 64)
	if err != nil || s < 0 {
		return 15 * time.Second
	}
	return time.Duration(s * float64(time.Second))
}
