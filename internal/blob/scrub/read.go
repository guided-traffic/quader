package scrub

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
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
	outcomeOK         outcome = iota // every byte read, the length is the blob's and the digest the blob's
	outcomeCorrupt                   // another length or another digest, or a refusal of the stored format that marks: evidence (R-STOR-052, R-STOR-082)
	outcomeMissing                   // the store answered "no such object"
	outcomeError                     // a timeout, a 5xx, throttling, an open circuit or a cut transfer: no evidence
	outcomeCancelled                 // the run ended (its duration, a drain, a cancellation) during the read
	outcomeUnknownKey                // the record names a storage key this replica does not hold: no evidence, the copy stays due (Q-STOR-311)
)

// readBuffer is the chunk a whole read hashes and throttles at a time.
const readBuffer = 64 << 10

// plainSize is the size of the blob a copy on c holds: its stored size on a plain connection, the plaintext size S(L) stands for on an
// encrypted one (R-STOR-076); a stored size no plaintext size produces is a refusal of reason size.
func plainSize(c *blob.Connection, cp blob.Copy) (int64, error) {
	if !c.Encrypted() {
		return cp.StoredSize, nil
	}
	return blobcrypt.PlainSize(cp.StoredSize)
}

// judge is the outcome of a refusal of the connection's reader or a failed read of its body: no such object is missing, a reason of
// the stored format is evidence as R-STOR-082 says (every reason but unknown_key marks, R-STOR-087), anything else is no evidence.
func judge(ctx context.Context, err error) outcome {
	if reason, ok := blobcrypt.ReasonOf(err); ok {
		if reason.MarksCorrupt() {
			return outcomeCorrupt
		}
		return outcomeUnknownKey
	}
	switch {
	case errors.Is(err, blob.ErrNotFound):
		return outcomeMissing
	case ctx.Err() != nil:
		return outcomeCancelled
	}
	return outcomeError
}

// readWhole reads a copy to its end through its connection's reader (R-STOR-062) and judges it by its bytes alone: their number
// against the blob's size and their SHA-256 against the blob's digest (R-STOR-027). On an encrypted connection the reader opens the
// record bound to the digest and authenticates every segment, the final segment and the digest (steps 1 to 7 of R-STOR-077), and a
// refusal is judged by its reason. Nothing the store says about the object (a Content-Length, an ETag, a checksum header) is taken as
// evidence: one store has no upload checksum of its own, and the bytes are what a client would get. At most the blob's size + 1 bytes
// are read, so a longer object is found without reading it all. It returns the outcome, the bytes read and, for outcomeError and
// outcomeUnknownKey, the cause.
func readWhole(ctx context.Context, c *blob.Connection, cp blob.Copy, d blob.Digest, th *throttle) (outcome, int64, error) {
	size, err := plainSize(c, cp)
	if err != nil {
		return outcomeCorrupt, 0, err
	}
	body, _, err := c.ReadBlob(ctx, d, size, cp, nil, true)
	if err != nil {
		out := judge(ctx, err)
		if out == outcomeMissing || out == outcomeCorrupt {
			err = nil
		}
		return out, 0, err
	}
	defer func() { _ = body.Close() }()
	h := sha256.New()
	r := io.LimitReader(body, size+1)
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
			switch out := judge(ctx, rerr); out {
			case outcomeCorrupt:
				return out, n, nil
			case outcomeCancelled:
				return out, n, ctx.Err()
			case outcomeMissing:
				return outcomeError, n, rerr // the object went while its body was read: no evidence of its own
			default:
				return out, n, rerr
			}
		}
	}
	if n != size || blob.Digest(h.Sum(nil)) != d {
		return outcomeCorrupt, n, nil
	}
	return outcomeOK, n, nil
}
