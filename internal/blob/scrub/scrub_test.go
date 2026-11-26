package scrub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
)

// The scrub's reader, its repair and its recheck over in-memory stores and a fake ledger (storage.md R-STOR-027, R-STOR-063:
// unit, objects of 1 KiB). The statements of the ledger and the whole flow against PostgreSQL are the integration test
// test/integration/blob/scrub_test.go.

// fakeLedger is the database side: the due copies of each connection (returned by the first page), the copy rows of each
// blob, and what the scrub recorded.
type fakeLedger struct {
	mu       sync.Mutex
	due      map[string][]Due
	copies   map[blob.Digest][]CopyRow
	verified []string
	damaged  []string
	whole    []string
	lookedAt map[string]bool
	// marks are the marks with what they named, "<connection> <status> <source> <reason>" (R-STOR-108); rewritten says per Whole
	// whether a repair wrote the object.
	marks     []string
	rewritten []bool
}

func newLedger() *fakeLedger {
	return &fakeLedger{due: map[string][]Due{}, copies: map[blob.Digest][]CopyRow{}, lookedAt: map[string]bool{}}
}

func (l *fakeLedger) Due(_ context.Context, conn string, _ time.Duration, cur Cursor, _ int) ([]Due, Cursor, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if cur.started {
		return nil, cur, nil
	}
	return slices.Clone(l.due[conn]), Cursor{started: true}, nil
}

func (l *fakeLedger) Verified(_ context.Context, d blob.Digest, conn string, _ int64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.verified = append(l.verified, conn+"/"+d.Hex()[:6])
	return nil
}

func (l *fakeLedger) set(d blob.Digest, conn, from, to string) bool {
	for i, cp := range l.copies[d] {
		if cp.Connection == conn && cp.State == from {
			l.copies[d][i].State = to
			return true
		}
	}
	return false
}

func (l *fakeLedger) Damaged(_ context.Context, d blob.Digest, cp blob.Copy, status string, dmg blob.Damage) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.damaged = append(l.damaged, cp.Connection+"/"+d.Hex()[:6]+"/"+status)
	l.marks = append(l.marks, cp.Connection+" "+status+" "+dmg.Source+" "+dmg.Reason)
	return l.set(d, cp.Connection, blob.CopyPresent, status)
}

// Missing returns the copies marked missing of conn whose look is due: every one not in looked.
func (l *fakeLedger) Missing(_ context.Context, conn string, _ time.Duration, after blob.Digest, first bool, _ int) ([]Gone, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Gone
	for d, cs := range l.copies {
		for _, cp := range cs {
			if cp.Connection == conn && cp.State == blob.CopyMissing && !l.lookedAt[conn+"/"+d.Hex()] && (first || d.Hex() > after.Hex()) {
				out = append(out, Gone{Digest: d, Key: cp.ObjectKey, StoredSize: cp.StoredSize})
			}
		}
	}
	slices.SortFunc(out, func(a, b Gone) int { return strings.Compare(a.Digest.Hex(), b.Digest.Hex()) })
	return out, nil
}

func (l *fakeLedger) Looked(_ context.Context, d blob.Digest, conn string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lookedAt[conn+"/"+d.Hex()] = true
	return nil
}

func (l *fakeLedger) Copies(_ context.Context, d blob.Digest) ([]CopyRow, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cs, ok := l.copies[d]
	return slices.Clone(cs), ok, nil
}

func (l *fakeLedger) Whole(_ context.Context, d blob.Digest, conn, from string, _, stored int64, keyID string, rewritten bool) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.whole = append(l.whole, conn+"/"+d.Hex()[:6])
	l.rewritten = append(l.rewritten, rewritten)
	for i, cp := range l.copies[d] {
		if cp.Connection == conn && cp.State == from {
			l.copies[d][i].StoredSize = stored
			if keyID != "" {
				l.copies[d][i].StorageKeyID = keyID
			}
		}
	}
	return l.set(d, conn, from, blob.CopyPresent), nil
}

func (l *fakeLedger) recorded() (verified, damaged, whole []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.verified), slices.Clone(l.damaged), slices.Clone(l.whole)
}

// fakeClock advances by every sleep.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
	return ctx.Err()
}

func newClock() *fakeClock { return &fakeClock{now: time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)} }

