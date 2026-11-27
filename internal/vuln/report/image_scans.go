package report

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/guided-traffic/quader/internal/vuln/store"
)

// ImageScans is what the image scanning does now, for its page of the admin area (R-VULN-053, Q-VULN-298): how many manifests wait, are
// scanned now, are scanned and failed, why the waiting and the failed ones are where they are, the oldest that waits and the failure that
// came last.
type ImageScans struct {
	// Waiting rows have no detection under the database in use yet and no call runs for them; Scanning rows are in a call now.
	Waiting, Scanning int64
	// Scanned rows have a detection; Outdated is how many of them were detected against another database than the one in use, which
	// are detected again in turn and keep their findings meanwhile.
	Scanned, Outdated int64
	Failed            int64
	// Reasons are the codes of the waiting rows (why a row was put off) and of the failed rows, each with its count, the most first.
	Reasons []ScanReason
	// OldestWaiting is when the waiting row that waits longest was queued; nil when none waits.
	OldestWaiting *time.Time
	// LastFailure is the failed row that failed last; nil when none failed.
	LastFailure *store.ImageScanFailure
}

// ScanReason is a code of waiting or failed rows with their count.
type ScanReason struct {
	// State is waiting or failed; Code the rows' code, "" for waiting rows that were never put off.
	State, Code string
	N           int64
}

// The states of a reason.
const (
	ReasonWaiting = "waiting"
	ReasonFailed  = "failed"
)

// ImageScans reads the image scanning's state in two statements (VulnImageScanStates, VulnImageLastFailure), whatever the number of rows
// it groups. It answers nothing per row but the last failure.
func (s *Service) ImageScans(ctx context.Context) (ImageScans, error) {
	var out ImageScans
	if s.images == nil {
		return out, nil
	}
	db, _, err := s.images.DB(ctx)
	if err != nil {
		return out, err
	}
	groups, last, err := s.images.ScanStates(ctx, db.Digest, s.cfg.ScanCallBound)
	if err != nil {
		return out, err
	}
	out.LastFailure = last
	reasons := map[ScanReason]int64{}
	for _, g := range groups {
		switch {
		case g.State == store.ImageScanScanned:
			out.Scanned += g.N
			if g.Outdated {
				out.Outdated += g.N
			}
		case g.State == store.ImageScanFailed:
			out.Failed += g.N
			reasons[ScanReason{State: ReasonFailed, Code: g.Code}] += g.N
		case g.Running:
			out.Scanning += g.N
		default:
			out.Waiting += g.N
			reasons[ScanReason{State: ReasonWaiting, Code: g.Code}] += g.N
			if g.Oldest != nil && (out.OldestWaiting == nil || g.Oldest.Before(*out.OldestWaiting)) {
				out.OldestWaiting = g.Oldest
			}
		}
	}
	for r, n := range reasons {
		r.N = n
		out.Reasons = append(out.Reasons, r)
	}
	slices.SortFunc(out.Reasons, func(a, b ScanReason) int {
		return cmp.Or(cmp.Compare(a.State, b.State), cmp.Compare(b.N, a.N), cmp.Compare(a.Code, b.Code))
	})
	return out, nil
}
