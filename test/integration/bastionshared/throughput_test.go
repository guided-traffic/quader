//go:build integration

package bastionshared

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/settings"
	"github.com/guided-traffic/quader/internal/bastion/throughput"
)

// endless is a body of zeros without an end.
type endless struct{}

func (endless) Read(b []byte) (int, error) { clear(b); return len(b), nil }
func (endless) Close() error               { return nil }

// readFor reads a paced body from start for d of real time and returns the bytes it read before the end.
func readFor(ctx context.Context, p *throughput.Pacer, start time.Time, d time.Duration) int64 {
	time.Sleep(time.Until(start))
	body := p.Running(ctx, mainclient.Authenticated).Pace(endless{})
	defer body.Close()
	end := start.Add(d)
	buf := make([]byte, 32<<10)
	var n int64
	for {
		k, err := body.Read(buf)
		if err != nil || !time.Now().Before(end) {
			return n
		}
		n += int64(k)
	}
}

// nextSlice is the beginning of the next slice of the throughput, so that a second read from it is ten whole slices.
func nextSlice() time.Time { return time.Now().Truncate(throughput.Slice).Add(throughput.Slice) }

// TestR_BAS_097_WithValkeyTheReplicasShareOneThroughputCap: with Valkey the slices of the throughput are counters of the
// Bastion's prefix, so a replica alone reads more than the half that would be its share without Valkey, and two replicas
// reading at once stay within the one cap together (R-BAS-097, Q-BAS-332). 1 MB/s, one second each, bytes of zeros.
func TestR_BAS_097_WithValkeyTheReplicasShareOneThroughputCap(t *testing.T) {
	s := newSetup(t)
	a, b := s.pair(t)
	bytesPerSecond := int64(1_000_000)
	c := settings.Builtin()
	c.Revision = 2
	c.Limits.Throughput.BytesPerSecond = &bytesPerSecond
	if _, err := s.figures.Apply(ctx, c, nil); err != nil {
		t.Fatal(err)
	}
	pa := throughput.New(throughput.Options{Bytes: a.st.Bytes, Figures: s.figures.Current, Metrics: a.m})
	pb := throughput.New(throughput.Options{Bytes: b.st.Bytes, Figures: s.figures.Current, Metrics: b.m})

	alone := readFor(ctx, pa, nextSlice(), time.Second)
	if alone <= 600_000 || alone > 1_000_000 {
		t.Fatalf("one replica alone read %d bytes in a second, want more than its local share of 500,000 and at most the cap", alone)
	}
	var wg sync.WaitGroup
	var na, nb int64
	start := nextSlice()
	wg.Add(2)
	go func() { defer wg.Done(); na = readFor(ctx, pa, start, time.Second) }()
	go func() { defer wg.Done(); nb = readFor(ctx, pb, start, time.Second) }()
	wg.Wait()
	t.Logf("alone %d bytes; together %d + %d bytes", alone, na, nb)
	if na+nb > 1_000_000 || na+nb < 700_000 || na == 0 || nb == 0 {
		t.Fatalf("two replicas read %d and %d bytes in the same second, want both to read and together at most the one cap", na, nb)
	}
}
