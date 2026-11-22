package blob

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/config"
)

// Guard decorates an ObjectStore with the circuit breaker of R-STOR-033 and the
// request metrics. After circuit_breaker.failure_threshold consecutive failures
// (network errors, timeouts, 5xx, credential errors; not 404 or other 4xx: a credential error is a failure to get
// a credential or an answer that names the credential as the cause, IsCredentialRejection) the
// circuit opens for circuit_breaker.open_duration, during which every call
// fails at once with ErrCircuitOpen; then one probe request goes through and a
// success closes the circuit.
//
// There is one Guard per connection and replica (R-STOR-060); it keeps no state anywhere else, and one connection's open
// circuit changes nothing on another's.
type Guard struct {
	inner     ObjectStore
	threshold int
	openFor   time.Duration
	now       func() time.Time
	metrics   *Metrics
	log       *slog.Logger
	conn      string // the connection's id, the label connection of the metrics and the log
	source    string // the connection's credentials_source, named in the log line of a rejected credential

	mu        sync.Mutex
	failures  int
	openUntil time.Time
	open      bool
	probing   bool
}

// GuardOption configures a Guard.
type GuardOption func(*Guard)

// WithGuardMetrics sets the metrics.
func WithGuardMetrics(m *Metrics) GuardOption { return func(g *Guard) { g.metrics = m } }

// WithGuardConnection names the connection the Guard stands for: the value of the label connection of its metrics and
// of its log lines (R-STOR-060). Without it the Guard is the connection default's.
func WithGuardConnection(id string) GuardOption { return func(g *Guard) { g.conn = id } }

// WithGuardLogger sets the logger and the credential source (the connection's credentials_source) that the Guard names
// when the store rejects the credential (R-HA-007). The log line carries the connection, the source and the store's code,
// never a key or a secret.
func WithGuardLogger(log *slog.Logger, credentialSource string) GuardOption {
	return func(g *Guard) { g.log, g.source = log, credentialSource }
}

// WithGuardClock replaces the clock; tests use it.
func WithGuardClock(now func() time.Time) GuardOption { return func(g *Guard) { g.now = now } }

// NewGuard wraps inner.
func NewGuard(inner ObjectStore, cfg config.StorageS3Breaker, opts ...GuardOption) *Guard {
	g := &Guard{inner: inner, threshold: max(1, cfg.FailureThreshold), openFor: cfg.OpenDuration, now: time.Now, conn: config.DefaultConnectionID}
	for _, o := range opts {
		o(g)
	}
	g.metrics.circuit(g.conn, false)
	return g
}

var _ ObjectStore = (*Guard)(nil)

// Inner is the store the Guard decorates.
func (g *Guard) Inner() ObjectStore { return g.inner }

// Open reports whether the circuit is open (or probing).
func (g *Guard) Open() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.open
}

// The states of a circuit as the replica registry's object-store view writes them (R-OPS-003).
const (
	CircuitClosed   = "closed"
	CircuitOpen     = "open"
	CircuitHalfOpen = "half_open"
)

// Circuit is the state of the circuit: closed, open, or half_open once its open duration has passed (the next call is the
// probe) and while the probe is out.
func (g *Guard) Circuit() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case !g.open:
		return CircuitClosed
	case g.probing || !g.now().Before(g.openUntil):
		return CircuitHalfOpen
	}
	return CircuitOpen
}

// Rejecting reports whether a call would fail at once with ErrCircuitOpen, and the Retry-After a client is given
// (R-HA-010): the circuit is open and its open duration has not passed, or the probe is out.
func (g *Guard) Rejecting() (time.Duration, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.open || (!g.probing && !g.now().Before(g.openUntil)) {
		return 0, false
	}
	return circuitRetryAfter(g.openFor), true
}

// admit decides whether a call may go to the store.
func (g *Guard) admit() (probe bool, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.open {
		return false, nil
	}
	if g.probing || g.now().Before(g.openUntil) {
		return false, &circuitOpenError{retry: circuitRetryAfter(g.openFor)}
	}
	g.probing = true
	return true, nil
}

