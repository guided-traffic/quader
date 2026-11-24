package create

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/backup/format"
	"github.com/guided-traffic/quader/internal/backup/target"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
)

// The backup of encrypted connections at the unit tier (backup-and-restore.md R-BAK-031, R-BAK-032): the pipeline's fakes, objects of
// the stored format made by an encrypted connection's writer over an in-memory store, blobs of a few hundred bytes.

// sealedCopy gives the env's blob of content a copy on conn as an encrypted connection holds it: its ciphertext, its record and the
// storage key the record names, under a key drawn for conn. It returns the key's identifier.
func (e *env) sealedCopy(content, conn string) string {
	e.t.Helper()
	ks, store := blobtest.NewKeys(), blobtest.New()
	id := ks.Draw(conn, 1)
	p := "quader"
	c := blob.NewConnection(config.StorageConnection{ID: conn, Bucket: "b", Prefix: &p, Encrypted: true}, store)
	c.UseKeys(ks)
	d := digestOf([]byte(content))
	cw, err := c.WriteSealedStream(context.Background(), blob.Digest(d), int64(len(content)), strings.NewReader(content), 1<<20)
	if err != nil {
		e.t.Fatal(err)
	}
	body, _ := store.Data(cw.Key)
	e.src.objects(conn)[d] = body
	e.src.enc[conn] = true
	if e.src.meta[conn] == nil {
		e.src.meta[conn], e.src.keys[conn] = map[format.Digest]map[string]string{}, map[format.Digest]string{}
	}
	e.src.meta[conn][d], e.src.keys[conn][d] = store.MetadataOf(cw.Key), id
	if !slices.Contains(e.src.rows[d], conn) {
		e.src.rows[d] = append(e.src.rows[d], conn)
	}
	return id
}

