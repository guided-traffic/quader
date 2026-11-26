package uploads

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
)

// The sessions on their connection (storage.md R-STOR-054), at the unit tier (R-STOR-063): two in-memory stores, each behind a
// guard with a clock of the test, the session tables of memRows and, for the finalize, the blob store's commit protocol over a table
// of blob rows in memory. Parts of testPart bytes, at most a few hundred bytes per test.

// twoConns is the fixture of two connections, primary and secondary.
type twoConns struct {
	*fixture
	a, b  *blobtest.Store
	clock *testClock
	conns *blob.Connections
	blobs *blobRows
	store *blob.Store
}

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }

func (c *testClock) advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// unavailable is a fault that answers every call with a transient 503.
func unavailable(op, key string) error {
	return &blob.StoreError{Op: op, Key: key, Code: "SlowDown", Status: 503, Kind: blob.KindTransient}
}

func newTwoConns(t *testing.T) *twoConns {
	t.Helper()
	f := newFixture(t)
	p := "quader"
	f.cfg.Connections = []config.StorageConnection{
		config.StorageConnection{ID: "primary", Bucket: "a", Prefix: &p}.WithDefaults(),
		config.StorageConnection{ID: "secondary", Bucket: "b", Prefix: &p}.WithDefaults(),
	}
	breaker := config.StorageS3Breaker{FailureThreshold: 1, OpenDuration: time.Minute}
	f.cfg.ObjectStore.CircuitBreaker = breaker
	x := &twoConns{fixture: f, a: blobtest.New(), b: blobtest.New(), clock: &testClock{t: time.Now()}, blobs: newBlobRows()}
	x.a.MinPartSize, x.b.MinPartSize = testPart, testPart
	ga := blob.NewGuard(x.a, breaker, blob.WithGuardConnection("primary"), blob.WithGuardClock(x.clock.now))
	gb := blob.NewGuard(x.b, breaker, blob.WithGuardConnection("secondary"), blob.WithGuardClock(x.clock.now))
	var err error
	if x.conns, err = blob.NewConnections(blob.NewConnection(f.cfg.Connections[0], ga), blob.NewConnection(f.cfg.Connections[1], gb)); err != nil {
		t.Fatal(err)
	}
	storeCfg := f.cfg
	storeCfg.Spool.Dir = t.TempDir()
	if x.store, err = blob.New(storeCfg, x.conns, x.blobs, blob.WithRows(x.blobs)); err != nil {
		t.Fatal(err)
	}
	return x
}

