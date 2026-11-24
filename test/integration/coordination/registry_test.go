//go:build integration

package coordination

import (
	"context"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/config"
)

func (e *env) list(rep *replica, sys config.System) map[string]cluster.ReplicaInfo {
	e.t.Helper()
	infos, err := cluster.ListReplicas(context.Background(), rep.db.Main(), sys)
	if err != nil {
		e.t.Fatal(err)
	}
	out := map[string]cluster.ReplicaInfo{}
	for _, i := range infos {
		out[i.Name] = i
	}
	return out
}

func TestReplicaRegistry(t *testing.T) {
	e := newEnv(t)
	sys := defaultSystem() // heartbeat 500 ms, stale after 3 s: the same logic as 10 s and 30 s, faster
	web := e.newReplica("reg-web", replicaOpts{role: "web", sys: sys})
	w1 := e.newReplica("reg-worker-1", replicaOpts{role: "worker", sys: sys, noWorker: true})
	w2 := e.newReplica("reg-worker-2", replicaOpts{role: "worker", sys: sys, noWorker: true})
	w1.leases.Hold(itLease, func(ctx context.Context, h *cluster.Held) { <-ctx.Done() })

	t.Run("R-OPS-003 every web and worker replica registers with its role", func(t *testing.T) {
		got := e.list(web, sys)
		for name, role := range map[string]string{"reg-web": "web", "reg-worker-1": "worker", "reg-worker-2": "worker"} {
			r, ok := got[name]
			if !ok {
				t.Fatalf("%s is not listed: %v", name, got)
			}
			if len(r.Roles) != 1 || r.Roles[0] != role || r.Version != "0.1.0" || r.Lost || r.State != "ready" {
				t.Errorf("%s = %+v", name, r)
			}
			if len(r.SecretKeyIDs) != 1 || r.ActiveSecretKeyID != "k1" {
				t.Errorf("%s: key IDs %v / %s", name, r.SecretKeyIDs, r.ActiveSecretKeyID)
			}
		}
	})

	t.Run("R-OPS-003 the heartbeat refreshes the row on PostgreSQL's clock", func(t *testing.T) {
		first := e.list(web, sys)["reg-web"].HeartbeatAt
		waitFor(t, "a newer heartbeat", 5*time.Second, func() bool { return e.list(web, sys)["reg-web"].HeartbeatAt.After(first) })
	})

	t.Run("R-OPS-003 the leases a worker holds are listed", func(t *testing.T) {
		waitFor(t, "the lease in the registry", 5*time.Second, func() bool {
			r := e.list(web, sys)["reg-worker-1"]
			return len(r.Leases) == 1 && r.Leases[0].Name == "it_singleton" && !r.Leases[0].Since.IsZero()
		})
	})

	t.Run("R-OPS-003 R-HA-003 a replica that stops heartbeating is listed as lost, and its replacement is listed", func(t *testing.T) {
		w2.registry.StopHeartbeat() // as a killed process: the row stays
		killed := time.Now()
		if e.list(web, sys)["reg-worker-2"].Lost {
			t.Fatal("lost at once")
		}
		e.newReplica("reg-worker-3", replicaOpts{role: "worker", sys: sys, noWorker: true})
		waitFor(t, "reg-worker-2 turning lost", 8*time.Second, func() bool { return e.list(web, sys)["reg-worker-2"].Lost })
		took := time.Since(killed)
		if took < sys.HeartbeatStaleAfter-sys.HeartbeatInterval-200*time.Millisecond {
			t.Errorf("lost after %s, before the stale bound %s could have passed", took, sys.HeartbeatStaleAfter)
		}
		got := e.list(web, sys)
		if r := got["reg-worker-3"]; r.Lost || r.State != "ready" {
			t.Errorf("the replacement: %+v", r)
		}
		if got["reg-worker-1"].Lost || got["reg-web"].Lost {
			t.Error("a live replica is listed as lost")
		}
		t.Logf("stopped replica listed as lost %s after its last heartbeat window (stale_after = %s)", took.Round(100*time.Millisecond), sys.HeartbeatStaleAfter)
	})

	t.Run("R-OPS-003 a draining replica shows draining and a clean shutdown deletes its row", func(t *testing.T) {
		ctx := context.Background()
		w1.registry.Drain(ctx)
		if st := e.list(web, sys)["reg-worker-1"].State; st != "draining" {
			t.Fatalf("state = %s", st)
		}
		time.Sleep(2 * sys.HeartbeatInterval)
		if st := e.list(web, sys)["reg-worker-1"].State; st != "draining" {
			t.Fatalf("a heartbeat after Drain reset the state to %s", st)
		}
		w1.stop()
		if _, ok := e.list(web, sys)["reg-worker-1"]; ok {
			t.Fatal("the row of a replica that shut down cleanly is still listed")
		}
	})

	t.Run("R-OPS-003 the prune deletes rows stale for 24 hours and a live replica re-registers", func(t *testing.T) {
		e.exec(`UPDATE replica SET heartbeat_at = now() - interval '25 hours' WHERE name = 'reg-worker-2'`)
		e.exec(`UPDATE replica SET heartbeat_at = now() - interval '23 hours' WHERE name = 'reg-worker-3'`)
		n, err := cluster.PruneReplicas(context.Background(), web.db.Main(), 24*time.Hour, sys.HeartbeatStaleAfter)
		if err != nil || n != 1 {
			t.Fatalf("PruneReplicas = %d, %v, want 1", n, err)
		}
		got := e.list(web, sys)
		if _, ok := got["reg-worker-2"]; ok {
			t.Error("the stale row survived")
		}
		// A row removed from under a live replica comes back with its next heartbeat.
		e.exec(`DELETE FROM replica WHERE name = 'reg-web'`)
		waitFor(t, "reg-web re-registering", 5*time.Second, func() bool { _, ok := e.list(web, sys)["reg-web"]; return ok })
	})
}

