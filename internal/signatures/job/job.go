// Package job is the work of signatures on artifacts on the queue signatures (signatures.md R-SIG-011, R-SIG-012, R-SIG-017,
// R-SIG-039): the job signature-verify (discovery of the items stored without attachments, the orphan removal, the daily retry of failed
// signatures, and the read and the integrity check of every due signature, each with its trust rows computed at once), the job
// signature-trust (the trust rows computed under an older anchor epoch, or missing, recomputed from the stored fields without reading
// any content), and the task signature-trust-root (the public Sigstore root and npm's keys refreshed from the TUF repository).
//
// One verify job and one trust job run at a time in the installation (coalesce). The verify job keeps signatures.concurrency checks in
// flight, gives its slot back after signatures.run_slice and enqueues the rest. Every write goes through the store
// (internal/signatures/store), which records the events.
package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"time"

	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/all"
	"github.com/guided-traffic/quader/internal/signatures/shipped"
	"github.com/guided-traffic/quader/internal/signatures/store"
	"github.com/guided-traffic/quader/internal/signatures/trust"
)

// The working bounds of the jobs.
const (
	// batch is the size of a batch of discovery, of the orphan removal and of the trust step (R-SIG-011, R-SIG-012).
	batch = 10000
	// firstBackoff is the wait before the second attempt of a check that could not run; it doubles per attempt up to maxBackoff
	// (R-SIG-011).
	firstBackoff = time.Minute
	maxBackoff   = 6 * time.Hour
	// discoveryPass is how often discovery starts again from the first item once it reached the last one.
	discoveryPass = 24 * time.Hour
	// maxEvidence is signatures.evidence's bound: evidence above it is not kept, and the trust step then knows no signer of it.
	maxEvidence = 512 << 10
)

// Run is the part of a job's run handle the jobs use. *jobs.Run implements it.
type Run interface {
	SetResult(v any) error
}

var _ Run = (*jobs.Run)(nil)

// Discoverer is the port of discovery (R-SIG-011): it asks the formats' detectors for the signatures of the items after the cursor
// (in item id order, at most limit items) and records what they name through the store, and answers the last item id it looked at
// (afterItemID when there is none after it). The format host implements it.
type Discoverer interface {
	Discover(ctx context.Context, afterItemID int64, limit int) (lastItemID int64, err error)
}

// Store is what the jobs need of the store; *store.Store implements it.
type Store interface {
	DueChecks(ctx context.Context, limit int, inFlight []int64) ([]store.Check, error)
	WriteCheck(ctx context.Context, tx meta.Tx, id int64, r signatures.Result, receivedAt time.Time) (signatures.State, bool, error)
	Defer(ctx context.Context, tx meta.Tx, id int64, spend int, notBefore time.Time, due bool, state signatures.State, reason string) error
	AddParts(ctx context.Context, tx meta.Tx, id int64, parts int) (int64, error)
	RecordInvalid(ctx context.Context, tx meta.Tx, id int64) error
	UpstreamKeys(ctx context.Context, signatureIDs []int64) (map[int64]scheme.Keys, error)
	RemoveOrphans(ctx context.Context, tx meta.Tx, limit int) (int64, int64, error)
	Discovery(ctx context.Context) (int64, time.Time, error)
	SetDiscovery(ctx context.Context, after int64, restart bool) error
	RetryFailed(ctx context.Context) (int64, error)
	Stats(ctx context.Context) (store.Stats, error)

	TrustMaterial(ctx context.Context) (store.TrustMaterial, error)
	TrustBatch(ctx context.Context, tx meta.Tx, afterID, epoch int64, limit int) ([]trust.Signature, error)
	TrustOf(ctx context.Context, tx meta.Tx, ids []int64) ([]trust.Signature, error)
	ReachingProjects(ctx context.Context, tx meta.Tx, signatureIDs []int64, projects []string) (map[int64][]string, error)
	WriteTrust(ctx context.Context, tx meta.Tx, epoch int64, signatureIDs []int64, rows []store.TrustRow) error
	DeleteTrust(ctx context.Context, tx meta.Tx, signatureIDs []int64) error
	SetIntegrity(ctx context.Context, tx meta.Tx, id int64, state signatures.State, reason string) (bool, error)
	DropOtherTrust(ctx context.Context, limit int) (int64, error)
	TrustPending(ctx context.Context) (bool, error)

	StoreTrustRoot(ctx context.Context, r shipped.Refreshed) (bool, error)
	TrustRootFailed(ctx context.Context, code, detail string) error

	ExpireRefusals(ctx context.Context, tx meta.Tx, retention time.Duration) (int64, error)
}

