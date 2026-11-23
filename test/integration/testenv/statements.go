package testenv

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Statement is a statement of the log and the instant the database logged it.
type Statement struct {
	At   time.Time
	Text string
}

// The statements a replica sends, counted from the PostgreSQL log: a test turns log_statement on for its own database (every
// test has a database of its own, so no other test's statement is in the window) before its replicas connect, and reads the
// container's log between two instants. A statement is counted by its sqlc name.

var statementLine = regexp.MustCompile(`(?:statement|execute [^:]*): (.*)$`)

// logInstant is the instant at the start of a line of the stack's log (log_line_prefix %m).
var logInstant = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3}) UTC`)

// LogStatements turns the statement log on for the database of the stack; replicas started after it log their statements.
// The test is skipped when docker is not available.
func (s *Stack) LogStatements(t testing.TB) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available: the statements are counted from the PostgreSQL log")
	}
	s.Exec("ALTER DATABASE " + s.Database() + " SET log_statement = 'all'")
}

// Only databases that set log_statement are logged, so the log holds the statements of the tests that turned it on for their own
// database. The line prefix of the stack (%m [%p]) names no database, so a test that runs beside another one that logs too cannot
// tell their statements apart; filtering by database needs %d in log_line_prefix, a change of test/integration/compose.yaml and a
// restart of the stack, which is not made here. The counting tests therefore run on a stack no other logging test shares.
//
// StatementLogSince returns the statements the database logged since an instant, with the instant of each, without the transaction
// control. A count of the statements of a request reads them through PerRequest, never by a bare threshold. The log is that of the
// PostgreSQL container of the stack the environment names (QUADER_TEST_PG_CONTAINER), so a run on another instance of the stack
// reads its own instance's log.
func StatementLogSince(t testing.TB, at time.Time) []Statement {
	t.Helper()
	container := Get(t, "QUADER_TEST_PG_CONTAINER")
	out, err := exec.CommandContext(t.Context(), "docker", "logs", "--since", at.UTC().Format(time.RFC3339Nano), container).CombinedOutput() //nolint:gosec // the stack's container name from its env file and a formatted instant
	if err != nil {
		t.Fatalf("docker logs: %v\n%s", err, out)
	}
	return parseStatements(string(out))
}

func parseStatements(log string) []Statement {
	var stmts []Statement
	for _, line := range strings.Split(log, "\n") {
		m := statementLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		s := strings.TrimSpace(m[1])
		switch strings.ToUpper(strings.Fields(s + " x")[0]) {
		case "BEGIN", "COMMIT", "ROLLBACK", "SET", "RESET", "SHOW", "DISCARD", "DEALLOCATE":
			continue
		}
		var at time.Time
		if ts := logInstant.FindStringSubmatch(line); ts != nil {
			at, _ = time.Parse("2006-01-02 15:04:05.000", ts[1]) //nolint:errcheck // a line the pattern matched parses; the zero time is no instant
		}
		stmts = append(stmts, Statement{At: at, Text: s})
	}
	return stmts
}

// PerRequest returns the statements of a window of requests by name, per request (R-ARCH-027, Q-MAL-92); starts holds the instant
// each request began, in order, one per request. A statement is the requests' when it ran for at least nine requests in ten and
// its runs spread over at least half of the requests' span (requestSpan): a statement of the request runs once in each of them,
// while a background burst of the same count (the scheduler's tick, which offers every task's slot at once, one InsertJob per
// task) runs within milliseconds. The log is read from a container that other writers share, so a line can be lost where two
// writers meet: nine in ten suffice, and the number per request is the nearest whole one. Names in background are the replica's
// own (probes, registration) and never count. A steady rhythm of a background loop that runs through the whole window (a
// scheduled task's first run, which a replica of the role all starts at its first tick) passes the rule like a statement of the
// requests; no rule on the instants of the log tells the two apart, so a test waits for such a run to end before its window.
func PerRequest(stmts []Statement, starts []time.Time, background map[string]bool) map[string]int {
	type runs struct {
		count       int
		first, last time.Time
	}
	n := len(starts)
	by := map[string]*runs{}
	for _, s := range stmts {
		name := StatementName(s.Text)
		r := by[name]
		if r == nil {
			r = &runs{first: s.At, last: s.At}
			by[name] = r
		}
		r.count++
		if s.At.Before(r.first) {
			r.first = s.At
		}
		if s.At.After(r.last) {
			r.last = s.At
		}
	}
	span := requestSpan(starts)
	out := map[string]int{}
	for name, r := range by {
		if n == 0 || background[name] || r.count*10 < n*9 || r.last.Sub(r.first) < span/2 {
			continue
		}
		out[name] = (r.count + n/2) / n
	}
	return out
}

// requestSpan is what the spread of a statement's runs is measured against: the time from the second request's start to the last
// one's. A statement that runs in the first and in the last request spreads at least over that, however slow any request was and
// wherever in a request it runs: its run in the first request ended before the second began, and its run in the last began at the
// last start. The time the requests took in all would not do: one slow request at the end made it long and the spread short, and a
// statement that ran sixty times in thirty requests was not counted (seen twice under load, on 2026-11-22 and 2026-11-23). A burst
// of the scheduler counts only when it lasts half of the span of the requests between, which are many milliseconds where its runs
// fall within a few.
func requestSpan(starts []time.Time) time.Duration {
	if len(starts) < 2 {
		return 0
	}
	return starts[len(starts)-1].Sub(starts[1])
}

// StatementName is the sqlc name of a statement ("-- name: GetLiveItem :many"), or its first words.
func StatementName(s string) string {
	if i := strings.Index(s, "-- name: "); i >= 0 {
		// The first word after the marker, without splitting the rest of the statement (RequestTransactions names every
		// statement of a request with it while the request runs).
		rest := strings.TrimLeft(s[i+len("-- name: "):], " \t\r\n")
		if j := strings.IndexAny(rest, " \t\r\n"); j >= 0 {
			rest = rest[:j]
		}
		if rest != "" {
			return rest
		}
	}
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}
