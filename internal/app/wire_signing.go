package app

import (
	"context"
	"time"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/signing/job"
	signingstore "github.com/guided-traffic/quader/internal/signing/store"
)

// AreaSigning is Quader's own signing identity and the job signing (signatures.md R-SIG-045 to R-SIG-049, R-SIG-055, R-SIG-057 to
// R-SIG-059): every role builds the store and the snapshot of the identities, because the management API writes the identities and every
// replica answers the public parts (the trusted root, the chain, later npm's keys). Only a role that runs jobs (worker, all) builds the
// key holder, the watcher of the files of the Helm values and the job: no other role holds or opens a private key (R-SIG-049). Building
// the store also links the replay of the signing's journal entries into the binary (R-SIG-059).
const AreaSigning Area = "signing"

// signingArea is the result of the signing's step.
type signingArea struct {
	store    *signingstore.Store
	snapshot *signingstore.Snapshot
	// keys, watcher and job are nil on a role that runs no jobs.
	keys    *job.Holder
	watcher *job.Watcher
	job     *job.Job
}

// wireSigning builds the store and the snapshot on every role with the database, and on a role that runs jobs the key holder, the job and
// the watcher, which looks at the identity's files once before the replica serves.
func (a *App) wireSigning(ctx context.Context, in infra) (signingArea, error) {
	if in.db == nil {
		return signingArea{}, nil
	}
	cfg := a.cfg.Signing
	so := signingstore.Options{
		Runner: in.db, Ring: in.ring, Audit: in.rec, Wake: job.Wake, AnnouncePeriod: cfg.Identity.AnnouncePeriod,
		BaseURL: a.cfg.Server.BaseURL,
	}
	if in.changes != nil {
		so.Changes = in.changes
	}
	st := signingstore.New(so)
	out := signingArea{store: st, snapshot: signingstore.NewSnapshot(st, in.caches)}
	if !a.sub.JobLoops {
		return out, nil
	}
	m, err := job.NewMetrics(a.tele.metrics.Registry())
	if err != nil {
		return signingArea{}, err
	}
	log := a.log.With("component", "signing")
	out.keys = job.NewHolder(in.db, in.ring)
	db := in.db
	out.job, err = job.New(job.Options{
		DB: db, Store: st, Keys: out.keys, Enabled: cfg.Enabled, Concurrency: cfg.Concurrency, MaxAttempts: cfg.MaxAttempts,
		Wake: func(ctx context.Context, delay time.Duration) error {
			return db.InTx(ctx, func(tx meta.Tx) error { return job.WakeAfter(ctx, tx, delay) })
		},
		Audit: in.rec, Metrics: m, Log: log,
	})
	if err != nil {
		return signingArea{}, err
	}
	id := cfg.Identity
	out.watcher = job.NewWatcher(job.WatcherOptions{
		Store: st, Keys: out.keys, Metrics: m, Log: log,
		Files: job.Files{Cert: id.CertFile, Key: id.KeyFile, CA: id.CAFile, Interval: id.ReloadInterval},
	})
	if err := a.startComponent(ctx, out.watcher); err != nil {
		return signingArea{}, err
	}
	return out, nil
}

// registerSigningJobs registers the kind signing and its task (R-SIG-049) on the job registry, which every role builds. The handler
// finds the job at run time; a role that builds none answers the run as skipped.
func (a *App) registerSigningJobs(r *jobs.Registry) {
	spec := job.KindSpec()
	spec.MaxAttempts = 3
	spec.Handler = func(ctx context.Context, rr *jobs.Run) error {
		j := part[signingArea](a.parts).job
		if j == nil {
			return rr.SetResult(map[string]string{"skipped": "the signing does not run on this replica"})
		}
		return j.RunSigning(ctx, rr)
	}
	r.RegisterKind(job.KindSigning, spec)
	task := jobs.TaskSpec{Schedule: job.TaskSchedule}
	if c, ok := a.cfg.Maintenance.Tasks[job.KindSigning]; ok && c.Schedule != "" {
		task.Schedule, task.Disabled = c.Schedule, !c.Enabled
	}
	r.RegisterTask(job.KindSigning, task)
}

// signingAPI is the services of the signing identity's operations of the management API (signing_api.go).
func (a *App) signingAPI() v1.SigningServices {
	sg := part[signingArea](a.parts)
	if sg.store == nil {
		return v1.SigningServices{}
	}
	return v1.SigningServices{
		Store: sg.store, Snapshot: sg.snapshot, Enabled: a.cfg.Signing.Enabled, Managed: a.cfg.Signing.Identity.Files(),
		AnnouncePeriod: a.cfg.Signing.Identity.AnnouncePeriod,
	}
}
