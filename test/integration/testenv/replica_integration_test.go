//go:build integration

package testenv

import (
	"strings"
	"testing"
)

// A replica of the harness drains for no time and serves the committed placeholder page, unless the test says otherwise
// (Q-ARCH-582).
func TestReplicaOfTheHarness(t *testing.T) {
	// drained stops r and returns the delay its log names for the drain, "" when it did not drain.
	drained := func(t *testing.T, r *Replica) string {
		t.Helper()
		if err := r.Stop(); err != nil {
			t.Fatalf("stop %s: %v", r.Name, err)
		}
		for _, line := range strings.Split(r.Logs.String(), "\n") {
			if strings.Contains(line, `"msg":"draining"`) {
				return line
			}
		}
		return ""
	}
	t.Run("a web replica drains for no time unless the test sets a delay, and the test's delay wins", func(t *testing.T) {
		s := NewStack(t, StackOptions{Name: "drain"})
		if line := drained(t, s.Start(t, ReplicaOptions{Name: "web-default"})); line != "" {
			t.Errorf("the harness's replica drained: %s", line)
		}
		if line := drained(t, s.Start(t, ReplicaOptions{Name: "web-own", Env: []string{"QUADER_HEALTH_SHUTDOWN_DRAIN_DELAY=250ms"}})); !strings.Contains(line, "250ms") {
			t.Errorf("a replica whose Env sets 250ms: drain line %q", line)
		}
		st := NewStack(t, StackOptions{Name: "drainst", Env: []string{"QUADER_HEALTH_SHUTDOWN_DRAIN_DELAY=300ms"}})
		if line := drained(t, st.Start(t, ReplicaOptions{Name: "web-stack"})); !strings.Contains(line, "300ms") {
			t.Errorf("a stack whose Env sets 300ms: drain line %q", line)
		}
	})
	t.Run("a replica whose options name no UI serves the committed placeholder page and publishes no UI file", func(t *testing.T) {
		s := NewStack(t, StackOptions{Name: "uiph"})
		r := s.Start(t, ReplicaOptions{Name: "web-ui"})
		a := s.Do(r, "GET", "/ui/", "", nil)
		if a.Status != 200 || !strings.Contains(string(a.Body), "built without the web interface") {
			t.Fatalf("GET /ui/: %d %.300s", a.Status, a.Body)
		}
		if n := s.Number(`SELECT count(*) FROM ui_assets`); n != 0 {
			t.Fatalf("the shared asset store holds %d files of the replica's UI, want none", n)
		}
	})
}
