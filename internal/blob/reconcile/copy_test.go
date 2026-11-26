package reconcile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// conn is a connection of the tests over an in-memory store, its payload checksum enforced as blobtest enforces it.
func conn(id string, s *blobtest.Store) *blob.Connection {
	p := "quader"
	c := blob.NewConnection(config.StorageConnection{ID: id, Bucket: "bucket-" + id, Prefix: &p}, s)
	c.SetCapabilities(blob.Capabilities{PayloadChecksumEnforced: true})
	return c
}

// seed writes data as the copy of its blob on c and returns the blob and the copy.
func seed(t *testing.T, c *blob.Connection, s *blobtest.Store, data []byte) (target, blob.Copy) {
	t.Helper()
	d := blob.Digest(sha256.Sum256(data))
	key := c.ObjectKey(d)
	s.Set(key, data, time.Now())
	return target{digest: d, size: int64(len(data))}, blob.Copy{Connection: c.ID(), ObjectKey: key, StoredSize: int64(len(data)), State: blob.CopyPresent}
}

func bytesOf(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31) + seed
	}
	return b
}

func newTestCopier(partSize int64) *copier {
	return newCopier(partSize, true, newLimiter(0, time.Now), time.Now, quiet)
}

// TestR_STOR_108_TheReconciliationsMarkNamesItsReason: a failure of a source read that is evidence about the source copy carries the
// reason its finding records (storage.md R-STOR-108): "no such object" not_found, a range the object does not have size, a stream that is
// not the blob digest, a refusal of the stored format its reason; a transient error and unknown_key are no evidence and carry none.
// Unit, no data.
func TestR_STOR_108_TheReconciliationsMarkNamesItsReason(t *testing.T) {
	for _, c := range []struct {
		err            error
		status, reason string
	}{
		{blob.ErrNotFound, blob.CopyMissing, blob.ReasonNotFound},
		{&blob.StoreError{Op: "GetObject", Status: 416, Kind: blob.KindOther}, blob.CopyCorrupt, blob.ReasonSize},
		{blob.ErrStreamNotBlob, blob.CopyCorrupt, blob.ReasonDigest},
		{blobcrypt.ErrSegment, blob.CopyCorrupt, "segment"},
		{blobcrypt.ErrUnknownKey, "", ""},
		{&blob.StoreError{Op: "GetObject", Status: 503, Kind: blob.KindTransient}, "", ""},
	} {
		var se *sourceError
		if !errors.As(sourceFailure(c.err), &se) || se.status != c.status || se.reason != c.reason {
			t.Errorf("R-STOR-108 %v: %+v, want %q with %q", c.err, se, c.status, c.reason)
		}
	}
}