func (x *twoConns) service() *Service {
	x.t.Helper()
	cfg := x.cfg
	cfg.Spool.Dir = x.t.TempDir()
	sp, err := blob.NewSpool(cfg.Spool)
	if err != nil {
		x.t.Fatal(err)
	}
	return newService(x.rows, Options{Connections: x.conns, Spool: sp, Storage: cfg, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

// commit is the finalize's commit as the host runs it: the blob commit protocol on the staged blob, the end of the session in its
// transaction.
func (x *twoConns) commit(ctx context.Context, staged *blob.Spooled, markDone func(context.Context, meta.Tx, int64) error) error {
	_, err := x.store.Commit(ctx, staged, func(ctx context.Context, tx meta.Tx) error { return markDone(ctx, tx, 42) })
	if err != nil {
		x.rows.rollbackTx()
		return err
	}
	x.rows.commitTx()
	return nil
}

// staged reports whether a store holds a staging object or an unfinished multipart upload of the session.
func staged(s *blobtest.Store, id uuid.UUID) bool {
	return len(s.Keys(blob.SessionPrefix("quader", id.String()))) > 0 || s.OpenUploads() > 0
}

func retryAfterOf(err error) time.Duration {
	var e *errs.Error
	if errors.As(err, &e) {
		return e.RetryAfter
	}
	return 0
}

func TestR_STOR_054_AnUploadSessionStaysOnItsConnection(t *testing.T) {
	ctx := context.Background()
	t.Run("R-STOR-054 a session records the first connection, stages only there, and its finalize writes the content object there and queues the second", func(t *testing.T) {
		x := newTwoConns(t)
		s := x.service()
		sess, err := s.Start(ctx, 3, x.sc)
		if err != nil {
			t.Fatal(err)
		}
		if got := x.rows.conn(sess.ID); got != "primary" {
			t.Fatalf("the session records %q, want primary", got)
		}
		data := random(3*testPart + 5) // two parts, then a tail
		if _, err := x.append(s, sess.ID, 0, data[:2*testPart+3]); err != nil {
			t.Fatal(err)
		}
		if !staged(x.a, sess.ID) || staged(x.b, sess.ID) {
			t.Fatalf("the parts and tails lie on primary %v and secondary %v", x.a.Keys(""), x.b.Keys(""))
		}
		if _, err := s.Finalize(ctx, sess.ID, x.sc, -1, sha256.Sum256(data), bytes.NewReader(data[2*testPart+3:]), 0, x.commit); err != nil {
			t.Fatal(err)
		}
		d := blob.Digest(sha256.Sum256(data))
		if got, ok := x.a.Data(blob.Key("quader", d)); !ok || !bytes.Equal(got, data) {
			t.Fatal("the content object is not on primary")
		}
		if _, ok := x.b.Data(blob.Key("quader", d)); ok {
			t.Fatal("the finalize wrote to secondary, which the reconciliation fills")
		}
		if !x.blobs.has("primary", d) || x.blobs.has("secondary", d) || !x.blobs.queued("secondary", d) {
			t.Fatalf("copies %v, queue %v: want a copy on primary and a queue row for secondary", x.blobs.copyKeys(), x.blobs.queueKeys())
		}
		if staged(x.a, sess.ID) {
			t.Fatalf("the staging objects remain: %v", x.a.Keys(blob.UploadsPrefix("quader")))
		}
	})
	t.Run("R-STOR-054 with its connection failing after the first part a PATCH answers 503 with Retry-After, and the session continues there once it is back", func(t *testing.T) {
		x := newTwoConns(t)
		s := x.service()
		sess, err := s.Start(ctx, 3, x.sc)
		if err != nil {
			t.Fatal(err)
		}
		data := random(3*testPart + 7)
		if _, err := x.append(s, sess.ID, 0, data[:testPart]); err != nil {
			t.Fatal(err)
		}
		x.a.SetFault(unavailable)
		for i := range 2 { // the first meets the failure and opens the circuit, the second meets the open circuit
			_, err := x.append(s, sess.ID, testPart, data[testPart:2*testPart])
			if errKind(err) != errs.Unavailable || retryAfterOf(err) < 5*time.Second {
				t.Fatalf("PATCH %d while primary fails: %v (Retry-After %s), want 503 with Retry-After", i+1, err, retryAfterOf(err))
			}
		}
		if got := x.rows.conn(sess.ID); got != "primary" {
			t.Fatalf("a session that has written moved to %q", got)
		}
		x.a.SetFault(nil)
		x.clock.advance(2 * time.Minute) // past the circuit's open duration: the next call is its probe
		if _, err := x.append(s, sess.ID, testPart, data[testPart:2*testPart]); err != nil {
			t.Fatalf("the same session after the fault: %v", err)
		}
		if _, err := s.Finalize(ctx, sess.ID, x.sc, -1, sha256.Sum256(data), bytes.NewReader(data[2*testPart:]), 0, x.commit); err != nil {
			t.Fatal(err)
		}
		if got, ok := x.a.Data(blob.Key("quader", sha256.Sum256(data))); !ok || !bytes.Equal(got, data) {
			t.Fatal("the content object on primary is not the blob")
		}
		if staged(x.b, sess.ID) {
			t.Fatal("the session staged on secondary")
		}
	})
	t.Run("R-STOR-054 a session whose connection is not writable before its first object moves to the second and lies there", func(t *testing.T) {
		x := newTwoConns(t)
		s := x.service()
		sess, err := s.Start(ctx, 3, x.sc)
		if err != nil {
			t.Fatal(err)
		}
		x.a.SetFault(unavailable)
		data := random(2*testPart + 3)
		if _, err := x.append(s, sess.ID, 0, data[:testPart+1]); errKind(err) != errs.Unavailable {
			t.Fatalf("the first PATCH meets primary failing: %v", err)
		}
		if _, err := x.append(s, sess.ID, 0, data[:testPart+1]); err != nil {
			t.Fatalf("the PATCH after the circuit opened: %v", err)
		}
		if got := x.rows.conn(sess.ID); got != "secondary" {
			t.Fatalf("the session records %q, want secondary", got)
		}
		if _, err := s.Finalize(ctx, sess.ID, x.sc, -1, sha256.Sum256(data), bytes.NewReader(data[testPart+1:]), 0, x.commit); err != nil {
			t.Fatal(err)
		}
		d := blob.Digest(sha256.Sum256(data))
		if got, ok := x.b.Data(blob.Key("quader", d)); !ok || !bytes.Equal(got, data) {
			t.Fatal("the content object is not on secondary")
		}
		x.a.SetFault(nil)
		if keys := x.a.Keys(""); len(keys) != 0 || x.a.OpenUploads() != 0 {
			t.Fatalf("primary holds %v", keys)
		}
		if !x.blobs.has("secondary", d) || !x.blobs.queued("primary", d) {
			t.Fatalf("copies %v, queue %v", x.blobs.copyKeys(), x.blobs.queueKeys())
		}
		// a session started while primary's circuit is open starts on secondary
		next, err := s.Start(ctx, 3, x.sc)
		if err != nil {
			t.Fatal(err)
		}
		if got := x.rows.conn(next.ID); got != "secondary" {
			t.Fatalf("a session started while primary is not writable records %q", got)
		}
	})
}

// blobRows is the blob store's table of blob rows, copies and queue rows in memory (blob.WithRows), enough for the commit of a
// finalize: a transaction's writes are undone when it fails.
type blobRows struct {
	mu     sync.Mutex
	live   map[blob.Digest]bool
	copies map[string]bool // connection/digest
	queue  map[string]bool // connection/digest
	writes map[string]blob.CopyWrite
}

func newBlobRows() *blobRows {
	return &blobRows{live: map[blob.Digest]bool{}, copies: map[string]bool{}, queue: map[string]bool{}, writes: map[string]blob.CopyWrite{}}
}

// write is the copy row a commit recorded for a blob on a connection.
func (b *blobRows) write(conn string, d blob.Digest) (blob.CopyWrite, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w, ok := b.writes[ref(conn, d)]
	return w, ok
}

type memTx struct{ meta.TxState }

func (b *blobRows) InTx(ctx context.Context, fn func(meta.Tx) error) error {
	b.mu.Lock()
	live, copies, queue := maps.Clone(b.live), maps.Clone(b.copies), maps.Clone(b.queue)
	b.mu.Unlock()
	tx := &memTx{}
	err := fn(tx)
	if err == nil {
		err = tx.RunBefore(ctx, tx)
	}
	if err != nil {
		b.mu.Lock()
		b.live, b.copies, b.queue = live, copies, queue
		b.mu.Unlock()
		tx.RunRollback()
		return err
	}
	tx.RunAfter()
	return nil
}

func ref(conn string, d blob.Digest) string { return conn + "/" + d.Hex() }

func (b *blobRows) has(conn string, d blob.Digest) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.copies[ref(conn, d)]
}

func (b *blobRows) queued(conn string, d blob.Digest) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.queue[ref(conn, d)]
}

func (b *blobRows) copyKeys() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Join(sortedKeys(b.copies), ",")
}

