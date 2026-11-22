package scrub

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
)

// Clock is the scrub's time: the throttle sleeps on it and a run's duration is measured with it; the unit tests advance a
// fake one.
type Clock interface {
	Now() time.Time
	// Sleep waits d or until ctx ends.
	Sleep(ctx context.Context, d time.Duration) error
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// throttle keeps the bytes one connection's reader moves in a run at its scrub_bytes_per_second (R-STOR-027): after each read
// it sleeps until the bytes so far would have taken that long at the rate. A nil throttle, or a rate of 0, does not wait (the
// recheck of one blob reads unthrottled).
type throttle struct {
	clock Clock
	rate  int64
	start time.Time
	moved int64
}

func newThrottle(c Clock, rate int64) *throttle {
	return &throttle{clock: c, rate: rate, start: c.Now()}
}

func (t *throttle) wait(ctx context.Context, n int) error {
	if t == nil || t.rate <= 0 || n <= 0 {
		return nil
	}
	t.moved += int64(n)
	due := t.start.Add(time.Duration(float64(t.moved) / float64(t.rate) * float64(time.Second)))
	if d := due.Sub(t.clock.Now()); d > 0 {
		return t.clock.Sleep(ctx, d)
	}
	return nil
}

// The outcomes of a whole read.
type outcome int

const (
	outcomeOK        outcome = iota // every byte read, the length is the stored size and the digest the blob's
	outcomeCorrupt                  // another length or another digest: a read to the end is evidence (R-STOR-052)
	outcomeMissing                  // the store answered "no such object"
	outcomeError                    // a timeout, a 5xx, throttling, an open circuit or a cut transfer: no evidence
	outcomeCancelled                // the run ended (its duration, a drain, a cancellation) during the read
)

// readBuffer is the chunk a whole read hashes and throttles at a time.
const readBuffer = 64 << 10

// readWhole reads a copy to its end through its connection's reader (R-STOR-062) and judges it by its bytes alone: their number
// against the copy's stored size and their SHA-256 against the blob's digest (R-STOR-027). Nothing the store says about the
// object (a Content-Length, an ETag, a checksum header) is taken as evidence: one store has no upload checksum of its own, and
// the bytes are what a client would get. At most stored size + 1 bytes are read, so a longer object is found without reading
// it all. It returns the outcome, the bytes read and, for outcomeError, the cause.
func readWhole(ctx context.Context, c *blob.Connection, cp blob.Copy, d blob.Digest, th *throttle) (outcome, int64, error) {
	body, _, err := c.Read(ctx, cp, nil)
	switch {
	case errors.Is(err, blob.ErrNotFound):
		return outcomeMissing, 0, nil
	case err != nil && ctx.Err() != nil:
		return outcomeCancelled, 0, ctx.Err()
	case err != nil:
		return outcomeError, 0, err
	}
	defer func() { _ = body.Close() }()
	h := sha256.New()
	r := io.LimitReader(body, cp.StoredSize+1)
	buf := make([]byte, readBuffer)
	var n int64
	for {
		k, rerr := r.Read(buf)
		if k > 0 {
			h.Write(buf[:k])
			n += int64(k)
			if werr := th.wait(ctx, k); werr != nil {
				return outcomeCancelled, n, werr
			}
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			if ctx.Err() != nil {
				return outcomeCancelled, n, ctx.Err()
			}
			return outcomeError, n, rerr
		}
	}
	if n != cp.StoredSize || blob.Digest(h.Sum(nil)) != d {
		return outcomeCorrupt, n, nil
	}
	return outcomeOK, n, nil
}