// TestReplicaSignOff is R-OPS-003's line between a replica that signed off and one that was lost (Q-OPS-210): a process
// killed while draining was stopped on purpose, so once its row goes stale it is neither live nor lost for any reader of
// the registry, and the prune deletes it at its next run; a process killed while ready stays listed as lost until the
// prune's 24 hours.
func TestReplicaSignOff(t *testing.T) {
	e := newEnv(t)
	sys := defaultSystem()
	host := e.newReplica("so-host", replicaOpts{role: "worker", sys: sys, noWorker: true})
	drained := e.newReplica("so-drained", replicaOpts{role: "web", sys: sys, noWorker: true})
	crashed := e.newReplica("so-crashed", replicaOpts{role: "web", sys: sys, noWorker: true})
	// Two web replicas that stay keep the fetch share above its floor of 1, so each kill shows in the count.
	e.newReplica("so-live-1", replicaOpts{role: "web", sys: sys, noWorker: true})
	e.newReplica("so-live-2", replicaOpts{role: "web", sys: sys, noWorker: true})
	fill, err := cluster.NewFillCount(host.db.ControlPool(), sys.HeartbeatStaleAfter, sys.HeartbeatInterval)
	if err != nil {
		t.Fatal(err)
	}
	count := func() int {
		if err := fill.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		return fill.Get()
	}
	if n := count(); n != 4 {
		t.Fatalf("the fetch share counts %d web replicas before the kills, want 4", n)
	}

	t.Run("R-OPS-003 R-HA-003 a replica killed while draining has signed off: it is not listed and not counted once its heartbeat is stale", func(t *testing.T) {
		drained.registry.Drain(context.Background())
		drained.registry.StopHeartbeat() // killed inside its drain: the row stays in the state draining
		if r, ok := e.list(host, sys)["so-drained"]; !ok || r.State != "draining" || r.Lost {
			t.Fatalf("a replica that drains right now: %+v, listed %v; want listed as draining", r, ok)
		}
		waitFor(t, "so-drained leaving the list", 8*time.Second, func() bool { _, ok := e.list(host, sys)["so-drained"]; return !ok })
		for name, r := range e.list(host, sys) {
			if r.Lost {
				t.Errorf("%s is listed as lost", name)
			}
		}
		if n := count(); n != 3 {
			t.Errorf("the fetch share counts %d web replicas, want 3: the signed-off one counts as live", n)
		}
	})

	t.Run("R-OPS-003 R-HA-003 a replica killed while ready is listed as lost and leaves the counts", func(t *testing.T) {
		crashed.registry.StopHeartbeat() // killed without a drain: a crash, a kill or the loss of its node
		waitFor(t, "so-crashed turning lost", 8*time.Second, func() bool { return e.list(host, sys)["so-crashed"].Lost })
		if r := e.list(host, sys)["so-crashed"]; r.State != "ready" {
			t.Errorf("the lost row keeps its last state: %s, want ready", r.State)
		}
		if n := count(); n != 2 {
			t.Errorf("the fetch share counts %d web replicas, want 2: the lost one counts as live", n)
		}
	})

	t.Run("R-OPS-003 the prune deletes the signed-off row at its next run and keeps the lost one for 24 hours", func(t *testing.T) {
		n, err := cluster.PruneReplicas(context.Background(), host.db.Main(), 24*time.Hour, sys.HeartbeatStaleAfter)
		if err != nil || n != 1 {
			t.Fatalf("PruneReplicas = %d, %v, want 1 (the signed-off row)", n, err)
		}
		var rows int
		if err := e.admin.QueryRow(context.Background(), `SELECT count(*) FROM replica WHERE name = 'so-drained'`).Scan(&rows); err != nil || rows != 0 {
			t.Fatalf("the signed-off row: %d rows, %v", rows, err)
		}
		if _, ok := e.list(host, sys)["so-crashed"]; !ok {
			t.Fatal("the lost row was pruned before its 24 hours")
		}
	})
}

