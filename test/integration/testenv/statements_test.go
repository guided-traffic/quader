package testenv

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestR_ARCH_027_AStatementOfTheRequestsRunsAcrossThem (Q-MAL-92): the scheduler's tick offers every task's slot at once, one
// InsertJob per task; with 27 tasks a tick inside a window of 30 downloads reached the nine-in-ten count and was taken for a
// statement of the download. A statement counts as the requests' only when its runs spread across them, and one a request does
// run (an ask of a scan, say) still counts.
func TestR_ARCH_027_AStatementOfTheRequestsRunsAcrossThem(t *testing.T) {
	const n = 30
	base := time.Date(2026, 11, 21, 2, 13, 5, 0, time.UTC)
	var b strings.Builder
	line := func(at time.Time, name string) {
		fmt.Fprintf(&b, "%s UTC [284034] LOG:  execute stmtcache_0deffbbe: -- name: %s :many\n\tSELECT 1\n", at.Format("2006-01-02 15:04:05.000"), name)
	}
	for i := range n {
		line(base.Add(time.Duration(i)*10*time.Millisecond), "GetLiveItem")
	}
	for i := range 27 { // one tick of the scheduler, 27 tasks within 13 ms
		line(base.Add(150*time.Millisecond+time.Duration(i)*500*time.Microsecond), "InsertJob")
	}
	for i := range n { // the replica's own probe, a background name
		fmt.Fprintf(&b, "%s UTC [284035] LOG:  statement: -- ping\n", base.Add(time.Duration(i)*10*time.Millisecond+3*time.Millisecond).Format("2006-01-02 15:04:05.000"))
	}
	fmt.Fprintf(&b, "%s UTC [284034] LOG:  statement: BEGIN\n", base.Format("2006-01-02 15:04:05.000"))
	stmts := parseStatements(b.String())
	if len(stmts) != 3*n-3 || !stmts[1].At.Equal(base.Add(10*time.Millisecond)) {
		t.Fatalf("parsed %d statements, the second at %v", len(stmts), stmts[1].At)
	}
	loop := 300 * time.Millisecond
	if got := PerRequest(stmts, n, loop, map[string]bool{"-- ping": true}); !reflect.DeepEqual(got, map[string]int{"GetLiveItem": 1}) {
		t.Errorf("per request %v, want the item lookup alone: the tick's burst is no statement of the downloads", got)
	}

	// A statement every download runs counts, also an InsertJob.
	for i := range n {
		line(base.Add(time.Duration(i)*10*time.Millisecond+5*time.Millisecond), "InsertJob")
	}
	got := PerRequest(parseStatements(b.String()), n, loop, map[string]bool{"-- ping": true})
	if !reflect.DeepEqual(got, map[string]int{"GetLiveItem": 1, "InsertJob": 2}) {
		t.Errorf("per request %v, want GetLiveItem 1 and InsertJob 2 (the burst and the requests' own runs together)", got)
	}
}
