package blob_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/errs"
)

// two is a store over two connections, primary and secondary, each an in-memory store behind its own guard, and the fake
// database (R-STOR-063: two blobtest stores, the fault hook, the fake database, objects of 1 KiB).
type two struct {
	store  *blob.Store
	a, b   *blobtest.Store
	ga, gb *blob.Guard
	db     *fakeDB
}

func newTwo(t *testing.T, mutate func(*config.Storage), opts ...blob.Option) *two {
	t.Helper()
	cfg := blobtest.Storage(t, t.TempDir())
	cfg.Upload.BlobWaitTimeout = 2 * time.Second
	p := "quader"
	cfg.Connections = []config.StorageConnection{
		config.StorageConnection{ID: "primary", Bucket: "a", Prefix: &p}.WithDefaults(),
		config.StorageConnection{ID: "secondary", Bucket: "b", Prefix: &p}.WithDefaults(),
	}
	if mutate != nil {
		mutate(&cfg)
	}
	x := &two{a: blobtest.New(), b: blobtest.New(), db: newFakeDB()}
	x.a.MinPartSize, x.b.MinPartSize = 0, 0
	breaker := config.StorageS3Breaker{FailureThreshold: 1, OpenDuration: time.Minute}
	x.ga = blob.NewGuard(x.a, breaker, blob.WithGuardConnection("primary"))
	x.gb = blob.NewGuard(x.b, breaker, blob.WithGuardConnection("secondary"))
	conns, err := blob.NewConnections(blob.NewConnection(cfg.Connections[0], x.ga), blob.NewConnection(cfg.Connections[1], x.gb))
	if err != nil {
		t.Fatal(err)
	}
	if x.store, err = blob.New(cfg, conns, x.db, append([]blob.Option{blob.WithRows(x.db), blob.WithWaitPoll(5 * time.Millisecond)}, opts...)...); err != nil {
		t.Fatal(err)
	}
	return x
}

