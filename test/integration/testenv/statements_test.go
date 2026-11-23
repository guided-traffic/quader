package testenv

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// statementLog writes lines of the stack's log: a statement by its sqlc name at an instant.
type statementLog struct{ strings.Builder }

func (b *statementLog) line(at time.Time, name string) {
	fmt.Fprintf(b, "%s UTC [284034] LOG:  execute stmtcache_0deffbbe: -- name: %s :many\n\tSELECT 1\n", at.Format("2006-01-02 15:04:05.000"), name)
}

// TestR_ARCH_027_AStatementOfTheRequestsRunsAcrossThem (Q-MAL-92): the scheduler's tick offers every task's slot at once, one
// InsertJob per task; with 27 tasks a tick inside a window of 30 downloads reached the nine-in-ten count and was taken for a
// statement of the download. A statement counts as the requests' only when its runs spread across them, and one a request does
// run (an ask of a scan, say) still counts.
func TestR_ARCH_027_AStatementOfTheRequestsRunsAcrossThem(t *testing.T) {
	const n = 30
	base := time.Date(2026, 11, 21, 2, 13, 5, 0, time.UTC)
	var b statementLog
	starts := make([]time.Time, n)
	for i := range n {
		starts[i] = base.Add(time.Duration(i) * 10 * time.Millisecond)
		b.line(starts[i], "GetLiveItem")
	}
	for i := range 27 { // one tick of the scheduler, 27 tasks within 13 ms
		b.line(base.Add(150*time.Millisecond+time.Duration(i)*500*time.Microsecond), "InsertJob")
	}
	for i := range n { // the replica's own probe, a background name
		fmt.Fprintf(&b, "%s UTC [284035] LOG:  statement: -- ping\n", base.Add(time.Duration(i)*10*time.Millisecond+3*time.Millisecond).Format("2006-01-02 15:04:05.000"))
	}
	fmt.Fprintf(&b, "%s UTC [284034] LOG:  statement: BEGIN\n", base.Format("2006-01-02 15:04:05.000"))
	stmts := parseStatements(b.String())
	if len(stmts) != 3*n-3 || !stmts[1].At.Equal(base.Add(10*time.Millisecond)) {
		t.Fatalf("parsed %d statements, the second at %v", len(stmts), stmts[1].At)
	}
	if got := PerRequest(stmts, starts, map[string]bool{"-- ping": true}); !reflect.DeepEqual(got, map[string]int{"GetLiveItem": 1}) {
		t.Errorf("per request %v, want the item lookup alone: the tick's burst is no statement of the downloads", got)
	}

	// A statement every download runs counts, also an InsertJob.
	for i := range n {
		b.line(starts[i].Add(5*time.Millisecond), "InsertJob")
	}
	got := PerRequest(parseStatements(b.String()), starts, map[string]bool{"-- ping": true})
	if !reflect.DeepEqual(got, map[string]int{"GetLiveItem": 1, "InsertJob": 2}) {
		t.Errorf("per request %v, want GetLiveItem 1 and InsertJob 2 (the burst and the requests' own runs together)", got)
	}
}

// TestR_ARCH_027_OneSlowRequestDoesNotHideTheRequestsStatement: a window of thirty requests of 10 ms in which one request takes 2 s
// (a stalled read from the object store under load) still counts the statement each request runs twice (the miss at the local
// member and the hit at the remote one), wherever the slow request falls and wherever in it the statement runs, and a tick's burst
// in the same window still does not count. Measured against the time the requests took in all, the case of the slow last request
// counted nothing (seen twice under load in the query budget's integration area).
func TestR_ARCH_027_OneSlowRequestDoesNotHideTheRequestsStatement(t *testing.T) {
	const n, fast, slow = 30, 10 * time.Millisecond, 2 * time.Second
	base := time.Date(2026, 11, 22, 21, 40, 0, 0, time.UTC)
	early := func(time.Duration) [2]time.Duration { return [2]time.Duration{time.Millisecond, 2 * time.Millisecond} }
	late := func(d time.Duration) [2]time.Duration {
		return [2]time.Duration{d - 3*time.Millisecond, d - 2*time.Millisecond}
	}
	split := func(d time.Duration) [2]time.Duration {
		return [2]time.Duration{time.Millisecond, d - 2*time.Millisecond}
	}
	for _, c := range []struct {
		name string
		slow int
		// at is where in a request of the duration d its two statements run, after its start.
		at func(d time.Duration) [2]time.Duration
	}{
		{"the last request slow, its statements before the stall", n - 1, early},
		{"the last request slow, its statements after the stall", n - 1, late},
		{"the first request slow, its statements after the stall", 0, late},
		{"a request in the middle slow, one statement on each side of the stall", n / 2, split},
	} {
		t.Run(c.name, func(t *testing.T) {
			var b statementLog
			starts := make([]time.Time, n)
			at := base
			for i := range n {
				d := fast
				if i == c.slow {
					d = slow
				}
				starts[i] = at
				for _, o := range c.at(d) {
					b.line(at.Add(o), "GetLiveItem")
				}
				at = at.Add(d)
			}
			for i := range 27 { // a tick of the scheduler inside the window
				b.line(base.Add(100*time.Millisecond+time.Duration(i)*500*time.Microsecond), "InsertJob")
			}
			if got := PerRequest(parseStatements(b.String()), starts, nil); !reflect.DeepEqual(got, map[string]int{"GetLiveItem": 2}) {
				t.Errorf("per request %v, want GetLiveItem 2 and no InsertJob", got)
			}
		})
	}
}
