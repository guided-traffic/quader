package app

import (
	"context"
	"time"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/formathost"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/outbound"
	"github.com/guided-traffic/quader/internal/qname"
	sgate "github.com/guided-traffic/quader/internal/signatures/gate"
	"github.com/guided-traffic/quader/internal/signatures/job"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	sigstore "github.com/guided-traffic/quader/internal/signatures/store"
)

// AreaSignatures is the store and the jobs of signatures on artifacts (signatures.md): every role builds the store, because every role
// records or reads signatures: the format host and /files/ record what a commit carries (R-SIG-011), the management API reads and
// writes anchors, blocks and the switch (R-SIG-034), the worker runs the jobs signature-verify, signature-trust and the task
// signature-trust-root (R-SIG-011, R-SIG-012, R-SIG-017). Building it also links the replay of the signatures' journal entries into the
// binary (R-SIG-041).
const AreaSignatures Area = "signatures"

// The default schedules of the two tasks (R-SIG-011, R-SIG-017).
const (
	signatureVerifySchedule    = "*/5 * * * *"
	signatureTrustRootSchedule = "17 4 * * *"
)

// signaturesArea is the result of the signatures' step.
type signaturesArea struct {
	// store is nil while signatures.enabled is false: nothing is detected, checked or shown, and the stored rows stay (R-SIG-040).
	store *sigstore.Store
	// job is the jobs on a role that runs jobs, metrics their metrics (also the counter the background fetch reports to).
	job     *job.Job
	metrics *job.Metrics
	// gate is the gate per repository of stage 3 (R-SIG-064), which the format host and /files/ ask; nil while the signatures are off.
	gate *sgate.Gate
}

// signaturePorts is what the format data ports take of the signatures' configuration (R-SIG-040): the bounds of the store an
// Ansible post records through (R-SIG-031), whether a record wakes the job signature-verify, and the refresh of the background fetch's
// queue (R-SIG-026), and Quader's npm signing the npm data port reaches (R-SIG-050). The zero value is the defaults without a wake-up
// and without signing (the restore's ports).
type signaturePorts struct {
	Enabled       bool
	MaxObjectSize int64
	MaxPerSubject int
	RefreshAfter  time.Duration
	// NPMSigning is the keys of a local npm repository and the work of a publish; nil: no keys, no work.
	NPMSigning *npmSigningPort
}

func (a *App) signaturePorts() signaturePorts {
	cfg := a.cfg.Signatures
	return signaturePorts{
		Enabled: cfg.Enabled, MaxObjectSize: int64(cfg.MaxObjectSize), MaxPerSubject: cfg.MaxPerSubject, RefreshAfter: cfg.Remote.RefreshAfter,
		NPMSigning: &npmSigningPort{a: a},
	}
}

// storeOptions are the options of a signature store a data port records through.
func (p signaturePorts) storeOptions(db meta.Runner) sigstore.Options {
	o := sigstore.Options{Runner: db, MaxObjectSize: p.MaxObjectSize, MaxPerSubject: p.MaxPerSubject}
	if p.Enabled {
		o.Wake = job.WakeVerify
	}
	return o
}

// wireSignatures builds the store with the bounds and the wake-ups of the configuration, and the jobs on a role that runs jobs. The
// discovery of items stored before stage 1 runs through the format host's discoverer, bound at run time, because the format area is
// built after this one.
func (a *App) wireSignatures(ctx context.Context, in infra) (signaturesArea, error) {
	cfg := a.cfg.Signatures
	if !cfg.Enabled || in.db == nil {
		return signaturesArea{}, nil
	}
	var blobs scheme.Blobs
	if in.blobs != nil {
		blobs = job.BlobSource(in.blobs)
	}
	so := sigstore.Options{
		Runner: in.db, MaxObjectSize: int64(cfg.MaxObjectSize), MaxPerSubject: cfg.MaxPerSubject, Wake: job.WakeVerify,
		WakeTrust: job.WakeTrust, Audit: in.rec, Blobs: blobs,
	}
	if in.changes != nil {
		so.Changes = in.changes
	}
	out := signaturesArea{store: sigstore.New(so)}
	g, err := a.newSignatureGate(ctx, in, out.store)
	if err != nil {
		return signaturesArea{}, err
	}
	out.gate = g
	if !a.sub.JobLoops {
		return out, nil
	}
	m, err := job.NewMetrics(a.tele.metrics.Registry())
	if err != nil {
		return signaturesArea{}, err
	}
	out.metrics = m
	o := job.Options{
		DB: in.db, Store: out.store, Discoverer: lateDiscoverer{a: a}, Blobs: blobs, Enqueue: job.WakeVerify, EnqueueTrust: job.WakeTrust,
		Concurrency: cfg.Concurrency, RunSlice: cfg.RunSlice, MaxAttempts: cfg.MaxAttempts, CheckTimeout: cfg.CheckTimeout,
		Limits: scheme.Limits{
			MaxObjectSize: int64(cfg.MaxObjectSize), MaxChain: cfg.MaxChain, MaxContentSize: int64(cfg.MaxContentSize),
		},
		TrustRoot: job.TrustRootOptions{Refresh: cfg.TrustRoot.Refresh, URL: cfg.TrustRoot.URL},
		// Stage 3: the refused downloads' retention, and the signal a download waiting at a gate hears (R-SIG-067, R-SIG-073).
		RefusalsRetention: cfg.Gate.RefusalsRetention,
		Signal:            signatureVerdictSignal(in.signals), SignalAll: signatureVerdictSignalAll(in.signals),
		Metrics: m, Log: a.log.With("component", "signatures-job"),
	}
	if in.out != nil {
		o.Client = in.out.Client(outbound.PurposeSignatureTrustRoot)
	}
	if out.job, err = job.New(o); err != nil {
		return signaturesArea{}, err
	}
	return out, nil
}

