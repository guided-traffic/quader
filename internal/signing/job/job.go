// Package job is the work of Quader's signing in the worker (signatures.md R-SIG-046, R-SIG-048, R-SIG-049, R-SIG-057): the job signing
// (the switch of an announced identity after its announce period, discovery, and the due work of the kinds the packages that sign
// register), the watcher of the identity's files of the Helm values, and the holder of the private keys. Only the roles worker and all
// build it; no other role holds or opens a key.
//
// One job runs at a time in the installation (coalesce). It keeps signing.concurrency operations in flight; each runs in a transaction
// that locks its row of work and holds the current identity's row shared, so that no signature is written after the identity's switch
// (R-SIG-048). An operation that could not run spends an attempt with a doubling back-off from a minute; after signing.max_attempts it
// is failed, signing.failed is recorded once an hour per kind and code, and the job makes it due again once a day.
package job

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/signing"
	"github.com/guided-traffic/quader/internal/signing/store"
)

// The working bounds of the job (R-SIG-049).
const (
	// discoveryBatch is the most items one discovery step of a kind looks at.
	discoveryBatch = 10000
	// firstBackoff is the wait after the first attempt that could not run; it doubles per attempt up to maxBackoff.
	firstBackoff = time.Minute
	maxBackoff   = 6 * time.Hour
	// runSlice is how long one job works before the rest goes to the next job, so the queue's slot is given back.
	runSlice = 10 * time.Minute
	// failedRetry is when a failed row is made due again by the daily retry.
	failedRetry = 24 * time.Hour
)

// Run is the part of a job's run handle the job uses; *jobs.Run implements it.
type Run interface {
	SetResult(v any) error
}

var _ Run = (*jobs.Run)(nil)

// Store is what the job needs of the store; *store.Store implements it.
type Store interface {
	Identities(ctx context.Context) ([]signing.Identity, error)
	SwitchDue(ctx context.Context) (bool, time.Time, error)
	RetryFailedDaily(ctx context.Context) (int64, error)
	DueWork(ctx context.Context, kinds []string, limit int, inFlight []string) ([]store.Work, error)
	DeferWork(ctx context.Context, w store.Work, notBefore time.Time, failed bool, code string) error
	ClaimFailedReport(ctx context.Context, kind, code string) (bool, error)
	Backlog(ctx context.Context) ([]store.Backlog, error)
	Discovery(ctx context.Context) (map[string]int64, error)
	SetDiscovery(ctx context.Context, kind string, after int64) error
}

var _ Store = (*store.Store)(nil)

// Options configure [New].
type Options struct {
	DB    meta.Runner
	Store Store
	Keys  *Holder
	// Kinds are the kinds of work the packages that sign registered.
	Kinds []Kind
	// Enabled is signing.enabled: false takes no work; the switch still runs.
	Enabled bool
	// Concurrency is signing.concurrency, MaxAttempts signing.max_attempts.
	Concurrency int
	MaxAttempts int
	// Wake enqueues the job after a delay: the next run at the end of an announce period, the rest of the work after the slice.
	Wake    func(ctx context.Context, delay time.Duration) error
	Audit   audit.Recorder
	Metrics *Metrics
	Log     *slog.Logger
	Now     func() time.Time
	// hold is the shared lock of an operation on the current identity's row; a test replaces it.
	hold func(ctx context.Context, tx meta.Tx, id int64) (time.Time, bool, error)
	// lock and done are the row of work's lock and removal in an operation's transaction; a test replaces them.
	lock func(ctx context.Context, tx meta.Tx, w store.Work) (bool, error)
	done func(ctx context.Context, tx meta.Tx, w store.Work) error
}

// Job is the job signing.
type Job struct {
	o     Options
	kinds map[string]Kind
	names []string
}

