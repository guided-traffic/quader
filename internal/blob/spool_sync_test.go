package blob

import (
	"bytes"
	"context"
	"io"
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/syncwrite"
)

// syncRecorder is a spool file that records the bytes written when each flush came.
type syncRecorder struct {
	f       syncwrite.File
	written int
	flushes []int
}

func (r *syncRecorder) Write(p []byte) (int, error) {
	n, err := r.f.Write(p)
	r.written += n
	return n, err
}

func (r *syncRecorder) Sync() error {
	r.flushes = append(r.flushes, r.written)
	return r.f.Sync()
}

// Q-STOR-486: the spool file of an upload is flushed to the disk every syncEvery bytes while the body arrives, so the dirty pages of
// a large upload never count against the replica's memory at once. The threshold is set to 4 bytes and the body is 10 bytes, read
// two bytes at a time; the flushes come at 4 and 8, and the rest stays for the page cache (the file is scratch).
func TestQ_STOR_486_TheSpoolFlushesAnUploadWhileItArrives(t *testing.T) {
	var rec *syncRecorder
	sp := newSpool(t, func(c *config.StorageSpool) { c.MemoryThreshold = 0 })
	sp.syncEvery = 4
	sp.wrapFile = func(f syncwrite.File) syncwrite.File {
		rec = &syncRecorder{f: f}
		return rec
	}
	body := data(10)
	got, err := sp.Ingest(context.Background(), &chunkReader{b: body, n: 2}, IngestOptions{ContentLength: int64(len(body))})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = got.Close() }()
	if rec == nil || !slices.Equal(rec.flushes, []int{4, 8}) {
		t.Fatalf("flushes %+v, want at 4 and 8", rec)
	}
	b := make([]byte, 10)
	if _, err := got.Reader().Read(b); err != nil || !bytes.Equal(b, body) {
		t.Fatalf("the spool holds %v, %v", b, err)
	}
}

// chunkReader reads n bytes at a time.
type chunkReader struct {
	b []byte
	n int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if len(c.b) == 0 {
		return 0, io.EOF
	}
	k := copy(p[:min(len(p), c.n)], c.b)
	c.b = c.b[k:]
	return k, nil
}
