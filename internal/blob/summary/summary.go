// Package summary is the storage summary (storage.md R-STOR-028, operations-and-observability.md R-OPS-023) and the read of the
// object-store connections for the API (storage.md R-STOR-061): the task storage-summary computes the figures from PostgreSQL once an
// hour and keeps the latest result in the one row of storage_summary; GET /api/v1/storage/summary, the connections' list and the
// gauges of the summary read that row and never compute. Package F of the build plan of several connections
// (docs/developer/blob-store.md).
//
// The package is infrastructure like the other packages of the blob store: it runs sqlc statements and imports no domain package.
package summary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/guided-traffic/quader/internal/blob/scrub"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// Task is the task and the kind of the storage summary (R-OPS-023).
const Task = "storage-summary"

// DefaultSchedule is the fallback of maintenance.tasks.storage-summary, whose default the configuration has as well: hourly.
const DefaultSchedule = "17 * * * *"

// DefaultScrubPeriod is the period a connection that the computing worker does not configure is judged by (the default of
// scrub_period, R-STOR-050).
const DefaultScrubPeriod = 720 * time.Hour

// Summary is the storage summary (R-STOR-028): the envelope of R-OPS-023 (ComputedAt, DurationMs) and the figures. Sizes are bytes and
// ratios numbers from 0 to 1, rounded to four decimals.
type Summary struct {
	ComputedAt time.Time `json:"computedAt"`
	DurationMs int64     `json:"durationMs"`
	// Blobs are the live blob rows: the physical content, with what only the trash can references and what awaits collection.
	Blobs Blobs `json:"blobs"`
	// Logical is the untrashed files and folders of every listed repository; one blob in two repositories counts twice.
	Logical Logical `json:"logical"`
	// DeduplicationRatio is Blobs.Bytes / Logical.Bytes; 0 while Logical.Bytes is 0.
	DeduplicationRatio float64 `json:"deduplicationRatio"`
	// Trash is the trashed items and their logical bytes (R-STOR-022).
	Trash Trash `json:"trash"`
	// PendingCollection is the digests of blob_gc_queue whose blob is live and the rows in state deleting, with their sizes.
	PendingCollection Pending `json:"pendingCollection"`
	// Damaged counts the live blobs per content state other than ok (R-STOR-027): blobs, not copies.
	Damaged Damaged `json:"damaged"`
	// Connections are the figures per connection that is not retired, by id (R-STOR-061).
	Connections []Connection `json:"connections"`
	// Repositories are the local and remote repositories and the virtual ones that hold generated items (Q-STOR-215), by project and
	// key.
	Repositories []Repository `json:"repositories"`
}

// Blobs are the live blobs and their bytes.
type Blobs struct {
	Count int64 `json:"count"`
	Bytes int64 `json:"bytes"`
}

// Logical is the untrashed content of the repositories.
type Logical struct {
	Files   int64 `json:"files"`
	Folders int64 `json:"folders"`
	Bytes   int64 `json:"bytes"`
}

// Trash is the trashed items.
type Trash struct {
	Items int64 `json:"items"`
	Bytes int64 `json:"bytes"`
}

// Pending is what awaits collection.
type Pending struct {
	Blobs int64 `json:"blobs"`
	Bytes int64 `json:"bytes"`
}

// Damaged counts the damaged blobs.
type Damaged struct {
	Missing int64 `json:"missing"`
	Corrupt int64 `json:"corrupt"`
}

// Connection is the figures of one connection (R-STOR-028, R-STOR-061), counted over the copies of live blobs.
type Connection struct {
	ID string `json:"id"`
	// PresentCopies and PresentBytes are the present copies and their stored bytes.
	PresentCopies int64 `json:"presentCopies"`
	PresentBytes  int64 `json:"presentBytes"`
	MissingCopies int64 `json:"missingCopies"`
	CorruptCopies int64 `json:"corruptCopies"`
	// VerifiedWithinPeriod counts the present copies read whole within the connection's scrub_period; ScrubOverdue those not read
	// whole within the period and a day (Q-STOR-266).
	VerifiedWithinPeriod int64 `json:"verifiedWithinPeriod"`
	ScrubOverdue         int64 `json:"scrubOverdue"`
	// BacklogBlobs and BacklogBytes are the live blobs and their bytes without a present copy on the connection.
	BacklogBlobs int64 `json:"backlogBlobs"`
	BacklogBytes int64 `json:"backlogBytes"`
	// FillRatio is PresentCopies / Blobs.Count, 1 while there is no blob.
	FillRatio float64 `json:"fillRatio"`
	// SoleCopies counts the live blobs whose only present copy lies on this connection (Q-STOR-371): what a detached connection's
	// removal would cost.
	SoleCopies int64 `json:"soleCopies"`
	// KeyCopies counts the copies per storage key that wrapped them (R-STOR-088); empty on a plain connection.
	KeyCopies map[string]int64 `json:"keyCopies,omitempty"`
}

