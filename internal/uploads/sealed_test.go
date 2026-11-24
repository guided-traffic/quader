package uploads

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
)

// The sessions on an encrypted connection (storage.md R-STOR-081), at the unit tier: an encrypted connection and a plain one on two
// in-memory stores, the session tables of memRows, the blob store's commit over a table in memory, a key ring that keeps the sealed
// keys in a map, and parts of 64 KiB; every test moves less than 256 KiB.

const sealedPart = 64 << 10

// fakeRing seals a session's data key as an index into a map: the seam SessionKeys, whose real sealing is the key ring's.
type fakeRing struct {
	mu   sync.Mutex
	keys map[string][]byte
}

func (r *fakeRing) Seal(session string, k blobcrypt.Key) (string, error) {
	b, err := k.Bytes()
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	sealed := fmt.Sprintf("v1:test:%s:%d", session, len(r.keys))
	r.keys[sealed] = b
	return sealed, nil
}

func (r *fakeRing) Open(session, sealed string) (blobcrypt.Key, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.keys[sealed]
	if !ok || !strings.Contains(sealed, ":"+session+":") {
		return blobcrypt.Key{}, errors.New("the value does not open under this session")
	}
	return blobcrypt.KeyFromBytes(b)
}

// storeKeys are the replica's storage key and name key of the encrypted connection: the seam blob.StorageKeys.
type storeKeys struct {
	sk *blobcrypt.StorageKey
	nk blobcrypt.Key
}

func (k storeKeys) Active(context.Context, string) (*blobcrypt.StorageKey, error) { return k.sk, nil }

func (k storeKeys) NameKey(string) (blobcrypt.Key, error) { return k.nk, nil }

func (k storeKeys) Lookup(context.Context, string) blobcrypt.KeyLookup {
	return func(id string) (*blobcrypt.StorageKey, error) {
		if id == k.sk.ID {
			return k.sk, nil
		}
		return nil, nil
	}
}

// sentStore keeps the bytes of every Put and UploadPart below the staging area, to look for plaintext in them.
type sentStore struct {
	blob.ObjectStore
	mu   sync.Mutex
	sent [][]byte
}

func (s *sentStore) keep(key string, body io.ReadSeeker) error {
	if !strings.Contains(key, "/_uploads/") {
		return nil
	}
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	_, err = body.Seek(0, io.SeekStart)
	s.mu.Lock()
	s.sent = append(s.sent, b)
	s.mu.Unlock()
	return err
}

func (s *sentStore) Put(ctx context.Context, key string, body io.ReadSeeker, size int64, sum blob.Checksum, md blob.Metadata) error {
	if err := s.keep(key, body); err != nil {
		return err
	}
	return s.ObjectStore.Put(ctx, key, body, size, sum, md)
}

func (s *sentStore) UploadPart(ctx context.Context, key, id string, n int, body io.ReadSeeker, size int64, sum blob.Checksum) (blob.Part, error) {
	if err := s.keep(key, body); err != nil {
		return blob.Part{}, err
	}
	return s.ObjectStore.UploadPart(ctx, key, id, n, body, size, sum)
}

type sealedSessions struct {
	*fixture
	archive *blobtest.Store
	sent    *sentStore
	conns   *blob.Connections
	blobs   *blobRows
	store   *blob.Store
	ring    *fakeRing
	keys    storeKeys
}

func newSealedSessions(t *testing.T, caps blob.Capabilities) *sealedSessions {
	t.Helper()
	f := newFixture(t)
	f.cfg.Uploads.PartSize = sealedPart
	p := "quader"
	f.cfg.Connections = []config.StorageConnection{
		config.StorageConnection{ID: "archive", Bucket: "a", Prefix: &p, Encrypted: true}.WithDefaults(),
		config.StorageConnection{ID: "plain", Bucket: "b", Prefix: &p}.WithDefaults(),
	}
	x := &sealedSessions{fixture: f, archive: blobtest.New(), blobs: newBlobRows(), ring: &fakeRing{keys: map[string][]byte{}}}
	x.archive.MinPartSize = 0
	x.sent = &sentStore{ObjectStore: x.archive}
	plain := blobtest.New()
	var err error
	if x.conns, err = blob.NewConnections(blob.NewConnection(f.cfg.Connections[0], x.sent), blob.NewConnection(f.cfg.Connections[1], plain)); err != nil {
		t.Fatal(err)
	}
	x.conns.First().SetCapabilities(caps)
	raw := make([]byte, blobcrypt.KeySize)
	_, _ = rand.Read(raw)
	sk, err := blobcrypt.NewStorageKey("sk-archive-1-0a1b2c3d", raw)
	if err != nil {
		t.Fatal(err)
	}
	nk, err := blobcrypt.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	x.keys = storeKeys{sk: sk, nk: nk}
	storeCfg := f.cfg
	storeCfg.Spool.Dir = t.TempDir()
	if x.store, err = blob.New(storeCfg, x.conns, x.blobs, blob.WithRows(x.blobs), blob.WithStorageKeys(x.keys)); err != nil {
		t.Fatal(err)
	}
	return x
}

