// Package fetch is the background fetch of a remote repository's signatures (docs/spec/signatures.md R-SIG-026, Q-SIG-6, Q-SIG-33):
// for content that came through a remote, Quader asks the upstream itself for what the format defines as its signatures, so that the
// badge is right even if no client ever asked.
//
// A format's data port queues a target ([Queue.Enqueue]) in the transaction that stored what asks for it (an OCI tag's manifest, an
// npm tarball), or as a refresh from a request that found a stored document unchanged. The job signature-fetch ([Handler]) runs it in
// the worker: it reads the repository's detail setting signatures.fetch, takes the upstream of the repository for one run (the
// background requests of a mirror's pass: the token handshake, the circuit, the background slots, the egress rules), bounds the
// requests by signatures.remote.requests_per_hour per remote and replica, and hands the upstream and the target to the format's
// [Fetcher]. What the format stores through the upstream is detected by the commit hook as a client's fill is (R-SIG-011).
//
// The package names no format: a format's remote adapter implements formats.SignatureFetcher, which satisfies [Fetcher].
package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// JobKind is the kind of the job that runs one target.
const JobKind = "signature-fetch"

// The defaults of signatures.remote (R-SIG-040, Q-SIG-45); the configuration that changes them belongs to the package of R-SIG-040.
const (
	// DefaultRefreshAfter is signatures.remote.refresh_after: a target is asked for again on a refresh when its last fetch is older.
	DefaultRefreshAfter = 24 * time.Hour
	// DefaultRequestsPerHour is signatures.remote.requests_per_hour: the upstream requests of the job per remote and replica.
	DefaultRequestsPerHour = 600
	// jobAttempts and jobTimeout bound one target's job: a run asks a handful of requests.
	jobAttempts = 5
	jobTimeout  = 5 * time.Minute
)

// Args are the arguments of the job.
type Args struct {
	RepoID int64  `json:"repoId"`
	Target string `json:"target"`
}

// Key is the dedup key of the job of a target.
func Key(repoID int64, target string) string {
	return JobKind + ":" + strconv.FormatInt(repoID, 10) + ":" + target
}

// Enqueuer is the job queue as the fetch queues through it; *jobs.Queue implements it.
type Enqueuer interface {
	Enqueue(ctx context.Context, tx meta.Tx, e jobs.EnqueueSpec) (jobs.Result, error)
}

// Options configure [NewQueue].
type Options struct {
	// DB runs the transaction of an enqueue that has none of the caller's.
	DB meta.Runner
	// Jobs is the job queue.
	Jobs Enqueuer
	// RefreshAfter is signatures.remote.refresh_after; 0 is the default.
	RefreshAfter time.Duration
}

// Queue queues the targets of the background fetch.
type Queue struct{ o Options }

// NewQueue creates the queue.
func NewQueue(o Options) *Queue {
	if o.RefreshAfter <= 0 {
		o.RefreshAfter = DefaultRefreshAfter
	}
	return &Queue{o: o}
}

// Enqueue queues the job of a target of a remote repository: a target never fetched once (a failed one included), and, with refresh,
// one whose last fetch is older than signatures.remote.refresh_after. A target that is queued already is not queued again. tx nil runs
// a transaction of its own. The format is the repository's, for the row; the job reads the repository again when it runs. subject is
// the digest of the item whose signatures the target fetches (the gate of stage 3 finds the row by it, R-SIG-067); the zero digest is
// a target of no item (npm's keys).
func (qu *Queue) Enqueue(ctx context.Context, tx meta.Tx, repoID int64, format, target string, subject [32]byte, refresh bool) error {
	if qu == nil || qu.o.Jobs == nil {
		return nil // a replica that has not wired the fetch queues nothing (the composition root wires it where the database is)
	}
	if target == "" || len(target) > 1024 || repoID <= 0 {
		return nil
	}
	if tx == nil {
		if qu.o.DB == nil {
			return errors.New("signatures: the fetch queue has no runner")
		}
		return qu.o.DB.InTx(ctx, func(tx meta.Tx) error { return qu.Enqueue(ctx, tx, repoID, format, target, subject, refresh) })
	}
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return errors.New("signatures: the fetch queue needs a PostgreSQL transaction")
	}
	var sub []byte
	if subject != ([32]byte{}) {
		sub = subject[:]
	}
	claimed, err := q.New(ptx).SignatureFetchQueue(ctx, q.SignatureFetchQueueParams{
		RepoID: repoID, Target: target, Format: format, Subject: sub, Refresh: refresh, RefreshAfterSeconds: qu.o.RefreshAfter.Seconds(),
	})
	if err != nil || len(claimed) == 0 {
		return err
	}
	_, err = qu.o.Jobs.Enqueue(ctx, tx, jobs.EnqueueSpec{
		Kind: JobKind, DedupKey: Key(repoID, target), Args: Args{RepoID: repoID, Target: target},
		TargetKind: "repository", TargetKey: strconv.FormatInt(repoID, 10),
	})
	return err
}

