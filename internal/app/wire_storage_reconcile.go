package app

import (
	"context"
	"encoding/json"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/blob/reconcile"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/observability"
)

// The reconciliation between the connections (storage.md R-STOR-056): its queue and pass, the server-side and the streamed
// copies, the fill, the drain, the retirement and the journal pass, built in internal/blob/reconcile. Package C of the build
// plan of several connections (docs/developer/blob-store.md).

// registerStorageReconcile registers the kind and the task storage-reconcile: queue maintenance, exclusive under its name,
// every minute by default (maintenance.tasks.storage-reconcile), a task that rewrites storage and so is not scheduled in
// read-only maintenance mode (R-OPS-024). The handler finds the reconciler at run time; only worker and all build one.
func (a *App) registerStorageReconcile(r *jobs.Registry) {
	r.RegisterKind(reconcile.Kind, jobs.KindSpec{
		Queue: "maintenance", DedupMode: jobs.Exclusive,
		Handler: a.storageReconcile.handler("storage reconciliation", func(ctx context.Context, h jobs.Handler, run *jobs.Run) error {
			return h(ctx, run)
		}),
	})
	spec := jobs.TaskSpec{Schedule: reconcile.DefaultSchedule, Mutating: true}
	if t, ok := a.cfg.Maintenance.Tasks[reconcile.Kind]; ok && t.Schedule != "" {
		spec.Schedule, spec.Disabled = t.Schedule, !t.Enabled
	}
	r.RegisterTask(reconcile.Kind, spec)
}

// wireStorageReconcile builds what the reconciliation needs at run time from the blob area: on every role the gauges of its queue
// and of the filling connections (read from PostgreSQL in the background) and, with two connections that hold every blob, the
// signal a commit sends after it queued a gap; on worker and all the reconciler the job's handler runs.
func (a *App) wireStorageReconcile(ctx context.Context, area blobArea) error {
	store := area.store
	if store == nil {
		return nil
	}
	a.registerReconcileGauges()
	log := a.log.With("component", "storage-reconcile")
	if len(store.Connections().Targets()) > 1 {
		sig := reconcile.NewSignal(func(ctx context.Context) error {
			qu := a.Queue()
			if qu == nil {
				return nil // the replica is still starting; the scheduled run copies the blob
			}
			_, err := qu.EnqueueNow(ctx, jobs.EnqueueSpec{Kind: reconcile.Kind})
			return err
		}, log)
		if err := a.startComponent(ctx, sig); err != nil {
			return err
		}
		store.SetGapSignal(sig.Notify)
	}
	if !a.sub.JobLoops {
		return nil
	}
	rec, err := reconcile.New(reconcile.Options{
		Store: store, DB: a.database(), Storage: a.cfg.Storage, Metrics: reconcile.NewMetrics(a.tele.metrics), Log: log,
		StaleAfter: a.cfg.System.HeartbeatStaleAfter, JournalDir: journal.Dir, Drained: connectionDrained(part[auditArea](a.parts).rec),
	})
	if err != nil {
		return err
	}
	a.storageReconcile.set(func(ctx context.Context, run *jobs.Run) error { return rec.RunJob(ctx, run) })
	return nil
}

// registerReconcileGauges adds quader_storage_reconcile_queue, quader_storage_reconcile_queue_oldest_seconds and
// quader_storage_connection_filling and quader_storage_connection_detached to the gauges read from PostgreSQL (R-STOR-056,
// R-STOR-060, ha-and-helm.md Q-HA-393).
func (a *App) registerReconcileGauges() {
	if a.sub.Shell || !a.sub.MainPool {
		return
	}
	g := reconcile.NewGauges()
	stale := a.cfg.System.HeartbeatStaleAfter.Seconds()
	a.tele.metrics.DBGauges().Add(observability.GaugeSource{
		Name: "storage-reconcile", Descs: g.Descs(),
		Collect: func(ctx context.Context) ([]prometheus.Metric, error) {
			db := a.database()
			if db == nil {
				return nil, nil
			}
			rows, err := q.New(db.Main()).ReconcileGauges(ctx, stale)
			if err != nil {
				return nil, err
			}
			out := make([]reconcile.GaugeRow, len(rows))
			for i, r := range rows {
				out[i] = reconcile.GaugeRow{Connection: r.ID, Queued: r.Queued, OldestSeconds: r.OldestSeconds, Filling: r.Filling, Detached: r.Detached}
			}
			return g.Metrics(out), nil
		},
	})
}

// connectionDrained writes system.storage.connection_drained in the transaction that recorded a draining connection as drained
// (R-STOR-056), actor system:worker.
func connectionDrained(rec audit.Recorder) func(ctx context.Context, tx meta.Tx, conn string, deleted int64) error {
	if rec == nil {
		return nil
	}
	return func(ctx context.Context, tx meta.Tx, conn string, deleted int64) error {
		details, err := json.Marshal(map[string]any{"connection": conn, "copiesDeleted": deleted})
		if err != nil {
			return err
		}
		return rec.RecordTx(ctx, tx, audit.Event{
			Type: audit.EventStorageConnectionDrained, Outcome: audit.Success, Actor: audit.Actor{Kind: "system", Name: "worker"}, Details: details,
		})
	}
}
