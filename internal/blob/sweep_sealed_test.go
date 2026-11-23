package blob_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
)

// The orphan sweep's reading of an object's record on an encrypted connection (storage.md R-STOR-087, Q-STOR-314) at the unit tier:
// an in-memory store and the replica's keys of blobtest.Keys, objects of a kibibyte. What the sweep then does by the digest (adopt,
// claim, delete) runs against PostgreSQL in test/integration/blob/encrypted_jobs_test.go.
func TestR_STOR_087_TheOrphanSweepPlacesAnObjectByItsRecord(t *testing.T) {
	ctx := context.Background()
	p := "quader"
	mk := func(keyed bool) (*blob.Connection, *blobtest.Store, *blobtest.Keys, string) {
		s, ks := blobtest.New(), blobtest.NewKeys()
		id := ks.Draw("archive", 1)
		el := config.StorageConnection{ID: "archive", Bucket: "a", Prefix: &p, Encrypted: true, KeyedNames: &keyed}
		c := blob.NewConnection(el, s)
		c.UseKeys(ks)
		return c, s, ks, id
	}
	write := func(t *testing.T, c *blob.Connection, data []byte) (blob.Digest, blob.CopyWrite) {
		t.Helper()
		d := blob.Digest(sha256.Sum256(data))
		cw, err := c.WriteSealedStream(ctx, d, int64(len(data)), bytes.NewReader(data), 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		return d, cw
	}

	t.Run("R-STOR-087 an object below keyed/ is placed by the digest its record binds, with its storage key and S(L)", func(t *testing.T) {
		c, _, _, id := mk(true)
		d, cw := write(t, c, content(1024, 61))
		got, keyID, stored, ok, err := blob.PlaceByRecord(ctx, c, cw.Key)
		if err != nil || !ok || got != d || keyID != id || stored != cw.StoredSize {
			t.Fatalf("placed %v %x %s %d, %v", ok, got[:4], keyID, stored, err)
		}
	})
	t.Run("R-STOR-087 an object whose record names a key the replica does not hold is not placed, and so never deleted", func(t *testing.T) {
		c, _, ks, id := mk(true)
		_, cw := write(t, c, content(1024, 62))
		ks.Forget("archive", id)
		if _, _, _, ok, err := blob.PlaceByRecord(ctx, c, cw.Key); ok || err != nil {
			t.Fatalf("placed %v, %v", ok, err)
		}
	})
	t.Run("R-STOR-087 a genuine object moved to another keyed name, and one without a record, are not placed", func(t *testing.T) {
		c, s, _, _ := mk(true)
		_, cw := write(t, c, content(1024, 63))
		_, other := write(t, c, content(1024, 64))
		body, _ := s.Data(cw.Key)
		md := s.MetadataOf(cw.Key)
		s.Set(other.Key, body, time.Now())
		s.SetMetadata(other.Key, md)
		if _, _, _, ok, err := blob.PlaceByRecord(ctx, c, other.Key); ok || err != nil {
			t.Fatalf("a moved object: placed %v, %v", ok, err)
		}
		s.SetMetadata(other.Key, nil)
		if _, _, _, ok, err := blob.PlaceByRecord(ctx, c, other.Key); ok || err != nil {
			t.Fatalf("an object without a record: placed %v, %v", ok, err)
		}
	})
	t.Run("R-STOR-087 without keyed names the record must bind the digest the name carries", func(t *testing.T) {
		c, s, _, _ := mk(false)
		d, cw := write(t, c, content(1024, 65))
		if cw.Key != blob.Key(p, d) {
			t.Fatalf("the object lies at %s", cw.Key)
		}
		if got, _, _, ok, err := blob.PlaceByRecord(ctx, c, cw.Key); !ok || err != nil || got != d {
			t.Fatalf("placed %v, %v", ok, err)
		}
		_, other := write(t, c, content(1024, 66))
		body, _ := s.Data(cw.Key)
		s.Set(other.Key, body, time.Now())
		s.SetMetadata(other.Key, s.MetadataOf(cw.Key))
		if _, _, _, ok, err := blob.PlaceByRecord(ctx, c, other.Key); ok || err != nil {
			t.Fatalf("the record of another digest under this name: placed %v, %v", ok, err)
		}
		if blobcrypt.KeyedPrefix(p) == blob.KeyPrefix(p) {
			t.Fatal("the families coincide")
		}
	})
}