// settle records the outcome of a call that was admitted.
func (g *Guard) settle(probe bool, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if probe {
		g.probing = false
	}
	if err != nil && (errors.Is(err, context.Canceled) || (errors.Is(err, context.DeadlineExceeded) && !IsTransient(err))) {
		// The caller gave up or its deadline ended the call: no evidence either
		// way. A deadline that ended a call inside the transport arrives as a
		// transient StoreError and counts.
		return
	}
	if err == nil || !countsAgainstCircuit(err) {
		// The store answered; a 404 or another 4xx is an answer too.
		g.failures = 0
		if g.open {
			g.open = false
			g.metrics.circuit(g.conn, false)
		}
		return
	}
	g.failures++
	if probe || g.failures >= g.threshold {
		if g.log != nil && IsCredentialRejection(err) && (probe || !g.open) {
			var se *StoreError
			_ = errors.As(err, &se)
			g.log.Warn("the object store rejects the credential; blob operations on the connection fail fast until it accepts one",
				"connection", g.conn, "credential_source", g.source, "code", se.Code, "status", se.Status, "op", se.Op)
		}
		g.open = true
		g.openUntil = g.now().Add(g.openFor)
		g.metrics.circuit(g.conn, true)
	}
}

// countsAgainstCircuit is whether a failed call is evidence that the store is unavailable to this replica: a transient
// failure, or an answer that rejects the credential (R-HA-007).
func countsAgainstCircuit(err error) bool { return IsTransient(err) || IsCredentialRejection(err) }

func requestOutcome(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrCircuitOpen):
		return "circuit_open"
	}
	return "error"
}

// call runs f under the breaker and records metrics for op.
func (g *Guard) call(op string, f func() error) error {
	probe, err := g.admit()
	if err != nil {
		g.metrics.request(g.conn, op, "circuit_open", 0)
		return err
	}
	start := g.now()
	err = f()
	g.settle(probe, err)
	if err != nil && countsAgainstCircuit(err) {
		// the call that opened the circuit, or the probe that kept it open, is answered with the circuit's Retry-After too
		if after, rejecting := g.Rejecting(); rejecting {
			err = &retryHintError{err: err, retry: after}
		}
	}
	g.metrics.request(g.conn, op, requestOutcome(err), g.now().Sub(start).Seconds())
	return err
}

// Put implements ObjectStore.
func (g *Guard) Put(ctx context.Context, key string, body io.ReadSeeker, size int64, sum Checksum, md Metadata) error {
	err := g.call("put", func() error { return g.inner.Put(ctx, key, body, size, sum, md) })
	if err == nil {
		g.metrics.bytes(g.conn, "up", size)
	}
	return err
}

// CreateMultipart implements ObjectStore.
func (g *Guard) CreateMultipart(ctx context.Context, key, algorithm string, md Metadata) (id string, err error) {
	err = g.call("create_multipart", func() (e error) { id, e = g.inner.CreateMultipart(ctx, key, algorithm, md); return })
	return id, err
}

// UploadPart implements ObjectStore.
func (g *Guard) UploadPart(ctx context.Context, key, uploadID string, number int, body io.ReadSeeker, size int64, sum Checksum) (p Part, err error) {
	err = g.call("upload_part", func() (e error) {
		p, e = g.inner.UploadPart(ctx, key, uploadID, number, body, size, sum)
		return
	})
	if err == nil {
		g.metrics.bytes(g.conn, "up", size)
	}
	return p, err
}

// CompleteMultipart implements ObjectStore.
func (g *Guard) CompleteMultipart(ctx context.Context, key, uploadID string, parts []Part) error {
	return g.call("complete_multipart", func() error { return g.inner.CompleteMultipart(ctx, key, uploadID, parts) })
}

// AbortMultipart implements ObjectStore.
func (g *Guard) AbortMultipart(ctx context.Context, key, uploadID string) error {
	return g.call("abort_multipart", func() error { return g.inner.AbortMultipart(ctx, key, uploadID) })
}

// Copy implements ObjectStore.
func (g *Guard) Copy(ctx context.Context, srcKey, dstKey string, size int64, md Metadata) error {
	return g.call("copy", func() error { return g.inner.Copy(ctx, srcKey, dstKey, size, md) })
}

