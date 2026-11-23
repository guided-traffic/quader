package reconcile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
)

// The reconciliation's four cases of storage.md R-STOR-087 at the unit tier: two in-memory stores, the replica's keys of
// blobtest.Keys, blobs of a kibibyte and one of three segments where a part's boundary is the point.

// sealedConn is an encrypted connection with keyed names over s, holding the keys of ks.
func sealedConn(id string, s *blobtest.Store, ks *blobtest.Keys) *blob.Connection {
	p := "quader"
	c := blob.NewConnection(config.StorageConnection{ID: id, Bucket: "bucket-" + id, Prefix: &p, Encrypted: true}, s)
	c.SetCapabilities(blob.Capabilities{PayloadChecksumEnforced: true, Metadata: true})
	c.UseKeys(ks)
	return c
}

// sealedSeed writes data onto the encrypted connection c through its writer and returns the blob and its copy.
func sealedSeed(t *testing.T, c *blob.Connection, data []byte) (target, blob.Copy) {
	t.Helper()
	d := blob.Digest(sha256.Sum256(data))
	cw, err := c.WriteSealedStream(context.Background(), d, int64(len(data)), bytes.NewReader(data), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return target{digest: d, size: int64(len(data))}, blob.Copy{Connection: c.ID(), ObjectKey: cw.Key, StoredSize: cw.StoredSize, StorageKeyID: cw.StorageKeyID, State: blob.CopyPresent}
}

// readBack reads the copy the made row describes through its connection's reader.
func readBack(t *testing.T, c *blob.Connection, b target, m made) []byte {
	t.Helper()
	body, _, err := c.ReadBlob(context.Background(), b.digest, b.size, blob.Copy{Connection: c.ID(), ObjectKey: m.key, StoredSize: m.stored}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// plaintextRun reports whether a run of 16 bytes of plain appears in stored.
func plaintextRun(stored, plain []byte) bool {
	for i := 0; i+16 <= len(plain); i += 16 {
		if bytes.Contains(stored, plain[i:i+16]) {
			return true
		}
	}
	return false
}

func TestR_STOR_087_TheReconciliationsFourCases(t *testing.T) {
	ctx := context.Background()
	t.Run("R-STOR-087 plain to encrypted: the blob is sealed under a new data key below keyed/, its row names S(L) and the storage key, and it is verified", func(t *testing.T) {
		a, b, ks := blobtest.New(), blobtest.New(), blobtest.NewKeys()
		active := ks.Draw("archive", 1)
		src, dst := conn("primary", a), sealedConn("archive", b, ks)
		data := bytesOf(1024, 21)
		bl, cp := seed(t, src, a, data)
		m, err := newTestCopier(64<<10).copyBlob(ctx, bl, cp, src, dst)
		if err != nil {
			t.Fatal(err)
		}
		stored, _ := blobcrypt.StoredSize(1024)
		if m.mode != modeStream || !m.verified || m.stored != stored || m.keyID != active || !isKeyed(m.key) {
			t.Fatalf("got %+v, want a verified stream of %d stored bytes under %s below keyed/", m, stored, active)
		}
		body, _ := b.Data(m.key)
		if plaintextRun(body, data) || b.MetadataOf(m.key)[blobcrypt.MetaKey] != active {
			t.Fatal("the encrypted connection holds plaintext or an object without the record of the active key")
		}
		if got := readBack(t, dst, bl, m); !bytes.Equal(got, data) {
			t.Fatal("the copy does not read back")
		}
	})
	t.Run("R-STOR-087 Q-STOR-308 encrypted to encrypted: new bytes under a new data key and the target's own storage key, never the source's ciphertext; both read back", func(t *testing.T) {
		a, b, ks := blobtest.New(), blobtest.New(), blobtest.NewKeys()
		ks.Draw("archive", 1)
		other := ks.Draw("vault", 1)
		src, dst := sealedConn("archive", a, ks), sealedConn("vault", b, ks)
		data := bytesOf(1024, 22)
		bl, cp := sealedSeed(t, src, data)
		m, err := newTestCopier(64<<10).copyBlob(ctx, bl, cp, src, dst)
		if err != nil {
			t.Fatal(err)
		}
		srcBody, _ := a.Data(cp.ObjectKey)
		dstBody, _ := b.Data(m.key)
		if bytes.Equal(srcBody, dstBody) || m.keyID != other || a.MetadataOf(cp.ObjectKey)[blobcrypt.MetaWrap] == b.MetadataOf(m.key)[blobcrypt.MetaWrap] {
			t.Fatal("the copy between two encrypted connections carried the source's ciphertext or its record")
		}
		if got := readBack(t, dst, bl, m); !bytes.Equal(got, data) {
			t.Fatal("the target does not read back")
		}
		if got := readBack(t, src, bl, made{key: cp.ObjectKey, stored: cp.StoredSize}); !bytes.Equal(got, data) {
			t.Fatal("the source does not read back")
		}
	})
	t.Run("R-STOR-087 encrypted to plain: the plaintext is streamed through the source's reader, the row names the plain size and no key", func(t *testing.T) {
		a, b, ks := blobtest.New(), blobtest.New(), blobtest.NewKeys()
		ks.Draw("archive", 1)
		src, dst := sealedConn("archive", a, ks), conn("secondary", b)
		data := bytesOf(1024, 23)
		bl, cp := sealedSeed(t, src, data)
		m, err := newTestCopier(64<<10).copyBlob(ctx, bl, cp, src, dst)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := b.Data(dst.ObjectKey(bl.digest)); !bytes.Equal(got, data) || m.stored != 1024 || m.keyID != "" || m.key != dst.ObjectKey(bl.digest) {
			t.Fatalf("the plain target holds %d bytes, row %+v", len(got), m)
		}
	})
	t.Run("R-STOR-087 a blob of three segments into an encrypted connection goes in parts of one segment each and reads back", func(t *testing.T) {
		a, b, ks := blobtest.New(), blobtest.New(), blobtest.NewKeys()
		b.MinPartSize = 1
		ks.Draw("archive", 1)
		src, dst := conn("primary", a), sealedConn("archive", b, ks)
		data := bytesOf(2*blobcrypt.SegmentSize+100, 24)
		bl, cp := seed(t, src, a, data)
		m, err := newTestCopier(blobcrypt.SegmentSize).copyBlob(ctx, bl, cp, src, dst)
		if err != nil {
			t.Fatal(err)
		}
		if b.Count("upload_part") != 3 || b.Count("complete_multipart") != 1 {
			t.Fatalf("%d parts, %d completions; want 3, 1", b.Count("upload_part"), b.Count("complete_multipart"))
		}
		if got := readBack(t, dst, bl, m); !bytes.Equal(got, data) {
			t.Fatal("the copy does not read back")
		}
	})
	t.Run("R-STOR-087 a plain source whose bytes are not the blob is evidence against it and leaves nothing on the encrypted target, in one request and in parts", func(t *testing.T) {
		for _, size := range []int{1024, 2*blobcrypt.SegmentSize + 100} {
			a, b, ks := blobtest.New(), blobtest.New(), blobtest.NewKeys()
			b.MinPartSize = 1
			ks.Draw("archive", 1)
			src, dst := conn("primary", a), sealedConn("archive", b, ks)
			bl, cp := seed(t, src, a, bytesOf(size, 25))
			a.Set(cp.ObjectKey, bytesOf(size, 26), time.Now())
			_, err := newTestCopier(blobcrypt.SegmentSize).copyBlob(ctx, bl, cp, src, dst)
			var se *sourceError
			if !errors.As(err, &se) || se.status != blob.CopyCorrupt {
				t.Fatalf("size %d: got %v, want the source corrupt", size, err)
			}
			if len(b.Keys("")) != 0 || b.OpenUploads() != 0 {
				t.Fatalf("size %d: the target holds %v and %d open uploads", size, b.Keys(""), b.OpenUploads())
			}
		}
	})
	t.Run("R-STOR-087 R-STOR-082 an encrypted source with a changed segment is corrupt; one whose record names a key the replica lost is no evidence", func(t *testing.T) {
		a, b, ks := blobtest.New(), blobtest.New(), blobtest.NewKeys()
		lost := ks.Draw("archive", 1)
		src, dst := sealedConn("archive", a, ks), sealedConn("vault", b, ks)
		ks.Draw("vault", 1)
		data := bytesOf(1024, 27)
		bl, cp := sealedSeed(t, src, data)
		body, _ := a.Data(cp.ObjectKey)
		changed := bytes.Clone(body)
		changed[40] ^= 1
		md := a.MetadataOf(cp.ObjectKey)
		a.Set(cp.ObjectKey, changed, time.Now())
		a.SetMetadata(cp.ObjectKey, md)
		var se *sourceError
		if _, err := newTestCopier(64<<10).copyBlob(ctx, bl, cp, src, dst); !errors.As(err, &se) || se.status != blob.CopyCorrupt {
			t.Fatalf("a changed segment: %v, want the source corrupt", err)
		}
		a.Set(cp.ObjectKey, body, time.Now())
		a.SetMetadata(cp.ObjectKey, md)
		ks.Forget("archive", lost)
		if _, err := newTestCopier(64<<10).copyBlob(ctx, bl, cp, src, dst); !errors.As(err, &se) || se.status != "" || !errors.Is(err, blobcrypt.ErrUnknownKey) {
			t.Fatalf("an unknown key: %v, want no evidence", err)
		}
		if len(b.Keys("")) != 0 {
			t.Fatalf("the target holds %v", b.Keys(""))
		}
	})
}

func isKeyed(key string) bool { return bytes.HasPrefix([]byte(key), []byte("quader/keyed/")) }