// TestReplicaLostWithDefaults is R-OPS-003's bound with the shipped values:
// heartbeat every 10 s, lost after 30 s without one.
func TestReplicaLostWithDefaults(t *testing.T) {
	e := newEnv(t)
	sys := config.System{HeartbeatInterval: 10 * time.Second, HeartbeatStaleAfter: 30 * time.Second}
	web := e.newReplica("def-web", replicaOpts{role: "web", sys: sys})
	victim := e.newReplica("def-worker-1", replicaOpts{role: "worker", sys: sys, noWorker: true})

	t.Run("R-OPS-003 a stopped replica is lost within 30 s and its replacement is listed", func(t *testing.T) {
		victim.registry.StopHeartbeat()
		start := time.Now()
		e.newReplica("def-worker-2", replicaOpts{role: "worker", sys: sys, noWorker: true})
		if e.list(web, sys)["def-worker-1"].Lost {
			t.Fatal("lost at once")
		}
		time.Sleep(20 * time.Second)
		if e.list(web, sys)["def-worker-1"].Lost {
			t.Fatal("lost after 20 s, before the 30 s window")
		}
		waitFor(t, "lost", 12*time.Second, func() bool { return e.list(web, sys)["def-worker-1"].Lost })
		if took := time.Since(start); took > 31*time.Second {
			t.Fatalf("marked lost after %s, want within 30 s of the last heartbeat", took)
		}
		if r := e.list(web, sys)["def-worker-2"]; r.Lost || len(r.Roles) != 1 || r.Roles[0] != "worker" {
			t.Fatalf("the replacement: %+v", r)
		}
	})
}

// TestReplicaListLostLeases: the registry lists a lease for the live holder
// only; the stale row of a lost replica names none.
func TestReplicaListLostLeases(t *testing.T) {
	e := newEnv(t)
	sys := defaultSystem()
	holder := e.newReplica("ll-holder", replicaOpts{role: "worker", sys: sys, noWorker: true})
	stale := e.newReplica("ll-stale", replicaOpts{role: "worker", sys: sys, noWorker: true})
	holder.leases.Hold(itLease, func(ctx context.Context, h *cluster.Held) { <-ctx.Done() })

	t.Run("R-OPS-003 the leases of a lost replica are not listed", func(t *testing.T) {
		stale.registry.StopHeartbeat() // a killed process: the row stays, with the leases it last wrote
		e.exec(`UPDATE replica SET leases = '[{"name":"it_singleton","since":"2026-01-01T00:00:00Z"}]' WHERE name = 'll-stale'`)
		waitFor(t, "ll-stale turning lost", 8*time.Second, func() bool { return e.list(holder, sys)["ll-stale"].Lost })
		waitFor(t, "the live holder listing the lease", 5*time.Second, func() bool {
			r := e.list(holder, sys)["ll-holder"]
			return len(r.Leases) == 1 && r.Leases[0].Name == "it_singleton"
		})
		got := e.list(holder, sys)
		if n := len(got["ll-stale"].Leases); n != 0 {
			t.Fatalf("the lost replica lists %d lease(s): %+v", n, got["ll-stale"].Leases)
		}
		holders := 0
		for _, r := range got {
			for _, l := range r.Leases {
				if l.Name == "it_singleton" {
					holders++
				}
			}
		}
		if holders != 1 {
			t.Fatalf("%d rows list the lease, want 1", holders)
		}
	})
}

