package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// Replica states of the registry row (R-OPS-003).
const (
	ReplicaStarting = "starting"
	ReplicaReady    = "ready"
	ReplicaUnready  = "unready"
	ReplicaDraining = "draining"
)

// ReplicaIdentity is what a replica says about itself once, at registration.
// Backup pods, the restore Job and the migration Job never register
// (R-HA-003).
type ReplicaIdentity struct {
	// UID is random per process start (replica.instance_uid), a UUID.
	UID string
	// Name is the pod name, else the host name.
	Name     string
	Version  string
	Revision string
	// Role is api, packages, worker, web or all (the Role* constants, Q-OPS-161).
	Role string
	// Node is the Kubernetes node name; empty outside Kubernetes.
	Node string
	// Pool is the R-HA-062 pool name; empty for a role's main Deployment.
	Pool      string
	StartedAt time.Time
	// SecretKeyIDs and ActiveSecretKeyID are the key IDs of the replica's
	// stored-secret key ring (R-ARCH-021): IDs only, never key material.
	SecretKeyIDs      []string
	ActiveSecretKeyID string
}

// ReplicaCheck is the last result of one check as the row stores it.
type ReplicaCheck struct {
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
	ConfigError bool   `json:"configError,omitempty"`
	// DurationMs is how long the check's last run took, in milliseconds
	// (Q-OPS-175); nil for a check without a finished run, and in the rows of a
	// release before it.
	DurationMs *float64 `json:"durationMs,omitempty"`
}

// ReplicaRuntime is the replica's measurement of the period since its previous
// heartbeat (Q-OPS-175), as the `runtime` column stores it. The JSON names are
// those of the API's ReplicaRuntime, and a row is read by replicas of other
// releases: a field is added, never renamed.
type ReplicaRuntime struct {
	SampledAt time.Time `json:"sampledAt"`
	// WindowSeconds is the period the counts, the rate and the percentiles cover.
	WindowSeconds float64 `json:"windowSeconds"`
	// Requests and Errors are the requests the client listener finished in the
	// window and those of them answered with a 5xx status.
	Requests int64 `json:"requests"`
	Errors   int64 `json:"errors"`
	// InFlight is the requests being served when the snapshot was taken.
	InFlight  int64          `json:"inFlight"`
	LatencyMs ReplicaLatency `json:"latencyMs"`
	// CPUCores is CPU time used per second of the window; CPULimitCores is
	// GOMAXPROCS, nil when unknown.
	CPUCores      float64  `json:"cpuCores"`
	CPULimitCores *float64 `json:"cpuLimitCores"`
	// MemoryBytes is what the Go runtime holds from the system; MemoryLimitBytes
	// the container limit, 0 when none is known.
	MemoryBytes      int64 `json:"memoryBytes"`
	MemoryLimitBytes int64 `json:"memoryLimitBytes"`
	Goroutines       int   `json:"goroutines"`
}

// ReplicaLatency holds the latency percentiles of a window in milliseconds; nil
// when no request finished in it.
type ReplicaLatency struct {
	P50 *float64 `json:"p50"`
	P95 *float64 `json:"p95"`
	P99 *float64 `json:"p99"`
}

// ReplicaSnapshot is what each heartbeat writes besides the identity.
type ReplicaSnapshot struct {
	// State is one of the Replica* constants.
	State string
	// Checks holds every check of R-OPS-001, gating and reported.
	Checks map[string]ReplicaCheck
	// Valkey is the replica's view of Valkey (R-OPS-003), rendered by the
	// caller; opaque here. No credential, ACL user name or key appears.
	Valkey json.RawMessage
	// ObjectStore is the replica's object-store view (R-OPS-003, storage.md R-STOR-060): {"connections": [{"id", "circuit",
	// "drain"}]}, rendered by the caller; opaque here. No endpoint, bucket or credential appears. Nil writes '{}'.
	ObjectStore json.RawMessage
	// Runtime is the replica's measurement of the period since the previous
	// snapshot (Q-OPS-175); nil writes '{}', which reads back as no runtime.
	Runtime *ReplicaRuntime
}