// New returns the job.
func New(o Options) (*Job, error) {
	if o.DB == nil || o.Store == nil || o.Keys == nil {
		return nil, errors.New("signing job: the database, the store and the key holder are required")
	}
	if o.Concurrency < 1 {
		o.Concurrency = 4
	}
	if o.MaxAttempts < 1 {
		o.MaxAttempts = 5
	}
	if o.Audit == nil {
		o.Audit = audit.Nop{}
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.hold == nil {
		o.hold = store.HoldCurrent
	}
	if o.lock == nil {
		o.lock = store.LockWork
	}
	if o.done == nil {
		o.done = store.DoneWork
	}
	j := &Job{o: o, kinds: map[string]Kind{}}
	for _, k := range o.Kinds {
		if k.Name == "" || k.Sign == nil {
			return nil, errors.New("signing job: a kind needs a name and a Sign")
		}
		if _, dup := j.kinds[k.Name]; dup {
			return nil, errors.New("signing job: the kind " + k.Name + " is registered twice")
		}
		j.kinds[k.Name] = k
		j.names = append(j.names, k.Name)
	}
	return j, nil
}

// Backoff is the wait after the attempt-th attempt that could not run: a minute, doubling, at most six hours (R-SIG-049).
func Backoff(attempt int) time.Duration {
	d := firstBackoff
	for i := 1; i < attempt && d < maxBackoff; i++ {
		d *= 2
	}
	return min(d, maxBackoff)
}

// errNotCurrent stops a run: the identity it signs with is not current any more, or its certificate expired. The work stays due and
// spends no attempt.
var errNotCurrent = errors.New("the signing identity is not current any more")

// result is what a run reports as the job's result.
type result struct {
	Switched   bool   `json:"switched"`
	Retried    int64  `json:"retried,omitempty"`
	Discovered int    `json:"discovered,omitempty"`
	Signed     int    `json:"signed,omitempty"`
	Failed     int    `json:"failed,omitempty"`
	Skipped    string `json:"skipped,omitempty"`
}

// RunSigning is the handler of the job signing (R-SIG-048, R-SIG-049): the switch of an announced identity once its announce period
// passed, the daily retry of failed work, then, while signing is on and this worker holds the current identity's key, discovery and the
// due work by priority and time until nothing is due or the slice ended. It enqueues itself for the end of an announce period and for
// the rest of the work.
func (j *Job) RunSigning(ctx context.Context, run Run) error {
	var res result
	switched, next, err := j.o.Store.SwitchDue(ctx)
	if err != nil {
		return err
	}
	res.Switched = switched
	if res.Retried, err = j.o.Store.RetryFailedDaily(ctx); err != nil {
		return err
	}
	if err := j.served(ctx); err != nil {
		return err
	}
	more, err := j.sign(ctx, &res)
	if err != nil {
		return err
	}
	j.observe(ctx)
	if err := j.reschedule(ctx, next, more); err != nil {
		return err
	}
	return run.SetResult(res)
}

// served lets every kind that serves by the identities' states follow them (Kind.Served), after the switch and before any signing, so
// that a document never serves a signature of an identity that stopped being listed for longer than one run.
func (j *Job) served(ctx context.Context) error {
	var ids []signing.Identity
	for _, name := range j.names {
		k := j.kinds[name]
		if k.Served == nil {
			continue
		}
		if ids == nil {
			var err error
			if ids, err = j.o.Store.Identities(ctx); err != nil {
				return err
			}
		}
		if err := k.Served(ctx, ids); err != nil {
			return err
		}
	}
	return nil
}

// sign runs discovery and the due work with the current identity; more is true when the slice ended with work left.
func (j *Job) sign(ctx context.Context, res *result) (bool, error) {
	switch {
	case !j.o.Enabled:
		res.Skipped = "signing.enabled is false"
		return false, nil
	case len(j.kinds) == 0:
		res.Skipped = "no kind of work is registered"
		return false, nil
	}
	cur, ok, err := j.current(ctx)
	if err != nil || !ok {
		if err == nil {
			res.Skipped = "no signing identity is current, or this worker does not hold its key"
		}
		return false, err
	}
	n, err := j.discover(ctx, cur.Identity)
	if err != nil {
		return false, err
	}
	res.Discovered = n
	return j.work(ctx, cur, res)
}

// current answers the identity that signs now and its key, when this worker holds it and its certificate has not expired.
func (j *Job) current(ctx context.Context) (Session, bool, error) {
	ids, err := j.o.Store.Identities(ctx)
	if err != nil {
		return Session{}, false, err
	}
	for _, i := range ids {
		if !i.SignsAt(j.o.Now()) {
			continue
		}
		k, ok, err := j.o.Keys.For(ctx, i)
		if err != nil {
			j.o.Log.Warn("the current signing identity's key cannot be opened; this worker signs nothing", "identity", i.ID, "error", err)
			return Session{}, false, nil
		}
		return Session{Identity: i, Key: k}, ok, nil
	}
	return Session{}, false, nil
}

// discover runs one step of discovery per kind that has one, from its cursor; a step that reached the last item starts the next pass
// from the first.
func (j *Job) discover(ctx context.Context, cur signing.Identity) (int, error) {
	cursors, err := j.o.Store.Discovery(ctx)
	if err != nil {
		return 0, err
	}
	steps := 0
	for _, name := range j.names {
		k := j.kinds[name]
		if k.Discover == nil {
			continue
		}
		after := cursors[name]
		last, err := k.Discover(ctx, cur, after, discoveryBatch)
		if err != nil {
			return steps, err
		}
		if last <= after {
			last = 0
		}
		if err := j.o.Store.SetDiscovery(ctx, name, last); err != nil {
			return steps, err
		}
		steps++
	}
	return steps, nil
}

// work runs the due rows in batches with signing.concurrency operations in flight, until nothing is due, the identity changed, or the
// slice ended (more).
func (j *Job) work(ctx context.Context, cur Session, res *result) (bool, error) {
	deadline := j.o.Now().Add(runSlice)
	for {
		if !j.o.Now().Before(deadline) {
			return true, nil
		}
		batch, err := j.o.Store.DueWork(ctx, j.names, j.o.Concurrency*4, nil)
		if err != nil || len(batch) == 0 {
			return false, err
		}
		stop, err := j.batch(ctx, cur, batch, res)
		if err != nil || stop {
			return false, err
		}
	}
}

// batch runs the operations of one batch, signing.concurrency at a time; stop is true when the identity changed.
func (j *Job) batch(ctx context.Context, cur Session, batch []store.Work, res *result) (bool, error) {
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		stop     bool
		firstErr error
	)
	slots := make(chan struct{}, j.o.Concurrency)
	for _, w := range batch {
		mu.Lock()
		halt := stop || firstErr != nil
		mu.Unlock()
		if halt {
			break
		}
		slots <- struct{}{}
		wg.Add(1)
		go func(w store.Work) {
			defer wg.Done()
			defer func() { <-slots }()
			signed, err := j.operate(ctx, cur, w)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case errors.Is(err, errNotCurrent):
				stop = true
			case err != nil && firstErr == nil:
				firstErr = err
			case signed:
				res.Signed++
			case err == nil:
			default:
			}
		}(w)
	}
	wg.Wait()
	return stop, firstErr
}

