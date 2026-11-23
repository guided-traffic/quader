package store

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/remotecuration/rules"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The record of a client's refusal by the rules (R-CUR-004 "Audit", "Metric", "Record"): the metric counts every refusal; the event
// remote.delivery.refused is written at most once a minute per repository, package, version and client address on each replica, as
// vulnerability.download_blocked is; and the refusals are counted in the request list's entry of the subject (R-CUR-015), summed on
// each replica and written at most once per flush period per subject (Q-CUR-40), so a client refused many times in a loop costs a
// write per period, not per request, and the list reads no audit log.

// EventWindow is the window of the event's throttle (R-CUR-004).
const EventWindow = time.Minute

// DefaultFlushEvery is the period the summed refusals are written with (Q-CUR-40).
const DefaultFlushEvery = 5 * time.Second

// maxTracked bounds the maps of the throttle and of the pending counts: beyond it the oldest windows are forgotten (a second event
// then), and pending counts are written at once.
const maxTracked = 8192

// RecorderOptions configure [NewRecorder].
type RecorderOptions struct {
	Runner meta.Runner
	Audit  audit.Recorder
	// Refusals is quader_remote_delivery_refusals_total{repo,reason,kind}; nil counts nothing. Label caps the repo label (R-OPS-007);
	// nil is the name itself.
	Refusals *prometheus.CounterVec
	Label    func(repo string) string
	// MaxClientEntries is proxy.delivery.max_client_entries; zero is 10,000.
	MaxClientEntries int
	// FlushEvery is the period of the written counts; zero is DefaultFlushEvery.
	FlushEvery time.Duration
	Log        *slog.Logger // nil logs nothing
	// Now replaces the clock, for tests.
	Now func() time.Time
}

// Recorder records refusals. It is safe for concurrent use; Run writes the summed counts until its context ends.
type Recorder struct {
	o RecorderOptions

	mu      sync.Mutex
	seen    map[string]time.Time // the throttle's windows: repo, name, version, client address
	pending map[subjectKey]int64 // refusals not yet written
	wake    chan struct{}
}

type subjectKey struct {
	repo          int64
	name, version string
}

// NewRecorder returns the recorder.
func NewRecorder(o RecorderOptions) *Recorder {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler) // the composition root hands the replica's logger (R-OPS-009)
	}
	if o.MaxClientEntries <= 0 {
		o.MaxClientEntries = 10000
	}
	if o.FlushEvery <= 0 {
		o.FlushEvery = DefaultFlushEvery
	}
	return &Recorder{o: o, seen: map[string]time.Time{}, pending: map[subjectKey]int64{}, wake: make(chan struct{}, 1)}
}

// RefusalContext is what the answer of a client's request tells the record besides the refusal: who asked, from where, on which endpoint.
type RefusalContext struct {
	Actor    audit.Actor
	Source   audit.Source
	Endpoint string
}

// Refused records one refusal answered to a client.
func (r *Recorder) Refused(ctx context.Context, rf *rules.Refusal, rc RefusalContext) {
	if r == nil || rf == nil {
		return
	}
	if r.o.Refusals != nil {
		repo := rf.Repo.String()
		if r.o.Label != nil {
			repo = r.o.Label(repo)
		}
		r.o.Refusals.WithLabelValues(repo, rules.EventReason(rf.Reason), string(rf.Kind)).Inc()
	}
	now := r.o.Now()
	key := rf.Repo.String() + "\x00" + rf.Name + "\x00" + rf.Version + "\x00" + rc.Source.ClientAddr
	r.mu.Lock()
	first := true
	if at, ok := r.seen[key]; ok && now.Sub(at) < EventWindow {
		first = false
	} else {
		if len(r.seen) >= maxTracked {
			r.forget(now)
		}
		r.seen[key] = now
	}
	sk := subjectKey{repo: rf.RepoID, name: rf.Name, version: rf.Version}
	r.pending[sk]++
	full := len(r.pending) >= maxTracked
	r.mu.Unlock()
	if full {
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}
	if first && r.o.Audit != nil {
		r.event(ctx, rf, rc)
	}
}

// forget drops the windows that are over, and every window when none is.
func (r *Recorder) forget(now time.Time) {
	for k, at := range r.seen {
		if now.Sub(at) >= EventWindow {
			delete(r.seen, k)
		}
	}
	if len(r.seen) >= maxTracked {
		clear(r.seen)
	}
}

func (r *Recorder) event(ctx context.Context, rf *rules.Refusal, rc RefusalContext) {
	var version any
	if rf.Version != "" {
		version = rf.Version
	}
	details, err := json.Marshal(map[string]any{
		"path": rf.Path, "package": rf.Name, "version": version, "reason": rules.EventReason(rf.Reason), "kind": string(rf.Kind),
	})
	if err != nil { // strings only
		return
	}
	r.o.Audit.Record(ctx, audit.Event{
		Type: audit.EventRemoteDeliveryRefused, Actor: rc.Actor, Outcome: audit.Denied, Reason: rules.EventReason(rf.Reason),
		Source: rc.Source, Subject: audit.Subject{Kind: "repository", ID: strconv.FormatInt(rf.RepoID, 10), Name: rf.Repo.String()},
		Repository: rf.Repo.String(), Project: rf.Repo.Project, Path: rf.Path, Endpoint: rc.Endpoint, Details: details,
	})
}

// Run writes the summed counts every flush period, and once more when ctx ends.
func (r *Recorder) Run(ctx context.Context) {
	t := time.NewTicker(r.o.FlushEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			r.Flush(fctx)
			cancel()
			return
		case <-t.C:
		case <-r.wake:
		}
		r.Flush(ctx)
	}
}

// Flush writes the summed counts now: each subject's count into its open entry, or the entry declined last, or a new open entry
// without an asker; the entries without an asker beyond proxy.delivery.max_client_entries are removed, the oldest last refusal first.
// A failed write puts its count back for the next flush.
func (r *Recorder) Flush(ctx context.Context) {
	if r == nil || r.o.Runner == nil {
		return
	}
	r.mu.Lock()
	batch := r.pending
	r.pending = map[subjectKey]int64{}
	r.mu.Unlock()
	for k, n := range batch {
		if err := r.write(ctx, k, n); err != nil {
			r.o.Log.WarnContext(ctx, "the refusals of the delivery rules could not be counted in the request list", "repository", k.repo, "error", err)
			r.mu.Lock()
			if len(r.pending) < maxTracked {
				r.pending[k] += n
			}
			r.mu.Unlock()
		}
	}
}

func (r *Recorder) write(ctx context.Context, k subjectKey, n int64) error {
	return r.o.Runner.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		updated, err := qs.DeliveryRefusalCount(ctx, q.DeliveryRefusalCountParams{Refusals: n, RepoID: k.repo, Name: k.name, Version: k.version})
		if err != nil || updated > 0 {
			return err
		}
		if err := qs.DeliveryRefusalOpen(ctx, q.DeliveryRefusalOpenParams{
			ID: uuid.Must(uuid.NewV7()).String(), RepoID: k.repo, Name: k.name, Version: k.version, Refusals: n,
		}); err != nil {
			return err
		}
		_, err = qs.DeliveryRefusalBound(ctx, q.DeliveryRefusalBoundParams{RepoID: k.repo, MaxEntries: int32(min(r.o.MaxClientEntries, 1<<30))}) //nolint:gosec // bounded
		return err
	})
}