// RegistryOptions configure [NewRegistry].
type RegistryOptions struct {
	// DB is the replica's control connection (DB.ControlPool): a request pool
	// exhausted under load must not make a live replica look lost.
	DB q.DBTX
	// ShutdownDB is the pool of the two writes a stopping replica makes to its row: marking it draining (Drain, step 1) and the
	// final deletion (Stop, step 6). It is the request pool, which no longer serves anything then. The control connection is
	// busy at that point with the change-log poll and the database and schema checks, which run until the close phase ends,
	// and a statement queued behind them is cut by the deadline of its phase (Q-HA-202, Q-HA-283). Nil uses DB.
	ShutdownDB q.DBTX
	Identity   ReplicaIdentity
	// Snapshot returns the current state, checks and Valkey view; called on
	// every heartbeat. Nil reports "ready" with no checks.
	Snapshot func() ReplicaSnapshot
	// Leases returns the leases the replica holds (the `leases` column); nil
	// for none.
	Leases          func() []HeldInfo
	Config          config.System
	WatchdogTimeout time.Duration
	Watchdog        Watchdog
	Log             *slog.Logger
}

// Registry is the replica registry of one replica (R-OPS-003): it registers the
// replica at start with its role, refreshes the row every
// system.heartbeat_interval, marks it draining at SIGTERM and deletes it on a
// clean shutdown. A row whose heartbeat is older than system.heartbeat_stale_after
// is listed as lost, on PostgreSQL's clock.
type Registry struct {
	o   RegistryOptions
	log *slog.Logger

	mu       sync.Mutex
	draining bool
	// marked is set once a write of the state draining reached the row (Drain or a heartbeat after it): a row left behind by a
	// failed deletion is then signed off, not lost (Q-OPS-210).
	marked bool
	// lastWritten is when a heartbeat last reached the row; staleLogged says that its age past system.heartbeat_stale_after has
	// been logged (Q-HA-309).
	lastWritten time.Time
	staleLogged bool
	// life is the heartbeat loop's: Stop waits for a heartbeat in flight instead of cutting it (Q-ARCH-503). Nil before Start.
	life *loopLife
}

// NewRegistry creates the registry.
func NewRegistry(o RegistryOptions) (*Registry, error) {
	switch {
	case o.DB == nil:
		return nil, errors.New("cluster: the replica registry needs the control connection")
	case o.Identity.UID == "" || o.Identity.Name == "" || o.Identity.Role == "":
		return nil, errors.New("cluster: the replica identity needs UID, Name and Role")
	case !config.ValidRole(o.Identity.Role):
		return nil, fmt.Errorf("cluster: the role %q is not one of api, packages, worker, web and all", o.Identity.Role)
	}
	log := o.Log
	if log == nil {
		log = slog.Default()
	}
	return &Registry{o: o, log: log}, nil
}

// Name implements the composition root's Component.
func (r *Registry) Name() string { return "registry" }

// Start registers the replica and begins the heartbeat. The registration
// retries until PostgreSQL answers or ctx ends (R-ARCH-024 step 7).
func (r *Registry) Start(ctx context.Context) error {
	for {
		err := r.beat(ctx)
		if err == nil {
			r.mu.Lock()
			r.lastWritten = time.Now()
			r.mu.Unlock()
			break
		}
		r.log.Warn("replica registration failed; retrying", "error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.o.Config.HeartbeatInterval):
		}
	}
	r.life = newLoopLife(ctx)
	r.life.run(r.loop)
	return nil
}

func (r *Registry) loop() {
	ctx := r.life.ctx
	loop := WatchLoop(r.o.Watchdog, "cluster.registry", r.o.WatchdogTimeout)
	defer loop.Stop()
	t := time.NewTicker(r.o.Config.HeartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.life.stopping:
			return
		case <-t.C:
		}
		if r.life.stopped() {
			return
		}
		err := r.beat(ctx)
		if ctx.Err() == nil {
			r.noteHeartbeat(err)
		}
		loop.Beat()
	}
}

// beat writes the row: registration and heartbeat are one upsert, so a row
// removed by the prune or a restore comes back with the next heartbeat.
func (r *Registry) beat(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout(r.o.WatchdogTimeout))
	defer cancel()
	snap := ReplicaSnapshot{State: ReplicaReady}
	if r.o.Snapshot != nil {
		snap = r.o.Snapshot()
	}
	r.mu.Lock()
	draining := r.draining
	if draining {
		snap.State = ReplicaDraining
	}
	r.mu.Unlock()
	var held []HeldInfo
	if r.o.Leases != nil {
		held = r.o.Leases()
	}
	params, err := upsertParams(r.o.Identity, snap, held)
	if err != nil {
		return err
	}
	if err := q.New(r.o.DB).UpsertReplica(ctx, params); err != nil {
		return err
	}
	if draining {
		r.mu.Lock()
		r.marked = true
		r.mu.Unlock()
	}
	return nil
}