// rejecting is a store whose circuit is open on this replica.
type rejecting struct{ *blobtest.Store }

func (rejecting) Rejecting() (time.Duration, bool) { return 10 * time.Second, true }

func connection(id string, s blob.ObjectStore, rate config.ByteSize) *blob.Connection {
	p := "quader"
	return blob.NewConnection(config.StorageConnection{ID: id, Bucket: "bucket-" + id, Prefix: &p, ScrubPeriod: 720 * time.Hour, ScrubBytesPerSecond: rate}.WithDefaults(), s)
}

func newScrubber(t *testing.T, l Ledger, clock Clock, conns ...*blob.Connection) *Scrubber {
	t.Helper()
	cs, err := blob.NewConnections(conns...)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{Connections: cs, Ledger: l, Clock: clock, RunDuration: time.Hour, PartSize: 1024})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func data(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7) + seed
	}
	return b
}

// put stores the bytes of a blob on each store and records a present copy and a due entry per store.
func put(l *fakeLedger, b []byte, ids []string, stores ...*blobtest.Store) blob.Digest {
	d := blob.Digest(sha256.Sum256(b))
	key := blob.Key("quader", d)
	for i, s := range stores {
		s.Set(key, b, time.Now())
		l.copies[d] = append(l.copies[d], CopyRow{Copy: blob.Copy{Connection: ids[i], ObjectKey: key, StoredSize: int64(len(b)), State: blob.CopyPresent}})
		l.due[ids[i]] = append(l.due[ids[i]], Due{Digest: d, Key: key, StoredSize: int64(len(b)), CreatedUs: 1})
	}
	return d
}

func result(t *testing.T, res Result, conn string) ConnectionResult {
	t.Helper()
	for _, r := range res.Connections {
		if r.Connection == conn {
			return r
		}
	}
	t.Fatalf("no result for %s: %+v", conn, res)
	return ConnectionResult{}
}