// requeue queues the job of a target again after a wait, while the run that asks for it still runs: the coalescing key keeps one
// waiting job beside it.
func (qu *Queue) requeue(ctx context.Context, a Args, after time.Duration) error {
	if qu == nil || qu.o.Jobs == nil || qu.o.DB == nil {
		return errors.New("signatures: the fetch queue is not wired")
	}
	return qu.o.DB.InTx(ctx, func(tx meta.Tx) error {
		_, err := qu.o.Jobs.Enqueue(ctx, tx, jobs.EnqueueSpec{
			Kind: JobKind, DedupKey: Key(a.RepoID, a.Target), Args: a, Delay: after,
			TargetKind: "repository", TargetKey: strconv.FormatInt(a.RepoID, 10),
		})
		return err
	})
}

// Fetcher is a format's fetch of a remote's signatures (formats.SignatureFetcher).
type Fetcher interface {
	FetchSignatures(ctx context.Context, u discover.Upstream, target string) error
}

// Background gives the job the upstream of a remote repository for one run (Host.BackgroundUpstream, Q-REPO-272).
type Background interface {
	Upstream(ctx context.Context, repoID int64, jobID string) (discover.Upstream, error)
}

// HandlerOptions configure [Handler].
type HandlerOptions struct {
	// Queue is the queue the job requeues a target through when the budget of the hour is spent.
	Queue *Queue
	// Background gives the upstream of a run.
	Background Background
	// Fetcher returns the fetch of a format; false for a format whose signatures come with what a client fetches anyway.
	Fetcher func(format string) (Fetcher, bool)
	// RequestsPerHour is signatures.remote.requests_per_hour; 0 is the default.
	RequestsPerHour int
	// Observe, when set, is told every request of a run with the format and "ok" or "error": the counter
	// quader_signatures_remote_requests_total of R-SIG-039, which the package of the metrics registers.
	Observe func(format, result string)
	// RunContext, when set, decorates the context a run hands the format's fetch: the composition root carries the bounds of R-SIG-040
	// (signatures.remote.max_manifests_per_subject, signatures.max_object_size) through it, which the format reads.
	RunContext func(context.Context) context.Context
	// Now is the clock of the budget; nil is time.Now.
	Now func() time.Time
}

// errBudget is the end of a run whose budget of requests for the hour is spent.
type errBudget struct{ after time.Duration }

func (e errBudget) Error() string {
	return "signatures: the requests of the hour for this remote are spent"
}