var _ Store = (*store.Store)(nil)

// TrustRootOptions are signatures.trust_root (R-SIG-040): whether the task refreshes, and the TUF repository it reads.
type TrustRootOptions struct {
	Refresh bool
	URL     string
}

// Options configure [New].
type Options struct {
	DB    meta.Runner
	Store Store
	// Discoverer runs discovery; nil skips it.
	Discoverer Discoverer
	// Blobs reads the stored blobs a check reads (an OCI layer, the subject, the content).
	Blobs scheme.Blobs
	// Enqueue wakes the verify job in a transaction (the continuation after the slice); EnqueueTrust wakes the trust job.
	Enqueue      func(ctx context.Context, tx meta.Tx) error
	EnqueueTrust func(ctx context.Context, tx meta.Tx) error
	// Concurrency is signatures.concurrency, RunSlice signatures.run_slice, MaxAttempts signatures.max_attempts, CheckTimeout
	// signatures.check_timeout, Limits the bounds of R-SIG-013 (zero values are their defaults).
	Concurrency  int
	RunSlice     time.Duration
	MaxAttempts  int
	CheckTimeout time.Duration
	Limits       scheme.Limits
	TrustRoot    TrustRootOptions
	// Client fetches the TUF repository through the outbound client and its egress rules.
	Client *http.Client
	// RefusalsRetention is signatures.gate.refusals_retention: discovery removes the refused downloads of stage 3 older than it
	// (R-SIG-073); zero keeps them.
	RefusalsRetention time.Duration
	// Signal, when set, is told the subject of a signature whose verdict a run committed, and SignalAll that a batch of the trust step
	// committed: the signal area signature_verdict that wakes a download waiting at a repository's gate (stage 3, R-SIG-067).
	Signal    func(subject [32]byte)
	SignalAll func()
	Metrics   *Metrics
	Log       *slog.Logger
	Now       func() time.Time
}

// Job is the jobs of signatures on artifacts.
type Job struct {
	o Options
	// fetch reads the next due checks, check runs one, and refresh reads the TUF repository; a test replaces them.
	fetch   func(ctx context.Context, limit int, inFlight []int64) ([]store.Check, error)
	check   func(ctx context.Context, in scheme.Input, timeout time.Duration) (scheme.Output, error)
	refresh func(ctx context.Context, o shipped.RefreshOptions) (shipped.Refreshed, error)
}

