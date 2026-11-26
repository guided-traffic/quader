// Package scrub is the scrub of the stored content (storage.md R-STOR-027): a worker job reads every present copy on every
// connection of its configuration to its end once within that connection's scrub_period, throttled per connection, judges it by
// its bytes against the blob's digest, marks a damaged copy as a download does (R-STOR-055) and replaces it at once from a whole
// copy on another connection, reading back what it wrote before the copy's row says it is good (Q-STOR-265). It also holds the
// recheck of one blob and the list of damaged blobs of the API (R-STOR-027, Q-API-386) and the gauge of the copies that are
// overdue.
//
// The package is infrastructure and does not import the job framework: the composition root registers the kind and the task
// scrub with [Scrubber.RunJob] as the handler's body (internal/app/wire_storage_scrub.go).
package scrub

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
)

// DuePage is the number of due copies a connection's reader asks the ledger for at a time.
const DuePage = 200

// ErrUnknownConnection is the error of a run asked for a connection this replica does not configure.
var ErrUnknownConnection = errors.New("scrub: the connection is not in this replica's configuration")

// Options configure a [Scrubber].
type Options struct {
	Connections *blob.Connections
	Ledger      Ledger
	// RunDuration ends a run (storage.scrub.run_duration); the next scheduled run continues.
	RunDuration time.Duration
	// PartSize is storage.object_store.multipart.part_size: the most a repair holds in memory, and the part of the multipart
	// upload of a larger object.
	PartSize int64
	Metrics  *Metrics
	Log      *slog.Logger
	// Clock replaces the real time in tests.
	Clock Clock
}

// Scrubber runs the scrub. It is safe for concurrent use; the job is exclusive, so one run is active per installation.
type Scrubber struct {
	o Options
}