// noteHeartbeat logs a failed heartbeat; once, when the row has not been written for longer than system.heartbeat_stale_after,
// that the other replicas list this one as lost; and when a heartbeat reaches the row again after that (Q-HA-309). The
// heartbeat runs on the control connection, which the `database` check probes, so the same outage shows in the replica's
// readiness as well.
func (r *Registry) noteHeartbeat(err error) {
	now := time.Now()
	r.mu.Lock()
	since := now.Sub(r.lastWritten)
	stale := err != nil && !r.staleLogged && r.o.Config.HeartbeatStaleAfter > 0 && since > r.o.Config.HeartbeatStaleAfter
	recovered := err == nil && r.staleLogged
	if err == nil {
		r.lastWritten, r.staleLogged = now, false
	} else if stale {
		r.staleLogged = true
	}
	r.mu.Unlock()
	switch {
	case stale:
		r.log.Error("replica heartbeat not written for longer than system.heartbeat_stale_after: the other replicas list this replica as lost until a heartbeat reaches the row",
			"since", since.Round(time.Second).String(), "error", err)
	case err != nil:
		r.log.Warn("replica heartbeat failed", "error", err)
	case recovered:
		r.log.Info("replica heartbeat written again", "after", since.Round(time.Second).String())
	}
}

// upsertParams renders the row of one heartbeat: the identity, the snapshot and
// the held leases. The JSON columns are written as text and cast in the
// statement; an absent Valkey view or runtime is '{}'.
func upsertParams(id ReplicaIdentity, snap ReplicaSnapshot, held []HeldInfo) (q.UpsertReplicaParams, error) {
	checks, err := json.Marshal(nonNilChecks(snap.Checks))
	if err != nil {
		return q.UpsertReplicaParams{}, err
	}
	leases := []byte("[]")
	if len(held) > 0 {
		sort.Slice(held, func(i, j int) bool { return held[i].Name < held[j].Name })
		if leases, err = json.Marshal(held); err != nil {
			return q.UpsertReplicaParams{}, err
		}
	}
	valkey := string(snap.Valkey)
	if valkey == "" {
		valkey = "{}"
	}
	runtime := []byte("{}")
	if snap.Runtime != nil {
		if runtime, err = json.Marshal(snap.Runtime); err != nil {
			return q.UpsertReplicaParams{}, err
		}
	}
	objectStore := string(snap.ObjectStore)
	if objectStore == "" {
		objectStore = "{}"
	}
	keyIDs := id.SecretKeyIDs
	if keyIDs == nil {
		keyIDs = []string{}
	}
	return q.UpsertReplicaParams{
		InstanceUid: id.UID, Name: id.Name, Version: id.Version, Revision: id.Revision,
		Roles: []string{id.Role}, State: snap.State, Checks: string(checks), Leases: string(leases),
		SecretKeyIds: keyIDs, ActiveSecretKeyID: id.ActiveSecretKeyID, Node: id.Node,
		Valkey: valkey, Pool: id.Pool, Runtime: string(runtime), ObjectStore: objectStore,
		StartedAt: pgtype.Timestamptz{Time: id.StartedAt, Valid: true},
	}, nil
}

func nonNilChecks(m map[string]ReplicaCheck) map[string]ReplicaCheck {
	if m == nil {
		return map[string]ReplicaCheck{}
	}
	return m
}

// shutdownDB is the pool of the writes a stopping replica makes to its row.
func (r *Registry) shutdownDB() q.DBTX {
	if r.o.ShutdownDB != nil {
		return r.o.ShutdownDB
	}
	return r.o.DB
}

// The two writes a stopping replica makes to its row are retried within the time of their step, because a switchover of the
// PostgreSQL primary ends every connection for some seconds and a single attempt fails exactly then (Q-HA-307). The pause starts
// at shutdownRetryFirst and doubles up to shutdownRetryMax. The deletion retries for at most deleteWindow and never past the
// close deadline; each of its attempts has at most deleteAttempt.
const (
	shutdownRetryFirst = 250 * time.Millisecond
	shutdownRetryMax   = 2 * time.Second
	deleteWindow       = 15 * time.Second
	deleteAttempt      = 5 * time.Second
)

