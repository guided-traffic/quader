//go:build integration

package uploads

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/uploads"
)

// sealedKeys are the storage key and name key of the encrypted connection and the seals of the sessions' data keys, in memory: the
// real sealing is the key ring's (tested in internal/blob/keys); this case is about the session's statements in PostgreSQL.
type sealedKeys struct {
	sk     *blobcrypt.StorageKey
	nk     blobcrypt.Key
	mu     sync.Mutex
	sealed map[string][]byte
}

func (k *sealedKeys) Active(context.Context, string) (*blobcrypt.StorageKey, error) { return k.sk, nil }

func (k *sealedKeys) NameKey(string) (blobcrypt.Key, error) { return k.nk, nil }

func (k *sealedKeys) Lookup(context.Context, string) blobcrypt.KeyLookup {
	return func(id string) (*blobcrypt.StorageKey, error) {
		if id == k.sk.ID {
			return k.sk, nil
		}
		return nil, nil
	}
}

func (k *sealedKeys) Seal(session string, dk blobcrypt.Key) (string, error) {
	b, err := dk.Bytes()
	if err != nil {
		return "", err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	v := "v1:it:" + session
	k.sealed[v] = b
	return v, nil
}

func (k *sealedKeys) Open(session, sealed string) (blobcrypt.Key, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if b, ok := k.sealed[sealed]; ok && strings.HasSuffix(sealed, session) {
		return blobcrypt.KeyFromBytes(b)
	}
	return blobcrypt.Key{}, errors.New("not sealed under this session")
}

// TestR_STOR_081_ASessionOnAnEncryptedConnectionInPostgres runs what only PostgreSQL shows of a session on an encrypted connection
// (R-STOR-081, Q-STOR-380): the sealed data key in its row, the stored size and SHA-256 of its part, the counter, and the claim that
// refuses past the bound. The store of the stack, one part of 5 MiB (the smallest S3 takes) and a tail.
func TestR_STOR_081_ASessionOnAnEncryptedConnectionInPostgres(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	raw := make([]byte, blobcrypt.KeySize)
	_, _ = rand.Read(raw)
	sk, err := blobcrypt.NewStorageKey("sk-enc-1-0a1b2c3d", raw)
	if err != nil {
		t.Fatal(err)
	}
	nk, _ := blobcrypt.NewKey(rand.Reader)
	keys := &sealedKeys{sk: sk, nk: nk, sealed: map[string][]byte{}}
	cfg := w.e.storage(t)
	cfg.Connections[0].Encrypted = true
	conns, err := blob.NewConnections(blob.NewConnection(cfg.Connections[0], w.e.raw))
	if err != nil {
		t.Fatal(err)
	}
	caps, err := blob.CheckConnection(ctx, conns.First(), conns.List(), blob.StartCheckOptions{Replica: "it-enc"})
	if err != nil {
		t.Fatal(err)
	}
	conns.First().SetCapabilities(caps)
	store, err := blob.New(cfg, conns, w.p.db, blob.WithLogger(quiet), blob.WithWaitPoll(25*time.Millisecond), blob.WithStorageKeys(keys))
	if err == nil {
		err = store.RecordConnections(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
	svc, err := uploads.New(uploads.Options{DB: w.p.db, Connections: conns, Spool: store.Spool(), Storage: cfg, Log: quiet, SessionKeys: keys})
	if err != nil {
		t.Fatal(err)
	}
	r := &replica{store: store, svc: svc, pg: w.p, cfg: cfg}

	t.Run("R-STOR-081 a session seals its data key into its row, records its part as stored, counts its segments and finalizes", func(t *testing.T) {
		sess, err := svc.Start(ctx, w.p.repo, w.sc)
		if err != nil {
			t.Fatal(err)
		}
		id := sess.ID.String()
		if n := w.p.count(t, "upload_sessions WHERE id = $1 AND sealed_data_key = $2", id, "v1:it:"+id); n != 1 {
			t.Fatal("the row does not hold the sealed data key")
		}
		data := random(5*mib + 10<<10)
		if _, err := r.append(w.sc, sess.ID, 0, data[:5*mib+4<<10]); err != nil {
			t.Fatal(err)
		}
		partStored, _ := blobcrypt.TailStoredSize(5 * mib) // the data segments of one part
		if n := w.p.count(t, "upload_session_parts WHERE session_id = $1 AND size = $2 AND stored_size = $3 AND stored_sha256 IS NOT NULL AND stored_sha256 <> sha256",
			id, 5*mib, partStored); n != 1 {
			t.Fatal("the part's row does not record its stored size and SHA-256")
		}
		// the claim reserved one part and two segments, the part's record added its 80 segments, the tail's its one
		if got, want := w.p.scalarInt(t, "SELECT sealed_segments FROM upload_sessions WHERE id = $1", id), int64(80+2+80+1); got != want {
			t.Errorf("sealed_segments %d, want %d", got, want)
		}
		if err := r.finalize(w.sc, sess.ID, data, data[5*mib+4<<10:], "enc.bin"); err != nil {
			t.Fatal(err)
		}
		d := sha256.Sum256(data)
		name, _ := blobcrypt.KeyedName(nk, w.e.prefix, d)
		body, info, err := w.e.raw.Get(ctx, name, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer body.Close()
		dk, err := blobcrypt.Open(info.Metadata, keys.Lookup(ctx, ""), blobcrypt.Want{Known: true, Digest: d, Size: int64(len(data)), StoredSize: info.Size, ObjectSize: info.Size})
		if err != nil {
			t.Fatalf("the record of the content object: %v", err)
		}
		rd, err := blobcrypt.NewReader(body, dk, true)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := io.ReadAll(rd); err != nil || !bytes.Equal(got, data) {
			t.Fatalf("the content object reads back %v", err)
		}
		if n := w.p.count(t, "blob_copies WHERE sha256 = $1 AND storage_key_id = $2 AND object_key = $3", d[:], sk.ID, name); n != 1 {
			t.Error("the copy row does not name the storage key and the keyed name")
		}
	})
	t.Run("R-STOR-081 Q-STOR-318 the claim refuses a session whose counter would pass the bound, and the request answers 409 session_exhausted", func(t *testing.T) {
		sess, err := svc.Start(ctx, w.p.repo, w.sc)
		if err != nil {
			t.Fatal(err)
		}
		w.p.exec(t, "UPDATE upload_sessions SET sealed_segments = $2 WHERE id = $1", sess.ID.String(), blobcrypt.MaxSegmentsPerKey-81)
		_, err = r.append(w.sc, sess.ID, 0, random(1000))
		var e *errs.Error
		if !errors.As(err, &e) || e.Code != errs.CodeSessionExhausted {
			t.Fatalf("the request answered %v", err)
		}
		if st := w.p.sessionState(t, sess.ID.String()); st != "aborted" && st != "" {
			t.Errorf("the session is %q", st)
		}
	})
}
