//go:build e2e

package e2e

// R-HA-058's switchover behind the poolers (Q-HA-302) under the npm load, every time: failover.sh promotes the standby while the
// eight npm ci loops, the publish loop and k6 run through both poolers. The failure table of R-HA-007 promises a stall for a
// switchover, not an outage: every replica stays in its Service and the edge never answers for itself. The set is ha-pooler.
// Before this test the only switchover behind the poolers was the pick of drain variant D, which moves the PostgreSQL primary only
// when it sits on the picked node, so the requirement was tested by pod placement.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// poolerLogFailure matches the lines of a CloudNativePG PgBouncer that mean "no server for the clients": the server connections
// that the switchover ended, a server login that failed, and a client refused while PgBouncer waits out server_login_retry.
var poolerLogFailure = regexp.MustCompile(`closing because: server conn crashed\?|closing because: connect failed|server login failed|server login has been failing|closing because: password authentication failed`)

// poolerFailureWindow is what one pooler instance logged in a time range: the first and the last failure line, and how many
// clients it refused with "server login has been failing".
type poolerFailureWindow struct {
	pod         string
	first, last time.Time
	lines       int
	refused     int
}

func (w poolerFailureWindow) length() time.Duration {
	if w.first.IsZero() {
		return 0
	}
	return w.last.Sub(w.first)
}

// poolerRecord is one line of the CloudNativePG pooler manager: PgBouncer's own log line under "record".
type poolerRecord struct {
	Record struct {
		Timestamp string `json:"timestamp"`
		Msg       string `json:"msg"`
	} `json:"record"`
}

// poolerFailures reads the log of every instance of both poolers since from and returns, per instance, the window of its failure
// lines. The log copy is written to the artifact directory as well.
func poolerFailures(t *testing.T, from time.Time) []poolerFailureWindow {
	t.Helper()
	var out []poolerFailureWindow
	for _, name := range []string{poolerRW, poolerControl} {
		for _, p := range livePods(t, poolerSel(name)) {
			logs, err := kubectl("-n", cfg.namespace, "logs", p.Name, "-c", "pgbouncer", "--since-time="+from.UTC().Format(time.RFC3339))
			if err != nil {
				t.Errorf("logs of %s: %v", p.Name, err)
				continue
			}
			_ = os.WriteFile(filepath.Join(cfg.out, "pooler-switchover-"+p.Name+".log"), []byte(logs), 0o644)
			w := poolerFailureWindow{pod: p.Name}
			for _, l := range strings.Split(logs, "\n") {
				if !poolerLogFailure.MatchString(l) {
					continue
				}
				var r poolerRecord
				if i := strings.IndexByte(l, '{'); i < 0 || json.Unmarshal([]byte(l[i:]), &r) != nil {
					continue
				}
				at, err := time.Parse("2006-01-02 15:04:05.000 MST", r.Record.Timestamp)
				if err != nil {
					continue
				}
				w.lines++
				if strings.Contains(r.Record.Msg, "server login has been failing") && strings.Contains(r.Record.Msg, "pooler error") {
					w.refused++
				}
				if w.first.IsZero() || at.Before(w.first) {
					w.first = at
				}
				if at.After(w.last) {
					w.last = at
				}
			}
			out = append(out, w)
		}
	}
	return out
}

func TestHAPooler_Switchover(t *testing.T) {
	h := npmHAFx(t)
	requirePoolerProfile(t)
	var info struct {
		RetryMaxElapsedMS int64 `json:"retry_max_elapsed_ms"`
		OutageGraceMS     int64 `json:"database_outage_grace_ms"`
	}
	if err := jsonUnmarshal(driver(t, "info"), &info); err != nil {
		t.Fatal(err)
	}
	grace := time.Duration(info.OutageGraceMS) * time.Millisecond
	if grace <= 0 {
		t.Fatalf("health.database_outage_grace is %s: the profile must run with the grace the spec gives a switchover", grace)
	}

	t.Run("R-HA-058 R-HA-007 R-HA-052 Q-HA-302 E5.10 a PostgreSQL switchover behind both poolers under the npm load: every npm command exits 0, no quader container restarts, k6 only 2xx and 304 (no 503 of the edge), /readyz 200 on every replica throughout, every pooler serves again within health.database_outage_grace", func(t *testing.T) {
		before := pgPrimary(t)
		r := runNPMFault(t, h, "pooler-switchover", haFullLoad(), time.Minute, time.Minute, func() { script(t, "failover.sh") })
		after := pgPrimary(t)
		if after == before {
			t.Fatalf("failover.sh returned with %s still the primary: no switchover happened", before)
		}
		t.Logf("switchover %s -> %s, failover.sh took %s", before, after, r.faultEnd.Sub(r.faultAt).Round(100*time.Millisecond))

		// what the poolers saw: the length of their failure window is the time without a database for Quader's connections
		windows := poolerFailures(t, r.faultAt.Add(-5*time.Second))
		var longest time.Duration
		saw := 0
		for _, w := range windows {
			if w.lines == 0 {
				t.Logf("pooler %s: no failure line after the switchover", w.pod)
				continue
			}
			saw++
			longest = max(longest, w.length())
			t.Logf("pooler %s: failure lines from %s to %s (%s after the switchover began), %s long, %d lines, %d clients refused (server_login_retry)",
				w.pod, w.first.Format("15:04:05.000"), w.last.Format("15:04:05.000"), w.first.Sub(r.faultAt).Round(100*time.Millisecond),
				w.length().Round(100*time.Millisecond), w.lines, w.refused)
		}
		if saw == 0 {
			// every pooler instance holds server connections to the primary under the load, and the switchover ends them
			t.Errorf("no pooler instance logged the switchover (server connections crashed, logins failing): the logs were not read, or no switchover reached the poolers")
		}
		// a replica turns unready only once its database check has failed for longer than health.database_outage_grace (Q-OPS-31);
		// a pooler that serves again within the grace keeps every replica ready (R-HA-058, Q-HA-302)
		if longest >= grace {
			t.Errorf("R-HA-058 Q-HA-302: a pooler had no server for %s, not less than health.database_outage_grace (%s): server_login_retry is not short enough for a switchover", longest.Round(100*time.Millisecond), grace)
		}

		r.steady(t)
		r.res.allExitZero(t)
		r.res.loopsSpan(t, r.faultAt, r.faultEnd)
		edge503 := 0
		for _, f := range r.res.K6.Failures {
			if f.Status == 503 && (f.Replica == "" || f.Replica == "-") {
				edge503++
			}
		}
		if edge503 != 0 {
			t.Errorf("R-HA-058: %d k6 requests got the edge's own 503 (no X-Quader-Replica): the switchover left the edge without a ready replica", edge503)
		}
		r.res.K6.only2xx304(t)
		if len(r.mon.ready) != 0 {
			pods := make([]string, 0, len(r.mon.ready))
			for p := range r.mon.ready {
				pods = append(pods, p)
			}
			slices.Sort(pods)
			t.Errorf("R-HA-058 R-HA-007: /readyz answered non-200 during the switchover on %d replicas %v: %s", len(pods), pods, fmt.Sprint(r.mon.ready))
		}
		r.res.installPublished(t, h)
		quaderSettled(t, 3, 3, 4)
	})
}