func TestR_STOR_056_TheStreamedCopy(t *testing.T) {
	ctx := context.Background()
	t.Run("R-STOR-056 a blob of one part is streamed with the target's payload checksum and is verified where the target enforces it", func(t *testing.T) {
		a, b := blobtest.New(), blobtest.New()
		src, dst := conn("primary", a), conn("secondary", b)
		bl, cp := seed(t, src, a, bytesOf(1024, 1))
		m, err := newTestCopier(4096).copyBlob(ctx, bl, cp, src, dst)
		if err != nil {
			t.Fatal(err)
		}
		if m.mode != modeStream || !m.verified || m.bytes != 1024 {
			t.Fatalf("got %+v, want a verified stream of 1024 bytes", m)
		}
		if got, _ := b.Data(dst.ObjectKey(bl.digest)); !bytes.Equal(got, bytesOf(1024, 1)) || b.Count("put") != 1 {
			t.Fatalf("the target holds %d bytes after %d puts", len(got), b.Count("put"))
		}
	})
	t.Run("R-STOR-056 a blob above one part is a multipart upload of ranged reads, one part held at a time", func(t *testing.T) {
		a, b := blobtest.New(), blobtest.New()
		b.MinPartSize = 1
		src, dst := conn("primary", a), conn("secondary", b)
		data := bytesOf(2500, 2)
		bl, cp := seed(t, src, a, data)
		if _, err := newTestCopier(1024).copyBlob(ctx, bl, cp, src, dst); err != nil {
			t.Fatal(err)
		}
		if got, _ := b.Data(dst.ObjectKey(bl.digest)); !bytes.Equal(got, data) {
			t.Fatalf("the target holds %d bytes", len(got))
		}
		if a.Count("get") != 3 || b.Count("upload_part") != 3 || b.Count("complete_multipart") != 1 {
			t.Fatalf("%d ranged reads, %d parts, %d completions; want 3, 3, 1", a.Count("get"), b.Count("upload_part"), b.Count("complete_multipart"))
		}
	})
	t.Run("R-STOR-056 a source whose bytes are not the blob is evidence against it and leaves no object on the target, also above one part", func(t *testing.T) {
		for _, size := range []int{1024, 2500} {
			a, b := blobtest.New(), blobtest.New()
			b.MinPartSize = 1
			src, dst := conn("primary", a), conn("secondary", b)
			bl, cp := seed(t, src, a, bytesOf(size, 3))
			wrong := bytesOf(size, 4)
			a.Set(cp.ObjectKey, wrong, time.Now()) // same length, other bytes
			_, err := newTestCopier(1024).copyBlob(ctx, bl, cp, src, dst)
			var se *sourceError
			if !errors.As(err, &se) || se.status != blob.CopyCorrupt {
				t.Fatalf("size %d: got %v, want the source corrupt", size, err)
			}
			if _, ok := b.Data(dst.ObjectKey(bl.digest)); ok || b.OpenUploads() != 0 {
				t.Fatalf("size %d: the target holds an object or an open upload after a digest mismatch", size)
			}
		}
	})
	t.Run("R-STOR-056 a source of another size or without its object is evidence; a transient error is not", func(t *testing.T) {
		a, b := blobtest.New(), blobtest.New()
		src, dst := conn("primary", a), conn("secondary", b)
		bl, cp := seed(t, src, a, bytesOf(1024, 5))
		a.Set(cp.ObjectKey, bytesOf(1000, 5), time.Now())
		var se *sourceError
		if _, err := newTestCopier(4096).copyBlob(ctx, bl, cp, src, dst); !errors.As(err, &se) || se.status != blob.CopyCorrupt {
			t.Fatalf("a shorter object: %v, want corrupt", err)
		}
		a.SetFault(func(op, _ string) error {
			if op == "get" {
				return blob.ErrNotFound
			}
			return nil
		})
		if _, err := newTestCopier(4096).copyBlob(ctx, bl, cp, src, dst); !errors.As(err, &se) || se.status != blob.CopyMissing {
			t.Fatalf("no object: %v, want missing", err)
		}
		a.SetFault(func(op, _ string) error {
			if op == "get" {
				return &blob.StoreError{Op: "GetObject", Status: 503, Kind: blob.KindTransient}
			}
			return nil
		})
		if _, err := newTestCopier(4096).copyBlob(ctx, bl, cp, src, dst); !errors.As(err, &se) || se.status != "" {
			t.Fatalf("a transient error: %v, want no evidence", err)
		}
	})
	t.Run("R-STOR-056 a target's failure is the target's, transient as unavailable and a refusal as refused", func(t *testing.T) {
		a, b := blobtest.New(), blobtest.New()
		src, dst := conn("primary", a), conn("secondary", b)
		bl, cp := seed(t, src, a, bytesOf(512, 6))
		for _, tc := range []struct {
			err    error
			reason string
		}{
			{&blob.StoreError{Op: "PutObject", Status: 503, Kind: blob.KindTransient}, ReasonUnavailable},
			{blob.ErrCircuitOpen, ReasonUnavailable},
			{&blob.StoreError{Op: "PutObject", Code: "AccessDenied", Status: 403, Kind: blob.KindConfig}, ReasonRefused},
		} {
			b.SetFault(func(op, _ string) error {
				if op == "put" {
					return tc.err
				}
				return nil
			})
			_, err := newTestCopier(4096).copyBlob(ctx, bl, cp, src, dst)
			var te *targetError
			if !errors.As(err, &te) || te.reason() != tc.reason {
				t.Fatalf("%v: got %v, want the target's %s", tc.err, err, tc.reason)
			}
		}
	})
}