// retryShutdownWrite runs write until it succeeds or ctx ends, each attempt bounded by attempt. The first failure is logged with
// the write's name, so a shutdown that waits on the database says so at once; the caller logs the outcome. It returns the
// number of attempts and the last error.
func (r *Registry) retryShutdownWrite(ctx context.Context, what string, attempt time.Duration, write func(context.Context) error) (int, error) {
	pause := shutdownRetryFirst
	for n := 1; ; n++ {
		actx, cancel := context.WithTimeout(ctx, attempt)
		err := write(actx)
		cancel()
		if err == nil {
			return n, nil
		}
		if n == 1 {
			r.log.Warn("replica row: write failed; retrying until its shutdown step ends", "write", what, "error", err)
		}
		select {
		case <-ctx.Done():
			return n, err
		case <-time.After(pause):
		}
		pause = min(2*pause, shutdownRetryMax)
	}
}

// Drain turns the row draining at once and keeps it draining in later heartbeats (R-ARCH-024 step 1). The write runs on the
// request pool: the control connection is the one a stalled database holds (Q-HA-283). It is retried until it reaches the row or
// ctx, the end of the drain step, ends (Q-ARCH-460, Q-HA-307); after that the heartbeat, which writes draining from now on,
// keeps trying on the control connection until the close phase. A heartbeat that was under way when the drain began cannot turn
// the row back: the upsert keeps a row's state draining (Q-HA-308).
func (r *Registry) Drain(ctx context.Context) {
	r.mu.Lock()
	r.draining = true
	r.mu.Unlock()
	n, err := r.retryShutdownWrite(ctx, "mark draining", callTimeout(r.o.WatchdogTimeout), func(c context.Context) error {
		_, err := q.New(r.shutdownDB()).SetReplicaState(c, q.SetReplicaStateParams{InstanceUid: r.o.Identity.UID, State: ReplicaDraining})
		return err
	})
	if err != nil {
		r.log.Warn("replica row not marked draining in the drain step; the heartbeat keeps writing draining until the close phase", "attempts", n, "error", err)
		return
	}
	r.mu.Lock()
	r.marked = true
	r.mu.Unlock()
	if n > 1 {
		r.log.Info("replica row marked draining", "attempts", n)
	}
}

// StopHeartbeat ends the heartbeat at once and leaves the row, as a process that was killed does: a heartbeat in flight is cut.
// Fault-injection tests use it to show a replica turning lost.
func (r *Registry) StopHeartbeat() {
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	r.stopHeartbeat(ended)
}

// stopHeartbeat ends the heartbeat loop; a heartbeat in flight is waited for until ctx ends (Q-ARCH-503).
func (r *Registry) stopHeartbeat(ctx context.Context) {
	if r.life != nil {
		r.life.stop(ctx)
	}
}

// Stop ends the heartbeat and deletes the replica's own row (a clean shutdown, R-OPS-003). A replica that is killed leaves the
// row to go stale. A heartbeat in flight is waited for, not cut (Q-ARCH-503). The deletion is a close step (R-ARCH-024 step 6):
// it is retried for at most deleteWindow and never past ctx, the deadline of the close phase, so the process exits inside its
// grace period (R-HA-011, Q-HA-307). A row that could not be deleted in time goes stale: signed off when the mark draining
// reached it, lost otherwise (Q-OPS-210); the log says which.
func (r *Registry) Stop(ctx context.Context) error {
	r.stopHeartbeat(ctx)
	wctx, cancel := context.WithTimeout(ctx, deleteWindow)
	defer cancel()
	n, err := r.retryShutdownWrite(wctx, "delete", deleteAttempt, func(c context.Context) error {
		return q.New(r.shutdownDB()).DeleteReplica(c, r.o.Identity.UID)
	})
	if err == nil {
		if n > 1 {
			r.log.Info("replica row deleted", "attempts", n)
		}
		return nil
	}
	r.mu.Lock()
	marked := r.marked
	r.mu.Unlock()
	if marked {
		r.log.Warn("replica row not deleted; it was marked draining and counts as signed off once its heartbeat is stale", "attempts", n, "error", err)
	} else {
		r.log.Error("replica row neither marked draining nor deleted: the database did not answer during the shutdown, and the row is listed as lost once its heartbeat is stale",
			"attempts", n, "error", err)
	}
	return fmt.Errorf("replica row not deleted after %d attempts: %w", n, err)
}