func (x *sealedSessions) service() *Service {
	x.t.Helper()
	cfg := x.cfg
	cfg.Spool.Dir = x.t.TempDir()
	sp, err := blob.NewSpool(cfg.Spool)
	if err != nil {
		x.t.Fatal(err)
	}
	return newService(x.rows, Options{Connections: x.conns, Spool: sp, Storage: cfg, SessionKeys: x.ring, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

func (x *sealedSessions) commit(ctx context.Context, staged *blob.Spooled, markDone func(context.Context, meta.Tx, int64) error) error {
	_, err := x.store.Commit(ctx, staged, func(ctx context.Context, tx meta.Tx) error { return markDone(ctx, tx, 42) })
	if err != nil {
		x.rows.rollbackTx()
		return err
	}
	x.rows.commitTx()
	return nil
}

// content reads the blob d from its keyed name on the encrypted connection through the record that lies there.
func (x *sealedSessions) content(t *testing.T, d [32]byte, size int64) []byte {
	t.Helper()
	name, err := blobcrypt.KeyedName(x.keys.nk, "quader", d)
	if err != nil {
		t.Fatal(err)
	}
	body, info, err := x.archive.Get(context.Background(), name, nil)
	if err != nil {
		t.Fatalf("the content object at its keyed name: %v", err)
	}
	defer body.Close()
	dk, err := blobcrypt.Open(info.Metadata, x.keys.Lookup(context.Background(), "archive"), blobcrypt.Want{
		Known: true, Digest: d, Size: size, StoredSize: info.Size, ObjectSize: info.Size,
	})
	if err != nil {
		t.Fatalf("the record of the content object: %v", err)
	}
	r, err := blobcrypt.NewReader(body, dk, true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("the content object reads with %v", err)
	}
	return got
}

// session runs one upload in requests of the given sizes, the last of them the finalize, and returns the data and the session.
func (x *sealedSessions) session(t *testing.T, s *Service, sizes ...int) ([]byte, uuid.UUID) {
	t.Helper()
	total := 0
	for _, n := range sizes {
		total += n
	}
	data := random(total)
	sess, err := s.Start(context.Background(), 3, x.sc)
	if err != nil {
		t.Fatal(err)
	}
	off := 0
	for _, n := range sizes[:len(sizes)-1] {
		if _, err := x.append(s, sess.ID, int64(off), data[off:off+n]); err != nil {
			t.Fatal(err)
		}
		off += n
	}
	if _, err := s.Finalize(context.Background(), sess.ID, x.sc, int64(off), sha256.Sum256(data), bytes.NewReader(data[off:]), 0, x.commit); err != nil {
		t.Fatal(err)
	}
	return data, sess.ID
}

func TestR_STOR_081_UploadSessionsOnAnEncryptedConnection(t *testing.T) {
	ctx := context.Background()
	t.Run("R-STOR-081 three requests (64 KiB; 64 KiB and 10 KiB; 20 KiB) finalize with the digest, the content object carries the record and reads back, and nothing staged holds a run of the input", func(t *testing.T) {
		x := newSealedSessions(t, blob.Capabilities{Metadata: true, CopyReplacesMetadata: true})
		s := x.service()
		sess, err := s.Start(ctx, 3, x.sc)
		if err != nil {
			t.Fatal(err)
		}
		r := x.rows.row(sess.ID)
		if r.SealedKey == "" || r.Conn != "archive" {
			t.Fatalf("the session's row holds no sealed data key: %+v", r)
		}
		data := random(158 << 10)
		if _, err := x.append(s, sess.ID, 0, data[:64<<10]); err != nil {
			t.Fatal(err)
		}
		if _, err := x.append(s, sess.ID, 64<<10, data[64<<10:138<<10]); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Finalize(ctx, sess.ID, x.sc, 138<<10, sha256.Sum256(data), bytes.NewReader(data[138<<10:]), 0, x.commit); err != nil {
			t.Fatal(err)
		}
		d := sha256.Sum256(data)
		if got := x.content(t, d, int64(len(data))); !bytes.Equal(got, data) {
			t.Fatal("the content object reads back other bytes")
		}
		if len(x.sent.sent) < 5 { // three parts and two tails
			t.Fatalf("%d staging writes", len(x.sent.sent))
		}
		for i, b := range x.sent.sent {
			if plaintextIn(b, data) {
				t.Errorf("staging write %d (%d bytes) holds a run of the input", i, len(b))
			}
		}
		if w, ok := x.blobs.write("archive", d); !ok || w.Verified || w.StorageKeyID != x.keys.sk.ID {
			t.Errorf("copy row after the copy with the record: %+v (recorded %v), want present, not verified, with the storage key", w, ok)
		}
		if x.archive.Count("copy") != 1 {
			t.Errorf("%d copies, want the one of the finalize", x.archive.Count("copy"))
		}
		if keys := x.archive.Keys(blob.UploadsPrefix("quader")); len(keys) != 0 || x.archive.OpenUploads() != 0 {
			t.Errorf("staging left behind: %v, %d uploads", keys, x.archive.OpenUploads())
		}
	})
	t.Run("R-STOR-081 where a copy does not replace the metadata the finalize streams, and the copy row is verified", func(t *testing.T) {
		x := newSealedSessions(t, blob.Capabilities{Metadata: true, PayloadChecksumEnforced: true})
		data, _ := x.session(t, x.service(), 64<<10, 74<<10, 20<<10)
		d := sha256.Sum256(data)
		if got := x.content(t, d, int64(len(data))); !bytes.Equal(got, data) {
			t.Fatal("the streamed content object reads back other bytes")
		}
		if w, ok := x.blobs.write("archive", d); !ok || !w.Verified {
			t.Errorf("copy row after the stream: %+v (recorded %v), want verified", w, ok)
		}
		if x.archive.Count("copy") != 0 {
			t.Error("the finalize copied where the store does not replace metadata")
		}
	})
	t.Run("R-STOR-081 a blob that ends at a part boundary gets its final segment as a part of its own and reads back", func(t *testing.T) {
		x := newSealedSessions(t, blob.Capabilities{Metadata: true, CopyReplacesMetadata: true})
		data, _ := x.session(t, x.service(), 64<<10, 64<<10, 0)
		if got := x.content(t, sha256.Sum256(data), int64(len(data))); !bytes.Equal(got, data) {
			t.Fatal("the content object reads back other bytes")
		}
	})
	t.Run("R-STOR-081 a blob smaller than one part is written from its decrypted tails, not copied", func(t *testing.T) {
		x := newSealedSessions(t, blob.Capabilities{Metadata: true, CopyReplacesMetadata: true})
		data, _ := x.session(t, x.service(), 10<<10, 5<<10)
		if got := x.content(t, sha256.Sum256(data), int64(len(data))); !bytes.Equal(got, data) {
			t.Fatal("the content object reads back other bytes")
		}
		if x.archive.Count("copy") != 0 || x.archive.Count("create_multipart") != 0 {
			t.Error("a blob of tails only was copied or sent in parts")
		}
		for i, b := range x.sent.sent {
			if plaintextIn(b, data) {
				t.Errorf("tail %d holds a run of the input", i)
			}
		}
	})
	t.Run("R-STOR-081 a tail changed behind the session's back fails the session when it is read", func(t *testing.T) {
		x := newSealedSessions(t, blob.Capabilities{Metadata: true, CopyReplacesMetadata: true})
		s := x.service()
		sess, err := s.Start(ctx, 3, x.sc)
		if err != nil {
			t.Fatal(err)
		}
		data := random(80 << 10)
		if _, err := x.append(s, sess.ID, 0, data[:10<<10]); err != nil {
			t.Fatal(err)
		}
		tails := x.archive.Keys(blob.SessionPrefix("quader", sess.ID.String()))
		if len(tails) != 1 {
			t.Fatalf("tails %v", tails)
		}
		b, _ := x.archive.Data(tails[0])
		b[40] ^= 1
		x.archive.Set(tails[0], b, x.archive.Now())
		if _, err := x.append(s, sess.ID, 10<<10, data[10<<10:]); err == nil {
			t.Fatal("a request completed a part from a changed tail")
		}
		if x.rows.state(sess.ID) != StateAborted && x.rows.state(sess.ID) != stateGone {
			t.Errorf("the session is %s", x.rows.state(sess.ID))
		}
	})
	t.Run("R-STOR-081 a request whose session's counter would pass the bound answers 409 session_exhausted and aborts the session", func(t *testing.T) {
		x := newSealedSessions(t, blob.Capabilities{Metadata: true, CopyReplacesMetadata: true})
		s := x.service()
		sess, err := s.Start(ctx, 3, x.sc)
		if err != nil {
			t.Fatal(err)
		}
		x.rows.setSegments(sess.ID, blobcrypt.MaxSegmentsPerKey-reserve(sealedPart)+1)
		_, err = x.append(s, sess.ID, 0, random(1000))
		var e *errs.Error
		if !errors.As(err, &e) || e.Kind != errs.Conflict || e.Code != errs.CodeSessionExhausted {
			t.Fatalf("the request answered %v, want 409 session_exhausted", err)
		}
		if st := x.rows.state(sess.ID); st != StateAborted && st != stateGone {
			t.Errorf("the session is %s, want aborted", st)
		}
		// below the bound the claim takes the session and reserves one part's segments
		sess2, _ := s.Start(ctx, 3, x.sc)
		if _, err := x.append(s, sess2.ID, 0, random(1000)); err != nil {
			t.Fatal(err)
		}
		if got := x.rows.row(sess2.ID).SealedSegments; got != reserve(sealedPart)+1 {
			t.Errorf("the counter is %d after one request with a tail of one segment, want %d", got, reserve(sealedPart)+1)
		}
	})
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
