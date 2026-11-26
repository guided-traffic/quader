// Package history records the history of the stored content (storage.md R-STOR-108 to R-STOR-110, ADR-315): the intervals of a
// connection this replica could not reach or whose start check failed, and the hourly counts of what the reads and writes absorbed.
// The findings of damaged copies are not written here: the triggers of blob_copies open and resolve them (migration 00151), whatever
// path changed a copy. The task storage-history keeps the three tables bounded (task.go).
//
// One Recorder runs per replica with a blob store. It is fed in the request path (the counters of internal/blob and its
// reconciliation, the circuit breaker's transitions, the start check's outcomes) and keeps what it was told in memory; a loop writes
// it once a minute and at once when an interval opens or ends, and a last time when the replica stops. A replica that dies loses at
// most a minute of counts (H-2423).
package history

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
)

// The kinds of an interval (storage_events.kind).
const (
	KindUnreachable      = "unreachable"        // the connection's circuit was open on the replica (R-STOR-033)
	KindStartCheckFailed = "start_check_failed" // the connection's start check failed on the replica (R-STOR-035)
)

// The bounds of the record (Q-STOR-513, Q-STOR-514, Q-STOR-516).
const (
	// Hysteresis is how long a connection must stay reachable before its interval ends: a circuit that opens again within it (a
	// probe that passed, then five failures) continues the interval and counts one more occurrence.
	Hysteresis = time.Minute
	// FlushInterval is how often the counts and the open intervals are written.
	FlushInterval = time.Minute
	// EventsPerDay is the most intervals one connection and replica start in a UTC day; beyond it the last one counts occurrences.
	EventsPerDay = 100
)

// Count is one row of storage_counts as a replica adds it: the UTC hour, the connection, the kind, the reason, how many and the bytes.
type Count struct {
	Hour       time.Time
	Connection string
	Kind       string
	Reason     string
	N, Bytes   int64
}

// Event is an interval as it opens: its connection, kind, start, code and, for a start check, the step.
type Event struct {
	Connection, Kind string
	Started          time.Time
	Code, Step       string
}

// Store is the database side of a Recorder. [PG] is PostgreSQL; the unit tests use a fake.
type Store interface {
	// AddCounts adds counts of distinct keys to what is recorded.
	AddCounts(ctx context.Context, counts []Count) error
	// OpenEvent inserts an interval of replica, or takes over the open one of its connection, kind and replica. capped reports that
	// the day's cap was reached: nothing was inserted, and the last interval counted one more occurrence.
	OpenEvent(ctx context.Context, replica string, e Event, dayCap int) (id int64, capped bool, err error)
	// UpdateEvent writes what is known of an open interval: when it was last seen, its occurrences, its code and its end (zero
	// while it lasts).
	UpdateEvent(ctx context.Context, id int64, seen time.Time, occurrences int, code string, ended time.Time) error
	// EndReplicaEvents ends the intervals an earlier life of replica left open, at the time it last saw them.
	EndReplicaEvents(ctx context.Context, replica string) error
}

