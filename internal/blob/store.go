package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/guided-traffic/quader/internal/config"
)

// Store is the blob store of one replica: its spool, its object-store connections
// and the database it commits to. Several Stores (several replicas) work on
// the same connections and database without coordinating, which is what the commit
// protocol and garbage collection are built for.
type Store struct {
	cfg       config.Storage
	conns     *Connections
	db        DB
	rows      Rows
	spool     *Spool
	log       *slog.Logger
	metrics   *Metrics
	now       func() time.Time
	wait      time.Duration
	parts     chan struct{}
	onDamage  func(ctx context.Context, d Digest, status string)
	onAdded   ConnectionAddedFunc
	spoolOpts []SpoolOption
	memory    func(ctx context.Context, n int64) (undo func())
}

// Option configures a Store.
type Option func(*Store)

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) Option { return func(s *Store) { s.log = l } }

// WithMetrics sets the metrics.
func WithMetrics(m *Metrics) Option { return func(s *Store) { s.metrics = m } }

// WithClock replaces the clock of the commit deadline; tests use it.
func WithClock(now func() time.Time) Option { return func(s *Store) { s.now = now } }

// WithWaitPoll sets how often a commit that met a deleting row looks again.
func WithWaitPoll(d time.Duration) Option { return func(s *Store) { s.wait = d } }

// WithSpoolOptions passes options to the spool the store creates.
func WithSpoolOptions(opts ...SpoolOption) Option {
	return func(s *Store) { s.spoolOpts = append(s.spoolOpts, opts...) }
}

// WithMemory makes Open tell settle, before it asks the object store, what the
// reader will hold: DownloadCost bytes, or RangeCost for a ranged read, for the
// request of its context (Q-ARCH-328, Q-ARCH-330). settle returns the function
// that takes them off again; Open calls it when the reader is closed or the
// open fails.
func WithMemory(settle func(ctx context.Context, n int64) (undo func())) Option {
	return func(s *Store) { s.memory = settle }
}

// WithDamageHook is called after a download verification marked a blob damaged
// (R-STOR-013): the audit event of the detection hangs here.
func WithDamageHook(f func(ctx context.Context, d Digest, status string)) Option {
	return func(s *Store) { s.onDamage = f }
}

// New builds the store over its connections and its spool; the spool directory is emptied. The connections are recorded in
// the database by [Store.RecordConnections] before the store commits (the composition root does it at start).
func New(cfg config.Storage, conns *Connections, db DB, opts ...Option) (*Store, error) {
	if conns == nil || db == nil {
		return nil, errors.New("blob store needs its object-store connections and a database")
	}
	s := &Store{
		cfg: cfg, conns: conns, db: db, rows: pgRows{}, log: slog.Default(), now: time.Now, wait: 250 * time.Millisecond,
	}
	for _, o := range opts {
		o(s)
	}
	spoolOpts := append([]SpoolOption{WithSpoolLogger(s.log), WithSpoolMetrics(s.metrics)}, s.spoolOpts...)
	sp, err := NewSpool(cfg.Spool, spoolOpts...)
	if err != nil {
		return nil, fmt.Errorf("blob store: %w", err)
	}
	s.spool = sp
	// the parts of every connection share the replica's bound (R-STOR-053)
	n := cfg.ObjectStore.Multipart.MaxConcurrentParts
	if n < 1 {
		n = 1
	}
	s.parts = make(chan struct{}, n)
	return s, nil
}

// Spool is the store's spool.
func (s *Store) Spool() *Spool { return s.spool }

// Connections is the store's ordered set of connections.
func (s *Store) Connections() *Connections { return s.conns }

// minCopies is storage.replication.min_copies: the copies a commit writes before it answers (R-STOR-053).
func (s *Store) minCopies() int { return max(1, s.cfg.Replication.MinCopies) }

// Ingest spools and hashes a body with the store's default maximum size
// (R-STOR-004). The caller closes the result.
func (s *Store) Ingest(ctx context.Context, body io.Reader, o IngestOptions) (*Spooled, error) {
	if awaitedBody(ctx) {
		// the client waits for "100 Continue": while the circuit is open it is told so before the first read of the body (R-HA-010)
		if err := s.RejectUpload(); err != nil {
			return nil, err
		}
	}
	max := int64(s.cfg.Upload.MaxSize)
	if o.MaxSize <= 0 || o.MaxSize > max {
		o.MaxSize = max
	}
	return s.spool.Ingest(ctx, body, o)
}

type awaitedBodyKey struct{}

// WithAwaitedBody marks ctx as the context of a request whose client waits for "100 Continue" before it sends its body.
// [Store.Ingest] then refuses the upload before it reads the body while the object store's circuit is open; a context
// without the mark is never refused early (a remote fill, an upload without Expect, a deduplicated one).
func WithAwaitedBody(ctx context.Context) context.Context {
	return context.WithValue(ctx, awaitedBodyKey{}, true)
}

func awaitedBody(ctx context.Context) bool { v, _ := ctx.Value(awaitedBodyKey{}).(bool); return v }

// RejectUpload is the answer to an upload that has not sent its body yet, nil when it may go on: while fewer than
// storage.replication.min_copies connections are writable (their circuits open, R-STOR-053) a new blob cannot be stored, and
// a client that waits for "100 Continue" is told so before it sends the body (R-HA-010). The Retry-After is the soonest any
// of the connections that take uploads probes again.
func (s *Store) RejectUpload() error {
	if len(s.conns.Writable()) >= s.minCopies() {
		return nil
	}
	var after time.Duration
	for _, c := range s.conns.Targets() {
		if a, rejecting := c.Rejecting(); rejecting && (after == 0 || a < after) {
			after = a
		}
	}
	if after == 0 {
		after = retryAfter
	}
	return storeUnavailable(&circuitOpenError{retry: after})
}

// Row is a blob row (R-STOR-003).
type Row struct {
	Digests
	State         string
	ContentStatus string
	CreatedAt     time.Time
}

// WithRows replaces the store's access to the blob rows and their copies with a simulation: the seam of the unit tests that
// drive the commit protocol and the reads without PostgreSQL. The integration tier runs the same code against PostgreSQL.
func WithRows(r Rows) Option { return func(s *Store) { s.rows = r } }