// Handler is the handler of the job signature-fetch.
func Handler(o HandlerOptions) jobs.Handler {
	if o.RequestsPerHour <= 0 {
		o.RequestsPerHour = DefaultRequestsPerHour
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	budgets := newBudgets(o.RequestsPerHour, o.Now)
	return func(ctx context.Context, run *jobs.Run) error {
		var a Args
		if err := json.Unmarshal(run.Args, &a); err != nil {
			return jobs.Permanent(fmt.Errorf("signature-fetch: the arguments do not read: %w", err))
		}
		qs := q.New(run.DB())
		done := func(fetched bool) error {
			return qs.SignatureFetchDone(ctx, q.SignatureFetchDoneParams{RepoID: a.RepoID, Target: a.Target, Fetched: fetched})
		}
		repos, err := qs.SignatureFetchRemote(ctx, a.RepoID)
		if err != nil {
			return err
		}
		if len(repos) == 0 || !repos[0].Fetch {
			return done(false) // gone, no remote any more, or the fetch is switched off for its upstream (R-SIG-026)
		}
		format := repos[0].Format
		f, ok := fetcherOf(o, format)
		if !ok || o.Background == nil {
			return done(false)
		}
		lim := &limited{take: func() error { return budgets.take(a.RepoID) }, observe: observer(o.Observe, format)}
		lim.Upstream, err = o.Background.Upstream(ctx, a.RepoID, run.ID)
		if err == nil {
			fctx := ctx
			if o.RunContext != nil {
				fctx = o.RunContext(ctx)
			}
			err = f.FetchSignatures(fctx, lim, a.Target)
		}
		var spent errBudget
		switch {
		case errors.As(err, &spent):
			// The budget of the hour is spent: the target waits for it, queued again, and its row stays queued.
			return o.Queue.requeue(ctx, a, spent.after)
		case err != nil:
			return failed(ctx, qs, run, a, err)
		}
		if err := done(true); err != nil {
			return err
		}
		return run.SetResult(map[string]int{"requests": lim.sent})
	}
}

// failed counts a run that failed: the last attempt frees the target and marks it failed (Q-SIG-106), and the job ends as the queue
// decides.
func failed(ctx context.Context, qs *q.Queries, run *jobs.Run, a Args, err error) error {
	final := run.Attempt >= run.MaxAttempts || jobs.IsPermanent(err)
	if ferr := qs.SignatureFetchFailed(ctx, q.SignatureFetchFailedParams{RepoID: a.RepoID, Target: a.Target, Final: final}); ferr != nil {
		return errors.Join(err, ferr)
	}
	return err
}

func fetcherOf(o HandlerOptions, format string) (Fetcher, bool) {
	if o.Fetcher == nil {
		return nil, false
	}
	return o.Fetcher(format)
}

func observer(fn func(format, result string), format string) func(error) {
	return func(err error) {
		if fn == nil {
			return
		}
		if err != nil {
			fn(format, "error")
			return
		}
		fn(format, "ok")
	}
}

// RegisterKind registers the job kind with its handler. It runs in the queue index, beside the other background fetches of a
// remote's documents, so that neither the verification of signatures nor the maintenance waits behind it.
func RegisterKind(r *jobs.Registry, h jobs.Handler) {
	if h == nil {
		h = func(context.Context, *jobs.Run) error {
			return jobs.Permanent(errors.New("signature-fetch: this replica has not wired the fetch"))
		}
	}
	r.RegisterKind(JobKind, jobs.KindSpec{
		Queue: "index", DedupMode: jobs.Coalesce, MaxAttempts: jobAttempts, Timeout: jobTimeout, Handler: h,
	})
}

// limited is the upstream of a run: every request takes one of the hour's budget first and is told to the observer. An artifact the
// cache holds costs no upstream request and still takes one: the bound errs to the safe side.
type limited struct {
	discover.Upstream
	take    func() error
	observe func(error)
	sent    int
}

var _ discover.Header = (*limited)(nil)

func (l *limited) before() error {
	if err := l.take(); err != nil {
		return err
	}
	l.sent++
	return nil
}

func (l *limited) Get(ctx context.Context, u *url.URL, accept string, max int64) (*discover.Response, error) {
	if err := l.before(); err != nil {
		return nil, err
	}
	res, err := l.Upstream.Get(ctx, u, accept, max)
	l.observe(err)
	return res, err
}

func (l *limited) Head(ctx context.Context, u *url.URL, accept string) (*discover.Response, error) {
	if err := l.before(); err != nil {
		return nil, err
	}
	res, err := discover.Head(ctx, l.Upstream, u, accept)
	l.observe(err)
	return res, err
}

func (l *limited) Document(ctx context.Context, path, variant string) ([]byte, bool, error) {
	if err := l.before(); err != nil {
		return nil, false, err
	}
	doc, found, err := l.Upstream.Document(ctx, path, variant)
	l.observe(err)
	return doc, found, err
}

func (l *limited) Artifact(ctx context.Context, path string, max int64) ([]byte, bool, error) {
	if err := l.before(); err != nil {
		return nil, false, err
	}
	body, found, err := l.Upstream.Artifact(ctx, path, max)
	l.observe(err)
	return body, found, err
}

// StoredDocument passes the optional capability of a background run's upstream through: a stored document costs no request.
func (l *limited) StoredDocument(ctx context.Context, path, variant string) ([]byte, bool, error) {
	if s, ok := l.Upstream.(interface {
		StoredDocument(ctx context.Context, path, variant string) ([]byte, bool, error)
	}); ok {
		return s.StoredDocument(ctx, path, variant)
	}
	return nil, false, nil
}

// budgets are the requests per hour of each remote on this replica: a bucket of RequestsPerHour that refills at that rate.
type budgets struct {
	mu   sync.Mutex
	per  float64
	now  func() time.Time
	byID map[int64]*bucket
}

type bucket struct {
	tokens float64
	at     time.Time
}

func newBudgets(perHour int, now func() time.Time) *budgets {
	return &budgets{per: float64(perHour), now: now, byID: map[int64]*bucket{}}
}

// take takes one request of a remote's budget, or reports how long until there is one.
func (b *budgets) take(repoID int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	bk := b.byID[repoID]
	if bk == nil {
		bk = &bucket{tokens: b.per, at: now}
		b.byID[repoID] = bk
	}
	bk.tokens = min(b.per, bk.tokens+now.Sub(bk.at).Hours()*b.per)
	bk.at = now
	if bk.tokens < 1 {
		wait := time.Duration((1 - bk.tokens) / b.per * float64(time.Hour))
		return errBudget{after: max(wait, time.Second)}
	}
	bk.tokens--
	return nil
}
