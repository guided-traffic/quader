package app

import (
	"context"
	"log/slog"
	"time"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	npmdata "github.com/guided-traffic/quader/internal/formatdata/npm"
	"github.com/guided-traffic/quader/internal/formats/npm/rows"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	sigstore "github.com/guided-traffic/quader/internal/signatures/store"
	"github.com/guided-traffic/quader/internal/signing"
	"github.com/guided-traffic/quader/internal/signing/job"
	"github.com/guided-traffic/quader/internal/signing/npmsign"
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
	npmKind, err := npmSigningKind(in, st, log)
	if err != nil {
		return signingArea{}, err
	}
	attestation, err := a.attestationKind(in)
	if err != nil {
		return signingArea{}, err
	}
	out.job, err = job.New(job.Options{
		DB: db, Store: st, Keys: out.keys, Kinds: []job.Kind{npmKind, attestation},
		Enabled: cfg.Enabled, Concurrency: cfg.Concurrency, MaxAttempts: cfg.MaxAttempts,
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

// npmSigningKind is the kind npm of the job signing (R-SIG-050): npm's registry signature of the versions of local repositories, its
// discovery and the revision raise of R-SIG-048. Its signatures are written through a signature store of their own, which needs
// nothing of stage 1's switch: signing.enabled decides whether Quader signs (Q-SIG-161). The key reaches the signer as the session's
// crypto.Signer, inside the job alone (R-SIG-049).
func npmSigningKind(in infra, st *signingstore.Store, log *slog.Logger) (job.Kind, error) {
	o := npmsign.Options{DB: in.db, Signatures: sigstore.New(sigstore.Options{Runner: in.db}), Work: st, Log: log}
	if in.signals != nil { // a nil *cluster.Signals in the interface would not be nil
		o.Signals = in.signals
	}
	ns, err := npmsign.New(o)
	if err != nil {
		return job.Kind{}, err
	}
	return job.Kind{
		Name: signing.WorkNPM,
		Sign: func(ctx context.Context, tx meta.Tx, s job.Session, w signingstore.Work) error {
			return ns.Sign(ctx, tx, s.Identity, s.Key.Signer(), w)
		},
		Discover: ns.Discover,
		Served:   ns.Served,
	}, nil
}

// npmSigningPort is Quader's npm signing as the npm data port reaches it (R-SIG-049, R-SIG-050): the keys of a local repository from
// the replica's snapshot of the identities, and the work a publish writes. The format ports are wired before the signing area, so the
// port finds the area when it is called; a replica without it answers no keys and writes no work.
type npmSigningPort struct{ a *App }

var _ npmdata.RegistrySigning = npmSigningPort{}

// Keys implements [npmdata.RegistrySigning]: signing.NPMKeys over the snapshot, none while no identity is current.
func (p npmSigningPort) Keys(ctx context.Context) ([]rows.RegistryKey, error) {
	sg := part[signingArea](p.a.parts)
	if sg.snapshot == nil {
		return []rows.RegistryKey{}, nil
	}
	ids, err := sg.snapshot.Identities(ctx)
	if err != nil {
		return nil, err
	}
	keys := signing.NPMKeys(ids)
	out := make([]rows.RegistryKey, 0, len(keys))
	for _, k := range keys {
		out = append(out, rows.RegistryKey{KeyID: k.KeyID, KeyType: k.KeyType, Scheme: k.Scheme, Key: k.Key, Expires: k.ExpiresText()})
	}
	return out, nil
}

// Published implements [npmdata.RegistrySigning]: the work of a publish at priority 0, written while signing is on and an identity is
// current (Q-SIG-158); otherwise discovery signs the version later.
func (p npmSigningPort) Published(ctx context.Context, tx meta.Tx, itemID int64) error {
	sg := part[signingArea](p.a.parts)
	if sg.store == nil || !p.a.cfg.Signing.Enabled {
		return nil
	}
	return sg.store.EnqueueWhileCurrent(ctx, tx, signing.WorkNPM, signing.ReasonUpload, signing.PriorityPublish, []int64{itemID})
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
		Store: sg.store, Snapshot: sg.snapshot, Attestations: sg.store, Enabled: a.cfg.Signing.Enabled, Managed: a.cfg.Signing.Identity.Files(),
		AnnouncePeriod: a.cfg.Signing.Identity.AnnouncePeriod,
	}
}
