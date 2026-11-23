package verify

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/backup/listing/fixture"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	storagekeys "github.com/guided-traffic/quader/internal/blob/keys"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/secrets"
)

// verify --deep over encrypted pool objects (backup-and-restore.md R-BAK-031, Q-BAK-66) at the unit tier: the fixture's in-memory
// target, objects of the stored format made by an encrypted connection's writer, a key ring of one root key in a temporary directory.

// sealedSpec is a backup of two blobs an encrypted connection archive stored, and its storage_keys sealed with ring.
func sealedSpec(t *testing.T, ring *secrets.KeyRing) fixture.Spec {
	t.Helper()
	k, _ := blobcrypt.NewKey(rand.Reader)
	id, _ := storagekeys.NewKeyID(storagekeys.KindStorage, "archive", 1, rand.Reader)
	sealed, err := storagekeys.SealStorageKey(ring, id, k)
	if err != nil {
		t.Fatal(err)
	}
	sk, err := storagekeys.OpenStorageKey(ring, id, sealed)
	if err != nil {
		t.Fatal(err)
	}
	nk, _ := blobcrypt.NewKey(rand.Reader)
	ks, store := blobtest.NewKeys(), blobtest.New()
	ks.Add("archive", sk, &nk)
	p := "quader"
	c := blob.NewConnection(config.StorageConnection{ID: "archive", Bucket: "a", Prefix: &p, Encrypted: true}, store)
	c.UseKeys(ks)
	s := fixture.Spec{ID: fixture.ID(3), Blobs: []string{"plain"}, StorageKeys: id + "\tarchive\tstorage\t1\t" + sealed + "\tactive\n"}
	for _, content := range []string{"first sealed blob", "second sealed blob"} {
		cw, err := c.WriteSealedStream(context.Background(), blob.Digest(sha256.Sum256([]byte(content))), int64(len(content)), strings.NewReader(content), 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := store.Data(cw.Key)
		s.Sealed = append(s.Sealed, fixture.Sealed{Content: content, KeyID: id, Body: body, Metadata: store.MetadataOf(cw.Key)})
	}
	return s
}

func TestR_BAK_031_VerifyDeepOfEncryptedPoolObjects(t *testing.T) {
	ring := keyRing(t)
	t.Run("R-BAK-031 without the key ring the encrypted pool objects are checked by their records and counted as not authenticated", func(t *testing.T) {
		e := newEnv(t)
		e.put(t, sealedSpec(t, ring))
		rep, err := e.verify(backup.VerifyOptions{Latest: true, Deep: true})
		if err != nil || len(rep.Findings) != 0 || rep.NotAuthenticated != 2 {
			t.Fatalf("err %v, findings %v, not authenticated %d", err, rep.Findings, rep.NotAuthenticated)
		}
		if rep, _ := e.verify(backup.VerifyOptions{Latest: true}); rep.NotAuthenticated != 0 || len(rep.Findings) != 0 {
			t.Fatalf("a shallow run: %+v", rep)
		}
	})
	t.Run("R-BAK-031 with the key ring every encrypted pool object is read and authenticated; a flipped bit is digest_mismatch", func(t *testing.T) {
		e := newEnv(t)
		s := sealedSpec(t, ring)
		b := e.put(t, s)
		rep, err := e.verify(backup.VerifyOptions{Latest: true, Deep: true, KeyRing: ring})
		if err != nil || len(rep.Findings) != 0 || rep.NotAuthenticated != 0 {
			t.Fatalf("err %v, findings %v, not authenticated %d", err, rep.Findings, rep.NotAuthenticated)
		}
		var key string
		for _, x := range b.Entries {
			if x.Encrypted() {
				key = e.l.PoolObject(x)
				break
			}
		}
		raw, _ := e.mem.Data(key)
		o, _ := e.mem.Head(context.Background(), key)
		raw = bytes.Clone(raw)
		raw[30] ^= 1
		e.mem.Set(key, raw)
		e.mem.SetMetadata(key, o.Metadata)
		rep, err = e.verify(backup.VerifyOptions{Latest: true, Deep: true, KeyRing: ring})
		if err == nil || len(rep.Findings) != 1 || rep.Findings[0].Kind != backup.FindingDigestMismatch || !strings.Contains(rep.Findings[0].Object, ".sk-archive-1-") {
			t.Fatalf("err %v, findings %+v", err, rep.Findings)
		}
		if rep, _ := e.verify(backup.VerifyOptions{Latest: true, Deep: true}); len(rep.Findings) != 0 || rep.NotAuthenticated != 2 {
			t.Fatalf("without the key ring the flipped bit is not seen: %+v", rep)
		}
	})
	t.Run("R-BAK-031 R-BAK-019 an encrypted pool object of another size, without its record, or of a key the backup lacks is a finding", func(t *testing.T) {
		e := newEnv(t)
		s := sealedSpec(t, ring)
		s.Sealed[0].Metadata = map[string]string{}
		s.Sealed[1].Body = s.Sealed[1].Body[:len(s.Sealed[1].Body)-1]
		e.put(t, s)
		rep, _ := e.verify(backup.VerifyOptions{Latest: true, Deep: true})
		kinds := map[backup.FindingKind]int{}
		for _, f := range rep.Findings {
			kinds[f.Kind]++
		}
		if kinds[backup.FindingDigestMismatch] != 1 || kinds[backup.FindingSizeMismatch] != 1 {
			t.Fatalf("findings %+v", rep.Findings)
		}
		e = newEnv(t)
		s = sealedSpec(t, ring)
		s.StorageKeys = "sk-archive-1-00000000\tarchive\tstorage\t1\tv1:x:y\tactive\n"
		e.put(t, s)
		if rep, _ := e.verify(backup.VerifyOptions{Latest: true, Deep: true}); len(rep.Findings) != 2 || !strings.Contains(rep.Findings[0].Detail, "no key the backup") {
			t.Fatalf("findings %+v", rep.Findings)
		}
	})
}

// keyRing is a key ring of one root key, as --key-ring loads it.
func keyRing(t *testing.T) *secrets.KeyRing {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/r1.key"
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := secrets.Load(config.Secrets{EncryptionKeyFiles: []string{path}, ActiveKeyID: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