func (x *two) upload(t *testing.T, data []byte) (blob.Digest, error) {
	t.Helper()
	sp, err := x.store.Ingest(context.Background(), bytes.NewReader(data), blob.IngestOptions{ContentLength: int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	defer sp.Close()
	_, err = x.store.Commit(context.Background(), sp, noLink)
	return sp.SHA256, err
}

func (x *two) read(d blob.Digest, size int64) ([]byte, error) {
	r, err := x.store.Open(context.Background(), d, size, blob.OpenOptions{})
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// unavailable is a fault that answers every call of op (every call when op is empty) with a transient 503.
func unavailable(op string) func(string, string) error {
	return func(o, key string) error {
		if op == "" || o == op {
			return &blob.StoreError{Op: o, Key: key, Code: "SlowDown", Status: 503, Kind: blob.KindTransient}
		}
		return nil
	}
}

func key(d blob.Digest) string { return blob.Key("quader", d) }

func TestR_STOR_052_TheContentStateFollowsFromTheCopies(t *testing.T) {
	t.Run("R-STOR-052 present and missing give ok, missing and corrupt give corrupt, missing alone missing; a retired connection's present copy counts for nothing, a detached one's counts", func(t *testing.T) {
		for _, c := range []struct {
			name   string
			copies []blob.Copy
			want   string
		}{
			{"present + missing", []blob.Copy{{State: blob.CopyPresent}, {State: blob.CopyMissing}}, blob.StatusOK},
			{"missing + corrupt", []blob.Copy{{State: blob.CopyMissing}, {State: blob.CopyCorrupt}}, blob.StatusCorrupt},
			{"missing alone", []blob.Copy{{State: blob.CopyMissing}}, blob.StatusMissing},
			{"no copy", nil, blob.StatusMissing},
			{"present on a retired connection", []blob.Copy{{State: blob.CopyPresent, Retired: true}, {State: blob.CopyMissing}}, blob.StatusMissing},
			// a detached connection is one no live replica configures; its row is not retired, so its copy counts
			{"present on a detached connection", []blob.Copy{{Connection: "detached", State: blob.CopyPresent}}, blob.StatusOK},
			{"deleting", []blob.Copy{{State: blob.CopyDeleting}}, blob.StatusMissing},
		} {
			if got := blob.DeriveContentStatus(c.copies); got != c.want {
				t.Errorf("%s: %s, want %s", c.name, got, c.want)
			}
		}
	})
}

func TestR_STOR_053_WhereAnUploadIsWritten(t *testing.T) {
	data := content(1024, 41)
	t.Run("R-STOR-053 with both connections reachable and min_copies 1 an upload writes one object on the first store, one present copy row and a queue row for the second", func(t *testing.T) {
		x := newTwo(t, nil)
		d, err := x.upload(t, data)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := x.a.Data(key(d)); !ok || !bytes.Equal(got, data) {
			t.Fatal("the first store does not hold the object")
		}
		if x.b.Count("put") != 0 {
			t.Fatalf("the second store was written %d times", x.b.Count("put"))
		}
		cp, ok := x.db.copyOf(d, "primary")
		if !ok || cp.State != blob.CopyPresent || cp.StoredSize != int64(len(data)) || cp.ObjectKey != key(d) {
			t.Fatalf("copy row of primary: %+v %v", cp, ok)
		}
		if _, ok := x.db.copyOf(d, "secondary"); ok || !x.db.queued("secondary", d) || x.db.queued("primary", d) {
			t.Fatalf("the second connection has a copy row or no queue row")
		}
	})
	t.Run("R-STOR-053 with every PutObject of the first store answering 503 the upload succeeds on the second, with its copy row and a queue row for the first", func(t *testing.T) {
		x := newTwo(t, nil)
		x.a.SetFault(unavailable("put"))
		d, err := x.upload(t, data)
		if err != nil {
			t.Fatalf("the upload failed: %v", err)
		}
		if got, ok := x.b.Data(key(d)); !ok || !bytes.Equal(got, data) {
			t.Fatal("the second store does not hold the object")
		}
		if cp, ok := x.db.copyOf(d, "secondary"); !ok || cp.State != blob.CopyPresent {
			t.Fatalf("copy row of secondary: %+v %v", cp, ok)
		}
		if _, ok := x.db.copyOf(d, "primary"); ok || !x.db.queued("primary", d) {
			t.Fatal("the first connection has a copy row or no queue row")
		}
	})
	t.Run("R-STOR-053 with min_copies 2 and the second store failing the upload answers 503, no blob row exists and the first store's object has no copy row", func(t *testing.T) {
		x := newTwo(t, func(c *config.Storage) { c.Replication.MinCopies = 2 })
		x.b.SetFault(unavailable("put"))
		d, err := x.upload(t, data)
		var e *errs.Error
		if !errors.As(err, &e) || e.Kind != errs.Unavailable || e.RetryAfter <= 0 {
			t.Fatalf("err = %v, want a 503 with Retry-After", err)
		}
		if _, ok := x.db.rows[d]; ok {
			t.Fatal("a blob row exists")
		}
		if _, ok := x.a.Data(key(d)); !ok {
			t.Fatal("the first store's object is gone; the orphan sweep removes it, not the commit")
		}
		if _, ok := x.db.copyOf(d, "primary"); ok {
			t.Fatal("the first store's object has a copy row")
		}
	})
	t.Run("R-STOR-053 with min_copies 2 both stores hold the object, each with a copy row, and no gap is queued", func(t *testing.T) {
		x := newTwo(t, func(c *config.Storage) { c.Replication.MinCopies = 2 })
		d, err := x.upload(t, data)
		if err != nil {
			t.Fatal(err)
		}
		for _, conn := range []string{"primary", "secondary"} {
			if cp, ok := x.db.copyOf(d, conn); !ok || cp.State != blob.CopyPresent || x.db.queued(conn, d) {
				t.Fatalf("%s: %+v %v queued %v", conn, cp, ok, x.db.queued(conn, d))
			}
		}
	})
	t.Run("R-STOR-053 a connection with drain: true takes no upload and gets no queue row", func(t *testing.T) {
		x := newTwo(t, func(c *config.Storage) { c.Connections[0].Drain = true })
		d, err := x.upload(t, data)
		if err != nil {
			t.Fatal(err)
		}
		if x.a.Count("put") != 0 || x.db.queued("primary", d) {
			t.Fatalf("the draining connection was written %d times or queued", x.a.Count("put"))
		}
		if _, ok := x.db.copyOf(d, "secondary"); !ok {
			t.Fatal("the second connection has no copy row")
		}
	})
	t.Run("R-STOR-053 a write is verified when the connection enforces its payload checksum, and not before the start check said so", func(t *testing.T) {
		x := newTwo(t, func(c *config.Storage) { c.Replication.MinCopies = 2 })
		first, _ := x.store.Connections().Get("primary")
		first.SetCapabilities(blob.Capabilities{PayloadChecksumEnforced: true})
		d, err := x.upload(t, data)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := x.db.copyOf(d, "primary")
		b, _ := x.db.copyOf(d, "secondary")
		if a.VerifiedAt.IsZero() || !b.VerifiedAt.IsZero() {
			t.Fatalf("verified: primary %v, secondary %v", a.VerifiedAt, b.VerifiedAt)
		}
	})
	t.Run("R-STOR-053 an upload that waits for 100 Continue is refused before its body while fewer than min_copies connections are writable", func(t *testing.T) {
		x := newTwo(t, nil)
		x.a.SetFault(unavailable(""))
		_, _ = x.ga.Head(context.Background(), "k") // opens the first circuit
		if err := x.store.RejectUpload(); err != nil {
			t.Fatalf("one writable connection of min_copies 1 refused: %v", err)
		}
		x.b.SetFault(unavailable(""))
		_, _ = x.gb.Head(context.Background(), "k")
		if err := x.store.RejectUpload(); err == nil {
			t.Fatal("no writable connection, and the upload was not refused")
		}
	})
}

func TestR_STOR_055_ReadsOverTheCopies(t *testing.T) {
	data := content(1024, 43)
	both := func(t *testing.T) (*two, blob.Digest) {
		t.Helper()
		x := newTwo(t, func(c *config.Storage) { c.Replication.MinCopies = 2 })
		d, err := x.upload(t, data)
		if err != nil {
			t.Fatal(err)
		}
		return x, d
	}
	t.Run("R-STOR-055 Q-STOR-275 copies the caller read with its own statement are taken as given: the download asks PostgreSQL nothing, and falls over on what they no longer say", func(t *testing.T) {
		x, d := both(t)
		copies, err := x.store.Copies(context.Background(), d)
		if err != nil || len(copies) != 2 {
			t.Fatalf("copies %+v, %v", copies, err)
		}
		reads := func() int { x.db.mu.Lock(); defer x.db.mu.Unlock(); return x.db.copyReads }
		download := func() {
			t.Helper()
			r, err := x.store.Open(context.Background(), d, int64(len(data)), blob.OpenOptions{Copies: copies, CopiesRead: true})
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(r)
			_ = r.Close()
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("download: %v", err)
			}
		}
		before := reads()
		download()
		if n := reads() - before; n != 0 {
			t.Fatalf("the download read the copies %d times", n)
		}
		if err := x.a.Delete(context.Background(), key(d)); err != nil { // gone since the lookup read the copies
			t.Fatal(err)
		}
		download() // the stale copy is left for the second, and marked
		if cp, _ := x.db.copyOf(d, "primary"); cp.State != blob.CopyMissing {
			t.Fatalf("first copy %+v", cp)
		}
	})
	t.Run("R-STOR-055 Q-STOR-275 the copies of an item lookup are read from its JSON array, a timestamp of PostgreSQL and null as written", func(t *testing.T) {
		cs, err := blob.ParseCopies(`[{"connection": "primary", "objectKey": "quader/sha256/ab/cd/x", "storedSize": 3, "storageKeyId": "", "state": "present",
			"verifiedAt": "2026-11-22T10:00:00.123456+00:00", "checkedAt": null, "retired": false},
			{"connection": "old", "objectKey": "k", "storedSize": 3, "storageKeyId": "", "state": "missing", "verifiedAt": null, "checkedAt": null, "retired": true}]`)
		if err != nil || len(cs) != 2 || cs[0].Connection != "primary" || cs[0].ObjectKey != "quader/sha256/ab/cd/x" || cs[0].StoredSize != 3 ||
			cs[0].State != blob.CopyPresent || cs[0].VerifiedAt.IsZero() || !cs[0].CheckedAt.IsZero() || !cs[1].Retired {
			t.Fatalf("%+v, %v", cs, err)
		}
		if cs, err := blob.ParseCopies("[]"); err != nil || len(cs) != 0 {
			t.Fatalf("no copies: %+v, %v", cs, err)
		}
	})
	t.Run("R-STOR-055 the object deleted from the first store: the download is served whole from the second, the first copy is missing and queued, the blob ok", func(t *testing.T) {
		x, d := both(t)
		if err := x.a.Delete(context.Background(), key(d)); err != nil {
			t.Fatal(err)
		}
		got, err := x.read(d, int64(len(data)))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("download: %v", err)
		}
		if cp, _ := x.db.copyOf(d, "primary"); cp.State != blob.CopyMissing || !x.db.queued("primary", d) {
			t.Fatalf("first copy %+v, queued %v", cp, x.db.queued("primary", d))
		}
		if s := x.db.status(d); s != blob.StatusOK {
			t.Fatalf("blob %s", s)
		}
	})
	t.Run("R-STOR-055 the first object replaced by other bytes of its length: the download is aborted at its end, the copy is corrupt, and the next serves the second whole", func(t *testing.T) {
		x, d := both(t)
		x.a.Set(key(d), content(1024, 99), time.Now())
		if _, err := x.read(d, int64(len(data))); !errors.Is(err, blob.ErrCorrupt) {
			t.Fatalf("first download: %v, want ErrCorrupt at its end", err)
		}
		if cp, _ := x.db.copyOf(d, "primary"); cp.State != blob.CopyCorrupt || x.db.status(d) != blob.StatusOK {
			t.Fatalf("first copy %+v, blob %s", cp, x.db.status(d))
		}
		got, err := x.read(d, int64(len(data)))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("second download: %v", err)
		}
	})
	t.Run("R-STOR-055 both objects deleted: 503 content_unavailable, the blob missing, and one HeadObject per copy within the recheck interval, not one per read", func(t *testing.T) {
		x, d := both(t)
		now := time.Now()
		x.db.now = func() time.Time { return now }
		for _, s := range []*blobtest.Store{x.a, x.b} {
			if err := s.Delete(context.Background(), key(d)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := x.read(d, int64(len(data))); !errors.Is(err, blob.ErrNotFound) {
			t.Fatalf("download: %v, want ErrNotFound (content_unavailable)", err)
		}
		if s := x.db.status(d); s != blob.StatusMissing {
			t.Fatalf("blob %s", s)
		}
		heads := func() int { return x.a.Count("head") + x.b.Count("head") }
		before := heads()
		if _, err := x.read(d, int64(len(data))); !errors.Is(err, blob.ErrNotFound) || heads() != before || x.a.Count("get")+x.b.Count("get") != 2 {
			t.Fatalf("a read of a blob without a present copy asked the store: %v", err)
		}
		now = now.Add(blob.MissingRecheckInterval + time.Second)
		first := x.store.RecheckMissing(context.Background(), d, int64(len(data)))
		second := x.store.RecheckMissing(context.Background(), d, int64(len(data))) // inside the interval: no claim, no request
		if first || second {
			t.Fatal("a recheck found an object")
		}
		if got := heads() - before; got != 2 {
			t.Fatalf("%d HeadObject in the interval, want one per copy", got)
		}
		x.b.Set(key(d), data, time.Now())
		now = now.Add(blob.MissingRecheckInterval + time.Second)
		if !x.store.RecheckMissing(context.Background(), d, int64(len(data))) || x.db.status(d) != blob.StatusOK {
			t.Fatalf("the object back on the second store: blob %s", x.db.status(d))
		}
		if cp, _ := x.db.copyOf(d, "secondary"); cp.State != blob.CopyPresent {
			t.Fatalf("second copy %+v", cp)
		}
	})
	t.Run("R-STOR-055 every GetObject of the first store answering 503: downloads come from the second and no copy changes state", func(t *testing.T) {
		x, d := both(t)
		x.a.SetFault(unavailable("get"))
		for i := 0; i < 3; i++ {
			got, err := x.read(d, int64(len(data)))
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("download %d: %v", i, err)
			}
		}
		for _, conn := range []string{"primary", "secondary"} {
			if cp, _ := x.db.copyOf(d, conn); cp.State != blob.CopyPresent {
				t.Fatalf("%s: %+v", conn, cp)
			}
		}
		if x.a.Count("get") != 1 {
			t.Fatalf("the first store was asked %d times; its open circuit leaves it out", x.a.Count("get"))
		}
	})
	t.Run("R-STOR-055 a verified download stamps its copy at most once a day", func(t *testing.T) {
		now := time.Now()
		clock := func() time.Time { return now }
		x := newTwo(t, func(c *config.Storage) { c.Replication.MinCopies = 2 }, blob.WithClock(clock))
		x.db.now = clock
		d, err := x.upload(t, data)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			if _, err := x.read(d, int64(len(data))); err != nil {
				t.Fatal(err)
			}
		}
		if x.db.stamps != 1 {
			t.Fatalf("%d stamps for three downloads in a day", x.db.stamps)
		}
		now = now.Add(25 * time.Hour)
		if _, err := x.read(d, int64(len(data))); err != nil {
			t.Fatal(err)
		}
		if x.db.stamps != 2 {
			t.Fatalf("%d stamps after a day", x.db.stamps)
		}
	})
	t.Run("R-STOR-055 a draining connection's copy is read last", func(t *testing.T) {
		x := newTwo(t, func(c *config.Storage) { c.Replication.MinCopies = 1 })
		d, err := x.upload(t, data)
		if err != nil {
			t.Fatal(err)
		}
		x.db.putCopy(d, blob.Copy{Connection: "secondary", ObjectKey: key(d), StoredSize: int64(len(data)), State: blob.CopyPresent})
		x.b.Set(key(d), data, time.Now())
		servable, err := x.store.Servable(context.Background(), d)
		if err != nil || len(servable) != 2 || servable[0].Connection != "primary" {
			t.Fatalf("read order %+v %v", servable, err)
		}
	})
}

