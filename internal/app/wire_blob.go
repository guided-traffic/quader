package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/s3"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/observability/health"
	"github.com/guided-traffic/quader/internal/uploads"
)

// wireBlob is the blob area: it builds a client and a guard per object-store connection, the ordered set of connections
// and the blob store, records the connections (R-STOR-051), runs their start checks (R-STOR-035, R-STOR-060) and, on worker
// and all, the garbage collector (R-ARCH-024 step 6). A replica becomes ready once the start check passed on one connection
// that is not draining and no such connection has a configuration error; a transient failure is retried while the replica
// stays unready, and a configuration error such as a wrong bucket, a changed fixed property or a foreign marker ends the
// process.
func (a *App) wireBlob(ctx context.Context, keys keysArea, pg postgresArea, au auditArea) (blobArea, error) {
	if err := requireInputs(AreaBlob, given("outbound factory", keys.out), given("database", pg.db)); err != nil {
		return blobArea{}, err
	}
	cfg := a.cfg.Storage
	out, db := keys.out, pg.db
	// A CA file of proxy.outbound.tls.ca_files that has not loaded yet holds the
	// first readiness back (the outbound check) and loads later without a restart,
	// so wait for it here rather than skip the object store: skipping would let the
	// replica turn ready without one. Any other outbound configuration error is
	// fixed for the life of the process and ends the start (CR2-8).
	for delay := time.Second; ; delay = min(delay*2, 15*time.Second) {
		err := out.TrustPending()
		if err == nil {
			break
		}
		a.log.Error("object store waits for the outbound trust store", "error", err, "retry_in", delay.String())
		if !sleep(ctx, delay) {
			return blobArea{}, ctx.Err()
		}
	}
	if err := out.Err(); err != nil {
		return blobArea{}, fmt.Errorf("object store: outbound configuration: %w", err)
	}
	resolved, err := config.ResolveConnections(cfg.Connections, nil)
	if err != nil {
		return blobArea{}, fmt.Errorf("object store: %w", err)
	}
	bm := blob.NewMetrics(a.tele.metrics)
	log := a.log.With("component", "blob")
	clients := map[string]*s3.Client{}
	var list []*blob.Connection
	for i, el := range resolved {
		client, err := s3.NewFromFactory(ctx, config.ClientConfig(i, el, cfg.ObjectStore), out)
		if err != nil {
			return blobArea{}, err // the error names the connection and its key: storage.connections[i] (id): ca_file
		}
		guard := blob.NewGuard(client, cfg.ObjectStore.CircuitBreaker, blob.WithGuardMetrics(bm), blob.WithGuardConnection(el.ID),
			blob.WithGuardLogger(log, el.CredentialsSource))
		clients[el.ID] = client
		list = append(list, blob.NewConnection(el, guard))
	}
	conns, err := blob.NewConnections(list...)
	if err != nil {
		return blobArea{}, fmt.Errorf("object store: %w", err)
	}
	skeys, err := a.newStorageKeys(ctx, keys.ring, db, log)
	if err != nil {
		return blobArea{}, err
	}
	store, err := blob.New(cfg, conns, db, blob.WithLogger(log), blob.WithMetrics(bm), blob.WithMemory(httpserver.SettleMemory),
		blob.WithConnectionAdded(connectionAdded(au.rec)), blob.WithStorageKeys(skeys), blob.WithDamageHook(blobDamaged(au.rec)))
	if err != nil {
		return blobArea{}, err
	}
	if err := a.recordConnections(ctx, store, log); err != nil {
		return blobArea{}, err
	}
	a.objectStores.set(conns)
	opts := blob.StartCheckOptions{Replica: a.id.Instance, HoldsCopies: store.HoldsCopies, Record: store.RecordCapabilities, Log: log}
	if facts, err := db.SystemFacts(ctx); err == nil {
		opts.Installation = facts.InstallationID
	} else {
		log.Warn("the installation id could not be read; the connections' markers are not checked at this start", "error", err)
	}
	check, err := startConnections(ctx, conns, cfg.ObjectStore.StartupCheck, opts, a.cfg.Health.RequireObjectStore)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return blobArea{}, err
		}
		return blobArea{}, fmt.Errorf("object store startup check: %w", err)
	}
	if err := a.ensureStorageKeys(ctx, skeys, conns, log); err != nil {
		return blobArea{}, err
	}
	if a.cfg.Health.RequireObjectStore {
		a.health.AddReadiness("objectStore", check, false)
	} else {
		a.health.AddReported("objectStore", check)
	}

	area := blobArea{store: store, storageKeys: skeys}
	// The upload sessions (R-STOR-011): a replica that accepts uploads continues any session; the collection run of a worker
	// expires them. A session starts on the first writable connection and stays on it (R-STOR-054).
	first := conns.Targets()[0]
	svc, err := uploads.New(uploads.Options{
		DB: db, Connections: conns, Spool: store.Spool(), Storage: cfg, Metrics: bm, Log: a.log.With("component", "uploads"),
	})
	if err != nil {
		return blobArea{}, err
	}
	a.uploads = svc
	if a.sub.JobLoops {
		// collection deletes every copy on its connection (R-STOR-057); the same collector runs the orphan sweep (R-STOR-026)
		area.gc = blob.NewCollector(cfg, conns, db, blob.WithCollectorLogger(log), blob.WithCollectorMetrics(bm), blob.WithUploadSessions(cfg.Uploads),
			blob.WithInstallation(opts.Installation))
		a.gc.set(area.gc)
		a.wireJournalShipper(clients[first.ID()], first.Prefix(), pg.db)
		a.wireBackupCheck(clients[first.ID()])
		// The usage fold of every worker replica (R-STOR-003): no lease, the folds of several replicas take disjoint rows.
		fold, err := blob.NewUsageFolder(blob.UsageFolderOptions{
			DB: db, Interval: cfg.Usage.FoldInterval, WatchdogTimeout: a.cfg.Health.WatchdogTimeout,
			Loop:        cluster.WatchLoop(a.health, "blob.usage_fold", a.cfg.Health.WatchdogTimeout),
			CallTimeout: cluster.CallTimeout(a.cfg.Health.WatchdogTimeout),
			Log:         a.log.With("component", "usage-fold"),
		})
		if err != nil {
			return blobArea{}, err
		}
		if err := a.startComponent(ctx, fold); err != nil {
			return blobArea{}, err
		}
	}
	for _, wire := range []func(context.Context, blobArea) error{a.wireStorageSweep, a.wireStorageReconcile, a.wireStorageScrub} {
		if err := wire(ctx, area); err != nil {
			return blobArea{}, err
		}
	}
	return area, nil
}

