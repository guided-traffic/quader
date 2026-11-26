package history

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobtest"
	"github.com/guided-traffic/quader/internal/config"
)

// The recorder of the history over a fake store and a fake clock (storage.md R-STOR-109, R-STOR-110): unit, no data. The statements
// against PostgreSQL, the triggers and the task are test/integration/blob/history_test.go.

type fakeEvent struct {
	Event
	id          int64
	seen, ended time.Time
	occurrences int
	code        string
}

type fakeStore struct {
	mu     sync.Mutex
	counts []Count
	events []*fakeEvent
	opens  int
}

func (s *fakeStore) AddCounts(_ context.Context, counts []Count) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts = append(s.counts, counts...)
	return nil
}

func (s *fakeStore) OpenEvent(_ context.Context, _ string, e Event, _ int) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opens++
	s.events = append(s.events, &fakeEvent{Event: e, id: int64(len(s.events) + 1), seen: e.Started, occurrences: 1, code: e.Code})
	return int64(len(s.events)), false, nil
}

func (s *fakeStore) UpdateEvent(_ context.Context, id int64, seen time.Time, occurrences int, code string, ended time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.events[id-1]
	e.seen, e.occurrences, e.code, e.ended = seen, occurrences, code, ended
	return nil
}

func (s *fakeStore) EndReplicaEvents(context.Context, string) error { return nil }

func (s *fakeStore) event(t *testing.T, i int) fakeEvent {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if i >= len(s.events) {
		t.Fatalf("%d intervals recorded, want at least %d", len(s.events), i+1)
	}
	return *s.events[i]
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }

