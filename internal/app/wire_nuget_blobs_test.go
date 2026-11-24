package app

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/meta"
)

type noDB struct{}

func (noDB) InTx(ctx context.Context, fn func(meta.Tx) error) error { return fn(nil) }

// oneCopy answers the copies of every blob with one present copy of size bytes on the connection default; a read needs
// nothing else of the rows (R-STOR-055).
type oneCopy struct {
	blob.Rows
	size int64
}

func (r oneCopy) Copies(_ context.Context, _ meta.Tx, d blob.Digest) ([]blob.Copy, error) {
	return []blob.Copy{{Connection: config.DefaultConnectionID, ObjectKey: blob.Key("quader", d), StoredSize: r.size, State: blob.CopyPresent}}, nil
}

// TestNuGetReadmePathMemory reads a stored package through the ranged reader of the NuGet package view
// (one Open per ReadAt, Q-NUGET-48) inside one request: however many ranged reads the zip reader makes,
// the request is never charged more than the admission charge (R-ARCH-005, Q-ARCH-330).
func TestNuGetReadmePathMemory(t *testing.T) {
	t.Run("R-ARCH-005 R-NUGET-027 the ranged reads of a package's README stay within one admission charge", func(t *testing.T) {
		var pkg bytes.Buffer
		zw := zip.NewWriter(&pkg)
		for i := range 150 {
			w, err := zw.CreateHeader(&zip.FileHeader{Name: "lib/f" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".dll", Method: zip.Store})
			if err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write(bytes.Repeat([]byte{byte(i)}, 20<<10))
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}

		cfg := blobtest.Storage(t, t.TempDir())
		mem := blobtest.New()
		var d blob.Digest
		d[0] = 7
		var peak, opens int
		var guard *httpserver.MemoryGuard
		store, err := blob.New(cfg, blob.SingleConnection("quader", mem), noDB{}, blob.WithRows(oneCopy{size: int64(pkg.Len())}), blob.WithMemory(func(ctx context.Context, n int64) func() {
			undo := httpserver.SettleMemory(ctx, n)
			opens++
			peak = max(peak, guard.InUse())
			return undo
		}))
		if err != nil {
			t.Fatal(err)
		}
		mem.Set(blob.Key("quader", d), pkg.Bytes(), time.Now())
		guard = httpserver.NewMemoryGuard(httpserver.MemoryGuardConfig{
			Limit: 1 << 30, RequestBytes: requestMemory(&config.Config{Storage: cfg}), Overhead: requestOverhead,
			Read: func() (uint64, uint64) { return 0, 0 },
		})

		router := httpserver.NewClientRouter()
		router.HandleFunc("GET /readme", func(w http.ResponseWriter, r *http.Request) {
			ra := nugetBlobs(store).ReaderAt(r.Context(), d[:], int64(pkg.Len()))
			zr, err := zip.NewReader(ra, int64(pkg.Len()))
			if err != nil {
				t.Errorf("zip: %v", err)
				return
			}
			for _, f := range zr.File {
				rc, err := f.Open()
				if err != nil {
					t.Errorf("open %s: %v", f.Name, err)
					return
				}
				_, _ = io.Copy(io.Discard, rc)
				_ = rc.Close()
			}
		})
		rec := httptest.NewRecorder()
		httpserver.Chain{Router: router, Memory: guard}.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/readme", nil))
		if opens < 150 {
			t.Fatalf("only %d ranged opens: the case does not exercise the path", opens)
		}
		if peak > 1 {
			t.Fatalf("%d ranged opens charged %d full downloads at once, want at most the one the admission paid for", opens, peak)
		}
		if guard.InUse() != 0 {
			t.Fatalf("%d slots in use after the request", guard.InUse())
		}
	})
}
