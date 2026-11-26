package app

import (
	"context"
	"encoding/json"
	"errors"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/scrub"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/jobs"
)

// The scrub that reads every copy whole once per connection's period, marks what it finds damaged and replaces it from another
// connection (storage.md R-STOR-027, Q-STOR-265): package D of the build plan of several connections
// (docs/developer/blob-store.md).

// TaskScrub is the task and the kind of the scrub.
const TaskScrub = "scrub"

// defaultScrubSchedule is the fallback of maintenance.tasks.scrub, whose default the configuration has as well.
const defaultScrubSchedule = "7 * * * *"

// registerStorageScrub registers the kind and the task scrub: queue maintenance, exclusive, hourly, a mutating task (its repairs
// write objects). The handler finds the scrubber at run time (a.scrub); a run asked for a connection this replica does not
// configure fails at once and is not tried again.
func (a *App) registerStorageScrub(r *jobs.Registry) {
	r.RegisterKind(TaskScrub, jobs.KindSpec{
		Queue: "maintenance", DedupMode: jobs.Exclusive,
		Handler: a.scrub.handler("scrub", func(ctx context.Context, s *scrub.Scrubber, run *jobs.Run) error {
			err := s.RunJob(ctx, run.Args, run)
			if errors.Is(err, scrub.ErrUnknownConnection) {
				return jobs.Permanent(err)
			}
			return err
		}),
	})
	spec := jobs.TaskSpec{Schedule: defaultScrubSchedule, Mutating: true}
	if t, ok := a.cfg.Maintenance.Tasks[TaskScrub]; ok && t.Schedule != "" {
		spec.Schedule, spec.Disabled = t.Schedule, !t.Enabled
	}
	r.RegisterTask(TaskScrub, spec)
}

// wireStorageScrub builds the scrubber from the blob area on every role: a worker runs the job with it, an api replica the
// recheck of the damaged-blobs list. Every replica exports quader_storage_scrub_overdue_copies from PostgreSQL.
func (a *App) wireStorageScrub(_ context.Context, area blobArea) error {
	db := a.database()
	if db == nil || area.store == nil {
		return nil
	}
	log := a.log.With("component", "scrub")
	// every connection, the encrypted ones included: the scrub reads an encrypted copy through the connection's reader, which opens
	// its record and authenticates every segment, and repairs it through the connection's writer (R-STOR-087)
	conns := area.store.Connections()
	s, err := scrub.New(scrub.Options{
		Connections: conns,
		Ledger:      scrub.PGLedger{DB: db, Store: area.store, Log: log},
		RunDuration: a.cfg.Storage.Scrub.RunDuration,
		PartSize:    int64(a.cfg.Storage.ObjectStore.Multipart.PartSize),
		Metrics:     scrub.NewMetrics(a.tele.metrics),
		Log:         log,
	})
	if err != nil {
		return err
	}
	a.scrub.set(s)
	a.tele.metrics.DBGauges().Add(scrub.OverdueGauge(db, conns))
	return nil
}

// blobDamaged writes system.storage.blob_damaged when a detection (a read, the scrub, its recheck, the reconciliation's source)
// turned a blob away from ok: no connection holds it whole any more (R-STOR-027). The store calls it after the mark's transaction
// committed, so the event is asynchronous; nil without a recorder.
func blobDamaged(rec audit.Recorder) func(ctx context.Context, d blob.Digest, status string) {
	if rec == nil {
		return nil
	}
	return func(ctx context.Context, d blob.Digest, status string) {
		details, err := json.Marshal(map[string]string{"sha256": d.Hex(), "status": status})
		if err != nil {
			return
		}
		rec.Record(ctx, audit.Event{
			Type: audit.EventStorageBlobDamaged, Outcome: audit.Warning, Reason: "content_" + status,
			Actor: audit.Actor{Kind: "system", Name: "replica"}, Details: details,
		})
	}
}

// storageAPI is the services of the list of damaged blobs and of the recheck of one blob (R-STOR-027): the list reads
// PostgreSQL, the recheck reads every copy of the blob through this replica's connections.
func (a *App) storageAPI() v1.StorageServices {
	db := a.database()
	if db == nil {
		return v1.StorageServices{}
	}
	return v1.StorageServices{
		Damaged: func(ctx context.Context, dq scrub.DamagedQuery) ([]scrub.DamagedBlob, error) {
			return scrub.ListDamaged(ctx, db, dq)
		},
		Recheck: func(ctx context.Context, d blob.Digest) ([]scrub.CopyRow, error) {
			s, ok := a.scrub.get()
			if !ok {
				return nil, errs.E(errs.Unavailable, errs.CodeUnavailable, "The recheck of a blob is not available on this replica yet.")
			}
			return s.Recheck(ctx, d)
		},
		Connections: a.storageConnections,
		Summary:     a.storageSummary,
		Overview:    a.storageOverview,
	}
}