// TestReplicaRegistryRoles is R-OPS-003's role vocabulary after the cut into task areas (Q-OPS-161) and the count of the
// fetch share (R-REPO-040, Q-ARCH-297): the registry stores the role names api, packages, worker, web and all as given,
// and only the replicas that fill client misses, pools included, are counted.
func TestReplicaRegistryRoles(t *testing.T) {
	e := newEnv(t)
	sys := defaultSystem()
	host := e.newReplica("roles-host", replicaOpts{role: "web", sys: sys})
	register := func(name, role, pool string) *cluster.Registry {
		t.Helper()
		reg, err := cluster.NewRegistry(cluster.RegistryOptions{
			DB: host.db.ControlPool(),
			Identity: cluster.ReplicaIdentity{
				UID: uuidV4(), Name: name, Version: "0.1.0", Revision: "it", Role: role, Pool: pool, StartedAt: time.Now(),
				SecretKeyIDs: []string{"k1"}, ActiveSecretKeyID: "k1",
			},
			Config: sys, WatchdogTimeout: 30 * time.Second, Log: quiet,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := reg.Start(context.Background()); err != nil {
			t.Fatalf("registry.Start(%s): %v", name, err)
		}
		t.Cleanup(func() { _ = reg.Stop(context.Background()) })
		return reg
	}
	var pooled *cluster.Registry
	// The host is a web replica of its own, so it is counted as one that fills.
	fill, err := cluster.NewFillCount(host.db.ControlPool(), sys.HeartbeatStaleAfter, sys.HeartbeatInterval)
	if err != nil {
		t.Fatal(err)
	}
	count := func() int {
		if err := fill.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		return fill.Get()
	}

	t.Run("R-OPS-003 every role value of the cut is stored as given, the pool next to it", func(t *testing.T) {
		register("roles-api", "api", "")
		register("roles-pk", "packages", "")
		pooled = register("roles-pk-ep", "packages", "ep-customers")
		register("roles-wk", "worker", "")
		register("roles-wk-vuln", "worker", "vuln")
		register("roles-all", "all", "")
		got := e.list(host, sys)
		for name, want := range map[string][2]string{
			"roles-api": {"api", ""}, "roles-pk": {"packages", ""}, "roles-pk-ep": {"packages", "ep-customers"},
			"roles-wk": {"worker", ""}, "roles-wk-vuln": {"worker", "vuln"}, "roles-all": {"all", ""}, "roles-host": {"web", ""},
		} {
			r, ok := got[name]
			if !ok || len(r.Roles) != 1 || r.Roles[0] != want[0] || r.Pool != want[1] {
				t.Errorf("%s = %+v, want role %s pool %q", name, r, want[0], want[1])
			}
		}
	})

	t.Run("R-REPO-040 the fetch share counts packages, web and all with their pools, never api or worker", func(t *testing.T) {
		// web (host), packages, packages of a pool and all: four. api, worker and a worker pool hold no share.
		if n := count(); n != 4 {
			t.Fatalf("n = %d, want 4", n)
		}
	})

	t.Run("R-REPO-040 a lost replica leaves the count and a new one joins it", func(t *testing.T) {
		pooled.StopHeartbeat() // as a killed process: the row stays and ages
		e.exec(`UPDATE replica SET heartbeat_at = now() - interval '1 hour' WHERE name = 'roles-pk-ep'`)
		if n := count(); n != 3 {
			t.Fatalf("n = %d after one packages replica was lost, want 3", n)
		}
		register("roles-pk-2", "packages", "")
		if n := count(); n != 4 {
			t.Fatalf("n = %d after a packages replica joined, want 4", n)
		}
	})

	t.Run("R-OPS-003 a role outside the vocabulary does not register", func(t *testing.T) {
		_, err := cluster.NewRegistry(cluster.RegistryOptions{
			DB:       host.db.ControlPool(),
			Identity: cluster.ReplicaIdentity{UID: uuidV4(), Name: "roles-bad", Role: "fetch"},
		})
		if err == nil {
			t.Fatal("the role fetch registered")
		}
	})
}
