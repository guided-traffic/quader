package blob_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"sync"
	"syscall"
	"testing"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/observability"
)

// The blob store's side of the history of the stored content (storage.md R-STOR-108 to R-STOR-110): what each mark names, what the
// reads and writes hand to the recorder, the codes and steps a failure is recorded with, and the server-side encryption the start
// check reads. Unit, the in-memory stores and the simulated tables; objects of 1 KiB. The triggers that turn a mark into a finding
// and the recorder's writes are the integration tests of test/integration/blob/history_test.go.

// sinkRecorder is a blob.HistorySink that keeps what it was told: "<connection> <kind> <reason> <bytes>".
type sinkRecorder struct {
	mu  sync.Mutex
	got []string
}

func (s *sinkRecorder) Count(conn, kind, reason string, bytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, fmt.Sprintf("%s %s %s %d", conn, kind, reason, bytes))
}

func (s *sinkRecorder) counted() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.got)
}

func (f *fakeDB) marked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.marks)
}

// twoStores is a store over two plain connections, one and two, each an in-memory store, with a recorder of what the metrics count.
type twoStores struct {
	store    *blob.Store
	one, two *blobtest.Store
	db       *fakeDB
	sink     *sinkRecorder
}

func newTwoStores(t *testing.T, minCopies int) *twoStores {
	t.Helper()
	cfg := blobtest.Storage(t, t.TempDir())
	p := "quader"
	cfg.Connections = []config.StorageConnection{
		config.StorageConnection{ID: "one", Bucket: "a", Prefix: &p}.WithDefaults(),
		config.StorageConnection{ID: "two", Bucket: "b", Prefix: &p}.WithDefaults(),
	}
	cfg.Replication.MinCopies = minCopies
	s := &twoStores{one: blobtest.New(), two: blobtest.New(), db: newFakeDB(), sink: &sinkRecorder{}}
	conns, err := blob.NewConnections(blob.NewConnection(cfg.Connections[0], s.one), blob.NewConnection(cfg.Connections[1], s.two))
	if err != nil {
		t.Fatal(err)
	}
	bm := blob.NewMetrics(observability.NewMetrics("web"))
	bm.SetHistory(s.sink)
	if s.store, err = blob.New(cfg, conns, s.db, blob.WithRows(s.db), blob.WithMetrics(bm)); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *twoStores) commit(t *testing.T, data []byte) (*blob.Spooled, error) {
	t.Helper()
	sp, err := s.store.Ingest(context.Background(), bytes.NewReader(data), blob.IngestOptions{ContentLength: int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sp.Close() })
	_, err = s.store.Commit(context.Background(), sp, noLink)
	return sp, err
}

func (s *twoStores) read(sp *blob.Spooled, rng *blob.ByteRange) error {
	r, err := s.store.Open(context.Background(), sp.SHA256, sp.Size, blob.OpenOptions{Range: rng})
	if err != nil {
		return err
	}
	defer r.Close()
	_, err = io.ReadAll(r)
	return err
}