func TestR_STOR_027_TheScrubReadsEveryDueCopyWhole(t *testing.T) {
	ctx := context.Background()
	ids := []string{"default", "secondary"}

	t.Run("R-STOR-027 a copy whose bytes were replaced with others of the same length is marked corrupt and replaced from the other connection, read back, and recorded whole", func(t *testing.T) {
		l, first, second := newLedger(), blobtest.New(), blobtest.New()
		good := data(1024, 1)
		d := put(l, good, ids, first, second)
		intact := put(l, data(1024, 2), ids, first, second)
		first.Set(blob.Key("quader", d), data(1024, 9), time.Now())
		s := newScrubber(t, l, newClock(), connection("default", first, 50<<20), connection("secondary", second, 50<<20))
		res, err := s.Run(ctx, RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		r := result(t, res, "default")
		if r.Read != 2 || r.Corrupt != 1 || r.Verified != 1 || r.Repaired != 1 || r.Ended != EndedDone {
			t.Fatalf("default = %+v", r)
		}
		if got, _ := first.Data(blob.Key("quader", d)); !bytes.Equal(got, good) {
			t.Error("the corrupt object was not replaced with the blob's bytes")
		}
		verified, damaged, whole := l.recorded()
		if !slices.Equal(damaged, []string{"default/" + d.Hex()[:6] + "/corrupt"}) || !slices.Equal(whole, []string{"default/" + d.Hex()[:6]}) {
			t.Errorf("damaged = %v, whole = %v", damaged, whole)
		}
		if !slices.Contains(verified, "default/"+intact.Hex()[:6]) || !slices.Contains(verified, "secondary/"+d.Hex()[:6]) || len(verified) != 3 {
			t.Errorf("verified = %v, want the three whole copies", verified)
		}
		// R-STOR-108: the mark names the scrub and the digest, and the repair records a copy written again
		if !slices.Equal(l.marks, []string{"default corrupt scrub digest"}) || !slices.Equal(l.rewritten, []bool{true}) {
			t.Errorf("marks = %v, rewritten = %v", l.marks, l.rewritten)
		}
	})

	t.Run("R-STOR-027 with one connection a deleted object marks its copy missing and nothing can replace it", func(t *testing.T) {
		l, only := newLedger(), blobtest.New()
		d := put(l, data(1024, 3), ids[:1], only)
		if err := only.Delete(ctx, blob.Key("quader", d)); err != nil {
			t.Fatal(err)
		}
		res, err := newScrubber(t, l, newClock(), connection("default", only, 50<<20)).Run(ctx, RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if r := result(t, res, "default"); r.Missing != 1 || r.RepairsFailed != 1 || r.Repaired != 0 {
			t.Fatalf("default = %+v", r)
		}
		if _, damaged, whole := l.recorded(); !slices.Equal(damaged, []string{"default/" + d.Hex()[:6] + "/missing"}) || len(whole) != 0 {
			t.Errorf("damaged = %v, whole = %v", damaged, whole)
		}
		if !slices.Equal(l.marks, []string{"default missing scrub not_found"}) { // R-STOR-108
			t.Errorf("marks = %v", l.marks)
		}
	})

	t.Run("R-STOR-027 a store answering 503 to every read leaves every state unchanged and counts each copy as an error", func(t *testing.T) {
		l, first := newLedger(), blobtest.New()
		for i := range 3 {
			put(l, data(1024, byte(10+i)), ids[:1], first)
		}
		first.SetFault(func(op, _ string) error {
			if op == "get" {
				return &blob.StoreError{Op: "GetObject", Status: 503, Kind: blob.KindTransient}
			}
			return nil
		})
		res, err := newScrubber(t, l, newClock(), connection("default", first, 50<<20)).Run(ctx, RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if r := result(t, res, "default"); r.Errors != 3 || r.Verified+r.Corrupt+r.Missing != 0 {
			t.Fatalf("default = %+v", r)
		}
		if verified, damaged, whole := l.recorded(); len(verified)+len(damaged)+len(whole) != 0 {
			t.Errorf("states changed: %v %v %v", verified, damaged, whole)
		}
	})

	t.Run("R-STOR-027 with a throttle of 1 KiB/s and 4 KiB due a run's reads take at least four seconds of the clock", func(t *testing.T) {
		l, first, clock := newLedger(), blobtest.New(), newClock()
		for i := range 4 {
			put(l, data(1024, byte(20+i)), ids[:1], first)
		}
		start := clock.Now()
		res, err := newScrubber(t, l, clock, connection("default", first, 1024)).Run(ctx, RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if took := clock.Now().Sub(start); took < 4*time.Second {
			t.Errorf("the run took %v of the clock, want at least 4s", took)
		}
		if r := result(t, res, "default"); r.Verified != 4 || r.Bytes != 4096 {
			t.Errorf("default = %+v", r)
		}
	})

	t.Run("R-STOR-027 a run ends after its duration and leaves the rest due; a drain and an open circuit end a reader before it reads", func(t *testing.T) {
		l, first, clock := newLedger(), blobtest.New(), newClock()
		for i := range 4 {
			put(l, data(1024, byte(30+i)), ids[:1], first)
		}
		s := newScrubber(t, l, clock, connection("default", first, 1024))
		s.o.RunDuration = 2 * time.Second
		res, err := s.Run(ctx, RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if r := result(t, res, "default"); r.Ended != EndedDuration || r.Verified != 2 {
			t.Errorf("default = %+v, want two copies read in two seconds and the run ended", r)
		}

		closed := make(chan struct{})
		close(closed)
		gets := first.Count("get")
		res, _ = newScrubber(t, l, clock, connection("default", first, 50<<20)).Run(ctx, RunOptions{Draining: closed})
		if r := result(t, res, "default"); r.Ended != EndedDraining || first.Count("get") != gets {
			t.Errorf("draining: %+v", r)
		}
		res, _ = newScrubber(t, l, clock, connection("default", rejecting{first}, 50<<20)).Run(ctx, RunOptions{})
		if r := result(t, res, "default"); r.Ended != EndedCircuitOpen || first.Count("get") != gets {
			t.Errorf("circuit open: %+v", r)
		}
	})

	t.Run("R-STOR-027 a run limited to one connection reads only it, and one for a connection this replica lacks fails as unknown", func(t *testing.T) {
		l, first, second := newLedger(), blobtest.New(), blobtest.New()
		put(l, data(1024, 40), ids, first, second)
		s := newScrubber(t, l, newClock(), connection("default", first, 50<<20), connection("secondary", second, 50<<20))
		res, err := s.Run(ctx, RunOptions{Connection: "secondary"})
		if err != nil || len(res.Connections) != 1 || first.Count("get") != 0 || second.Count("get") != 1 {
			t.Fatalf("res = %+v, err = %v, gets %d/%d", res, err, first.Count("get"), second.Count("get"))
		}
		var rec recorder
		if err := s.RunJob(ctx, json.RawMessage(`{"connection":"archive"}`), &rec); !errors.Is(err, ErrUnknownConnection) {
			t.Errorf("err = %v, want ErrUnknownConnection", err)
		}
		if err := s.RunJob(ctx, json.RawMessage(`{"connection":"Not An Id"}`), &rec); !errors.Is(err, ErrUnknownConnection) {
			t.Errorf("err = %v, want ErrUnknownConnection for a malformed id", err)
		}
	})
}

// recorder is the job's run handle.
type recorder struct{ result any }

func (r *recorder) SetResult(v any) error     { r.result = v; return nil }
func (r *recorder) Draining() <-chan struct{} { return nil }

func TestR_STOR_027_TheRepairOfADamagedCopy(t *testing.T) {
	ctx := context.Background()
	ids := []string{"default", "secondary"}

	t.Run("R-STOR-027 an object larger than a part is replaced with a multipart upload of ranged reads of the source", func(t *testing.T) {
		l, first, second := newLedger(), blobtest.New(), blobtest.New()
		first.MinPartSize = 1024
		good := data(3000, 5)
		d := put(l, good, ids, first, second)
		if err := first.Delete(ctx, blob.Key("quader", d)); err != nil {
			t.Fatal(err)
		}
		res, err := newScrubber(t, l, newClock(), connection("default", first, 50<<20), connection("secondary", second, 50<<20)).Run(ctx, RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if r := result(t, res, "default"); r.Missing != 1 || r.Repaired != 1 {
			t.Fatalf("default = %+v", r)
		}
		if got, _ := first.Data(blob.Key("quader", d)); !bytes.Equal(got, good) || first.Count("upload_part") != 3 {
			t.Errorf("the object was not written in three parts: %d parts, equal %v", first.Count("upload_part"), bytes.Equal(got, good))
		}
	})

	t.Run("R-STOR-027 a written copy that does not read back whole (an overwrite the store applies late) stays marked", func(t *testing.T) {
		l, first, second := newLedger(), blobtest.New(), blobtest.New()
		d := put(l, data(1024, 6), ids, first, second)
		stale := data(1024, 7)
		first.SetGetBody(func(io.ReadCloser) io.ReadCloser { return io.NopCloser(bytes.NewReader(stale)) })
		res, err := newScrubber(t, l, newClock(), connection("default", first, 50<<20), connection("secondary", second, 50<<20)).Run(ctx, RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if r := result(t, res, "default"); r.Corrupt != 1 || r.Repaired != 0 || r.RepairsFailed != 1 {
			t.Fatalf("default = %+v", r)
		}
		if _, _, whole := l.recorded(); len(whole) != 0 {
			t.Errorf("whole = %v, want nothing recorded", whole)
		}
		if cs, _, _ := l.Copies(ctx, d); cs[0].State != blob.CopyCorrupt {
			t.Errorf("the copy is %s, want corrupt", cs[0].State)
		}
	})

	t.Run("R-STOR-027 a source that turns out damaged is marked and the next source is taken", func(t *testing.T) {
		l, first, second, third := newLedger(), blobtest.New(), blobtest.New(), blobtest.New()
		all := []string{"default", "secondary", "archive"}
		good := data(1024, 8)
		d := put(l, good, all, first, second, third)
		first.Set(blob.Key("quader", d), data(1024, 11), time.Now())
		second.Set(blob.Key("quader", d), data(1024, 12), time.Now())
		// default is read first, and the repair tries secondary (list order) before archive
		s := newScrubber(t, l, newClock(), connection("default", first, 50<<20), connection("secondary", second, 50<<20), connection("archive", third, 50<<20))
		res, err := s.Run(ctx, RunOptions{Connection: "default"})
		if err != nil {
			t.Fatal(err)
		}
		if r := result(t, res, "default"); r.Repaired != 1 {
			t.Fatalf("default = %+v", r)
		}
		if _, damaged, _ := l.recorded(); !slices.Contains(damaged, "secondary/"+d.Hex()[:6]+"/corrupt") {
			t.Errorf("damaged = %v, want the damaged source marked", damaged)
		}
		if got, _ := first.Data(blob.Key("quader", d)); !bytes.Equal(got, good) {
			t.Error("the copy was not replaced from archive")
		}
	})

	t.Run("R-STOR-027 a copy marked missing whose object reads whole again is recorded whole; one still gone is looked at and left", func(t *testing.T) {
		l, only := newLedger(), blobtest.New()
		back := put(l, data(1024, 17), ids[:1], only)
		gone := put(l, data(1024, 18), ids[:1], only)
		l.due = map[string][]Due{}
		for _, d := range []blob.Digest{back, gone} {
			l.copies[d][0].State = blob.CopyMissing
		}
		if err := only.Delete(ctx, blob.Key("quader", gone)); err != nil {
			t.Fatal(err)
		}
		res, err := newScrubber(t, l, newClock(), connection("default", only, 50<<20)).Run(ctx, RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if r := result(t, res, "default"); r.MissingLooked != 2 || r.MissingFound != 1 || r.Ended != EndedDone {
			t.Fatalf("default = %+v", r)
		}
		if _, _, whole := l.recorded(); !slices.Equal(whole, []string{"default/" + back.Hex()[:6]}) || !l.lookedAt["default/"+gone.Hex()] {
			t.Errorf("whole = %v, looked = %v", whole, l.lookedAt)
		}
	})

	t.Run("R-STOR-027 a copy on a draining connection is marked and not replaced", func(t *testing.T) {
		l, first, second := newLedger(), blobtest.New(), blobtest.New()
		d := put(l, data(1024, 13), ids, first, second)
		first.Set(blob.Key("quader", d), data(1024, 14), time.Now())
		p := "quader"
		draining := blob.NewConnection(config.StorageConnection{ID: "default", Bucket: "b", Prefix: &p, Drain: true, ScrubPeriod: 720 * time.Hour}.WithDefaults(), first)
		res, err := newScrubber(t, l, newClock(), draining, connection("secondary", second, 50<<20)).Run(ctx, RunOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if r := result(t, res, "default"); r.Corrupt != 1 || r.Repaired+r.RepairsFailed != 0 || first.Count("put") != 0 {
			t.Errorf("default = %+v, puts %d", r, first.Count("put"))
		}
	})
}

func TestR_STOR_027_TheRecheckOfOneBlob(t *testing.T) {
	ctx := context.Background()
	ids := []string{"default", "secondary"}
	l, first, second := newLedger(), blobtest.New(), blobtest.New()
	d := put(l, data(1024, 15), ids, first, second)
	// the copy on default was marked missing by a read while its object was away; it is back now
	l.copies[d][0].State = blob.CopyMissing
	second.Set(blob.Key("quader", d), data(1024, 16), time.Now())
	s := newScrubber(t, l, newClock(), connection("default", first, 1024), connection("secondary", second, 1024))
	clock := s.o.Clock.(*fakeClock)
	start := clock.Now()
	copies, err := s.Recheck(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if clock.Now() != start {
		t.Error("the recheck waited on the throttle")
	}
	states := map[string]string{}
	for _, cp := range copies {
		states[cp.Connection] = cp.State
	}
	if states["default"] != blob.CopyPresent || states["secondary"] != blob.CopyCorrupt {
		t.Errorf("states = %v, want default present again and secondary corrupt", states)
	}
	if first.Count("put")+second.Count("put") != 0 {
		t.Error("the recheck wrote an object")
	}
	// R-STOR-108: the recheck's mark names it, and the copy found whole again was not written
	if !slices.Equal(l.marks, []string{"secondary corrupt recheck digest"}) || !slices.Equal(l.rewritten, []bool{false}) {
		t.Errorf("marks = %v, rewritten = %v", l.marks, l.rewritten)
	}
	if _, err := s.Recheck(ctx, blob.Digest{1}); !errors.Is(err, ErrNoBlob) {
		t.Errorf("err = %v, want ErrNoBlob", err)
	}
}