// New builds a scrubber.
func New(o Options) (*Scrubber, error) {
	if o.Connections == nil || o.Ledger == nil {
		return nil, errors.New("scrub: the connections and a ledger are required")
	}
	if o.RunDuration <= 0 {
		o.RunDuration = 50 * time.Minute
	}
	if o.PartSize <= 0 {
		o.PartSize = 16 << 20
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.Clock == nil {
		o.Clock = realClock{}
	}
	return &Scrubber{o: o}, nil
}

// RunOptions select what a run reads.
type RunOptions struct {
	// Connection limits the run to one connection; empty reads every connection of the configuration.
	Connection string
	// Draining is closed when the replica begins to stop: the readers end after the copy they read.
	Draining <-chan struct{}
}

// How a connection's reader ended.
const (
	EndedDone        = "done"         // no due copy was left
	EndedDuration    = "duration"     // storage.scrub.run_duration passed
	EndedDraining    = "draining"     // the replica began to stop
	EndedCircuitOpen = "circuit_open" // the connection's circuit was open on this replica
	EndedCancelled   = "cancelled"    // the job was cancelled or lost its lease
	EndedError       = "error"        // the database did not answer
)

// ConnectionResult is what a run did on one connection.
type ConnectionResult struct {
	Connection string `json:"connection"`
	// Read is the number of copies read; Verified, Corrupt, Missing, Errors and UnknownKey split it by result. UnknownKey counts the
	// copies on an encrypted connection whose record names a storage key this replica does not hold: not marked, still due
	// (R-STOR-087, Q-STOR-311).
	Read       int64 `json:"read"`
	Verified   int64 `json:"verified"`
	Corrupt    int64 `json:"corrupt"`
	Missing    int64 `json:"missing"`
	Errors     int64 `json:"errors"`
	UnknownKey int64 `json:"unknownKey"`
	// Bytes is what the reader moved, its repairs included.
	Bytes int64 `json:"bytes"`
	// Repaired and RepairsFailed count the damaged copies the run replaced, or could not, from another connection.
	Repaired      int64 `json:"repaired"`
	RepairsFailed int64 `json:"repairsFailed"`
	// MissingLooked counts the copies marked missing that the run read once more; MissingFound those that were whole again.
	MissingLooked int64  `json:"missingLooked"`
	MissingFound  int64  `json:"missingFound"`
	Ended         string `json:"ended"`
	Error         string `json:"error,omitempty"`
}

// Result is the result of a run, stored as the job's result.
type Result struct {
	Connections []ConnectionResult `json:"connections"`
}

// Run reads the due copies of every connection of this replica, or of the one named, at once: one reader per connection, each
// throttled to that connection's scrub_bytes_per_second, so a slow provider holds back no fast one. It ends when every reader
// ended; the error is the first database error of a reader, the result is complete either way.
func (s *Scrubber) Run(ctx context.Context, o RunOptions) (Result, error) {
	conns := s.o.Connections.List()
	if o.Connection != "" {
		c, ok := s.o.Connections.Get(o.Connection)
		if !ok {
			return Result{Connections: []ConnectionResult{}}, fmt.Errorf("%w: %s", ErrUnknownConnection, o.Connection)
		}
		conns = []*blob.Connection{c}
	}
	deadline := s.o.Clock.Now().Add(s.o.RunDuration)
	res := Result{Connections: make([]ConnectionResult, len(conns))}
	var wg sync.WaitGroup
	for i, c := range conns {
		wg.Add(1)
		go func(i int, c *blob.Connection) {
			defer wg.Done()
			res.Connections[i] = s.connection(ctx, c, deadline, o.Draining)
		}(i, c)
	}
	wg.Wait()
	var err error
	for _, cr := range res.Connections {
		if cr.Ended == EndedError && err == nil {
			err = fmt.Errorf("scrub of connection %s: %s", cr.Connection, cr.Error)
		}
	}
	return res, err
}

// stop reports why a reader stops before its next copy, or "" to go on.
func (s *Scrubber) stop(ctx context.Context, deadline time.Time, draining <-chan struct{}) string {
	select {
	case <-draining:
		return EndedDraining
	default:
	}
	switch {
	case ctx.Err() != nil:
		return EndedCancelled
	case !s.o.Clock.Now().Before(deadline):
		return EndedDuration
	}
	return ""
}

// connection is one connection's reader: it reads the copies marked missing whose look is due once more, then walks the due
// copies, never verified first, then the oldest verification first, and reads each to its end.
func (s *Scrubber) connection(ctx context.Context, c *blob.Connection, deadline time.Time, draining <-chan struct{}) ConnectionResult {
	r := ConnectionResult{Connection: c.ID()}
	th := newThrottle(s.o.Clock, c.ScrubBytesPerSecond())
	if ended := s.missing(ctx, c, deadline, draining, th, &r); ended != "" {
		r.Ended = ended
		return r
	}
	cur := Cursor{}
	for {
		if why := s.stop(ctx, deadline, draining); why != "" {
			r.Ended = why
			return r
		}
		if _, rejecting := c.Rejecting(); rejecting {
			r.Ended = EndedCircuitOpen
			return r
		}
		page, next, err := s.o.Ledger.Due(ctx, c.ID(), c.ScrubPeriod(), cur, DuePage)
		if err != nil {
			if ctx.Err() != nil {
				r.Ended = EndedCancelled
				return r
			}
			r.Ended, r.Error = EndedError, err.Error()
			return r
		}
		if len(page) == 0 {
			r.Ended = EndedDone
			return r
		}
		cur = next
		for _, due := range page {
			if why := s.stop(ctx, deadline, draining); why != "" {
				r.Ended = why
				return r
			}
			if ended := s.copy(ctx, c, due, th, &r); ended != "" {
				r.Ended = ended
				return r
			}
		}
	}
}

// MissingLookInterval is how long after the store last answered for a copy marked missing the scrub reads it once more
// (Q-STOR-265): a mark that a store's wrong "no such object" left, on a connection where no other copy repairs it, heals within a
// day of the store answering right again; a copy whose object is gone costs one request a day.
const MissingLookInterval = 24 * time.Hour

// missing reads the copies of c marked missing whose look is due (MissingLookInterval): one whose object reads whole again is
// recorded present and verified; one still gone or damaged has its checked_at moved. It returns why the reader ends, or "".
func (s *Scrubber) missing(ctx context.Context, c *blob.Connection, deadline time.Time, draining <-chan struct{}, th *throttle, r *ConnectionResult) string {
	var after blob.Digest
	first := true
	for {
		if why := s.stop(ctx, deadline, draining); why != "" {
			return why
		}
		if _, rejecting := c.Rejecting(); rejecting {
			return EndedCircuitOpen
		}
		page, err := s.o.Ledger.Missing(ctx, c.ID(), MissingLookInterval, after, first, DuePage)
		if err != nil {
			if ctx.Err() != nil {
				return EndedCancelled
			}
			r.Error = err.Error()
			return EndedError
		}
		if len(page) == 0 {
			return ""
		}
		first, after = false, page[len(page)-1].Digest
		for _, g := range page {
			if why := s.stop(ctx, deadline, draining); why != "" {
				return why
			}
			if ended := s.lookAgain(ctx, c, g, th, r); ended != "" {
				return ended
			}
		}
	}
}

// lookAgain reads one copy marked missing to its end.
func (s *Scrubber) lookAgain(ctx context.Context, c *blob.Connection, g Gone, th *throttle, r *ConnectionResult) string {
	out, n, err := readWhole(ctx, c, blob.Copy{Connection: c.ID(), ObjectKey: g.Key, StoredSize: g.StoredSize}, g.Digest, th)
	r.Bytes += n
	s.o.Metrics.read(c.ID(), n)
	switch out {
	case outcomeCancelled:
		return EndedCancelled
	case outcomeError:
		if errors.Is(err, blob.ErrCircuitOpen) {
			return EndedCircuitOpen
		}
		return ""
	case outcomeOK:
		r.MissingLooked++
		if ok, werr := s.o.Ledger.Whole(ctx, g.Digest, c.ID(), blob.CopyMissing, g.StateChangedUs, g.StoredSize, "", false); werr == nil && ok {
			r.MissingFound++
			s.o.Log.Info("the object of a copy marked missing is whole again", "connection", c.ID(), "sha256", g.Digest.Hex())
		}
		return ""
	}
	r.MissingLooked++
	if lerr := s.o.Ledger.Looked(ctx, g.Digest, c.ID()); lerr != nil {
		s.o.Log.Warn("could not record the look at a copy marked missing", "connection", c.ID(), "sha256", g.Digest.Hex(), "error", lerr)
	}
	return ""
}

// copy reads one due copy and records what it found. It returns why the reader ends, or "" to go on.
func (s *Scrubber) copy(ctx context.Context, c *blob.Connection, due Due, th *throttle, r *ConnectionResult) string {
	cp := blob.Copy{Connection: c.ID(), ObjectKey: due.Key, StoredSize: due.StoredSize, State: blob.CopyPresent}
	out, n, err := readWhole(ctx, c, cp, due.Digest, th)
	r.Bytes += n
	s.o.Metrics.read(c.ID(), n)
	switch out {
	case outcomeCancelled:
		return EndedCancelled
	case outcomeOK:
		r.Read++
		r.Verified++
		s.o.Metrics.copy(c.ID(), ResultOK)
		if err := s.o.Ledger.Verified(ctx, due.Digest, c.ID(), due.CreatedUs); err != nil {
			s.o.Log.Warn("could not stamp a verified copy", "connection", c.ID(), "sha256", due.Digest.Hex(), "error", err)
		}
	case outcomeCorrupt, outcomeMissing:
		status, result := blob.CopyCorrupt, ResultCorrupt
		r.Read++
		if out == outcomeMissing {
			status, result = blob.CopyMissing, ResultMissing
			r.Missing++
		} else {
			r.Corrupt++
		}
		s.o.Metrics.copy(c.ID(), result)
		if s.o.Ledger.Damaged(ctx, due.Digest, cp, status, blob.Damage{Source: blob.DamageScrub, Reason: reasonOf(err, blob.ReasonDigest)}) {
			s.repairCounted(ctx, c, due.Digest, th, r)
		}
	case outcomeError:
		r.Read++
		r.Errors++
		s.o.Metrics.copy(c.ID(), ResultError)
		s.o.Log.Warn("the scrub could not read a copy; it stays due", "connection", c.ID(), "sha256", due.Digest.Hex(), "error", err)
		if errors.Is(err, blob.ErrCircuitOpen) {
			return EndedCircuitOpen
		}
	case outcomeUnknownKey:
		r.Read++
		r.UnknownKey++
		s.o.Metrics.copy(c.ID(), ResultUnknownKey)
		s.o.Log.Warn("the record of a copy names a storage key this replica does not hold; it is not marked and stays due until the export of the storage keys is imported",
			"connection", c.ID(), "sha256", due.Digest.Hex(), "error", err)
	}
	return ""
}

// repairCounted repairs a copy the run marked and counts the outcome.
func (s *Scrubber) repairCounted(ctx context.Context, c *blob.Connection, d blob.Digest, th *throttle, r *ConnectionResult) {
	if c.Drain() {
		return // a draining connection's copies are deleted, not repaired (R-STOR-056)
	}
	outcome, moved := s.repair(ctx, c, d, th)
	r.Bytes += moved
	switch outcome {
	case "":
		return
	case RepairDone:
		r.Repaired++
	default:
		r.RepairsFailed++
	}
	s.o.Metrics.repair(c.ID(), outcome)
}