func TestR_STOR_108_EveryMarkCarriesItsSourceAndReason(t *testing.T) {
	data := content(1024, 3)
	key := func(sp *blob.Spooled) string { return blob.Key("quader", sp.SHA256) }
	for _, c := range []struct {
		name  string
		rng   *blob.ByteRange
		alter func(m *blobtest.Store, k string)
		want  string
	}{
		{
			"a whole read of another digest marks download digest", nil,
			func(m *blobtest.Store, k string) { b, _ := m.Data(k); b[10] ^= 1; m.Set(k, b, m.Now()) }, "one corrupt download digest",
		},
		{
			"a whole read of another size marks download size", nil,
			func(m *blobtest.Store, k string) { b, _ := m.Data(k); m.Set(k, b[:100], m.Now()) }, "one corrupt download size",
		},
		{
			"a whole read of no such object marks download not_found", nil,
			func(m *blobtest.Store, k string) { _ = m.Delete(context.Background(), k) }, "one missing download not_found",
		},
		{
			"a ranged read of no such object marks read not_found", &blob.ByteRange{Offset: 10, Length: 20},
			func(m *blobtest.Store, k string) { _ = m.Delete(context.Background(), k) }, "one missing read not_found",
		},
		{
			"a ranged read of another size marks read size", &blob.ByteRange{Offset: 10, Length: 20},
			func(m *blobtest.Store, k string) { b, _ := m.Data(k); m.Set(k, b[:100], m.Now()) }, "one corrupt read size",
		},
	} {
		t.Run("R-STOR-108 "+c.name, func(t *testing.T) {
			s := newTwoStores(t, 2)
			sp, err := s.commit(t, data)
			if err != nil {
				t.Fatal(err)
			}
			c.alter(s.one, key(sp))
			_ = s.read(sp, c.rng)
			if got := s.db.marked(); !slices.Contains(got, c.want) {
				t.Fatalf("marks %v, want %q", got, c.want)
			}
		})
	}
}

func TestR_STOR_110_TheStoreCountsWhatItAbsorbed(t *testing.T) {
	data := content(1024, 5)
	transient := &blob.StoreError{Op: "PutObject", Status: 503, Kind: blob.KindTransient}
	t.Run("R-STOR-110 a read that falls over is counted as read_fallover with the metric's reason", func(t *testing.T) {
		s := newTwoStores(t, 2)
		sp, _ := s.commit(t, data)
		_ = s.one.Delete(context.Background(), blob.Key("quader", sp.SHA256))
		if err := s.read(sp, nil); err != nil {
			t.Fatal(err)
		}
		if got := s.sink.counted(); !slices.Contains(got, "one read_fallover not_found 0") {
			t.Fatalf("counted %v", got)
		}
	})
	t.Run("R-STOR-110 a read no copy serves is counted as read_unavailable on the last connection it tried, with its bytes", func(t *testing.T) {
		s := newTwoStores(t, 2)
		sp, _ := s.commit(t, data)
		_ = s.one.Delete(context.Background(), blob.Key("quader", sp.SHA256))
		_ = s.two.Delete(context.Background(), blob.Key("quader", sp.SHA256))
		if err := s.read(sp, nil); !errors.Is(err, blob.ErrNotFound) {
			t.Fatalf("the read ended with %v", err)
		}
		if got := s.sink.counted(); !slices.Contains(got, "two read_unavailable no_copy 1024") {
			t.Fatalf("counted %v", got)
		}
	})
	t.Run("R-STOR-110 a write the next connection takes is counted as write_fallover with the failure's code", func(t *testing.T) {
		s := newTwoStores(t, 1)
		s.one.SetFault(func(op, _ string) error {
			if op == "put" {
				return transient
			}
			return nil
		})
		if _, err := s.commit(t, data); err != nil {
			t.Fatal(err)
		}
		if got := s.sink.counted(); !slices.Contains(got, "one write_fallover http_5xx 0") {
			t.Fatalf("counted %v", got)
		}
	})
	t.Run("R-STOR-110 an upload with fewer than min_copies copies is counted as upload_refused on the last connection that failed", func(t *testing.T) {
		s := newTwoStores(t, 2)
		s.two.SetFault(func(op, _ string) error {
			if op == "put" {
				return transient
			}
			return nil
		})
		if _, err := s.commit(t, data); err == nil {
			t.Fatal("the upload was taken with one copy")
		}
		if got := s.sink.counted(); !slices.Contains(got, "two upload_refused http_5xx 1024") {
			t.Fatalf("counted %v", got)
		}
	})
}