func (b *blobRows) queueKeys() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Join(sortedKeys(b.queue), ",")
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func (b *blobRows) LockShare(_ context.Context, _ meta.Tx, d blob.Digest) (string, string, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.live[d] {
		return "live", blob.StatusOK, true, nil
	}
	return "", "", false, nil
}

func (b *blobRows) InsertLive(_ context.Context, _ meta.Tx, sp *blob.Spooled) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.live[sp.SHA256] {
		return false, nil
	}
	b.live[sp.SHA256] = true
	return true, nil
}

func (b *blobRows) Exists(_ context.Context, _ meta.Tx, d blob.Digest) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.live[d], nil
}

func (b *blobRows) Enqueue(context.Context, meta.Tx, blob.Digest) error { return nil }

func (b *blobRows) Row(_ context.Context, _ meta.Tx, d blob.Digest) (blob.Row, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.live[d] {
		return blob.Row{}, false, nil
	}
	return blob.Row{State: "live", ContentStatus: blob.StatusOK}, true, nil
}

func (b *blobRows) ClaimRecheck(context.Context, meta.Tx, blob.Digest, time.Duration) (bool, error) {
	return false, nil
}

func (b *blobRows) Copies(context.Context, meta.Tx, blob.Digest) ([]blob.Copy, error) {
	return nil, nil
}

func (b *blobRows) LockForMark(_ context.Context, _ meta.Tx, d blob.Digest) (string, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return "live", b.live[d], nil
}

func (b *blobRows) PutCopy(_ context.Context, _ meta.Tx, d blob.Digest, w blob.CopyWrite) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.copies[ref(w.Connection, d)] = true
	b.writes[ref(w.Connection, d)] = w
	return nil
}

func (b *blobRows) SetCopyState(context.Context, meta.Tx, blob.Digest, string, string, string, blob.Damage) (bool, error) {
	return false, nil
}

func (b *blobRows) StampVerified(context.Context, meta.Tx, blob.Digest, string, time.Duration) error {
	return nil
}

func (b *blobRows) QueueCopy(_ context.Context, _ meta.Tx, conn string, d blob.Digest) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.queue[ref(conn, d)] = true
	return nil
}

func (b *blobRows) SetContentStatus(context.Context, meta.Tx, blob.Digest, string) (bool, error) {
	return false, nil
}