func (c *clock) add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func TestR_STOR_109_AnOutageIsOneInterval(t *testing.T) {
	ctx := context.Background()
	t.Run("R-STOR-109 a circuit that opens, flaps for 40 s and stays closed for 60 s is one interval with its occurrences and its code", func(t *testing.T) {
		c := &clock{t: time.Date(2026, 11, 26, 21, 0, 0, 0, time.UTC)}
		st := &fakeStore{}
		rec := New(Options{Store: st, Replica: "web-0", Now: c.now})
		mem := blobtest.New()
		g := blob.NewGuard(mem, config.StorageS3Breaker{FailureThreshold: 5, OpenDuration: 10 * time.Second},
			blob.WithGuardConnection("archive"), blob.WithGuardClock(c.now), blob.WithGuardTransitions(rec.Circuit))
		down := func() {
			mem.SetFault(func(string, string) error {
				return &blob.StoreError{Op: "HeadObject", Status: 503, Kind: blob.KindTransient}
			})
			for range 5 {
				_, _ = g.Head(ctx, "k")
			}
		}
		up := func() {
			mem.SetFault(nil)
			_, _ = g.Head(ctx, "k") // the probe after the open duration closes the circuit
		}
		start := c.now()
		down()
		if err := rec.Flush(ctx); err != nil {
			t.Fatal(err)
		}
		for range 2 { // open 10 s, probe, closed 10 s, open again: within the hysteresis
			c.add(10 * time.Second)
			up()
			c.add(10 * time.Second)
			if err := rec.Flush(ctx); err != nil {
				t.Fatal(err)
			}
			down()
		}
		c.add(10 * time.Second)
		up()
		lastUp := c.now()
		c.add(30 * time.Second)
		if err := rec.Flush(ctx); err != nil {
			t.Fatal(err)
		}
		if e := st.event(t, 0); !e.ended.IsZero() {
			t.Fatalf("the interval ended %v after 30 s of reachability, within the hysteresis", e.ended.Sub(start))
		}
		c.add(30 * time.Second)
		if err := rec.Flush(ctx); err != nil {
			t.Fatal(err)
		}
		e := st.event(t, 0)
		if st.opens != 1 || e.Kind != KindUnreachable || e.Connection != "archive" || e.code != blob.CodeHTTP5xx || e.occurrences != 3 ||
			!e.Started.Equal(start) || !e.ended.Equal(lastUp) {
			t.Fatalf("opens %d, interval %+v (started %v, want %v; ended %v, want %v)", st.opens, e, e.Started, start, e.ended, lastUp)
		}
	})
	t.Run("R-STOR-109 a start check that fails is an interval with its step and code, ended when it passes", func(t *testing.T) {
		c := &clock{t: time.Date(2026, 11, 26, 21, 0, 0, 0, time.UTC)}
		st := &fakeStore{}
		rec := New(Options{Store: st, Replica: "web-0", Now: c.now})
		p := "quader"
		mem := blobtest.New()
		mem.SetFault(func(op, _ string) error {
			if op == "list" {
				return &blob.StoreError{Op: "ListObjectsV2", Status: 503, Kind: blob.KindTransient}
			}
			return nil
		})
		conn := blob.NewConnection(config.StorageConnection{ID: "archive", Bucket: "a", Prefix: &p}.WithDefaults(), mem)
		_, err := blob.CheckConnection(ctx, conn, nil, blob.StartCheckOptions{Replica: "web-0"})
		rec.Checked("archive", err)
		c.add(30 * time.Second)
		mem.SetFault(nil)
		_, err = blob.CheckConnection(ctx, conn, nil, blob.StartCheckOptions{Replica: "web-0"})
		rec.Checked("archive", err)
		passed := c.now()
		c.add(Hysteresis)
		if err := rec.Flush(ctx); err != nil {
			t.Fatal(err)
		}
		e := st.event(t, 0)
		if e.Kind != KindStartCheckFailed || e.Step != blob.StepList || e.code != blob.CodeHTTP5xx || !e.ended.Equal(passed) {
			t.Fatalf("interval %+v, ended %v, want %v", e, e.ended, passed)
		}
	})
	t.Run("R-STOR-109 a replica that stops ends its open intervals now", func(t *testing.T) {
		c := &clock{t: time.Date(2026, 11, 26, 21, 0, 0, 0, time.UTC)}
		st := &fakeStore{}
		rec := New(Options{Store: st, Replica: "web-0", Now: c.now})
		if err := rec.Start(ctx); err != nil {
			t.Fatal(err)
		}
		rec.Circuit("archive", true, &blob.StoreError{Status: 0, Kind: blob.KindTransient, Err: context.DeadlineExceeded})
		c.add(5 * time.Minute)
		if err := rec.Stop(ctx); err != nil {
			t.Fatal(err)
		}
		if e := st.event(t, 0); e.code != blob.CodeTimeout || !e.ended.Equal(c.now()) {
			t.Fatalf("interval %+v, ended %v, want %v", e, e.ended, c.now())
		}
	})
}

func TestR_STOR_110_CountsAreFlushedPerHour(t *testing.T) {
	ctx := context.Background()
	c := &clock{t: time.Date(2026, 11, 26, 21, 59, 0, 0, time.UTC)}
	st := &fakeStore{}
	rec := New(Options{Store: st, Replica: "web-0", Now: c.now})
	rec.Count("default", blob.HistoryReadFallOver, "not_found", 0)
	rec.Count("default", blob.HistoryReadFallOver, "not_found", 0)
	rec.Count("archive", blob.HistoryEncryptedReadRefused, "unknown_key", 0)
	c.add(2 * time.Minute) // the next hour
	rec.Count("default", blob.HistoryReadFallOver, "not_found", 0)
	rec.Count("default", blob.HistoryUploadRefused, "http_5xx", 2048)
	if err := rec.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, x := range st.counts {
		got = append(got, fmt.Sprintf("%s %s %s %s %d %d", x.Hour.Format("15:04"), x.Connection, x.Kind, x.Reason, x.N, x.Bytes))
	}
	slices.Sort(got)
	want := []string{
		"21:00 archive encrypted_read_refused unknown_key 1 0",
		"21:00 default read_fallover not_found 2 0",
		"22:00 default read_fallover not_found 1 0",
		"22:00 default upload_refused http_5xx 1 2048",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("flushed %q, want %q", got, want)
	}
	if err := rec.Flush(ctx); err != nil || len(st.counts) != 4 {
		t.Fatalf("a second flush with nothing new wrote %d rows in all (%v)", len(st.counts), err)
	}
}
