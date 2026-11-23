package blob_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/observability"
)

// sealedFixture is a store whose first connection, archive, is encrypted with keyed names, and whose second, plain, is not: the
// in-memory stores, the simulated tables and the replica's keys as a map (R-STOR-080 to R-STOR-082, R-STOR-086).
type sealedFixture struct {
	store   *blob.Store
	archive *blobtest.Store
	plain   *blobtest.Store
	db      *fakeDB
	keys    heldKeys
	sk      *blobcrypt.StorageKey
	nk      blobcrypt.Key
	metrics *observability.Metrics
}

// newSealedFixture builds the fixture; wrap, when not nil, stands between the archive's connection and its in-memory store.
func newSealedFixture(t *testing.T, mutate func(*config.Storage), wrap func(blob.ObjectStore) blob.ObjectStore) *sealedFixture {
	t.Helper()
	cfg := blobtest.Storage(t, t.TempDir())
	p := "quader"
	cfg.Connections = []config.StorageConnection{
		config.StorageConnection{ID: "archive", Bucket: "a", Prefix: &p, Encrypted: true}.WithDefaults(),
		config.StorageConnection{ID: "plain", Bucket: "b", Prefix: &p}.WithDefaults(),
	}
	if mutate != nil {
		mutate(&cfg)
	}
	f := &sealedFixture{archive: blobtest.New(), plain: blobtest.New(), db: newFakeDB(), metrics: observability.NewMetrics("web")}
	f.archive.MinPartSize, f.plain.MinPartSize = 0, 0
	var archive blob.ObjectStore = f.archive
	if wrap != nil {
		archive = wrap(f.archive)
	}
	conns, err := blob.NewConnections(blob.NewConnection(cfg.Connections[0], archive), blob.NewConnection(cfg.Connections[1], f.plain))
	if err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, blobcrypt.KeySize)
	_, _ = rand.Read(raw)
	if f.sk, err = blobcrypt.NewStorageKey("sk-archive-1-0a1b2c3d", raw); err != nil {
		t.Fatal(err)
	}
	if f.nk, err = blobcrypt.NewKey(rand.Reader); err != nil {
		t.Fatal(err)
	}
	f.keys = heldKeys{conn: "archive", keys: map[string]*blobcrypt.StorageKey{f.sk.ID: f.sk}, active: f.sk.ID, name: &f.nk}
	f.store, err = blob.New(cfg, conns, f.db, blob.WithRows(f.db), blob.WithStorageKeys(f.keys),
		blob.WithMetrics(blob.NewMetrics(f.metrics)))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// commit uploads data and returns its spooled form.
func (f *sealedFixture) commit(t *testing.T, data []byte) *blob.Spooled {
	t.Helper()
	sp, err := f.store.Ingest(context.Background(), bytes.NewReader(data), blob.IngestOptions{ContentLength: int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sp.Close() })
	if _, err := f.store.Commit(context.Background(), sp, noLink); err != nil {
		t.Fatal(err)
	}
	return sp
}

func (f *sealedFixture) name(t *testing.T, d blob.Digest) string {
	t.Helper()
	n, err := blobcrypt.KeyedName(f.nk, "quader", d)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *sealedFixture) failures(t *testing.T, reason string) float64 {
	t.Helper()
	return counter(t, f.metrics, "quader_storage_encrypted_read_failures_total", map[string]string{"connection": "archive", "reason": reason})
}

// plaintextIn reports whether a run of 16 bytes of plain appears anywhere in stored.
func plaintextIn(stored, plain []byte) bool {
	windows := make(map[string]bool, len(stored))
	for i := 0; i+16 <= len(stored); i++ {
		windows[string(stored[i:i+16])] = true
	}
	for i := 0; i+16 <= len(plain); i++ {
		if windows[string(plain[i:i+16])] {
			return true
		}
	}
	return false
}