func TestR_STOR_056_TheServerSideCopy(t *testing.T) {
	ctx := context.Background()
	pair := func() (*blobtest.Endpoint, *blob.Connection, *blob.Connection) {
		e := blobtest.NewEndpoint("objects.example")
		src, dst := conn("primary", e.Bucket("bucket-primary")), conn("secondary", e.Bucket("bucket-secondary"))
		dst.SetCapabilities(blob.Capabilities{PayloadChecksumEnforced: true, ServerSideCopyFrom: map[string]bool{"primary": true}})
		return e, src, dst
	}
	t.Run("R-STOR-056 within one endpoint, where the start check found it to work, the copy is server-side, present and not verified", func(t *testing.T) {
		e, src, dst := pair()
		bl, cp := seed(t, src, e.Bucket("bucket-primary"), bytesOf(700, 7))
		m, err := newTestCopier(4096).copyBlob(ctx, bl, cp, src, dst)
		if err != nil {
			t.Fatal(err)
		}
		if m.mode != modeServer || m.verified || m.bytes != 0 {
			t.Fatalf("got %+v, want a server-side copy that is not verified and moved no byte", m)
		}
		target := e.Bucket("bucket-secondary")
		if got, _ := target.Data(dst.ObjectKey(bl.digest)); !bytes.Equal(got, bytesOf(700, 7)) || target.Count("copy_from") != 1 || e.Bucket("bucket-primary").Count("get") != 0 {
			t.Fatalf("the target holds %d bytes after %d server-side copies", len(got), target.Count("copy_from"))
		}
	})
	t.Run("R-STOR-056 with server_side never, or without the start check's word, the copy is streamed", func(t *testing.T) {
		e, src, dst := pair()
		bl, cp := seed(t, src, e.Bucket("bucket-primary"), bytesOf(700, 8))
		never := newCopier(4096, false, newLimiter(0, time.Now), time.Now, quiet)
		if m, err := never.copyBlob(ctx, bl, cp, src, dst); err != nil || m.mode != modeStream {
			t.Fatalf("server_side never: %+v, %v", m, err)
		}
		dst.SetCapabilities(blob.Capabilities{PayloadChecksumEnforced: true})
		if m, err := newTestCopier(4096).copyBlob(ctx, bl, cp, src, dst); err != nil || m.mode != modeStream {
			t.Fatalf("no capability: %+v, %v", m, err)
		}
	})
	t.Run("R-STOR-056 a refused server-side copy falls back to streaming for that pair for the rest of the run", func(t *testing.T) {
		e, src, dst := pair()
		bl, cp := seed(t, src, e.Bucket("bucket-primary"), bytesOf(700, 9))
		target := e.Bucket("bucket-secondary")
		target.SetFault(func(op, _ string) error {
			if op == "copy_from" {
				return &blob.StoreError{Op: "CopyObject", Code: "AccessDenied", Status: 403, Kind: blob.KindConfig}
			}
			return nil
		})
		c := newTestCopier(4096)
		for range 2 {
			if m, err := c.copyBlob(ctx, bl, cp, src, dst); err != nil || m.mode != modeStream {
				t.Fatalf("got %+v, %v, want a stream", m, err)
			}
		}
		if target.Count("copy_from") != 1 {
			t.Fatalf("%d server-side copies tried, want 1: the refusal holds for the run", target.Count("copy_from"))
		}
	})
	t.Run("R-STOR-056 a server-side copy whose source is gone is evidence against the source", func(t *testing.T) {
		e, src, dst := pair()
		bl, cp := seed(t, src, e.Bucket("bucket-primary"), bytesOf(700, 10))
		cp.ObjectKey += "-gone"
		_, err := newTestCopier(4096).copyBlob(ctx, bl, cp, src, dst)
		var se *sourceError
		if !errors.As(err, &se) || se.status != blob.CopyMissing {
			t.Fatalf("got %v, want the source missing", err)
		}
	})
}

func TestR_STOR_056_TheThrottleSpacesTheStreamedBytes(t *testing.T) {
	now := time.Unix(0, 0)
	var slept time.Duration
	l := newLimiter(1000, func() time.Time { return now })
	l.sleep = func(_ context.Context, d time.Duration) error { slept += d; return nil }
	for range 4 {
		if err := l.wait(context.Background(), 500); err != nil {
			t.Fatal(err)
		}
	}
	// 2,000 bytes at 1,000 bytes a second: the first reservation goes at once, the last waits 1.5 s on a clock that stands
	if slept != 500*time.Millisecond+time.Second+1500*time.Millisecond {
		t.Fatalf("slept %v", slept)
	}
	var unlimited *limiter
	if err := unlimited.wait(context.Background(), 1<<30); err != nil {
		t.Fatal(err)
	}
}