// list reads the final blob list of a backup.
func (e *env) list(id string) []format.BlobEntry {
	e.t.Helper()
	raw, ok := e.mem.Data("quader/backups/" + id + "/blobs.idx.zst")
	if !ok {
		e.t.Fatalf("no blob list of %s", id)
	}
	r, err := format.NewBlobListReader(bytes.NewReader(raw))
	if err != nil {
		e.t.Fatal(err)
	}
	defer r.Close()
	out, err := format.ReadAll(r)
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

// plainRun reports whether a run of 16 bytes of plain appears in b.
func plainRun(b []byte, plain string) bool {
	for i := 0; i+16 <= len(plain); i += 16 {
		if bytes.Contains(b, []byte(plain[i:i+16])) {
			return true
		}
	}
	return false
}

func TestR_BAK_031_ThePoolKeepsAnEncryptedCopyAsStored(t *testing.T) {
	content := strings.Repeat("encrypted connection content ", 20)
	for _, conn := range []string{"default", "secondary"} { // default copies server-side, secondary streams through the pod
		t.Run("R-BAK-031 R-BAK-032 from "+conn+": the pool object is named by the digest and the key, holds the ciphertext with its record, and the list and the manifest name the key", func(t *testing.T) {
			e := newEnv(t)
			d := e.addBlob(content)
			e.src.rows[d] = nil
			delete(e.src.data, d)
			id := e.sealedCopy(content, conn)
			e.snap.keys = []backup.StorageKeyState{{ID: id}, {ID: "nk-" + conn + "-00000000"}, {ID: "sk-gone-1-00000000", Destroyed: true}}
			res, err := e.create(backup.CreateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			key := "quader/pool/sha256/" + d.Hex()[0:2] + "/" + d.Hex()[2:4] + "/" + d.Hex() + "." + id
			got, ok := e.mem.Data(key)
			if !ok {
				t.Fatalf("no pool object %s; keys %v", key, e.mem.Keys())
			}
			o, _ := e.mem.Head(context.Background(), key)
			if !bytes.Equal(got, e.src.objects(conn)[d]) || o.Metadata[blobcrypt.MetaKey] != id || blobcrypt.CheckRecordForm(o.Metadata) != nil || plainRun(got, content) {
				t.Fatal("the pool object is not the copy as stored, with its record and no plaintext")
			}
			if _, plain := e.mem.Data("quader/pool/sha256/" + d.Hex()[0:2] + "/" + d.Hex()[2:4] + "/" + d.Hex()); plain {
				t.Fatal("a plain pool object was written for an encrypted copy")
			}
			if l := e.list(res.BackupID); len(l) != 1 || l[0].Pool != id {
				t.Fatalf("the blob list names %+v, want the pool object of %s", l, id)
			}
			m := e.manifest(res.BackupID)
			stored, _ := blobcrypt.StoredSize(int64(len(content)))
			sk := m.StorageKeys
			if sk == nil || len(sk.Pool) != 1 || sk.Pool[0] != (format.PoolKey{KeyID: id, Objects: 1, Bytes: stored}) ||
				!slices.Equal(sk.Sealed, []string{"nk-" + conn + "-00000000", id}) || !slices.Equal(sk.Destroyed, []string{"sk-gone-1-00000000"}) {
				t.Fatalf("storageKeys = %+v", sk)
			}
			if _, pending := e.mem.Data("quader/backups/" + res.BackupID + "/blobs.pending.zst"); pending {
				t.Fatal("the provisional blob list was not deleted")
			}
		})
	}
	t.Run("R-BAK-031 R-BAK-030 an encrypted copy without its record is no copy to take: the run takes the plain copy and records no damage", func(t *testing.T) {
		e := newEnv(t)
		d := e.addBlob(content)
		e.sealedCopy(content, "secondary")
		e.src.meta["secondary"][d] = nil
		e.src.rows[d] = []string{"secondary", "default"}
		e.src.drain["default"] = true // the encrypted copy is tried first; the plain one is the fallback (Q-BAK-76)
		res, err := e.create(backup.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if l := e.list(res.BackupID); res.DamagedBlobs != 0 || res.SourceFailures["secondary"] != 1 || l[0].Pool != format.PoolPlain {
			t.Fatalf("result %+v, list %+v", res, l)
		}
	})
	t.Run("R-BAK-031 a server-side copy holds the source to the entity tag of its head: an object written again meanwhile is not taken, the next copy is", func(t *testing.T) {
		e := newEnv(t)
		d := e.addBlob(content)
		delete(e.src.data, d)
		e.src.rows[d] = nil
		e.sealedCopy(content, "default") // copied server-side
		second := e.sealedCopy(content, "secondary")
		e.src.etag["default"] = `"v1"`
		var asked []string
		e.mem.SourceETag = func(r target.ObjectRef) string { asked = append(asked, r.IfMatch); return `"v2"` } // written again since its head
		res, err := e.create(backup.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if l := e.list(res.BackupID); !slices.Equal(asked, []string{`"v1"`}) || res.DamagedBlobs != 0 || res.SourceFailures["default"] != 1 || len(l) != 1 || l[0].Pool != second {
			t.Fatalf("if-match %v, result %+v, list %+v", asked, res, l)
		}
		for _, k := range e.mem.Keys() {
			if strings.Contains(k, "/pool/") && !strings.HasSuffix(k, "."+second) {
				t.Fatalf("a pool object %s was left from the copy that changed", k)
			}
		}
	})
	t.Run("R-BAK-031 an encrypted copy without its record as the only copy is recorded as damaged", func(t *testing.T) {
		e := newEnv(t)
		d := e.addBlob(content)
		delete(e.src.data, d)
		e.src.rows[d] = nil
		e.sealedCopy(content, "secondary")
		e.src.meta["secondary"][d] = map[string]string{}
		res, err := e.create(backup.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if res.DamagedBlobs != 1 || e.list(res.BackupID)[0].Pool != format.PoolPlain {
			t.Fatalf("result %+v, list %+v", res, e.list(res.BackupID))
		}
	})
}

func TestR_BAK_032_TheCopyOrderAndTheRepool(t *testing.T) {
	content := strings.Repeat("a blob of a departing connection ", 12)
	t.Run("R-BAK-030 a draining connection's copy comes after every other copy, also before a server-side copy on the target's endpoint", func(t *testing.T) {
		e := newEnv(t)
		d := e.addBlob(content)
		id := e.sealedCopy(content, "secondary")
		e.src.rows[d] = []string{"default", "secondary"}
		e.src.drain["default"] = true
		res, err := e.create(backup.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if l := e.list(res.BackupID); l[0].Pool != id || e.src.heads["default"] != 0 {
			t.Fatalf("the list names %+v after %d reads of the draining copy", l, e.src.heads["default"])
		}
	})
	t.Run("R-BAK-032 a blob pooled under a key of a connection that drains is copied again from one that stays, under a name of its own; the older object stays", func(t *testing.T) {
		e := newEnv(t)
		d := e.addBlob(content)
		delete(e.src.data, d)
		e.src.rows[d] = nil
		id := e.sealedCopy(content, "secondary")
		first, err := e.create(backup.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		e.src.drain["secondary"] = true
		e.src.data[d] = []byte(content)
		e.src.rows[d] = []string{"secondary", "default"}
		second, err := e.create(backup.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if l := e.list(second.BackupID); l[0].Pool != format.PoolPlain || second.Repooled != 1 || second.BlobsCopied != 1 {
			t.Fatalf("second %+v, list %+v", second, l)
		}
		old := "quader/pool/sha256/" + d.Hex()[0:2] + "/" + d.Hex()[2:4] + "/" + d.Hex() + "." + id
		if _, ok := e.mem.Data(old); !ok {
			t.Fatal("the older pool object was removed: pool objects are immutable")
		}
		if l := e.list(first.BackupID); l[0].Pool != id {
			t.Fatalf("the first backup's list names %+v", l)
		}
		if m := e.manifest(second.BackupID); len(m.StorageKeys.Pool) != 0 {
			t.Fatalf("the second backup's pool names %+v", m.StorageKeys.Pool)
		}
	})
	t.Run("R-BAK-032 without a copy on a connection that stays the entry keeps its inherited pool object; a connection that is gone is a warning", func(t *testing.T) {
		e := newEnv(t)
		d := e.addBlob(content)
		delete(e.src.data, d)
		e.src.rows[d] = nil
		id := e.sealedCopy(content, "secondary")
		if _, err := e.create(backup.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		e.src.drain["secondary"] = true
		kept, err := e.create(backup.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if l := e.list(kept.BackupID); l[0].Pool != id || kept.Repooled != 0 || kept.DamagedBlobs != 0 || len(kept.Warnings) != 0 {
			t.Fatalf("kept %+v, list %+v", kept, l)
		}
		e.noSecondary = true
		e.src.rows[d] = nil
		gone, err := e.create(backup.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if l := e.list(gone.BackupID); l[0].Pool != id || gone.Outcome != string(backup.StateCompletedWithWarning) ||
			len(gone.Warnings) != 1 || !strings.Contains(gone.Warnings[0], id) {
			t.Fatalf("gone %+v, list %+v", gone, l)
		}
	})
}

func TestR_BAK_032_TheFinalListResolvesThePendingLinesFromThePins(t *testing.T) {
	e := newEnv(t)
	a := e.addBlob("first blob")
	b := e.addBlob("second blob")
	delete(e.src.data, b) // damaged in the source: listed, not in the pool
	res, err := e.create(backup.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	l := e.list(res.BackupID)
	if len(l) != 2 || e.runs.pinPools[a] != format.PoolPlain || e.runs.pinPools[b] != "" {
		t.Fatalf("list %+v, pin pools %v", l, e.runs.pinPools)
	}
	for _, x := range l {
		if x.Pool != format.PoolPlain {
			t.Fatalf("%s names %q", x.Digest.Hex(), x.Pool)
		}
	}
	raw, _ := e.mem.Data("quader/backups/" + res.BackupID + "/blobs.idx.zst")
	if got := sha256.Sum256(raw); format.Digest(got).Hex() != e.manifest(res.BackupID).Blobs.SHA256 {
		t.Fatal("the manifest names another list than the final one")
	}
	_, _ = io.Discard.Write(raw)
}
