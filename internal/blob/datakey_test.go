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

// multiKeys answers for the connections of several held key sets.
type multiKeys []heldKeys

func (m multiKeys) pick(connection string) heldKeys {
	for _, h := range m {
		if h.conn == connection {
			return h
		}
	}
	return heldKeys{}
}

func (m multiKeys) Active(ctx context.Context, connection string) (*blobcrypt.StorageKey, error) {
	return m.pick(connection).Active(ctx, connection)
}

func (m multiKeys) NameKey(connection string) (blobcrypt.Key, error) {
	return m.pick(connection).NameKey(connection)
}

func (m multiKeys) Lookup(ctx context.Context, connection string) blobcrypt.KeyLookup {
	return m.pick(connection).Lookup(ctx, connection)
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

func mustStored(t *testing.T, n int) int64 {
	t.Helper()
	s, err := blobcrypt.StoredSize(int64(n))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestR_STOR_083_CopyDataKeyOpensTheRecordOfTheCopy: CopyDataKey opens the record of the copy its caller read (one HeadObject, no
// statement, Q-STOR-470) and returns that object's own data key, never another copy's of the same blob (R-STOR-087); a plain
// connection, a copy that is not present or lies on a retired connection, a connection the replica does not configure and a store
// without storage keys give none; steps 1 to 6 run, so a key the replica does not hold is unknown_key and another stored size is
// size, and nothing is read or marked.
func TestR_STOR_083_CopyDataKeyOpensTheRecordOfTheCopy(t *testing.T) {
	cfg := blobtest.Storage(t, t.TempDir())
	p := "quader"
	cfg.Connections = []config.StorageConnection{
		config.StorageConnection{ID: "primary", Bucket: "a", Prefix: &p}.WithDefaults(),
		config.StorageConnection{ID: "archive", Bucket: "b", Prefix: &p, Encrypted: true}.WithDefaults(),
		config.StorageConnection{ID: "second", Bucket: "c", Prefix: &p, Encrypted: true}.WithDefaults(),
	}
	plainStore, archive, second := blobtest.New(), blobtest.New(), blobtest.New()
	conns, err := blob.NewConnections(blob.NewConnection(cfg.Connections[0], plainStore), blob.NewConnection(cfg.Connections[1], archive),
		blob.NewConnection(cfg.Connections[2], second))
	if err != nil {
		t.Fatal(err)
	}
	sk, err := blobcrypt.NewStorageKey("sk-archive-1-0a1b2c3d", bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sk2, err := blobcrypt.NewStorageKey("sk-second-1-0a1b2c3d", bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	db := newFakeDB()
	held := heldKeys{conn: "archive", keys: map[string]*blobcrypt.StorageKey{sk.ID: sk}}
	store, err := blob.New(cfg, conns, db, blob.WithRows(db), blob.WithStorageKeys(multiKeys{held, {conn: "second", keys: map[string]*blobcrypt.StorageKey{sk2.ID: sk2}}}))
	if err != nil {
		t.Fatal(err)
	}
	// put stores plain on a connection as a writer of stage 2 would, the object with its record, and returns the copy row a lookup reads.
	put := func(on *blobtest.Store, conn string, plain []byte, sk *blobcrypt.StorageKey, stored int64) (blob.Digest, blob.Copy, []byte, blobcrypt.DataKey) {
		body, rec, dk := encryptedObject(t, sk, plain)
		d := blob.Digest(dk.Digest)
		key := blob.Key("quader", d)
		if err := on.Put(context.Background(), key, bytes.NewReader(body), int64(len(body)), blob.Checksum{}, rec.Metadata()); err != nil {
			t.Fatal(err)
		}
		return d, blob.Copy{Connection: conn, ObjectKey: key, StoredSize: stored, StorageKeyID: sk.ID, State: blob.CopyPresent}, body, dk
	}
	plain := bytes.Repeat([]byte("handed over "), 100)
	size := int64(len(plain))

	t.Run("R-STOR-083 the data key of the copy, with its size and identifier, opens the object: one HeadObject and no statement", func(t *testing.T) {
		d, cp, body, want := put(archive, "archive", plain, sk, mustStored(t, len(plain)))
		got, err := store.CopyDataKey(context.Background(), d, size, cp)
		if err != nil {
			t.Fatal(err)
		}
		defer got.Key.Destroy()
		if !got.Key.Equal(want.Key) || got.Size != size || got.Digest != want.Digest || got.KeyID != sk.ID {
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
	t.Run("R-STOR-083 R-STOR-087 each copy of one blob gives its own object's key, never another copy's", func(t *testing.T) {
		one := bytes.Repeat([]byte("one blob, two copies "), 40)
		d, onArchive, _, keyArchive := put(archive, "archive", one, sk, mustStored(t, len(one)))
		d2, onSecond, _, keySecond := put(second, "second", one, sk2, mustStored(t, len(one)))
		if d != d2 || keyArchive.Key.Equal(keySecond.Key) {
			t.Fatal("the two copies of one blob should have one digest and two data keys")
		}
		for _, c := range []struct {
			cp          blob.Copy
			own, others blobcrypt.DataKey
		}{{onArchive, keyArchive, keySecond}, {onSecond, keySecond, keyArchive}} {
			got, err := store.CopyDataKey(context.Background(), d, int64(len(one)), c.cp)
			if err != nil {
				t.Fatalf("%s: %v", c.cp.Connection, err)
			}
			if !got.Key.Equal(c.own.Key) || got.Key.Equal(c.others.Key) {
				t.Errorf("the copy on %s did not give its own object's key", c.cp.Connection)
			}
			got.Key.Destroy()
		}
	})
	t.Run("R-STOR-083 a plain connection, a copy that is not present and a connection this replica does not configure give no key", func(t *testing.T) {
		d, cp, _, _ := put(archive, "archive", bytes.Repeat([]byte("b"), 10), sk, 10+28+68)
		onPlain := cp
		onPlain.Connection = "primary"
		if _, err := store.CopyDataKey(context.Background(), d, 10, onPlain); !errors.Is(err, blob.ErrNotEncrypted) {
			t.Errorf("plain: %v", err)
		}
		corrupt := cp
		corrupt.State = blob.CopyCorrupt
		if _, err := store.CopyDataKey(context.Background(), d, 10, corrupt); !errors.Is(err, blob.ErrNoPresentCopy) {
			t.Errorf("a corrupt copy: %v", err)
		}
		retired := cp
		retired.Retired = true
		if _, err := store.CopyDataKey(context.Background(), d, 10, retired); !errors.Is(err, blob.ErrNoPresentCopy) {
			t.Errorf("a copy on a retired connection: %v", err)
		}
		elsewhere := cp
		elsewhere.Connection = "elsewhere"
		if _, err := store.CopyDataKey(context.Background(), d, 10, elsewhere); err == nil {
			t.Error("a connection this replica does not configure gave a key")
		}
		bare, err := blob.New(cfg, conns, db, blob.WithRows(db))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := bare.CopyDataKey(context.Background(), d, 10, cp); err == nil {
			t.Error("a store without storage keys gave a key")
		}
	})
	t.Run("R-STOR-083 steps 1 to 6 run: a key the replica does not hold is unknown_key, another stored size or blob size is refused", func(t *testing.T) {
		other, _ := blobcrypt.NewStorageKey("sk-archive-2-0a1b2c3d", bytes.Repeat([]byte{8}, 32))
		d, cp, _, _ := put(archive, "archive", bytes.Repeat([]byte("c"), 20), other, 20+28+68)
		_, err := store.CopyDataKey(context.Background(), d, 20, cp)
		if r, _ := blobcrypt.ReasonOf(err); r != blobcrypt.ReasonUnknownKey {
			t.Errorf("an unknown key: %v", err)
		}
		d2, cp2, _, _ := put(archive, "archive", bytes.Repeat([]byte("e"), 30), sk, 30+28+68+1)
		_, err = store.CopyDataKey(context.Background(), d2, 30, cp2)
		if r, _ := blobcrypt.ReasonOf(err); r != blobcrypt.ReasonSize {
			t.Errorf("another stored size: %v", err)
		}
		d3, cp3, _, _ := put(archive, "archive", bytes.Repeat([]byte("f"), 40), sk, 40+28+68)
		if _, err = store.CopyDataKey(context.Background(), d3, 41, cp3); err == nil {
			t.Error("a blob size the record does not bind gave a key")
		}
	})
	t.Run("R-STOR-083 Q-STOR-470 CopyDataKey reads no row and marks nothing", func(t *testing.T) {
		if db.txCount != 0 {
			t.Errorf("%d transactions ran, want none", db.txCount)
		}
	})
}