// lateDiscoverer finds the format host's discoverer when a run asks for it: the format area is built after the signatures' area.
type lateDiscoverer struct{ a *App }

func (l lateDiscoverer) Discover(ctx context.Context, after int64, limit int) (int64, error) {
	fa := part[formatsArea](l.a.parts)
	d := fa.discoverer
	if d == nil {
		return after, nil
	}
	return d.Discover(ctx, after, limit)
}

// registerSignatureJobs registers the kinds signature-verify, signature-trust and signature-trust-root and the two tasks (R-SIG-011,
// R-SIG-012, R-SIG-017) on the job registry, which every role builds. The handlers find the jobs at run time; a role that builds none
// answers the run as skipped.
func (a *App) registerSignatureJobs(r *jobs.Registry) {
	handler := func(run func(j *job.Job, ctx context.Context, r *jobs.Run) error) jobs.Handler {
		return func(ctx context.Context, rr *jobs.Run) error {
			j := part[signaturesArea](a.parts).job
			if j == nil {
				return rr.SetResult(map[string]string{"skipped": "the signatures do not run on this replica"})
			}
			return run(j, ctx, rr)
		}
	}
	for _, k := range []struct {
		kind string
		spec jobs.KindSpec
		run  func(j *job.Job, ctx context.Context, r *jobs.Run) error
	}{
		{job.KindVerify, job.VerifyKindSpec(), func(j *job.Job, ctx context.Context, r *jobs.Run) error { return j.RunVerify(ctx, r) }},
		{job.KindTrust, job.TrustKindSpec(), func(j *job.Job, ctx context.Context, r *jobs.Run) error { return j.RunTrust(ctx, r) }},
		{job.KindTrustRoot, job.TrustRootKindSpec(), func(j *job.Job, ctx context.Context, r *jobs.Run) error { return j.RunTrustRoot(ctx, r) }},
	} {
		spec := k.spec
		spec.Handler, spec.MaxAttempts = handler(k.run), 3
		r.RegisterKind(k.kind, spec)
	}
	on := a.cfg.Signatures.Enabled
	for _, t := range []struct{ kind, schedule string }{
		{job.KindVerify, signatureVerifySchedule}, {job.KindTrustRoot, signatureTrustRootSchedule},
	} {
		task := jobs.TaskSpec{Schedule: t.schedule, Disabled: !on}
		if c, ok := a.cfg.Maintenance.Tasks[t.kind]; ok && c.Schedule != "" {
			task.Schedule, task.Disabled = c.Schedule, !c.Enabled || !on
		}
		r.RegisterTask(t.kind, task)
	}
}

// signaturesAPI is the services of the signatures' operations of the management API (signatures_api.go).
func (a *App) signaturesAPI() v1.SignatureServices {
	sg := part[signaturesArea](a.parts)
	if sg.store == nil {
		return v1.SignatureServices{}
	}
	out := v1.SignatureServices{
		Store: sg.store, Enabled: a.cfg.Signatures.Enabled, Repos: part[repositoryArea](a.parts).reader, Coverage: a.signatureCoverage,
	}
	if az := part[authzArea](a.parts); az.engine != nil {
		out.Decider = az.engine
	}
	return out
}

// signatureCoverage answers the signed documents of a repository that name a stored file through the format's coverage
// (formats.SignatureCoverage, Debian's releases, R-SIG-030) with the format's data port; a format without it covers nothing.
func (a *App) signatureCoverage(ctx context.Context, repoID int64, repo qname.Repo, format, class, path string, sha256 [32]byte) ([]v1.SignatureCoveringDocument, error) {
	sc := formats.CapabilitiesFor(formats.ID(format)).SignatureCoverage
	b := part[formatPortsArea](a.parts).bindings
	if sc == nil || b == nil {
		return nil, nil
	}
	ref := formats.RepoRef{ID: repoID, Name: repo, Class: formats.Class(class), Format: formats.ID(format)}
	docs, err := sc.Covering(ctx, b.Get(formats.ID(format)), ref, path, sha256)
	if err != nil {
		return nil, err
	}
	out := make([]v1.SignatureCoveringDocument, len(docs))
	for i, d := range docs {
		out[i] = v1.SignatureCoveringDocument{Path: d.Path, SHA256: d.SHA256, Name: d.Name, Date: d.Date}
	}
	return out, nil
}

// signatureVerdicts is the trust step as a format asks it before it serves a signature (formats.SignatureVerdicts, R-SIG-028): an npm
// local repository serves a provenance bundle only when the stored verdicts say npm would verify it. It answers nothing while the
// signatures are off; the host then vouches for nothing (formats.NoSignatureVerdicts).
func (a *App) signatureVerdicts() formathost.ProvenanceVerdicts {
	sg := part[signaturesArea](a.parts)
	if sg.store == nil {
		return nil
	}
	return storeVerdicts{st: sg.store}
}

// storeVerdicts answers the provenance verdicts of formats.SignatureVerdicts from the signature store.
type storeVerdicts struct{ st *sigstore.Store }

var _ formathost.ProvenanceVerdicts = storeVerdicts{}

// PublicProvenance implements [formathost.ProvenanceVerdicts].
func (v storeVerdicts) PublicProvenance(ctx context.Context, repo formats.RepoRef, paths []string) (map[string]string, error) {
	return v.st.PublicProvenance(ctx, repo.ID, paths)
}