// operate runs one row's operation in a transaction that locks the row and holds the current identity's row: signed is true when the
// kind's Sign wrote it. An operation that could not run spends an attempt; the error is returned only when the attempt could not be
// recorded either, or the identity changed (errNotCurrent).
func (j *Job) operate(ctx context.Context, cur Session, w store.Work) (bool, error) {
	k := j.kinds[w.Kind]
	ran := false
	err := j.o.DB.InTx(ctx, func(tx meta.Tx) error {
		ran = false
		ok, err := j.o.lock(ctx, tx, w)
		if err != nil || !ok {
			return err
		}
		notAfter, held, err := j.o.hold(ctx, tx, cur.Identity.ID)
		switch {
		case err != nil:
			return err
		case !held || (!notAfter.IsZero() && !j.o.Now().Before(notAfter)):
			return errNotCurrent
		}
		if err := k.Sign(ctx, tx, cur, w); err != nil {
			return &signError{err: err}
		}
		ran = true
		return j.o.done(ctx, tx, w)
	})
	switch {
	case err == nil:
		if ran {
			j.o.Metrics.operation(w.Kind, "signed")
		}
		return ran, nil
	case errors.Is(err, errNotCurrent):
		return false, errNotCurrent
	}
	return false, j.spend(ctx, w, err)
}

// signError marks an error of a kind's Sign, as distinct from one of the job's own statements.
type signError struct{ err error }

func (e *signError) Error() string { return e.err.Error() }
func (e *signError) Unwrap() error { return e.err }

// spend records an attempt that could not run: due again after the back-off, or failed after the last attempt with signing.failed
// recorded once an hour per kind and code (R-SIG-049, R-SIG-057).
func (j *Job) spend(ctx context.Context, w store.Work, cause error) error {
	code := codeOf(cause)
	attempt := w.Attempts + 1
	failed := attempt >= j.o.MaxAttempts
	next := j.o.Now().Add(Backoff(attempt))
	if failed {
		next = j.o.Now().Add(failedRetry)
	}
	j.o.Metrics.operation(w.Kind, "failed")
	j.o.Log.Warn("a signing operation could not run", "kind", w.Kind, "item", w.ItemID, "attempt", attempt, "code", code, "error", cause)
	if err := j.o.Store.DeferWork(ctx, w, next, failed, code); err != nil {
		return err
	}
	if !failed {
		return nil
	}
	report, err := j.o.Store.ClaimFailedReport(ctx, w.Kind, code)
	if err != nil {
		return err
	}
	if report {
		j.o.Audit.Record(ctx, store.FailedEvent(w.Kind, code, 1))
	}
	return nil
}

// codeOf is the internal/errs code of an error, "error" for any other.
func codeOf(err error) string {
	var e *errs.Error
	if errors.As(err, &e) && e.Code != "" {
		return e.Code
	}
	return "error"
}

// observe sets the gauges after a run.
func (j *Job) observe(ctx context.Context) {
	if j.o.Metrics == nil {
		return
	}
	if rows, err := j.o.Store.Backlog(ctx); err == nil {
		j.o.Metrics.work(rows, j.o.Now())
	}
}

// reschedule enqueues the next run: at the end of an announce period (next), or at once for the rest of the work.
func (j *Job) reschedule(ctx context.Context, next time.Time, more bool) error {
	if j.o.Wake == nil {
		return nil
	}
	switch {
	case more:
		return j.o.Wake(ctx, 0)
	case !next.IsZero():
		return j.o.Wake(ctx, max(next.Sub(j.o.Now()), time.Second))
	}
	return nil
}
