//go:build integration

package coordination

import (
	"context"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/cluster"
)

// R-OPS-048, Q-OPS-223, H-1161: the rows of the scanner replicas against PostgreSQL, the statements the scan listener and the API run.
// A report makes a row or refreshes it on the database's clock; the state's time moves only with the state or its reason; draining
// is the last state; a UID of another kind is refused; a kind has at most MaxRows live rows, and a stale row does not count; the list
// shows a stale row as lost and leaves out one that went stale while draining, and names the build time of the signature database in
// use; the sign-off removes only a row of its kind; the prune deletes by the registry's rule. One database, rows of bytes.
func TestR_OPS_048_TheRowsOfTheScannerReplicas(t *testing.T) {
	e := newEnv(t)
	sys := defaultSystem() // stale after 3 s
	web := e.newReplica("scan-web", replicaOpts{role: "web", sys: sys})
	db := web.db.Main()
	ctx := context.Background()
	store := cluster.ScannerReplicas{DB: db, System: sys, MaxRows: 2}
	list := func() map[string]cluster.ScannerReplica {
		t.Helper()
		rows, err := cluster.ListScannerReplicas(ctx, db, sys)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]cluster.ScannerReplica{}
		for _, r := range rows {
			out[r.InstanceUID] = r
		}
		return out
	}
	record := func(kind, uid string, r cluster.ScannerRecord) bool {
		t.Helper()
		ok, err := store.Record(ctx, kind, uid, r)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	const version = "main-62.daily-27841.bytecode-339"
	e.exec(`UPDATE malware_database SET version = $1, files = '[]', built_at = '2026-11-21T04:00:00Z', imported_at = now()`, version)
	clamav := cluster.ScannerRecord{
		Name: "quader-malware-scanner-clamav-6d9c7b5f4-x2v4q", Node: "pool-a-7x2k", Version: "0.42.0", Revision: "abc",
		State: "starting", Reason: "database", ToolVersion: "1.5.5", SlotsInUse: 0, Slots: 2,
	}
	a, b, c := uuidV4(), uuidV4(), uuidV4()

	if !record("clamav", a, clamav) {
		t.Fatal("the first report was refused")
	}
	first := list()[a]
	if first.Kind != "clamav" || first.State != "starting" || first.Reason != "database" || first.ToolVersion != "1.5.5" || first.Lost ||
		first.DatabaseVersion != "" || first.Slots != 2 || first.Node != "pool-a-7x2k" {
		t.Fatalf("the row: %+v", first)
	}

	t.Run("the state's time moves with the state, the heartbeat with every report", func(t *testing.T) {
		time.Sleep(20 * time.Millisecond)
		record("clamav", a, clamav)
		again := list()[a]
		if !again.StateSince.Equal(first.StateSince) || !again.HeartbeatAt.After(first.HeartbeatAt) {
			t.Fatalf("the same state: since %s → %s, heartbeat %s → %s", first.StateSince, again.StateSince, first.HeartbeatAt, again.HeartbeatAt)
		}
		ready := clamav
		ready.State, ready.Reason, ready.Database, ready.SlotsInUse = "ready", "", version, 1
		record("clamav", a, ready)
		now := list()[a]
		if !now.StateSince.After(first.StateSince) || now.State != "ready" || now.Reason != "" || now.SlotsInUse != 1 {
			t.Fatalf("a new state: %+v", now)
		}
		if now.DatabaseVersion != version || !now.DatabaseBuiltAt.Equal(time.Date(2026, 11, 21, 4, 0, 0, 0, time.UTC)) {
			t.Fatalf("the database in use: %s %s", now.DatabaseVersion, now.DatabaseBuiltAt)
		}
		old := ready
		old.Database = "main-62.daily-27830.bytecode-339"
		record("clamav", a, old)
		if r := list()[a]; r.DatabaseVersion != old.Database || !r.DatabaseBuiltAt.IsZero() {
			t.Fatalf("a version not in use has no date: %+v", r)
		}
	})

	t.Run("H-1161 a UID of another kind is refused", func(t *testing.T) {
		trivy := cluster.ScannerRecord{Name: "quader-vuln-scanner-trivy-7d9f-abcde", Version: "0.42.0", State: "ready", ToolVersion: "0.67.2", Slots: 1}
		if record("trivy", a, trivy) {
			t.Fatal("a trivy report took the row of a clamav replica")
		}
		if r := list()[a]; r.Kind != "clamav" || r.Name != clamav.Name {
			t.Fatalf("the row changed: %+v", r)
		}
	})

	t.Run("H-1161 a kind has at most MaxRows live rows, and a stale row does not count", func(t *testing.T) {
		if !record("clamav", b, clamav) {
			t.Fatal("the second row was refused")
		}
		// Q-HA-496: a row's key is its process's UID, so two processes that carry one name (the containers of one pod, which the chart
		// names after the pod) stand side by side and neither replaces the other.
		if rows := list(); rows[a].Name != clamav.Name || rows[b].Name != clamav.Name || rows[a].Lost || rows[b].Lost {
			t.Fatalf("two processes of one name: %+v %+v", rows[a], rows[b])
		}
		if record("clamav", c, clamav) {
			t.Fatal("a third row beyond MaxRows was admitted")
		}
		if !record("trivy", c, cluster.ScannerRecord{Name: "trivy-1", Version: "0.42.0", State: "ready", Slots: 1}) {
			t.Fatal("the bound of clamav refused a row of trivy")
		}
		e.exec(`UPDATE scanner_replica SET heartbeat_at = now() - interval '1 minute' WHERE instance_uid = $1`, b)
		d := uuidV4()
		if !record("clamav", d, clamav) {
			t.Fatal("a stale row still counted against the bound")
		}
		if r := list()[b]; !r.Lost {
			t.Fatalf("a stale row is not lost: %+v", r)
		}
		e.exec(`DELETE FROM scanner_replica WHERE instance_uid = $1`, d)
	})

	t.Run("draining is the last state, and a row that went stale while draining is not listed", func(t *testing.T) {
		draining := clamav
		draining.State, draining.Reason = "draining", ""
		record("clamav", a, draining)
		record("clamav", a, clamav)
		if r := list()[a]; r.State != "draining" {
			t.Fatalf("a report turned a draining row back: %+v", r)
		}
		e.exec(`UPDATE scanner_replica SET heartbeat_at = now() - interval '1 minute' WHERE instance_uid = $1`, a)
		if _, ok := list()[a]; ok {
			t.Fatal("a row that went stale while draining is listed")
		}
	})

	t.Run("the sign-off removes only a row of its kind", func(t *testing.T) {
		if ok, err := store.Remove(ctx, "trivy", b); err != nil || ok {
			t.Fatalf("another kind's sign-off: %v %v", ok, err)
		}
		if ok, err := store.Remove(ctx, "trivy", c); err != nil || !ok {
			t.Fatalf("its own: %v %v", ok, err)
		}
	})

	t.Run("the prune deletes a lost row after its day and a signed-off row at once", func(t *testing.T) {
		// a: draining and stale (signed off); b: lost for a minute, then for a day.
		n, err := cluster.PruneScannerReplicas(ctx, db, 24*time.Hour, sys.HeartbeatStaleAfter)
		if err != nil || n != 1 {
			t.Fatalf("the signed-off row: %d %v", n, err)
		}
		e.exec(`UPDATE scanner_replica SET heartbeat_at = now() - interval '25 hours' WHERE instance_uid = $1`, b)
		if n, err := cluster.PruneScannerReplicas(ctx, db, 24*time.Hour, sys.HeartbeatStaleAfter); err != nil || n != 1 {
			t.Fatalf("the lost row: %d %v", n, err)
		}
		if got := e.scalar(`SELECT count(*) FROM scanner_replica`); got != 0 {
			t.Fatalf("%d rows left", got)
		}
	})
}