func TestR_STOR_062_TheSeamsOfTheEncryptionAndTheSignedLinks(t *testing.T) {
	data := content(1024, 47)
	t.Run("R-STOR-062 Servable lists the copies in read order and leaves out a missing copy and a connection whose circuit is open", func(t *testing.T) {
		x := newTwo(t, func(c *config.Storage) { c.Replication.MinCopies = 2 })
		d, err := x.upload(t, data)
		if err != nil {
			t.Fatal(err)
		}
		list, err := x.store.Servable(context.Background(), d)
		if err != nil || len(list) != 2 || list[0].Connection != "primary" || list[1].Connection != "secondary" ||
			list[0].ObjectKey != key(d) || list[0].StoredSize != int64(len(data)) {
			t.Fatalf("%+v %v", list, err)
		}
		x.db.mu.Lock()
		x.db.copies[d]["primary"].State = blob.CopyMissing
		x.db.mu.Unlock()
		if list, _ = x.store.Servable(context.Background(), d); len(list) != 1 || list[0].Connection != "secondary" {
			t.Fatalf("a missing copy is listed: %+v", list)
		}
		x.b.SetFault(unavailable(""))
		_, _ = x.gb.Head(context.Background(), "k")
		if list, _ = x.store.Servable(context.Background(), d); len(list) != 0 {
			t.Fatalf("a connection with an open circuit is listed: %+v", list)
		}
		c, _ := x.store.Connections().Get("secondary")
		if _, err := c.Presign(context.Background(), key(d), blob.PresignOptions{Life: time.Minute}); !errors.Is(err, blob.ErrCircuitOpen) {
			t.Fatalf("a connection with an open circuit signed a link: %v", err)
		}
		if c.Encrypted() || c.LinkCheck().Ran {
			t.Fatal("a connection of stage 1 is encrypted or passed a link check")
		}
	})
	t.Run("R-STOR-062 blobtest keeps metadata on Put, CreateMultipart, Get and Head, keeps it on a Copy with nil and replaces it on a Copy with a value", func(t *testing.T) {
		ctx := context.Background()
		m := blobtest.New()
		m.MinPartSize = 0
		if err := m.Put(ctx, "a", bytes.NewReader(data), int64(len(data)), blob.Checksum{}, blob.Metadata{"quader-key": "one"}); err != nil {
			t.Fatal(err)
		}
		if info, _ := m.Head(ctx, "a"); info.Metadata["quader-key"] != "one" {
			t.Fatalf("head %+v", info)
		}
		body, info, err := m.Get(ctx, "a", nil)
		if err != nil || info.Metadata["quader-key"] != "one" {
			t.Fatalf("get %+v %v", info, err)
		}
		_ = body.Close()
		if err := m.Copy(ctx, "a", "b", int64(len(data)), nil); err != nil {
			t.Fatal(err)
		}
		if info, _ := m.Head(ctx, "b"); info.Metadata["quader-key"] != "one" {
			t.Fatalf("a copy with nil: %+v", info)
		}
		if err := m.Copy(ctx, "a", "a", int64(len(data)), blob.Metadata{"quader-key": "two"}); err != nil {
			t.Fatal(err)
		}
		if info, _ := m.Head(ctx, "a"); info.Metadata["quader-key"] != "two" {
			t.Fatalf("a copy with a value: %+v", info)
		}
		id, err := m.CreateMultipart(ctx, "mp", "", blob.Metadata{"quader-key": "mp"})
		if err != nil {
			t.Fatal(err)
		}
		p, err := m.UploadPart(ctx, "mp", id, 1, bytes.NewReader(data), int64(len(data)), blob.Checksum{})
		if err != nil {
			t.Fatal(err)
		}
		if err := m.CompleteMultipart(ctx, "mp", id, []blob.Part{p}); err != nil {
			t.Fatal(err)
		}
		if info, _ := m.Head(ctx, "mp"); info.Metadata["quader-key"] != "mp" {
			t.Fatalf("multipart %+v", info)
		}
	})
	t.Run("R-STOR-062 metadata keys outside [a-z0-9-] and more than 2 KiB are refused", func(t *testing.T) {
		if err := blob.CheckMetadata(blob.Metadata{"quader-key": "v"}); err != nil {
			t.Fatal(err)
		}
		if blob.CheckMetadata(blob.Metadata{"Quader": "v"}) == nil || blob.CheckMetadata(blob.Metadata{"k": strings.Repeat("x", 2048)}) == nil {
			t.Fatal("a bad key or too much metadata passed")
		}
	})
}

