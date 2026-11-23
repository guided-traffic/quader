package scrub

import (
	"context"
	"errors"
	"sync"

	"github.com/guided-traffic/quader/internal/blob"
)

// ErrNoBlob is the error of a recheck of a digest that no live blob row has.
var ErrNoBlob = errors.New("scrub: no live blob has this digest")

// Recheck reads every copy of one blob to its end at once, unthrottled, and sets the states by what it finds
// (`POST /api/v1/storage/damaged-blobs/{sha256}/recheck`, R-STOR-027, Q-STOR-267): a present copy that matches is stamped
// verified, one that does not is marked missing or corrupt with its repair queued; a copy marked missing or corrupt that now
// reads whole is present and verified again, and one that still does not keeps its mark. A copy on a connection this replica
// does not configure, whose circuit is open, that collection claims or whose read gets no answer is left as it is. It writes no
// object: the repairs are the scrub's and the reconciliation's. It returns the copies after the reads.
func (s *Scrubber) Recheck(ctx context.Context, d blob.Digest) ([]CopyRow, error) {
	copies, found, err := s.o.Ledger.Copies(ctx, d)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNoBlob
	}
	var wg sync.WaitGroup
	for _, cp := range copies {
		c, ok := s.o.Connections.Get(cp.Connection)
		if !ok || cp.Retired || cp.State == blob.CopyDeleting {
			continue
		}
		if _, rejecting := c.Rejecting(); rejecting {
			continue
		}
		wg.Add(1)
		go func(c *blob.Connection, cp CopyRow) {
			defer wg.Done()
			s.recheckCopy(ctx, c, d, cp)
		}(c, cp)
	}
	wg.Wait()
	copies, _, err = s.o.Ledger.Copies(ctx, d)
	return copies, err
}

func (s *Scrubber) recheckCopy(ctx context.Context, c *blob.Connection, d blob.Digest, cp CopyRow) {
	out, n, err := readWhole(ctx, c, cp.Copy, d, nil)
	s.o.Metrics.read(c.ID(), n)
	switch {
	case out == outcomeError || out == outcomeCancelled || out == outcomeUnknownKey:
		// an unknown storage key is no evidence about the object (Q-STOR-311): the copy keeps its state
		s.o.Log.Warn("the recheck could not read a copy; it is left as it is", "connection", c.ID(), "sha256", d.Hex(), "error", err)
	case cp.State == blob.CopyPresent && out == outcomeOK:
		err = s.o.Ledger.Verified(ctx, d, c.ID(), cp.CreatedUs)
	case cp.State == blob.CopyPresent && out == outcomeCorrupt:
		s.o.Ledger.Damaged(ctx, d, cp.Copy, blob.CopyCorrupt)
	case cp.State == blob.CopyPresent && out == outcomeMissing:
		s.o.Ledger.Damaged(ctx, d, cp.Copy, blob.CopyMissing)
	case out == outcomeOK:
		_, err = s.o.Ledger.Whole(ctx, d, c.ID(), cp.State, cp.StateChangedUs, cp.StoredSize, "")
	}
	if err != nil && out == outcomeOK {
		s.o.Log.Warn("the recheck could not record a whole copy", "connection", c.ID(), "sha256", d.Hex(), "error", err)
	}
}
