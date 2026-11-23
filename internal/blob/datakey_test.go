package blob_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"testing"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
)

// heldKeys is the replica's storage keys of one connection as a map: the seam blob.StorageKeys. active names the storage key new
// objects are wrapped under, name is the name key of a connection with keyed names.
type heldKeys struct {
	conn   string
	keys   map[string]*blobcrypt.StorageKey
	active string
	name   *blobcrypt.Key
}

func (h heldKeys) Active(_ context.Context, connection string) (*blobcrypt.StorageKey, error) {
	if sk := h.keys[h.active]; connection == h.conn && sk != nil {
		return sk, nil
	}
	return nil, errors.New("the replica holds no active storage key of the connection")
}

func (h heldKeys) NameKey(connection string) (blobcrypt.Key, error) {
	if connection != h.conn || h.name == nil {
		return blobcrypt.Key{}, errors.New("the replica holds no name key of the connection")
	}
	return *h.name, nil
}

func (h heldKeys) Lookup(_ context.Context, connection string) blobcrypt.KeyLookup {
	return func(id string) (*blobcrypt.StorageKey, error) {
		if connection != h.conn {
			return nil, nil
		}
		return h.keys[id], nil
	}
}

// encryptedObject seals plain under a new data key, wraps it under sk and returns the stored bytes, the record and the data key.
func encryptedObject(t *testing.T, sk *blobcrypt.StorageKey, plain []byte) ([]byte, blobcrypt.Record, blobcrypt.DataKey) {
	t.Helper()
	dk, err := blobcrypt.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	seed, _ := blobcrypt.NewSeed(rand.Reader)
	d := blobcrypt.DataKey{Key: dk, Digest: sha256.Sum256(plain), Size: int64(len(plain))}
	sec, err := blobcrypt.NewSection(dk, seed, bytes.NewReader(plain), 0, int64(len(plain)), &blobcrypt.Final{Size: d.Size, Digest: d.Digest})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(sec)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := blobcrypt.Wrap(sk, d, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return body, rec, d
}

func TestR_STOR_083_CopyDataKeyOpensTheRecordOfThePresentCopy(t *testing.T) {
	cfg := blobtest.Storage(t, t.TempDir())
	p := "quader"
	cfg.Connections = []config.StorageConnection{
		config.StorageConnection{ID: "primary", Bucket: "a", Prefix: &p}.WithDefaults(),
		config.StorageConnection{ID: "archive", Bucket: "b", Prefix: &p, Encrypted: true}.WithDefaults(),
	}
	plainStore, archive := blobtest.New(), blobtest.New()
	conns, err := blob.NewConnections(blob.NewConnection(cfg.Connections[0], plainStore), blob.NewConnection(cfg.Connections[1], archive))
	if err != nil {
		t.Fatal(err)
	}
	sk, err := blobcrypt.NewStorageKey("sk-archive-1-0a1b2c3d", bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	db := newFakeDB()
	held := heldKeys{conn: "archive", keys: map[string]*blobcrypt.StorageKey{sk.ID: sk}}
	store, err := blob.New(cfg, conns, db, blob.WithRows(db), blob.WithStorageKeys(held))
	if err != nil {
		t.Fatal(err)
	}
	// put stores plain on the archive as a writer of stage 2 would: the object with its record, a live blob row and a present copy.
	put := func(plain []byte, sk *blobcrypt.StorageKey, stored int64) (blob.Digest, []byte, blobcrypt.DataKey) {
		body, rec, dk := encryptedObject(t, sk, plain)
		d := blob.Digest(dk.Digest)
		key := blob.Key("quader", d)
		if err := archive.Put(context.Background(), key, bytes.NewReader(body), int64(len(body)), blob.Checksum{}, rec.Metadata()); err != nil {
			t.Fatal(err)
		}
		db.rows[d] = &fakeRow{state: "live", status: blob.StatusOK, size: int64(len(plain))}
		db.putCopy(d, blob.Copy{Connection: "archive", ObjectKey: key, StoredSize: stored, StorageKeyID: sk.ID, State: blob.CopyPresent})
		db.putCopy(d, blob.Copy{Connection: "primary", ObjectKey: key, StoredSize: int64(len(plain)), State: blob.CopyPresent})
		return d, body, dk
	}
	plain := bytes.Repeat([]byte("handed over "), 100)
	s1k, _ := blobcrypt.StoredSize(int64(len(plain)))

	t.Run("R-STOR-083 the data key of the copy, with its size and identifier, opens the object", func(t *testing.T) {
		d, body, want := put(plain, sk, s1k)
		got, err := store.CopyDataKey(context.Background(), d, "archive")
		if err != nil {
			t.Fatal(err)
		}
		defer got.Key.Destroy()
		if !got.Key.Equal(want.Key) || got.Size != int64(len(plain)) || got.Digest != want.Digest || got.KeyID != sk.ID {
			t.Fatalf("data key %v, want %v", got, want)
		}
		r, err := blobcrypt.NewReader(bytes.NewReader(body), got, true)
		if err != nil {
			t.Fatal(err)
		}
		if out, err := io.ReadAll(r); err != nil || !bytes.Equal(out, plain) {
			t.Fatalf("the object read with the data key: %v", err)
		}
		if n := archive.Count("head"); n != 1 {
			t.Errorf("%d requests HEAD, want one", n)
		}
	})
	t.Run("R-STOR-083 a plain connection, a connection without a present copy and one this replica does not configure give no key", func(t *testing.T) {
		d, _, _ := put(bytes.Repeat([]byte("b"), 10), sk, 10+28+68)
		if _, err := store.CopyDataKey(context.Background(), d, "primary"); !errors.Is(err, blob.ErrNotEncrypted) {
			t.Errorf("plain: %v", err)
		}
		if _, err := store.CopyDataKey(context.Background(), blob.Digest{9}, "archive"); !errors.Is(err, blob.ErrNoPresentCopy) {
			t.Errorf("no copy: %v", err)
		}
		cp, _ := db.copyOf(d, "archive")
		cp.State = blob.CopyCorrupt
		db.putCopy(d, cp)
		if _, err := store.CopyDataKey(context.Background(), d, "archive"); !errors.Is(err, blob.ErrNoPresentCopy) {
			t.Errorf("a corrupt copy: %v", err)
		}
		if _, err := store.CopyDataKey(context.Background(), d, "elsewhere"); err == nil {
			t.Error("a connection this replica does not configure gave a key")
		}
		bare, err := blob.New(cfg, conns, db, blob.WithRows(db))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := bare.CopyDataKey(context.Background(), d, "archive"); err == nil {
			t.Error("a store without storage keys gave a key")
		}
	})
	t.Run("R-STOR-083 steps 1 to 6 run: a key the replica does not hold is unknown_key, another stored size is size, and nothing is marked", func(t *testing.T) {
		other, _ := blobcrypt.NewStorageKey("sk-archive-2-0a1b2c3d", bytes.Repeat([]byte{8}, 32))
		d, _, _ := put(bytes.Repeat([]byte("c"), 20), other, 20+28+68)
		_, err := store.CopyDataKey(context.Background(), d, "archive")
		if r, _ := blobcrypt.ReasonOf(err); r != blobcrypt.ReasonUnknownKey {
			t.Errorf("an unknown key: %v", err)
		}
		d2, _, _ := put(bytes.Repeat([]byte("e"), 30), sk, 30+28+68+1)
		_, err = store.CopyDataKey(context.Background(), d2, "archive")
		if r, _ := blobcrypt.ReasonOf(err); r != blobcrypt.ReasonSize {
			t.Errorf("another stored size: %v", err)
		}
		for _, x := range []blob.Digest{d, d2} {
			if cp, _ := db.copyOf(x, "archive"); cp.State != blob.CopyPresent {
				t.Errorf("the copy was marked %s", cp.State)
			}
		}
	})
}