// New returns the jobs.
func New(o Options) (*Job, error) {
	if o.DB == nil || o.Store == nil {
		return nil, errors.New("signatures job: the database and the store are required")
	}
	if o.Concurrency < 1 {
		o.Concurrency = 4
	}
	if o.MaxAttempts < 1 {
		o.MaxAttempts = 5
	}
	if o.CheckTimeout <= 0 {
		o.CheckTimeout = 5 * time.Second
	}
	if o.Limits == (scheme.Limits{}) {
		o.Limits = scheme.DefaultLimits()
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	j := &Job{o: o, fetch: o.Store.DueChecks, check: all.Check, refresh: shipped.Refresh}
	return j, nil
}

// Backoff is the wait after the attempt-th attempt of a check that could not run: a minute, doubling, at most six hours (R-SIG-011).
func Backoff(attempt int) time.Duration {
	d := firstBackoff
	for i := 1; i < attempt && d < maxBackoff; i++ {
		d *= 2
	}
	return min(d, maxBackoff)
}

// byOrder orders checks as R-SIG-011 orders the due rows: by priority, then not_before, then id.
func byOrder(cs []store.Check) {
	sort.SliceStable(cs, func(a, b int) bool {
		x, y := cs[a].Signature, cs[b].Signature
		if x.Priority != y.Priority {
			return x.Priority < y.Priority
		}
		if !x.NotBefore.Equal(y.NotBefore) {
			return x.NotBefore.Before(y.NotBefore)
		}
		return x.ID < y.ID
	})
}

// RunVerify is the handler of signature-verify (R-SIG-011): discovery, the orphan removal, the daily retry of failed signatures, then
// the due checks by their order until nothing is due or the slice ended, the rest in a job of its own. It wakes the trust job when the
// trust step has work (after a restore every trust row is missing, R-SIG-041).
func (j *Job) RunVerify(ctx context.Context, run Run) error {
	deadline := j.deadline()
	more := false
	discovered, err := j.discover(ctx, deadline)
	if err != nil {
		return err
	}
	more = more || discovered
	orphans, err := j.removeOrphans(ctx)
	if err != nil {
		return err
	}
	retried, err := j.o.Store.RetryFailed(ctx)
	if err != nil {
		return fmt.Errorf("the daily retry of failed signatures: %w", err)
	}
	if err := j.expireRefusals(ctx); err != nil {
		return err
	}
	sc, err := j.scopes(ctx)
	if err != nil {
		return err
	}
	w := &work{j: j, sc: sc}
	done, left, err := w.loop(ctx, deadline)
	j.gauges(ctx)
	if err != nil {
		return err
	}
	more = more || left
	result := map[string]any{"checked": done, "orphans": orphans, "retried": retried}
	if more && j.o.Enqueue != nil {
		if err := j.o.DB.InTx(ctx, func(tx meta.Tx) error { return j.o.Enqueue(ctx, tx) }); err != nil {
			return err
		}
		result["continued"] = true
	}
	if pending, err := j.o.Store.TrustPending(ctx); err == nil && pending && j.o.EnqueueTrust != nil {
		if err := j.o.DB.InTx(ctx, func(tx meta.Tx) error { return j.o.EnqueueTrust(ctx, tx) }); err != nil {
			return err
		}
	}
	return run.SetResult(result)
}

func (j *Job) deadline() time.Time {
	if j.o.RunSlice <= 0 {
		return time.Time{}
	}
	return j.o.Now().Add(j.o.RunSlice)
}

func (j *Job) past(deadline time.Time) bool {
	return !deadline.IsZero() && !j.o.Now().Before(deadline)
}

// discover asks the formats for the signatures of the items after the cursor, in batches of 10,000 in item order, and keeps the cursor
// (R-SIG-011). A pass that reached the last item starts again from the first once a day, so that a row removed is found again. It
// reports whether it stopped at the deadline with items left.
func (j *Job) discover(ctx context.Context, deadline time.Time) (bool, error) {
	if j.o.Discoverer == nil {
		return false, nil
	}
	after, passStarted, err := j.o.Store.Discovery(ctx)
	if err != nil {
		return false, fmt.Errorf("discovery's cursor: %w", err)
	}
	for {
		if ctx.Err() != nil || j.past(deadline) {
			return true, ctx.Err()
		}
		last, err := j.o.Discoverer.Discover(ctx, after, batch)
		if err != nil {
			return false, fmt.Errorf("the discovery of signatures: %w", err)
		}
		if last <= after {
			if after > 0 && j.o.Now().Sub(passStarted) >= discoveryPass {
				if err := j.o.Store.SetDiscovery(ctx, 0, true); err != nil {
					return false, fmt.Errorf("discovery's cursor: %w", err)
				}
			}
			return false, nil
		}
		if err := j.o.Store.SetDiscovery(ctx, last, false); err != nil {
			return false, fmt.Errorf("discovery's cursor: %w", err)
		}
		after = last
	}
}

// expireRefusals removes the refused downloads of stage 3 whose last refusal is older than signatures.gate.refusals_retention (R-SIG-073).
func (j *Job) expireRefusals(ctx context.Context) error {
	if j.o.RefusalsRetention <= 0 {
		return nil
	}
	return j.o.DB.InTx(ctx, func(tx meta.Tx) error {
		if _, err := j.o.Store.ExpireRefusals(ctx, tx, j.o.RefusalsRetention); err != nil {
			return fmt.Errorf("the removal of old refused downloads: %w", err)
		}
		return nil
	})
}

// signal tells a waiting download of a gate that a verdict of its subject committed.
func (j *Job) signal(subject [32]byte) {
	if j.o.Signal != nil {
		j.o.Signal(subject)
	}
}

// removeOrphans removes the signatures without attachments and the objects without signatures, in batches (R-SIG-011).
func (j *Job) removeOrphans(ctx context.Context) (int64, error) {
	var total int64
	for {
		var sigs, objects int64
		if err := j.o.DB.InTx(ctx, func(tx meta.Tx) (err error) {
			sigs, objects, err = j.o.Store.RemoveOrphans(ctx, tx, batch)
			return err
		}); err != nil {
			return total, fmt.Errorf("the removal of orphan signatures: %w", err)
		}
		total += sigs
		if sigs < batch && objects < batch {
			return total, nil
		}
	}
}

// work is one run's loop over the due checks.
type work struct {
	j  *Job
	sc *scopes
}

type finished struct {
	id  int64
	err error
}

// loop keeps up to signatures.concurrency checks in flight until nothing is due or the slice ended. It returns the checks it ran and
// whether work is left.
func (w *work) loop(ctx context.Context, deadline time.Time) (int64, bool, error) {
	j := w.j
	inFlight := map[int64]bool{}
	results := make(chan finished, j.o.Concurrency)
	var done int64
	var firstErr error
	more := false
	for {
		taking := firstErr == nil && ctx.Err() == nil && !j.past(deadline)
		launched := 0
		if taking && len(inFlight) < j.o.Concurrency {
			ids := make([]int64, 0, len(inFlight))
			for id := range inFlight {
				ids = append(ids, id)
			}
			slices.Sort(ids)
			rows, err := j.fetch(ctx, j.o.Concurrency-len(inFlight), ids)
			if err != nil {
				firstErr = err
			}
			byOrder(rows)
			for _, c := range rows {
				if inFlight[c.Signature.ID] || len(inFlight) >= j.o.Concurrency {
					continue
				}
				inFlight[c.Signature.ID] = true
				launched++
				go func() { results <- finished{id: c.Signature.ID, err: w.one(ctx, c)} }()
			}
		}
		if !taking && j.past(deadline) {
			more = true
		}
		if len(inFlight) == 0 && (launched == 0 || !taking) {
			return done, more, firstErr
		}
		select {
		case f := <-results:
			delete(inFlight, f.id)
			done++
			if f.err != nil && firstErr == nil {
				firstErr = f.err
			}
		case <-ctx.Done():
			for len(inFlight) > 0 {
				f := <-results
				delete(inFlight, f.id)
			}
			return done, true, context.Cause(ctx)
		}
	}
}

// waitsForSubject reports whether a check cannot run yet because its subject is not stored: every signature but npm's registry
// signature, whose subject is the digest of its message (a row with a subject name and a signed digest, Q-SIG-50).
func waitsForSubject(c store.Check) bool {
	if c.SubjectStored {
		return false
	}
	return c.Signature.SubjectName == "" || len(c.Signature.SignedDigest) == 0
}

// receivedAt is the earlier of the object's receipt and the subject blob's (R-SIG-007).
func receivedAt(c store.Check) time.Time {
	t := c.Object.FirstSeenAt
	if s := c.SubjectCreatedAt; c.SubjectStored && !s.IsZero() && (t.IsZero() || s.Before(t)) {
		t = s
	}
	return t
}

// one checks one signature and writes what it ended with. Only an error of the job's own (the database, the context) is returned.
func (w *work) one(ctx context.Context, c store.Check) error {
	j := w.j
	id := c.Signature.ID
	if waitsForSubject(c) {
		// Not due until the subject's commit makes it due (R-SIG-011, SubjectArrived).
		return j.o.DB.InTx(ctx, func(tx meta.Tx) error {
			return j.o.Store.Defer(ctx, tx, id, 0, time.Time{}, false, signatures.StatePending, signatures.ReasonSubjectMissing)
		})
	}
	in, err := w.input(ctx, c)
	if err != nil {
		return w.transient(ctx, c, err)
	}
	started := j.o.Now()
	out, err := j.check(ctx, in, j.o.CheckTimeout)
	if err != nil {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		j.o.Metrics.check(c.Object.Scheme, string(signatures.StateFailed), j.o.Now().Sub(started))
		return w.transient(ctx, c, err)
	}
	j.o.Metrics.check(c.Object.Scheme, string(out.Result.State), j.o.Now().Sub(started))
	if err := j.o.DB.InTx(ctx, func(tx meta.Tx) error { return w.write(ctx, tx, c, in.ReceivedAt, out) }); err != nil {
		return err
	}
	j.signal(c.Signature.Subject)
	return nil
}

// input is the check's input: the row, its object and subject, and the keys and roots of every scope (R-SIG-006), with the keys the
// upstreams of the repositories that attach it announce.
func (w *work) input(ctx context.Context, c store.Check) (scheme.Input, error) {
	j := w.j
	s, o := c.Signature, c.Object
	in := scheme.Input{
		ID: s.ID, Scheme: o.Scheme, Role: s.Role, MediaType: o.MediaType, Part: s.Part,
		ObjectSHA256: o.SHA256, Raw: o.Raw, ItemBlob: o.ItemBlob, Locator: o.Locator, ObjectSize: o.Size,
		Subject: s.Subject, SubjectName: s.SubjectName,
		ContentBlob: s.ContentBlob, ContentLocator: s.ContentLocator, SignedDigest: s.SignedDigest, DigestAlg: s.DigestAlg,
		ReceivedAt: receivedAt(c), Now: j.o.Now(), Blobs: j.o.Blobs, Keys: w.sc.keys, Roots: w.sc.roots, Limits: j.o.Limits,
	}
	if c.SubjectStored {
		in.SubjectSHA512, in.SubjectSize = c.SubjectSHA512, c.SubjectSize
	}
	up, err := j.o.Store.UpstreamKeys(ctx, []int64{s.ID})
	if err != nil {
		return in, err
	}
	if k, ok := up[s.ID]; ok {
		in.Keys = scheme.Keys{
			OpenPGP: append(slices.Clone(in.Keys.OpenPGP), k.OpenPGP...),
			Public:  append(slices.Clone(in.Keys.Public), k.Public...),
		}
	}
	return in, nil
}

// transient spends an attempt of a check that could not run: due again after the back-off, failed after the last (R-SIG-011).
func (w *work) transient(ctx context.Context, c store.Check, cause error) error {
	j := w.j
	attempts := c.Signature.Attempts + 1
	j.o.Log.WarnContext(ctx, "a signature check could not run", "signature", c.Signature.ID, "scheme", c.Object.Scheme,
		"attempt", attempts, "error", cause)
	if err := j.o.DB.InTx(ctx, func(tx meta.Tx) error {
		if attempts >= j.o.MaxAttempts {
			return j.o.Store.Defer(ctx, tx, c.Signature.ID, 1, time.Time{}, false, signatures.StateFailed, "")
		}
		return j.o.Store.Defer(ctx, tx, c.Signature.ID, 1, j.o.Now().Add(Backoff(attempts)), true, signatures.StatePending, "")
	}); err != nil {
		return err
	}
	if attempts >= j.o.MaxAttempts {
		j.signal(c.Signature.Subject) // failed: a gate refuses it as not_checked now
	}
	return nil
}

// write stores a check's verdict with its evidence, adds the rows of the object's other parts, computes the signature's trust rows at
// once under the run's epoch, and records signature.invalid when the state became invalid.
func (w *work) write(ctx context.Context, tx meta.Tx, c store.Check, received time.Time, out scheme.Output) error {
	j := w.j
	id := c.Signature.ID
	r := out.Result
	if out.Evidence != nil && len(r.Evidence) == 0 {
		if b, err := json.Marshal(out.Evidence); err == nil {
			r.Evidence = b
		}
	}
	if len(r.Evidence) > maxEvidence {
		j.o.Log.WarnContext(ctx, "the evidence of a signature is larger than its bound and is not kept", "signature", id, "size", len(r.Evidence))
		r.Evidence = nil
	}
	before, ok, err := j.o.Store.WriteCheck(ctx, tx, id, r, received)
	if err != nil || !ok {
		return err
	}
	if out.Parts > 1 && c.Signature.Part == 0 {
		if _, err := j.o.Store.AddParts(ctx, tx, id, out.Parts); err != nil {
			return err
		}
	}
	if r.State == signatures.StateValid || r.State == signatures.StateNoKey {
		return applyTrust(ctx, tx, j.o.Store, w.sc, []int64{id}, nil)
	}
	if err := j.o.Store.DeleteTrust(ctx, tx, []int64{id}); err != nil {
		return err
	}
	if r.State == signatures.StateInvalid && before != signatures.StateInvalid {
		return j.o.Store.RecordInvalid(ctx, tx, id)
	}
	return nil
}

// gauges sets the worker's gauges after a run (R-SIG-039).
func (j *Job) gauges(ctx context.Context) {
	if j.o.Metrics == nil {
		return
	}
	st, err := j.o.Store.Stats(ctx)
	if err != nil {
		j.o.Log.WarnContext(ctx, "the gauges of signatures could not be read", "error", err)
		return
	}
	j.o.Metrics.set(gaugesOf(st, j.o.Now()))
}

func gaugesOf(st store.Stats, now time.Time) gauges {
	g := gauges{due: st.Due, oldest: st.OldestPending, invalid: st.Invalid, stale: st.Stale}
	fetched := st.RootFetchedAt
	if fetched.IsZero() {
		fetched = shipped.Release().BuiltAt
	}
	if !fetched.IsZero() {
		g.rootAge = now.Sub(fetched)
	}
	return g
}