func TestR_STOR_109_FailureCodesAndSteps(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{&blob.StoreError{Status: 503, Kind: blob.KindTransient}, blob.CodeHTTP5xx},
		{&blob.StoreError{Code: "SlowDown", Status: 503, Kind: blob.KindTransient}, blob.CodeThrottled},
		{&blob.StoreError{Code: "InvalidAccessKeyId", Status: 403, Kind: blob.KindConfig}, blob.CodeCredentialRejected},
		{&blob.StoreError{Code: "AccessDenied", Status: 403, Kind: blob.KindConfig}, blob.CodeAccessDenied},
		{&blob.StoreError{Code: "NoSuchBucket", Status: 404, Kind: blob.KindConfig}, blob.CodeNoSuchBucket},
		{&blob.StoreError{Kind: blob.KindTransient, Err: &net.DNSError{Err: "no such host", Name: "s3.invalid"}}, blob.CodeDNS},
		{&blob.StoreError{Kind: blob.KindTransient, Err: &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}}, blob.CodeRefused},
		{&blob.StoreError{Kind: blob.KindTransient, Err: context.DeadlineExceeded}, blob.CodeTimeout},
		{blob.ErrCircuitOpen, blob.CodeCircuitOpen},
		{fmt.Errorf("a marker of another installation"), blob.CodeOther},
	} {
		if got := blob.FailureCode(c.err); got != c.want {
			t.Errorf("R-STOR-109 FailureCode(%v) = %q, want %q", c.err, got, c.want)
		}
	}
	t.Run("R-STOR-109 a start check that fails is told with its step and code, and one that passes is told nil", func(t *testing.T) {
		p := "quader"
		m := blobtest.New()
		m.SetFault(func(op, _ string) error {
			if op == "list" {
				return &blob.StoreError{Op: "ListObjectsV2", Code: "AccessDenied", Status: 403, Kind: blob.KindConfig}
			}
			return nil
		})
		conns, err := blob.NewConnections(blob.NewConnection(config.StorageConnection{ID: "default", Bucket: "a", Prefix: &p}.WithDefaults(), m))
		if err != nil {
			t.Fatal(err)
		}
		var told []string
		o := blob.StartCheckOptions{Replica: "unit", Checked: func(conn string, err error) {
			told = append(told, conn+" "+blob.CheckStep(err)+" "+blob.FailureCode(err))
		}}
		if err := blob.AwaitConnections(context.Background(), conns, o); err == nil {
			t.Fatal("the start check passed")
		}
		m.SetFault(nil)
		if err := blob.AwaitConnections(context.Background(), conns, o); err != nil {
			t.Fatal(err)
		}
		if want := []string{"default list access_denied", "default  "}; !slices.Equal(told, want) {
			t.Fatalf("told %q, want %q", told, want)
		}
	})
}

func TestR_STOR_035_TheStoreAppliedEncryption(t *testing.T) {
	p := "quader"
	el := config.StorageConnection{ID: "default", Bucket: "a", Prefix: &p}.WithDefaults()
	for _, c := range []struct {
		name, alg, key string
		want           string
	}{
		{
			"a store that reports aws:kms with its key records both", "aws:kms", "arn:aws:kms:eu-central-1:123456789012:key/k1",
			`{"algorithm":"aws:kms","kmsKeyId":"arn:aws:kms:eu-central-1:123456789012:key/k1"}`,
		},
		{"a store that reports AES256 records it", "AES256", "", `{"algorithm":"AES256"}`},
		{"a store that reports nothing records an empty algorithm", "", "", `{"algorithm":""}`},
	} {
		t.Run("R-STOR-035 "+c.name, func(t *testing.T) {
			m := blobtest.New()
			m.SSE, m.SSEKMSKeyID = c.alg, c.key
			caps, err := blob.CheckConnection(context.Background(), blob.NewConnection(el, m), nil, blob.StartCheckOptions{Replica: "unit"})
			if err != nil {
				t.Fatal(err)
			}
			got, _ := json.Marshal(caps.SSEApplied)
			if string(got) != c.want {
				t.Fatalf("sseApplied %s, want %s", got, c.want)
			}
		})
	}
}
