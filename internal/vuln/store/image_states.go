package store

import (
	"context"
	"encoding/hex"
	"math"
	"time"

	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// ImageScanGroup is a group of scan rows by state, code and whether its call runs now or its detection is outdated (R-VULN-053).
type ImageScanGroup struct {
	// State is pending, scanned or failed; Code is the code the rows carry, "" for none.
	State, Code string
	// Running says that the rows wait in a call of the scanner that was taken within the call's bound (pending only).
	Running bool
	// Outdated says that the rows were detected against another database than the one in use (scanned only).
	Outdated bool
	N        int64
	// Oldest is when the first row of the group was queued; nil for none.
	Oldest *time.Time
}

// ImageScanFailure is the failed scan that failed last, with everything it carried (R-VULN-053).
type ImageScanFailure struct {
	// Repository is the qualified name of its repository; Name the image; Digest the manifest's, "sha256:<hex>".
	Repository, Name, Digest string
	Code, Detail             string
	Attempts                 int
	// At is when it failed; nil for a row that failed before the time was recorded.
	At *time.Time
}

// ScanStates reads the scan rows of the active repositories by group, and the failure of the row that failed last (R-VULN-053, Q-VULN-298).
// dbDigest is the database in use ("" for none): a scanned row detected against another counts as outdated. bound is the call's bound of
// Q-VULN-276; zero reads every taken row as running, as the report does.
func (s *Images) ScanStates(ctx context.Context, dbDigest string, bound time.Duration) (groups []ImageScanGroup, last *ImageScanFailure, err error) {
	secs := bound.Seconds()
	if bound <= 0 {
		secs = math.MaxInt32
	}
	err = s.tx(ctx, func(qs *q.Queries) error {
		rows, err := qs.VulnImageScanStates(ctx, q.VulnImageScanStatesParams{BoundSeconds: secs, DbDigest: dbDigest})
		if err != nil {
			return err
		}
		groups = make([]ImageScanGroup, 0, len(rows))
		for _, r := range rows {
			groups = append(groups, ImageScanGroup{State: r.State, Code: r.Code, Running: r.Running, Outdated: r.Outdated, N: r.N, Oldest: microTimePtr(r.OldestUs)})
		}
		fs, err := qs.VulnImageLastFailure(ctx)
		if err != nil || len(fs) == 0 {
			return err
		}
		f := fs[0]
		last = &ImageScanFailure{
			Repository: f.ProjectKey + "/" + f.RepoKey, Name: f.Name, Digest: "sha256:" + hex.EncodeToString(f.Digest),
			Code: f.Code, Detail: f.Detail, Attempts: int(f.Attempts), At: microTimePtr(f.FailedUs),
		}
		return nil
	})
	return groups, last, err
}
