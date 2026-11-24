package restore

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"io"
	"sort"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/backup/format"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	storagekeys "github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta/postgres"
)

// The restore's table of R-BAK-031 at the unit tier: the env's in-memory target with a backup whose pool holds one blob as an
// encrypted connection stored it (wrapped under a storage key the backup's storage_keys holds sealed with the env's key ring) and one
// plain blob; the sink an encrypted connection over an in-memory store; step 3 run on its own, blobs of a few bytes.

// sealedBackup turns the env's backup into one of an installation whose connection archive was encrypted: the blob alpha lies in the
// pool as archive stored it, under the name of its storage key; the backup's storage_keys holds archive's storage key and name key.
func sealedBackup(t *testing.T, e *env) (alpha format.Digest, skID string) {
	t.Helper()
	draw := func(kind string) (string, string, *blobcrypt.StorageKey) {
		k, err := blobcrypt.NewKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		id, err := storagekeys.NewKeyID(kind, "archive", 1, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		sealed, err := storagekeys.SealStorageKey(e.ring, id, k)
		if err != nil {
			t.Fatal(err)
		}
		sk, err := storagekeys.OpenStorageKey(e.ring, id, sealed)
		if err != nil {
			t.Fatal(err)
		}
		return id, sealed, sk
	}
	skID, skSealed, sk := draw(storagekeys.KindStorage)
	nkID, nkSealed, nk := draw(storagekeys.KindName)
	ks := blobtest.NewKeys()
	ks.Add("archive", sk, &nk.Key)
	src := blobtest.New()
	p := "quader"
	archive := blob.NewConnection(config.StorageConnection{ID: "archive", Bucket: "a", Prefix: &p, Encrypted: true}, src)
	archive.UseKeys(ks)
	alpha = format.Digest(sha256.Sum256([]byte("alpha")))
	cw, err := archive.WriteSealedStream(context.Background(), blob.Digest(alpha), 5, strings.NewReader("alpha"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := src.Data(cw.Key)
	e.mem.Remove(e.layout.Pool(alpha))
	key := e.layout.PoolObject(format.BlobEntry{Digest: alpha, Pool: skID})
	e.mem.Set(key, body)
	e.mem.SetMetadata(key, src.MetadataOf(cw.Key))
	// the blob list names alpha's pool object by its key
	var entries []format.BlobEntry
	for d, c := range e.pool {
		pool := format.PoolPlain
		if d == alpha {
			pool = skID
		}
		entries = append(entries, format.BlobEntry{Digest: d, Size: int64(len(c)), Pool: pool})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Digest.Less(entries[j].Digest) })
	list, err := format.EncodeBlobList(entries, 3)
	if err != nil {
		t.Fatal(err)
	}
	e.mem.Set(e.layout.BlobList(e.id), list)
	e.m.Blobs.SHA256 = sum(list)
	rows := skID + "\tarchive\tstorage\t1\t" + skSealed + "\tactive\t2026-10-01 00:00:00+00\t\\N\n" +
		nkID + "\tarchive\tname\t1\t" + nkSealed + "\tactive\t2026-10-01 00:00:00+00\t\\N\n"
	z := zstdBytes(t, []byte(rows))
	e.mem.Set(e.layout.Table(e.id, backup.StorageKeysTable), z)
	e.m.Tables = append(e.m.Tables, format.TableFile{
		Name: backup.StorageKeysTable, Class: "data", File: "db/storage_keys.copy.zst",
		Columns: [][2]string{
			{"key_id", "text"},
			{"connection_id", "text"},
			{"kind", "text"},
			{"generation", "integer"},
			{"sealed_key", "text"},
			{"state", "text"},
			{"created_at", "timestamp with time zone"},
			{"retired_at", "timestamp with time zone"},
		},
		Rows: 2, Bytes: int64(len(rows)), CompressedBytes: int64(len(z)), SHA256: sum(z),
	})
	e.m.StorageKeys = &format.StorageKeysInfo{Pool: []format.PoolKey{{KeyID: skID, Objects: 1}}, Sealed: []string{nkID, skID}, Destroyed: []string{}}
	e.putManifest()
	return alpha, skID
}

// encryptedSink makes the restoring configuration's first connection an encrypted one of the id over a new in-memory store.
func encryptedSink(e *env, id string) *blobtest.Store {
	s := blobtest.New()
	p := "quader"
	el := config.StorageConnection{ID: id, Bucket: "sink", Prefix: &p, Encrypted: true}.WithDefaults()
	e.extra = nil
	e.blobs = &fakeBlobs{objs: map[format.Digest][]byte{}}
	e.sinkConn = &Connection{ID: id, Bucket: "sink", Prefix: "quader", Store: e.blobs, Encrypted: true, KeyedNames: true, Writer: blob.NewConnection(el, s)}
	return s
}

// stepThree runs the checks' reading of the storage keys, the drawing and step 3.
func stepThree(t *testing.T, e *env) (*Restorer, error) {
	t.Helper()
	r := e.restorer()
	st := &state{id: e.id, m: e.m}
	if err := r.checkStorageKeys(context.Background(), st); err != nil {
		return r, err
	}
	drawn, err := r.drawKeys(st)
	if err != nil {
		return r, err
	}
	return r, r.stepBlobs(context.Background(), st, &postgres.RestoreMarker{BackupID: string(e.id), Phase: postgres.PhaseBlobs, StorageKeys: drawn})
}

func readSealed(t *testing.T, c *blob.Connection, d format.Digest, size int64, key string) []byte {
	t.Helper()
	stored, _ := blobcrypt.StoredSize(size)
	body, _, err := c.ReadBlob(context.Background(), blob.Digest(d), size, blob.Copy{Connection: c.ID(), ObjectKey: key, StoredSize: stored}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	b, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestR_BAK_031_TheRestoresTable(t *testing.T) {
	t.Run("R-BAK-031 into the connection of the backup's own id: its pool object as it is under the backup's name key, a plain one sealed under its active key", func(t *testing.T) {
		e := newEnv(t)
		alpha, skID := sealedBackup(t, e)
		sink := encryptedSink(e, "archive")
		r, err := stepThree(t, e)
		if err != nil {
			t.Fatal(err)
		}
		pool, _ := e.mem.Data(e.layout.PoolObject(format.BlobEntry{Digest: alpha, Pool: skID}))
		name, err := e.sinkConn.Writer.Name(blob.Digest(alpha))
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := sink.Data(name); !bytes.Equal(got, pool) || sink.MetadataOf(name)[blobcrypt.MetaKey] != skID {
			t.Fatal("the encrypted pool object was not written as it is, with its record, under the backup's name of the digest")
		}
		if got := readSealed(t, e.sinkConn.Writer, alpha, 5, name); string(got) != "alpha" {
			t.Fatalf("the restored object reads %q", got)
		}
		beta := format.Digest(sha256.Sum256([]byte("beta")))
		bname, _ := e.sinkConn.Writer.Name(blob.Digest(beta))
		if sink.MetadataOf(bname)[blobcrypt.MetaKey] != skID || string(readSealed(t, e.sinkConn.Writer, beta, 4, bname)) != "beta" {
			t.Fatal("the plain pool object was not sealed under the connection's active key")
		}
		if r.streamedAll || len(e.blobs.objs) != 0 {
			t.Fatalf("streamedAll %v after an object written as it is; the plain sink got %d objects", r.streamedAll, len(e.blobs.objs))
		}
	})
	t.Run("R-BAK-031 into an encrypted connection of another id: keys drawn for it, every blob decrypted and sealed anew", func(t *testing.T) {
		e := newEnv(t)
		alpha, skID := sealedBackup(t, e)
		sink := encryptedSink(e, "vault")
		r, err := stepThree(t, e)
		if err != nil {
			t.Fatal(err)
		}
		name, _ := e.sinkConn.Writer.Name(blob.Digest(alpha))
		pool, _ := e.mem.Data(e.layout.PoolObject(format.BlobEntry{Digest: alpha, Pool: skID}))
		got, _ := sink.Data(name)
		if bytes.Equal(got, pool) || !strings.HasPrefix(sink.MetadataOf(name)[blobcrypt.MetaKey], "sk-vault-1-") || r.sinkKeys.active.ID != sink.MetadataOf(name)[blobcrypt.MetaKey] {
			t.Fatalf("the object on vault is wrapped under %q", sink.MetadataOf(name)[blobcrypt.MetaKey])
		}
		if string(readSealed(t, e.sinkConn.Writer, alpha, 5, name)) != "alpha" {
			t.Fatal("the restored object does not read back")
		}
		if !r.streamedAll {
			t.Fatal("every blob was streamed and hashed, the copies are verified")
		}
		if rows := drawnRows(r.marker); len(rows) != 2 || rows[0].Connection != "vault" {
			t.Fatalf("the drawn keys the fix-up inserts: %+v", rows)
		}
	})
	t.Run("R-BAK-031 into a plain connection: the encrypted pool object is decrypted and streamed, hashed", func(t *testing.T) {
		e := newEnv(t)
		alpha, _ := sealedBackup(t, e)
		if _, err := stepThree(t, e); err != nil {
			t.Fatal(err)
		}
		if string(e.blobs.objs[alpha]) != "alpha" {
			t.Fatalf("the plain sink holds %q", e.blobs.objs[alpha])
		}
	})
	t.Run("R-BAK-031 a pool object whose key the restored database does not hold is refused, by check 3 and by step 3", func(t *testing.T) {
		e := newEnv(t)
		sealedBackup(t, e)
		e.m.StorageKeys.Pool = append(e.m.StorageKeys.Pool, format.PoolKey{KeyID: "sk-archive-2-00000000", Objects: 1})
		e.putManifest()
		_, err := stepThree(t, e)
		wantExit(t, err, backup.ExitPrecondition, "check 3", "sk-archive-2-00000000")

		e = newEnv(t)
		alpha, skID := sealedBackup(t, e)
		e.m.StorageKeys.Pool = nil
		e.putManifest()
		r := e.restorer()
		st := &state{id: e.id, m: e.m}
		if err := r.checkStorageKeys(context.Background(), st); err != nil {
			t.Fatal(err)
		}
		delete(st.keys, skID)
		err = r.stepBlobs(context.Background(), st, &postgres.RestoreMarker{BackupID: string(e.id), Phase: postgres.PhaseBlobs})
		wantExit(t, err, backup.ExitFailed, skID, "does not hold")
		if _, ok := e.blobs.objs[alpha]; ok {
			t.Fatal("the refused pool object was written")
		}
	})
}