func TestR_STOR_035_TheStartCheckPerConnection(t *testing.T) {
	ctx := context.Background()
	opts := blob.StartCheckOptions{Installation: "0b7c3f5e-0000-4000-8000-000000000001", Replica: "unit"}
	element := func(id, bucket string) config.StorageConnection {
		p := "quader"
		return config.StorageConnection{ID: id, Bucket: bucket, Prefix: &p}.WithDefaults()
	}
	t.Run("R-STOR-035 the capabilities of a store that enforces the checksum, deletes in batches and keeps and replaces metadata, and the server-side copy from the other bucket of its endpoint", func(t *testing.T) {
		ep := blobtest.NewEndpoint("s3.example")
		a, b := ep.Bucket("a"), ep.Bucket("b")
		ca, cb := blob.NewConnection(element("one", "a"), a), blob.NewConnection(element("two", "b"), b)
		caps, err := blob.CheckConnection(ctx, ca, []*blob.Connection{ca, cb}, opts)
		if err != nil {
			t.Fatal(err)
		}
		if !caps.PayloadChecksumEnforced || !caps.BatchDelete || !caps.Metadata || !caps.CopyReplacesMetadata || !caps.ServerSideCopyFrom["two"] || caps.CheckedAt.IsZero() {
			t.Fatalf("%+v", caps)
		}
		for _, s := range []*blobtest.Store{a, b} {
			for _, k := range s.Keys("") {
				if k != blob.MarkerKey("quader") {
					t.Errorf("the check left %s", k)
				}
			}
		}
		if _, ok := b.Data(blob.MarkerKey("quader")); ok {
			t.Error("the check of one connection wrote the other's marker")
		}
	})
	t.Run("R-STOR-051 the marker is written when absent, passes with this installation and id, and refuses another installation naming both", func(t *testing.T) {
		m := blobtest.New()
		c := blob.NewConnection(element("archive", "quader-blobs"), m)
		if _, err := blob.CheckConnection(ctx, c, nil, opts); err != nil {
			t.Fatal(err)
		}
		if _, ok := m.Data("quader/_connection"); !ok {
			t.Fatal("no marker written")
		}
		if _, err := blob.CheckConnection(ctx, c, nil, opts); err != nil {
			t.Fatalf("the own marker refused: %v", err)
		}
		other := opts
		other.Installation = "1c8d4a6f-0000-4000-8000-000000000002"
		_, err := blob.CheckConnection(ctx, c, nil, other)
		if err == nil || blob.IsTransient(err) || !strings.Contains(err.Error(), "quader-blobs/quader is connection 'archive' of installation 0b7c3f5e") ||
			!strings.Contains(err.Error(), "quader/_connection") {
			t.Fatalf("another installation: %v", err)
		}
		renamed := blob.NewConnection(element("primary", "quader-blobs"), m)
		if _, err := blob.CheckConnection(ctx, renamed, nil, opts); err == nil {
			t.Fatal("another connection id on the location passed")
		}
		if err := m.Put(ctx, "quader/_connection", strings.NewReader("{}"), 2, blob.Checksum{}, nil); err != nil {
			t.Fatal(err)
		}
		if _, found, err := blob.ReadMarker(ctx, m, "quader"); !found || !errors.Is(err, blob.ErrNotMarker) {
			t.Fatalf("an object that is no marker: found %v, %v", found, err)
		}
		if _, err := blob.CheckConnection(ctx, c, nil, opts); err == nil || !strings.Contains(err.Error(), "storage.connections[0] (archive): quader-blobs/quader holds an object quader/_connection that is no marker") {
			t.Fatalf("an object that is no marker: %v", err)
		}
	})
	t.Run("R-STOR-035 a refused payload checksum is a configuration error naming the keys and the values to try; an accepted wrong checksum is recorded", func(t *testing.T) {
		m := blobtest.New()
		m.SetFault(func(op, _ string) error {
			if op == "put" {
				return &blob.StoreError{Op: "PutObject", Code: "InvalidArgument", Status: 400, Kind: blob.KindOther}
			}
			return nil
		})
		_, err := blob.CheckConnection(ctx, blob.NewConnection(element("one", "a"), m), nil, opts)
		if !blob.IsConfig(err) || !strings.Contains(err.Error(), "payload_checksum: sha256") || !strings.Contains(err.Error(), "when_required") {
			t.Fatalf("got %v", err)
		}
		lax := &laxStore{Store: blobtest.New()}
		caps, err := blob.CheckConnection(ctx, blob.NewConnection(element("one", "a"), lax), nil, opts)
		if err != nil || caps.PayloadChecksumEnforced {
			t.Fatalf("a store that ignores checksums: %+v %v", caps, err)
		}
	})
	t.Run("R-STOR-035 a copy onto itself that replaces the metadata but loses the object's bytes does not count as replacing it", func(t *testing.T) {
		lossy := &lossyCopyStore{Store: blobtest.New()}
		caps, err := blob.CheckConnection(ctx, blob.NewConnection(element("one", "a"), lossy), nil, opts)
		if err != nil || !caps.Metadata || caps.CopyReplacesMetadata {
			t.Fatalf("a copy that loses the bytes: %+v %v", caps, err)
		}
	})
	t.Run("R-STOR-035 a DeleteObjects that fails is a configuration error naming batch_delete; with batch_delete false it is not tried", func(t *testing.T) {
		m := blobtest.New()
		m.SetFault(func(op, _ string) error {
			if op == "delete_many" {
				return &blob.StoreError{Op: "DeleteObjects", Code: "NotImplemented", Status: 501, Kind: blob.KindOther}
			}
			return nil
		})
		if _, err := blob.CheckConnection(ctx, blob.NewConnection(element("one", "a"), m), nil, opts); err == nil || !strings.Contains(err.Error(), "batch_delete") {
			t.Fatalf("got %v", err)
		}
		off := element("one", "a")
		f := false
		off.BatchDelete = &f
		caps, err := blob.CheckConnection(ctx, blob.NewConnection(off, m), nil, opts)
		if err != nil || caps.BatchDelete {
			t.Fatalf("%+v %v", caps, err)
		}
	})
	t.Run("R-STOR-060 the replica may become ready when one connection without drain passed; a transient failure of another is retried, its configuration error stops the start", func(t *testing.T) {
		fast := []blob.AwaitOption{blob.WithAwaitBackoff(time.Millisecond, 5*time.Millisecond)}
		o := opts
		o.Await = fast
		a, b := blobtest.New(), blobtest.New()
		b.SetFault(unavailable(""))
		var recorded atomic.Int32
		o.Record = func(context.Context, string, blob.Capabilities) error { recorded.Add(1); return nil }
		cs, _ := blob.NewConnections(blob.NewConnection(element("one", "a"), a), blob.NewConnection(element("two", "b"), b))
		if err := blob.AwaitConnections(ctx, cs, o); err != nil {
			t.Fatalf("one connection down: %v", err)
		}
		if recorded.Load() != 1 {
			t.Fatalf("%d capabilities recorded", recorded.Load())
		}
		denied := blobtest.New()
		denied.SetFault(func(op, _ string) error {
			return &blob.StoreError{Op: op, Code: "AccessDenied", Status: 403, Kind: blob.KindConfig}
		})
		cs, _ = blob.NewConnections(blob.NewConnection(element("one", "a"), blobtest.New()), blob.NewConnection(element("two", "b"), denied))
		if err := blob.AwaitConnections(ctx, cs, o); err == nil || !strings.Contains(err.Error(), "connection two") {
			t.Fatalf("a configuration error of a connection: %v", err)
		}
		drained := element("two", "b")
		drained.Drain = true
		cs, _ = blob.NewConnections(blob.NewConnection(element("one", "a"), blobtest.New()), blob.NewConnection(drained, denied))
		if err := blob.AwaitConnections(ctx, cs, o); err != nil {
			t.Fatalf("a draining connection's configuration error stopped the start: %v", err)
		}
		down := blobtest.New()
		down.SetFault(unavailable(""))
		cs, _ = blob.NewConnections(blob.NewConnection(element("one", "a"), down), blob.NewConnection(element("two", "b"), b))
		short, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		if err := blob.AwaitConnections(short, cs, o); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("every connection down: %v, want the wait until the context ends", err)
		}
	})
}

// laxStore accepts a write whatever its payload checksum, as a store that ignores the header does.
type laxStore struct{ *blobtest.Store }

func (l *laxStore) Put(ctx context.Context, key string, body io.ReadSeeker, size int64, _ blob.Checksum, md blob.Metadata) error {
	return l.Store.Put(ctx, key, body, size, blob.Checksum{}, md)
}

// lossyCopyStore answers a copy onto itself with the new metadata and an empty object, as a gateway that lost the data of
// such a copy did.
type lossyCopyStore struct{ *blobtest.Store }

func (l *lossyCopyStore) Copy(ctx context.Context, src, dst string, size int64, md blob.Metadata) error {
	if src != dst {
		return l.Store.Copy(ctx, src, dst, size, md)
	}
	return l.Put(ctx, dst, bytes.NewReader(nil), 0, blob.Checksum{}, md)
}