// Head implements ObjectStore.
func (g *Guard) Head(ctx context.Context, key string) (info ObjectInfo, err error) {
	err = g.call("head", func() (e error) { info, e = g.inner.Head(ctx, key); return })
	return info, err
}

// Get implements ObjectStore. The breaker judges the request and the response
// headers; a failure in the middle of the body is the reader's error.
func (g *Guard) Get(ctx context.Context, key string, rng *ByteRange) (body io.ReadCloser, info ObjectInfo, err error) {
	err = g.call("get", func() (e error) { body, info, e = g.inner.Get(ctx, key, rng); return })
	if err != nil {
		return nil, info, err
	}
	return &countingBody{ReadCloser: body, m: g.metrics, conn: g.conn}, info, nil
}

type countingBody struct {
	io.ReadCloser
	m    *Metrics
	conn string
}

func (c *countingBody) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	c.m.bytes(c.conn, "down", int64(n))
	return n, err
}

// Delete implements ObjectStore.
func (g *Guard) Delete(ctx context.Context, key string) error {
	return g.call("delete", func() error { return g.inner.Delete(ctx, key) })
}

// DeleteMany implements ObjectStore.
func (g *Guard) DeleteMany(ctx context.Context, keys []string) (failed []DeleteFailure, err error) {
	err = g.call("delete_many", func() (e error) { failed, e = g.inner.DeleteMany(ctx, keys); return })
	return failed, err
}

// List implements ObjectStore.
func (g *Guard) List(ctx context.Context, prefix string, opts ListOptions) (page ListPage, err error) {
	err = g.call("list", func() (e error) { page, e = g.inner.List(ctx, prefix, opts); return })
	return page, err
}

// ListMultipart implements ObjectStore.
func (g *Guard) ListMultipart(ctx context.Context, prefix string) (ups []MultipartUpload, err error) {
	err = g.call("list_multipart", func() (e error) { ups, e = g.inner.ListMultipart(ctx, prefix); return })
	return ups, err
}

// Presign implements ObjectStore. Signing is local work and sends no request, so it neither counts against the circuit nor
// closes it; but a connection whose circuit is open signs no link (R-STOR-062, Q-STOR-245): a link to a store the replica
// cannot reach would only move the failure to the client.
func (g *Guard) Presign(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if after, rejecting := g.Rejecting(); rejecting {
		return "", &circuitOpenError{retry: after}
	}
	return g.inner.Presign(ctx, key, ttl)
}

// ListMultipartPage implements [MultipartPager] when the guarded store does; a store that does not answers with the whole listing.
func (g *Guard) ListMultipartPage(ctx context.Context, prefix string, after MultipartMarker, max int) (page []MultipartUpload, next MultipartMarker, err error) {
	p, ok := g.inner.(MultipartPager)
	if !ok {
		if after != (MultipartMarker{}) {
			return nil, MultipartMarker{}, nil
		}
		page, err = g.ListMultipart(ctx, prefix)
		return page, MultipartMarker{}, err
	}
	err = g.call("list_multipart", func() (e error) { page, next, e = p.ListMultipartPage(ctx, prefix, after, max); return })
	return page, next, err
}

// errNoBucketCopy is CopyFrom of a guarded store that copies only inside its own bucket.
var errNoBucketCopy = errors.New("the object store copies only inside its own bucket")

// Location implements [BucketCopier]: the guarded store's, empty when it has none.
func (g *Guard) Location() (endpoint, bucket string) {
	if c, ok := g.inner.(BucketCopier); ok {
		return c.Location()
	}
	return "", ""
}

// CopyFrom implements [BucketCopier] under the breaker; a guarded store without it answers errNoBucketCopy, which is no
// failure of the store.
func (g *Guard) CopyFrom(ctx context.Context, srcBucket, srcKey, dstKey string, size int64, md Metadata) error {
	c, ok := g.inner.(BucketCopier)
	if !ok {
		return &StoreError{Op: "CopyObject", Key: srcKey, Code: "NotImplemented", Status: 501, Kind: KindOther, Err: errNoBucketCopy}
	}
	return g.call("copy", func() error { return c.CopyFrom(ctx, srcBucket, srcKey, dstKey, size, md) })
}