// Repository is one repository's figures.
type Repository struct {
	Project string `json:"project"`
	Key     string `json:"key"`
	Class   string `json:"class"`
	Format  string `json:"format"`
	Files   int64  `json:"files"`
	Folders int64  `json:"folders"`
	Bytes   int64  `json:"bytes"`
	// Packages and Versions count the untrashed rows.
	Packages int64 `json:"packages"`
	Versions int64 `json:"versions"`
	// ShareOfLogicalBytes is Bytes / Logical.Bytes, never a share of capacity.
	ShareOfLogicalBytes float64 `json:"shareOfLogicalBytes"`
}

// Options are what a computation needs besides the database.
type Options struct {
	// Periods is the scrub_period of each connection of the computing worker's configuration (R-STOR-050); a connection it does not
	// configure is judged by DefaultScrubPeriod.
	Periods map[string]time.Duration
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

// ErrNone is returned by Latest before the first computation.
var ErrNone = errors.New("no storage summary has been computed yet")

// Input is the results of the statements of one computation, the input of Assemble.
type Input struct {
	Totals       q.StorageSummaryTotalsRow
	Repositories []q.StorageSummaryRepositoriesRow
	Connections  []q.StorageSummaryConnectionsRow
	KeyCopies    []q.StorageSummaryKeyCopiesRow
}

// Compute runs the statements of the summary one after the other on one connection, outside a transaction (READ COMMITTED, R-ARCH-019:
// no snapshot is held across the statements), and assembles the result. The copies of the connections are counted first and the live
// blobs after, so that an upload between the two can only show as a gap the reconciliation fills anyway; Assemble bounds the backlog and
// the fill (Q-STOR-372). It writes nothing.
func Compute(ctx context.Context, db *postgres.DB, o Options) (Summary, error) {
	now := o.Now
	if now == nil {
		now = time.Now
	}
	start := now()
	ids := make([]string, 0, len(o.Periods))
	for id := range o.Periods {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	periods := make([]float64, len(ids))
	for i, id := range ids {
		periods[i] = o.Periods[id].Seconds()
	}
	var rows Input
	err := db.Read(ctx, func(c q.DBTX) error {
		qs := q.New(c)
		var err error
		rows.Connections, err = qs.StorageSummaryConnections(ctx, q.StorageSummaryConnectionsParams{
			Ids: ids, PeriodSeconds: periods, DefaultPeriodSeconds: DefaultScrubPeriod.Seconds(), GraceSeconds: scrub.OverdueGrace.Seconds(),
		})
		if err != nil {
			return fmt.Errorf("connections: %w", err)
		}
		if rows.KeyCopies, err = qs.StorageSummaryKeyCopies(ctx); err != nil {
			return fmt.Errorf("storage keys: %w", err)
		}
		if rows.Totals, err = qs.StorageSummaryTotals(ctx); err != nil {
			return fmt.Errorf("totals: %w", err)
		}
		if rows.Repositories, err = qs.StorageSummaryRepositories(ctx); err != nil {
			return fmt.Errorf("repositories: %w", err)
		}
		return nil
	})
	if err != nil {
		return Summary{}, fmt.Errorf("storage summary: %w", err)
	}
	s := Assemble(rows)
	end := now()
	s.ComputedAt, s.DurationMs = end.UTC(), max(end.Sub(start).Milliseconds(), 0)
	return s, nil
}

// Assemble derives the summary's figures from the rows of one computation (R-STOR-028): the logical totals over the listed
// repositories, the ratios, the backlog and the fill of every connection. A virtual repository without items is left out (Q-STOR-215).
// The live blobs are the folded counter and its unfolded deltas, counted after the copies (Compute): a backlog below 0 or a fill
// above 1 would need a blob deleted between the two statements, and both are bounded.
func Assemble(r Input) Summary {
	s := Summary{
		Blobs:             Blobs{Count: r.Totals.Blobs, Bytes: r.Totals.BlobBytes},
		Trash:             Trash{Items: r.Totals.TrashItems, Bytes: r.Totals.TrashBytes},
		PendingCollection: Pending{Blobs: r.Totals.PendingBlobs, Bytes: r.Totals.PendingBytes},
		Damaged:           Damaged{Missing: r.Totals.DamagedMissing, Corrupt: r.Totals.DamagedCorrupt},
		Connections:       []Connection{},
		Repositories:      []Repository{},
	}
	for _, rr := range r.Repositories {
		if rr.Class == "virtual" && rr.Files == 0 && rr.Folders == 0 {
			continue
		}
		s.Repositories = append(s.Repositories, Repository{
			Project: rr.Project, Key: rr.Key, Class: rr.Class, Format: rr.Format, Files: rr.Files, Folders: rr.Folders, Bytes: rr.LogicalBytes,
			Packages: rr.Packages, Versions: rr.Versions,
		})
		s.Logical.Files += rr.Files
		s.Logical.Folders += rr.Folders
		s.Logical.Bytes += rr.LogicalBytes
	}
	for i := range s.Repositories {
		s.Repositories[i].ShareOfLogicalBytes = ratio(s.Repositories[i].Bytes, s.Logical.Bytes, 0)
	}
	s.DeduplicationRatio = ratio(s.Blobs.Bytes, s.Logical.Bytes, 0)
	keys := map[string]map[string]int64{}
	for _, k := range r.KeyCopies {
		if keys[k.ConnectionID] == nil {
			keys[k.ConnectionID] = map[string]int64{}
		}
		keys[k.ConnectionID][k.StorageKeyID] = k.Copies
	}
	for _, c := range r.Connections {
		fill := 1.0
		if s.Blobs.Count > 0 {
			fill = math.Min(1, ratio(c.PresentCopies, s.Blobs.Count, 1))
		}
		s.Connections = append(s.Connections, Connection{
			ID: c.ID, PresentCopies: c.PresentCopies, PresentBytes: c.PresentBytes, MissingCopies: c.MissingCopies, CorruptCopies: c.CorruptCopies,
			VerifiedWithinPeriod: c.VerifiedWithinPeriod, ScrubOverdue: c.ScrubOverdue, SoleCopies: c.SoleCopies,
			BacklogBlobs: max(s.Blobs.Count-c.PresentCopies, 0), BacklogBytes: max(s.Blobs.Bytes-c.PresentBlobBytes, 0),
			FillRatio: fill, KeyCopies: keys[c.ID],
		})
	}
	return s
}

// ratio is part / whole rounded to four decimals, or zero when whole is not positive.
func ratio(part, whole int64, zero float64) float64 {
	if whole <= 0 {
		return zero
	}
	return math.Round(float64(part)/float64(whole)*10000) / 10000
}

// Save replaces the latest snapshot with s.
func Save(ctx context.Context, db *postgres.DB, s Summary) error {
	body, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return db.InTx(ctx, func(tx meta.Tx) error {
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			return errors.New("storage summary: no PostgreSQL transaction")
		}
		return q.New(ptx).StorageSummarySave(ctx, q.StorageSummarySaveParams{
			ComputedAt: s.ComputedAt.UTC().Format(time.RFC3339Nano), DurationMs: int32(min(s.DurationMs, math.MaxInt32)), Summary: body, //nolint:gosec // bounded above
		})
	})
}

// Latest reads the latest snapshot; ErrNone before the first computation. Reads never compute (R-OPS-023).
func Latest(ctx context.Context, db *postgres.DB) (Summary, error) {
	var rows []q.StorageSummaryLatestRow
	err := db.Read(ctx, func(c q.DBTX) error {
		var err error
		rows, err = q.New(c).StorageSummaryLatest(ctx)
		return err
	})
	if err != nil {
		return Summary{}, err
	}
	if len(rows) == 0 {
		return Summary{}, ErrNone
	}
	var s Summary
	if err := json.Unmarshal(rows[0].Summary, &s); err != nil {
		return Summary{}, fmt.Errorf("storage summary: the stored snapshot does not decode: %w", err)
	}
	s.ComputedAt, s.DurationMs = time.UnixMicro(rows[0].ComputedUs).UTC(), int64(rows[0].DurationMs)
	if s.Connections == nil {
		s.Connections = []Connection{}
	}
	if s.Repositories == nil {
		s.Repositories = []Repository{}
	}
	return s, nil
}