// recordConnections records the connections (R-STOR-051), retrying while PostgreSQL does not answer, like the steps before
// it; a refusal (a changed fixed property, a retired id) ends the start.
func (a *App) recordConnections(ctx context.Context, store *blob.Store, log interface {
	Warn(msg string, args ...any)
},
) error {
	for delay := time.Second; ; delay = min(delay*2, 15*time.Second) {
		err := store.RecordConnections(ctx)
		var refused *blob.Refusal
		switch {
		case err == nil:
			return nil
		case errors.As(err, &refused):
			return fmt.Errorf("object store: %w", err)
		}
		log.Warn("the object-store connections could not be recorded; retrying", "error", err, "retry_in", delay.String())
		if !sleep(ctx, delay) {
			return ctx.Err()
		}
	}
}

// connectionAdded writes system.storage.connection_added in the transaction that recorded a connection's first sight
// (R-STOR-051).
func connectionAdded(rec audit.Recorder) blob.ConnectionAddedFunc {
	if rec == nil {
		return nil
	}
	return func(ctx context.Context, tx meta.Tx, conn string) error {
		details, err := json.Marshal(map[string]string{"connection": conn})
		if err != nil {
			return err
		}
		return rec.RecordTx(ctx, tx, audit.Event{
			Type: audit.EventStorageConnectionAdded, Outcome: audit.Success, Actor: audit.Actor{Kind: "system", Name: "replica"}, Details: details,
		})
	}
}

// startConnections runs the start checks of the connections and returns the readiness check objectStore (R-STOR-060,
// Q-STOR-242). It returns when the check passed on one connection that is not draining and every such connection has passed
// or failed transiently once; a configuration error of such a connection is its error. With startupCheck false nothing is
// checked before the first readiness.
//
// The check asks every connection with one HEAD on a sentinel key: reported (gating=false), it fails while any connection
// fails, naming them ("unavailable: secondary"), which shows as degraded, and a configuration error of a connection that is not
// draining holds back the first readiness; gating (health.require_object_store), it fails only while no connection that is not
// draining answers.
func startConnections(ctx context.Context, conns *blob.Connections, startupCheck bool, opts blob.StartCheckOptions, gating bool) (health.CheckFunc, error) {
	if startupCheck {
		if err := blob.AwaitConnections(ctx, conns, opts); err != nil {
			return nil, err
		}
	}
	return objectStoreCheck(conns, gating), nil
}

// objectStoreCheck is the readiness check objectStore over every connection (R-OPS-001, R-STOR-060).
func objectStoreCheck(conns *blob.Connections, gating bool) health.CheckFunc {
	return func(ctx context.Context) error {
		var failing, configured []string
		var last error
		reachable := 0
		for _, c := range conns.List() {
			probe := strings.TrimPrefix(c.Prefix()+"/_probe/health", "/")
			_, err := c.Objects().Head(ctx, probe)
			switch {
			case err == nil || errors.Is(err, blob.ErrNotFound):
				if !c.Drain() {
					reachable++
				}
				continue
			case blob.IsConfig(err) && !c.Drain():
				configured = append(configured, c.ID())
			}
			failing, last = append(failing, c.ID()), err
		}
		switch {
		case gating && reachable > 0:
			return nil
		case len(failing) == 0:
			return nil
		case len(configured) > 0 && !gating:
			return health.ConfigError(last, "config: "+strings.Join(configured, ", "))
		}
		return health.WithReason(last, "unavailable: "+strings.Join(failing, ", "))
	}
}

// objectStoreView is the replica registry's object-store view (R-OPS-003, R-STOR-060): the ids of this replica's connections
// in order, each with its circuit state and its drain flag; no endpoint, bucket or credential. Nil before the blob area is
// wired.
func (a *App) objectStoreView() json.RawMessage {
	conns, ok := a.objectStores.get()
	if !ok {
		return nil
	}
	return objectStoreViewOf(conns)
}

func objectStoreViewOf(conns *blob.Connections) json.RawMessage {
	type entry struct {
		ID      string `json:"id"`
		Circuit string `json:"circuit"`
		Drain   bool   `json:"drain"`
	}
	view := struct {
		Connections []entry `json:"connections"`
	}{Connections: []entry{}}
	for _, c := range conns.List() {
		view.Connections = append(view.Connections, entry{ID: c.ID(), Circuit: c.Circuit(), Drain: c.Drain()})
	}
	b, err := json.Marshal(view)
	if err != nil {
		return nil
	}
	return b
}