func TestR_STOR_080_WritingToAnEncryptedConnection(t *testing.T) {
	t.Run("R-STOR-080 R-STOR-086 an upload of 1 KiB lies below keyed/ as 1,120 bytes with the four record keys, its copy row says so, and it downloads", func(t *testing.T) {
		f := newSealedFixture(t, nil, nil)
		data := content(1024, 3)
		sp := f.commit(t, data)
		name := f.name(t, sp.SHA256)
		stored, ok := f.archive.Data(name)
		if !ok || len(stored) != 1120 {
			t.Fatalf("the object at its keyed name: %d bytes (found %v), want 1,120", len(stored), ok)
		}
		if keys := f.archive.Keys("quader/sha256/"); len(keys) != 0 {
			t.Errorf("objects below sha256/ on a connection with keyed names: %v", keys)
		}
		if plaintextIn(stored, data) {
			t.Error("the stored object holds a run of the plaintext")
		}
		info, err := f.archive.Head(context.Background(), name)
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{blobcrypt.MetaFormat, blobcrypt.MetaKey, blobcrypt.MetaWrap, blobcrypt.MetaCommitment} {
			if info.Metadata[k] == "" {
				t.Errorf("the record lacks %s", k)
			}
		}
		cp, _ := f.db.copyOf(sp.SHA256, "archive")
		if cp.StoredSize != 1120 || cp.StorageKeyID != f.sk.ID || cp.ObjectKey != name {
			t.Errorf("copy row %+v", cp)
		}
		r, err := f.store.Open(context.Background(), sp.SHA256, sp.Size, blob.OpenOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := readAll(t, r); !bytes.Equal(got, data) {
			t.Fatal("the download differs from the upload")
		}
	})
	t.Run("R-STOR-080 an upload of 192 KiB in parts of 64 KiB whose second part is sent again completes, and the repeated part is byte for byte the first attempt", func(t *testing.T) {
		var rt *retryingStore
		f := newSealedFixture(t, func(c *config.Storage) {
			c.ObjectStore.Multipart.Threshold = 64 << 10
			c.ObjectStore.Multipart.PartSize = 64 << 10
		}, func(s blob.ObjectStore) blob.ObjectStore { rt = &retryingStore{ObjectStore: s, part: 2}; return rt })
		data := content(192<<10, 5)
		sp := f.commit(t, data)
		if len(rt.attempts) != 2 || !bytes.Equal(rt.attempts[0], rt.attempts[1]) || len(rt.attempts[0]) != 64<<10+28 {
			t.Fatalf("the attempts of part 2: %d, equal %v", len(rt.attempts), len(rt.attempts) == 2 && bytes.Equal(rt.attempts[0], rt.attempts[1]))
		}
		stored, _ := f.archive.Data(f.name(t, sp.SHA256))
		if want, _ := blobcrypt.StoredSize(int64(len(data))); int64(len(stored)) != want || plaintextIn(stored, data) {
			t.Fatalf("the object holds %d bytes, want %d, or a run of the plaintext", len(stored), want)
		}
		r, err := f.store.Open(context.Background(), sp.SHA256, sp.Size, blob.OpenOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := readAll(t, r); !bytes.Equal(got, data) {
			t.Fatal("the multipart object reads back other bytes")
		}
	})
	t.Run("R-STOR-080 a multipart object whose HeadObject names another storage key fails the write", func(t *testing.T) {
		f := newSealedFixture(t, func(c *config.Storage) {
			c.ObjectStore.Multipart.Threshold = 64 << 10
			c.ObjectStore.Multipart.PartSize = 64 << 10
		}, nil)
		f.archive.SetFault(func(op, key string) error {
			if op == "head" && strings.Contains(key, "/keyed/") {
				info := f.archive.MetadataOf(key)
				info[blobcrypt.MetaKey] = "sk-archive-2-0a1b2c3d"
				f.archive.SetMetadata(key, info)
			}
			return nil
		})
		data := content(130<<10, 7)
		sp, err := f.store.Ingest(context.Background(), bytes.NewReader(data), blob.IngestOptions{ContentLength: int64(len(data))})
		if err != nil {
			t.Fatal(err)
		}
		defer sp.Close()
		if _, err := f.store.Commit(context.Background(), sp, noLink); err == nil {
			t.Fatal("the write whose record changed committed")
		}
	})
	t.Run("R-STOR-080 R-STOR-088 two writes of one content write two bodies, each under its own data key, and the later one reads back (A6)", func(t *testing.T) {
		f := newSealedFixture(t, nil, nil)
		data := content(4096, 9)
		sp := f.commit(t, data)
		name := f.name(t, sp.SHA256)
		first, _ := f.archive.Data(name)
		// a second replica with its own tables writes the same content to the same connection
		other, err := blob.New(blobtest.Storage(t, t.TempDir()), f.store.Connections(), newFakeDB(), blob.WithRows(newFakeDB()), blob.WithStorageKeys(f.keys))
		if err != nil {
			t.Fatal(err)
		}
		sp2, err := other.Ingest(context.Background(), bytes.NewReader(data), blob.IngestOptions{ContentLength: int64(len(data))})
		if err != nil {
			t.Fatal(err)
		}
		defer sp2.Close()
		if _, err := other.Commit(context.Background(), sp2, noLink); err != nil {
			t.Fatal(err)
		}
		second, _ := f.archive.Data(name)
		if bytes.Equal(first, second) || len(first) != len(second) {
			t.Fatal("the two writes left the same body")
		}
		r, err := f.store.Open(context.Background(), sp.SHA256, sp.Size, blob.OpenOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := readAll(t, r); !bytes.Equal(got, data) {
			t.Fatal("the later write does not read back")
		}
	})
	t.Run("R-STOR-075 R-STOR-080 a write of a blob's object without a record is refused on an encrypted connection, every other key passes", func(t *testing.T) {
		f := newSealedFixture(t, nil, nil)
		objects := f.store.Connections().First().Objects()
		ctx := context.Background()
		d := blob.Digest{1}
		body := func() io.ReadSeeker { return bytes.NewReader([]byte("plain")) }
		for _, key := range []string{blob.Key("quader", d), f.name(t, d)} {
			if err := objects.Put(ctx, key, body(), 5, blob.Checksum{}, nil); !errors.Is(err, blob.ErrPlaintextRefused) {
				t.Errorf("Put %s: %v", key, err)
			}
			if _, err := objects.CreateMultipart(ctx, key, "", blob.Metadata{"quader-probe": "x"}); !errors.Is(err, blob.ErrPlaintextRefused) {
				t.Errorf("CreateMultipart %s: %v", key, err)
			}
			if err := objects.Copy(ctx, "quader/_uploads/s/data", key, 5, nil); !errors.Is(err, blob.ErrPlaintextRefused) {
				t.Errorf("Copy %s: %v", key, err)
			}
			if err := objects.(blob.BucketCopier).CopyFrom(ctx, "b", "quader/x", key, 5, nil); !errors.Is(err, blob.ErrPlaintextRefused) {
				t.Errorf("CopyFrom %s: %v", key, err)
			}
		}
		if err := objects.Put(ctx, "quader/_uploads/s/tail-w", body(), 5, blob.Checksum{}, nil); err != nil {
			t.Errorf("a staging object: %v", err)
		}
		if f.archive.Count("put") != 1 || f.archive.Count("create_multipart") != 0 || f.archive.Count("copy") != 0 {
			t.Error("a refused write reached the store")
		}
	})
}

func TestR_STOR_082_ReadingFromAnEncryptedConnection(t *testing.T) {
	data := content(196609, 11) // three full segments and one byte
	setup := func(t *testing.T, mutate func(*config.Storage), wrap func(blob.ObjectStore) blob.ObjectStore) (*sealedFixture, *blob.Spooled) {
		t.Helper()
		f := newSealedFixture(t, func(c *config.Storage) {
			c.Replication.MinCopies = 2 // the blob lies on both connections, so a refused read falls over to the plain copy
			if mutate != nil {
				mutate(c)
			}
		}, wrap)
		return f, f.commit(t, data)
	}
	read := func(t *testing.T, f *sealedFixture, sp *blob.Spooled, rng *blob.ByteRange) ([]byte, error) {
		t.Helper()
		r, err := f.store.Open(context.Background(), sp.SHA256, sp.Size, blob.OpenOptions{Range: rng})
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)
	}
	t.Run("R-STOR-082 a whole read returns the blob, and a ranged read asks the store for the segments of its window only", func(t *testing.T) {
		var rec *recordingStore
		f, sp := setup(t, nil, func(s blob.ObjectStore) blob.ObjectStore { rec = &recordingStore{ObjectStore: s}; return rec })
		if got, err := read(t, f, sp, nil); err != nil || !bytes.Equal(got, data) {
			t.Fatalf("whole read: %v", err)
		}
		for _, c := range []struct {
			rng         blob.ByteRange
			first, last int64
			final       bool
		}{{blob.ByteRange{Offset: 65530, Length: 11}, 0, 1, false}, {blob.ByteRange{Offset: 196599, Length: 10}, 2, 3, true}} {
			rec.reset()
			got, err := read(t, f, sp, &c.rng)
			if err != nil || !bytes.Equal(got, data[c.rng.Offset:c.rng.Offset+c.rng.Length]) {
				t.Fatalf("range %+v: %d bytes, %v", c.rng, len(got), err)
			}
			plan, _ := blobcrypt.PlanRange(int64(len(data)), c.rng.Offset, c.rng.Offset+c.rng.Length-1)
			if plan.FirstSegment != c.first || plan.LastSegment != c.last || plan.Final != c.final {
				t.Fatalf("plan %+v", plan)
			}
			if asked := rec.asked(); len(asked) != 1 || asked[0] == nil || *asked[0] != (blob.ByteRange{Offset: plan.Offset, Length: plan.Length}) {
				t.Fatalf("range %+v asked the store for %v, want the stored bytes %d+%d", c.rng, asked, plan.Offset, plan.Length)
			}
		}
	})
	type mutation struct {
		name   string
		reason blobcrypt.Reason
		apply  func(t *testing.T, f *sealedFixture, name string, stored []byte, md blob.Metadata)
	}
	flip := func(t *testing.T, f *sealedFixture, name string, stored []byte, md blob.Metadata) {
		b := bytes.Clone(stored)
		b[blobcrypt.FullSegmentSize+100] ^= 1 // inside the second segment
		f.archive.Set(name, b, f.archive.Now())
		f.archive.SetMetadata(name, md)
	}
	mutations := []mutation{
		{"a flipped bit in the second segment", blobcrypt.ReasonSegment, flip},
		{"a byte cut off the end", blobcrypt.ReasonSize, func(t *testing.T, f *sealedFixture, name string, stored []byte, md blob.Metadata) {
			f.archive.Set(name, stored[:len(stored)-1], f.archive.Now())
			f.archive.SetMetadata(name, md)
		}},
		{"another format", blobcrypt.ReasonFormat, func(t *testing.T, f *sealedFixture, name string, stored []byte, md blob.Metadata) {
			m := cloneMD(md)
			m[blobcrypt.MetaFormat] = "quader-blob-v2"
			f.archive.SetMetadata(name, m)
		}},
		{"another commitment", blobcrypt.ReasonCommitment, func(t *testing.T, f *sealedFixture, name string, stored []byte, md blob.Metadata) {
			m := cloneMD(md)
			m[blobcrypt.MetaCommitment] = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
			f.archive.SetMetadata(name, m)
		}},
		{"the object and record of another blob under this name", blobcrypt.ReasonRecord, func(t *testing.T, f *sealedFixture, name string, stored []byte, md blob.Metadata) {
			body, rec, _ := encryptedObject(t, f.sk, content(len(data), 99))
			f.archive.Set(name, body, f.archive.Now())
			f.archive.SetMetadata(name, rec.Metadata())
		}},
	}
	for _, m := range mutations {
		for _, ranged := range []bool{false, true} {
			kind := "a whole read"
			var rng *blob.ByteRange
			if ranged {
				kind, rng = "a ranged read", &blob.ByteRange{Offset: 65530, Length: 11}
			}
			t.Run("R-STOR-078 R-STOR-082 "+m.name+" is refused on "+kind+" with "+string(m.reason)+" and marks the copy corrupt", func(t *testing.T) {
				f, sp := setup(t, nil, nil)
				name := f.name(t, sp.SHA256)
				stored, _ := f.archive.Data(name)
				m.apply(t, f, name, stored, f.archive.MetadataOf(name))
				got, err := read(t, f, sp, rng)
				if m.reason == blobcrypt.ReasonSegment {
					// the body fails after the record opened: the read aborts, as on a plain connection
					if err == nil || !errors.Is(err, blob.ErrCorrupt) {
						t.Fatalf("the read of a damaged body ended with %v", err)
					}
				} else if err != nil || !bytes.Equal(got, sliceOf(data, rng)) {
					t.Fatalf("the read did not fall over to the plain copy: %v", err)
				}
				if cp, _ := f.db.copyOf(sp.SHA256, "archive"); cp.State != blob.CopyCorrupt {
					t.Errorf("the encrypted copy is %s, want corrupt", cp.State)
				}
				if n := f.failures(t, string(m.reason)); n != 1 {
					t.Errorf("%s counted %v times", m.reason, n)
				}
			})
		}
	}
	t.Run("R-STOR-082 Q-STOR-311 a record of a storage key the replica does not hold falls over to the second copy, marks nothing and counts unknown_key", func(t *testing.T) {
		f, sp := setup(t, nil, nil)
		name := f.name(t, sp.SHA256)
		other, _ := blobcrypt.NewStorageKey("sk-archive-2-0a1b2c3d", bytes.Repeat([]byte{5}, 32))
		body, rec, _ := encryptedObject(t, other, data)
		f.archive.Set(name, body, f.archive.Now())
		f.archive.SetMetadata(name, rec.Metadata())
		got, err := read(t, f, sp, nil)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("the read did not fall over: %v", err)
		}
		if cp, _ := f.db.copyOf(sp.SHA256, "archive"); cp.State != blob.CopyPresent {
			t.Errorf("the copy was marked %s", cp.State)
		}
		if n := f.failures(t, string(blobcrypt.ReasonUnknownKey)); n != 1 {
			t.Errorf("unknown_key counted %v times", n)
		}
	})
	t.Run("R-STOR-082 with storage.download.verify: false a flipped bit in the second segment still aborts the read", func(t *testing.T) {
		f, sp := setup(t, func(c *config.Storage) { c.Download.Verify = false }, nil)
		name := f.name(t, sp.SHA256)
		stored, _ := f.archive.Data(name)
		flip(t, f, name, stored, f.archive.MetadataOf(name))
		if _, err := read(t, f, sp, nil); !errors.Is(err, blob.ErrCorrupt) {
			t.Fatalf("the read ended with %v", err)
		}
	})
	t.Run("R-STOR-082 a plain connection refuses an object of the stored format as format and marks its copy corrupt", func(t *testing.T) {
		f, sp := setup(t, nil, nil)
		archived, _ := f.db.copyOf(sp.SHA256, "archive")
		archived.State = blob.CopyMissing
		f.db.putCopy(sp.SHA256, archived) // the read goes to the plain copy
		plainKey := blob.Key("quader", sp.SHA256)
		body, rec, _ := encryptedObject(t, f.sk, data)
		f.plain.Set(plainKey, body, f.plain.Now())
		f.plain.SetMetadata(plainKey, rec.Metadata())
		plainCopy, _ := f.db.copyOf(sp.SHA256, "plain")
		plainCopy.StoredSize = int64(len(body)) // the size check alone would not see it
		f.db.putCopy(sp.SHA256, plainCopy)
		if _, err := read(t, f, sp, &blob.ByteRange{Offset: 0, Length: 10}); err == nil {
			t.Fatal("a plain connection served an object of the stored format")
		}
		if cp, _ := f.db.copyOf(sp.SHA256, "plain"); cp.State != blob.CopyCorrupt {
			t.Errorf("the plain copy is %s, want corrupt", cp.State)
		}
	})
	t.Run("R-STOR-062 the connection's reader without the blob's digest decrypts whole and ranged reads for the stage-1 jobs", func(t *testing.T) {
		f, sp := setup(t, nil, nil)
		c := f.store.Connections().First()
		cp, _ := f.db.copyOf(sp.SHA256, "archive")
		body, info, err := c.Read(context.Background(), cp, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := readAll(t, body); !bytes.Equal(got, data) || info.Size != cp.StoredSize {
			t.Fatalf("whole: %d bytes, size %d", len(got), info.Size)
		}
		body, _, err = c.Read(context.Background(), cp, &blob.ByteRange{Offset: 70000, Length: 5000})
		if err != nil {
			t.Fatal(err)
		}
		if got := readAll(t, body); !bytes.Equal(got, data[70000:75000]) {
			t.Fatal("the ranged read differs")
		}
	})
}

func TestR_STOR_086_KeyedNamesDifferPerConnection(t *testing.T) {
	a, b := newSealedFixture(t, nil, nil), newSealedFixture(t, nil, nil)
	d := blob.Digest{7}
	na, nb := a.store.Connections().First().ObjectKey(d), b.store.Connections().First().ObjectKey(d)
	if na == "" || na == nb || !strings.HasPrefix(na, "quader/keyed/") || na != a.name(t, d) {
		t.Fatalf("names %q and %q", na, nb)
	}
	if got := a.store.Connections().List()[1].ObjectKey(d); got != blob.Key("quader", d) {
		t.Fatalf("the plain connection names %q", got)
	}
}

func TestR_STOR_091_TheConformanceCheckOfTheStoredFormat(t *testing.T) {
	var check blob.ConformanceCheck
	for _, c := range blob.ConformanceChecks() {
		if strings.HasPrefix(c.Name, "an object of the stored format") {
			check = c
		}
	}
	run := func(m *blobtest.Store) blob.CheckResult {
		return blob.RunCheck(context.Background(), m, blob.ConformanceOptions{Prefix: "quader", Replica: "unit", PayloadChecksum: "sha256"}, check)
	}
	t.Run("R-STOR-091 a store that keeps and replaces metadata passes the write, the whole and the ranged read and the copy with the record", func(t *testing.T) {
		m := blobtest.New()
		if r := run(m); !r.OK || r.Note != "" {
			t.Fatalf("%+v", r)
		}
		if keys := m.Keys(""); len(keys) != 0 {
			t.Fatalf("left behind: %v", keys)
		}
	})
	t.Run("R-STOR-091 a store without metadata is a skip, one whose copy keeps the record a note", func(t *testing.T) {
		m := blobtest.New()
		m.DropMetadata = true
		if r := run(m); !r.OK || !strings.HasPrefix(r.Note, "skipped") {
			t.Errorf("without metadata: %+v", r)
		}
		m = blobtest.New()
		m.CopyKeepsMetadata = true
		if r := run(m); !r.OK || !strings.Contains(r.Note, "copyReplacesMetadata: false") {
			t.Errorf("a copy that keeps the record: %+v", r)
		}
	})
}

func cloneMD(md blob.Metadata) blob.Metadata {
	out := blob.Metadata{}
	for k, v := range md {
		out[k] = v
	}
	return out
}

func sliceOf(data []byte, rng *blob.ByteRange) []byte {
	if rng == nil {
		return data
	}
	return data[rng.Offset : rng.Offset+rng.Length]
}

// retryingStore sends one part twice, as the SDK's retry does after a failed attempt: it reads the body, seeks back and reads it
// again, keeps both attempts and sends the second.
type retryingStore struct {
	blob.ObjectStore
	part     int
	mu       sync.Mutex
	attempts [][]byte
}

func (r *retryingStore) UploadPart(ctx context.Context, key, id string, number int, body io.ReadSeeker, size int64, sum blob.Checksum) (blob.Part, error) {
	if number == r.part {
		for range 2 {
			b, err := io.ReadAll(body)
			if err != nil {
				return blob.Part{}, err
			}
			if _, err := body.Seek(0, io.SeekStart); err != nil {
				return blob.Part{}, err
			}
			r.mu.Lock()
			r.attempts = append(r.attempts, b)
			r.mu.Unlock()
		}
	}
	return r.ObjectStore.UploadPart(ctx, key, id, number, body, size, sum)
}

// recordingStore keeps the ranges every Get asked for.
type recordingStore struct {
	blob.ObjectStore
	mu     sync.Mutex
	ranges []*blob.ByteRange
}

func (r *recordingStore) Get(ctx context.Context, key string, rng *blob.ByteRange) (io.ReadCloser, blob.ObjectInfo, error) {
	r.mu.Lock()
	if rng != nil {
		c := *rng
		rng = &c
	}
	r.ranges = append(r.ranges, rng)
	r.mu.Unlock()
	return r.ObjectStore.Get(ctx, key, rng)
}

func (r *recordingStore) reset() {
	r.mu.Lock()
	r.ranges = nil
	r.mu.Unlock()
}

func (r *recordingStore) asked() []*blob.ByteRange {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*blob.ByteRange(nil), r.ranges...)
}