// Options configure a Recorder.
type Options struct {
	Store Store
	// Replica is the name the intervals carry: the replica's instance name, as the replica registry lists it.
	Replica string
	Log     *slog.Logger
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

type countKey struct {
	hour                     int64 // Unix seconds of the UTC hour
	connection, kind, reason string
}

type countVal struct{ n, bytes int64 }

type eventKey struct{ connection, kind string }

// interval is what the replica knows of one open interval.
type interval struct {
	id          int64 // 0 until it is written
	started     time.Time
	seen        time.Time // when the replica last saw it open
	reachableAt time.Time // when it became reachable again, zero while it is not
	occurrences int
	code, step  string
	dirty       bool // something to write
}

// Recorder is the replica's recorder of the history. Its methods are safe for concurrent use; the request path calls Count, Circuit
// and Checked, which only change memory.
type Recorder struct {
	o Options

	mu     sync.Mutex
	counts map[countKey]*countVal
	open   map[eventKey]*interval
	kick   chan struct{}

	flushMu sync.Mutex // one flush at a time

	life   sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// New returns a Recorder; Start runs its loop.
func New(o Options) *Recorder {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	return &Recorder{o: o, counts: map[countKey]*countVal{}, open: map[eventKey]*interval{}, kick: make(chan struct{}, 1)}
}

var _ blob.HistorySink = (*Recorder)(nil)

// Count implements [blob.HistorySink]: one event of kind with reason on conn, of bytes bytes, in the current UTC hour.
func (r *Recorder) Count(conn, kind, reason string, bytes int64) {
	k := countKey{hour: r.o.Now().UTC().Truncate(time.Hour).Unix(), connection: conn, kind: kind, reason: reason}
	r.mu.Lock()
	v := r.counts[k]
	if v == nil {
		v = &countVal{}
		r.counts[k] = v
	}
	v.n++
	v.bytes += max(bytes, 0)
	r.mu.Unlock()
}

// Circuit is the transition hook of a connection's circuit breaker (blob.WithGuardTransitions): an open circuit opens or continues
// the connection's interval unreachable with the code of the failure that opened it; a closed one starts its hysteresis.
func (r *Recorder) Circuit(conn string, open bool, err error) {
	if open {
		r.opened(conn, KindUnreachable, code(err), "")
		return
	}
	r.reachable(conn, KindUnreachable)
}

// Checked is the outcome hook of a connection's start check (blob.StartCheckOptions.Checked): a failure opens or continues the
// connection's interval start_check_failed with its code and step; a pass starts its hysteresis.
func (r *Recorder) Checked(conn string, err error) {
	if err != nil {
		r.opened(conn, KindStartCheckFailed, code(err), blob.CheckStep(err))
		return
	}
	r.reachable(conn, KindStartCheckFailed)
}

// code is the short code an interval stores for err (Q-STOR-517).
func code(err error) string {
	if c := blob.FailureCode(err); c != "" {
		return c
	}
	return blob.CodeOther
}

func (r *Recorder) opened(conn, kind, code, step string) {
	now := r.o.Now()
	k := eventKey{connection: conn, kind: kind}
	r.mu.Lock()
	iv := r.open[k]
	switch {
	case iv == nil:
		r.open[k] = &interval{started: now, seen: now, occurrences: 1, code: code, step: step, dirty: true}
	case !iv.reachableAt.IsZero():
		// it fell away again within the hysteresis: the same interval, one more occurrence
		iv.reachableAt, iv.seen, iv.code, iv.dirty = time.Time{}, now, code, true
		iv.occurrences++
		if step != "" {
			iv.step = step
		}
	default:
		iv.seen = now
		if iv.code != code {
			iv.code, iv.dirty = code, true
		}
	}
	r.mu.Unlock()
	r.wake()
}

func (r *Recorder) reachable(conn, kind string) {
	r.mu.Lock()
	iv := r.open[eventKey{connection: conn, kind: kind}]
	if iv != nil && iv.reachableAt.IsZero() {
		iv.reachableAt, iv.dirty = r.o.Now(), true
	}
	r.mu.Unlock()
}

// wake asks the loop to write soon, without waiting.
func (r *Recorder) wake() {
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

// Name implements the component of the composition root.
func (r *Recorder) Name() string { return "storage-history" }

// Start ends what an earlier life of the replica left open and runs the loop in the background until Stop.
func (r *Recorder) Start(ctx context.Context) error {
	r.life.Lock()
	defer r.life.Unlock()
	if r.cancel != nil {
		return errors.New("history: the recorder is started")
	}
	if err := r.o.Store.EndReplicaEvents(ctx, r.o.Replica); err != nil {
		// not fatal: the task ends them once the registry no longer lists the earlier life
		r.o.Log.Warn("could not end the intervals an earlier life of this replica left open", "error", err)
	}
	lctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.cancel, r.done = cancel, make(chan struct{})
	go r.run(lctx, r.done)
	return nil
}

// Stop ends the loop and writes a last time: the counts, and every open interval ended now, since the replica stops watching.
func (r *Recorder) Stop(ctx context.Context) error {
	r.life.Lock()
	cancel, done := r.cancel, r.done
	r.life.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return r.flush(ctx, true)
}

func (r *Recorder) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	t := time.NewTicker(FlushInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-r.kick:
		}
		fctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := r.flush(fctx, false); err != nil && ctx.Err() == nil {
			r.o.Log.Warn("could not write the history of the stored content; it is written with the next flush", "error", err)
		}
		cancel()
	}
}

// Flush writes what the recorder holds now: the counts, the intervals that opened, changed or ended. Tests call it; the loop calls it
// once a minute.
func (r *Recorder) Flush(ctx context.Context) error { return r.flush(ctx, false) }

// eventWrite is one interval's write of a flush, taken under the lock.
type eventWrite struct {
	key         eventKey
	iv          *interval
	id          int64
	ev          Event
	seen, ended time.Time
	occurrences int
	code        string
}

func (r *Recorder) flush(ctx context.Context, stopping bool) error {
	r.flushMu.Lock()
	defer r.flushMu.Unlock()
	counts, writes := r.take(stopping)
	var errs []error
	if len(counts) > 0 {
		if err := r.o.Store.AddCounts(ctx, counts); err != nil {
			r.giveBack(counts)
			errs = append(errs, err)
		}
	}
	for _, w := range writes {
		if err := r.write(ctx, w); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// take moves the counts out of the recorder and collects the writes of the intervals: an interval reachable for the hysteresis (or
// every interval, when the replica stops) ends at the time it became reachable (or now); the others are written when they are new or
// changed, and once a minute with the time they were last seen.
func (r *Recorder) take(stopping bool) ([]Count, []eventWrite) {
	now := r.o.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	counts := make([]Count, 0, len(r.counts))
	for k, v := range r.counts {
		counts = append(counts, Count{Hour: time.Unix(k.hour, 0).UTC(), Connection: k.connection, Kind: k.kind, Reason: k.reason, N: v.n, Bytes: v.bytes})
	}
	clear(r.counts)
	var writes []eventWrite
	for k, iv := range r.open {
		w := eventWrite{
			key: k, iv: iv, id: iv.id, occurrences: iv.occurrences, code: iv.code,
			ev: Event{Connection: k.connection, Kind: k.kind, Started: iv.started, Code: iv.code, Step: iv.step},
		}
		switch {
		case !iv.reachableAt.IsZero() && (stopping || now.Sub(iv.reachableAt) >= Hysteresis):
			w.seen, w.ended = iv.reachableAt, iv.reachableAt
			delete(r.open, k)
		case stopping:
			w.seen, w.ended = now, now
			delete(r.open, k)
		case iv.reachableAt.IsZero():
			iv.seen = now
			w.seen = now
		default:
			if !iv.dirty && iv.id != 0 {
				continue // reachable, within the hysteresis, written already
			}
			w.seen = iv.reachableAt
		}
		iv.dirty = false
		writes = append(writes, w)
	}
	return counts, writes
}

// giveBack adds counts a failed write took out back to the recorder, for the next flush.
func (r *Recorder) giveBack(counts []Count) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range counts {
		k := countKey{hour: c.Hour.Unix(), connection: c.Connection, kind: c.Kind, reason: c.Reason}
		v := r.counts[k]
		if v == nil {
			v = &countVal{}
			r.counts[k] = v
		}
		v.n += c.N
		v.bytes += c.Bytes
	}
}

// write writes one interval: inserted when it has no row yet (an interval beyond the day's cap is counted on the last one and
// forgotten), then updated with what the replica knows.
func (r *Recorder) write(ctx context.Context, w eventWrite) error {
	id := w.id
	if id == 0 {
		var capped bool
		var err error
		id, capped, err = r.o.Store.OpenEvent(ctx, r.o.Replica, w.ev, EventsPerDay)
		if err != nil {
			r.retry(w)
			return err
		}
		if capped {
			r.forget(w)
			return nil
		}
		r.mu.Lock()
		w.iv.id = id
		r.mu.Unlock()
	}
	if err := r.o.Store.UpdateEvent(ctx, id, w.seen, w.occurrences, w.code, w.ended); err != nil {
		r.retry(w)
		return err
	}
	return nil
}

// retry keeps an interval whose write failed for the next flush; an ended one is put back as it was, reachable since its end.
func (r *Recorder) retry(w eventWrite) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w.iv.dirty = true
	if _, ok := r.open[w.key]; !ok && !w.ended.IsZero() {
		w.iv.reachableAt = w.ended
		r.open[w.key] = w.iv
	}
}

// forget drops an interval beyond the day's cap: the last interval counted it, and the next transition is counted the same way.
func (r *Recorder) forget(w eventWrite) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.open[w.key] == w.iv {
		delete(r.open, w.key)
	}
}
