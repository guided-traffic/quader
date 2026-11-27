package history

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The reads of the history for the storage page (storage.md R-STOR-111, queries/storage_history_read.sql). The records are folded by
// PostgreSQL where the page needs no single row (the counts and the findings per cell and per UTC day); the intervals are read as rows,
// bounded by the cap per day (Q-STOR-516), since merging them across replicas needs each. A finding belongs to a period while it was
// unresolved at the period's start or later (Q-STOR-525).

// Interval is one row of storage_events that overlaps a period; Ended is zero while it lasts.
type Interval struct {
	Connection, Kind, Replica string
	Started, Ended            time.Time
	Code, Step                string
	Occurrences               int64
}

// DayFindings is the findings of one connection detected on one UTC day.
type DayFindings struct {
	Connection string
	Day        time.Time
	N, Bytes   int64
	// First is the first detection; LastResolved the latest resolution, zero when none is resolved.
	First, LastResolved                                        time.Time
	Resolved, Open, Acknowledged, Unavailable, UnavailableOpen int64
	Sources, Reasons                                           []string
}

// DayCounts is one connection's counts of one kind on one UTC day: the first and the last hour that counted one, and the reasons.
type DayCounts struct {
	Connection, Kind string
	Day              time.Time
	N, Bytes         int64
	FirstHour        time.Time
	LastHour         time.Time
	Reasons          []string
}

// ProblemRecords is what the problems of a period are derived from (DeriveProblems).
type ProblemRecords struct {
	Intervals []Interval
	Findings  []DayFindings
	Counts    []DayCounts
}

// CellCount is one connection's counts of one kind in one cell; UnknownKey marks the refusals of the stored format whose key is unknown.
type CellCount struct {
	Connection, Kind string
	UnknownKey       bool
	Cell             int
	N, Bytes         int64
}

// CellFindings is one connection's findings detected in one cell.
type CellFindings struct {
	Connection       string
	Cell             int
	N, Bytes         int64
	UnavailableBlobs int64
}

// FindingTotal is one connection's findings of the period of one source and outcome (open, acknowledged, or the resolution).
type FindingTotal struct {
	Connection, Source, Outcome string
	N, Bytes                    int64
}

// CellRecords is what the cells of a period are derived from (DeriveCells), the intervals included.
type CellRecords struct {
	Intervals []Interval
	Counts    []CellCount
	Findings  []CellFindings
	Totals    []FindingTotal
}

// at is a time of seconds since the epoch, zero for 0.
func at(unixSeconds float64) time.Time {
	if unixSeconds == 0 {
		return time.Time{}
	}
	sec, frac := math.Modf(unixSeconds)
	return time.Unix(int64(sec), int64(frac*1e9)).UTC()
}

// micro is a time of microseconds since the epoch, zero for 0.
func micro(us int64) time.Time {
	if us == 0 {
		return time.Time{}
	}
	return time.UnixMicro(us).UTC()
}

func dayOf(n int64) time.Time { return time.Unix(n*86400, 0).UTC() }

// readIntervals reads the intervals that overlap [from, to).
func readIntervals(ctx context.Context, qs *q.Queries, from, to time.Time) ([]Interval, error) {
	rows, err := qs.HistoryReadEvents(ctx, q.HistoryReadEventsParams{FromUnix: unix(from), ToUnix: unix(to)})
	if err != nil {
		return nil, err
	}
	out := make([]Interval, 0, len(rows))
	for _, r := range rows {
		out = append(out, Interval{
			Connection: r.ConnectionID, Kind: r.Kind, Replica: r.Replica, Started: at(r.StartedUnix), Ended: at(r.EndedUnix),
			Code: r.Code, Step: r.Step, Occurrences: r.Occurrences,
		})
	}
	return out, nil
}

// ReadProblems reads the records the problems of [from, to) are derived from: the intervals, the findings per connection and day, and
// the refused uploads, the reads no copy served and the findings beyond the cap per connection, kind and day.
func ReadProblems(ctx context.Context, db *postgres.DB, from, to time.Time) (ProblemRecords, error) {
	var out ProblemRecords
	err := db.Read(ctx, func(c q.DBTX) error {
		qs := q.New(c)
		var err error
		if out.Intervals, err = readIntervals(ctx, qs, from, to); err != nil {
			return err
		}
		days, err := qs.HistoryReadDayFindings(ctx, unix(from))
		if err != nil {
			return err
		}
		out.Findings = make([]DayFindings, 0, len(days))
		for _, r := range days {
			out.Findings = append(out.Findings, DayFindings{
				Connection: r.ConnectionID, Day: dayOf(r.Day), N: r.N, Bytes: r.Bytes, First: at(r.FirstUnix), LastResolved: at(r.LastResolvedUnix),
				Resolved: r.Resolved, Open: r.Open, Acknowledged: r.Acknowledged, Unavailable: r.Unavailable, UnavailableOpen: r.UnavailableOpen,
				Sources: r.Sources, Reasons: r.Reasons,
			})
		}
		counts, err := qs.HistoryReadDayCounts(ctx, q.HistoryReadDayCountsParams{FromUnix: unix(from), ToUnix: unix(to)})
		if err != nil {
			return err
		}
		out.Counts = make([]DayCounts, 0, len(counts))
		for _, r := range counts {
			out.Counts = append(out.Counts, DayCounts{
				Connection: r.ConnectionID, Kind: r.Kind, Day: dayOf(r.Day), N: r.N, Bytes: r.Bytes, FirstHour: at(r.FirstHourUnix),
				LastHour: at(r.LastHourUnix), Reasons: r.Reasons,
			})
		}
		return nil
	})
	return out, err
}

// ReadCells reads the records the cells of period p ending at to are derived from.
func ReadCells(ctx context.Context, db *postgres.DB, p Period, to time.Time) (CellRecords, error) {
	from := to.Add(-p.Span())
	var out CellRecords
	err := db.Read(ctx, func(c q.DBTX) error {
		qs := q.New(c)
		var err error
		if out.Intervals, err = readIntervals(ctx, qs, from, to); err != nil {
			return err
		}
		cell := p.Cell.Seconds()
		counts, err := qs.HistoryReadCellCounts(ctx, q.HistoryReadCellCountsParams{FromUnix: unix(from), ToUnix: unix(to), CellSeconds: cell})
		if err != nil {
			return err
		}
		out.Counts = make([]CellCount, 0, len(counts))
		for _, r := range counts {
			out.Counts = append(out.Counts, CellCount{
				Connection: r.ConnectionID, Kind: r.Kind, UnknownKey: r.UnknownKey, Cell: int(r.Cell), N: r.N, Bytes: r.Bytes,
			})
		}
		findings, err := qs.HistoryReadCellFindings(ctx, q.HistoryReadCellFindingsParams{
			FromUnix: unix(from), ToUnix: unix(to), CellSeconds: cell, Cells: int32(p.Cells), //nolint:gosec // at most 52
		})
		if err != nil {
			return err
		}
		out.Findings = make([]CellFindings, 0, len(findings))
		for _, r := range findings {
			out.Findings = append(out.Findings, CellFindings{
				Connection: r.ConnectionID, Cell: int(r.Cell), N: r.N, Bytes: r.Bytes, UnavailableBlobs: r.UnavailableBlobs,
			})
		}
		totals, err := qs.HistoryReadFindingTotals(ctx, unix(from))
		if err != nil {
			return err
		}
		out.Totals = make([]FindingTotal, 0, len(totals))
		for _, r := range totals {
			out.Totals = append(out.Totals, FindingTotal{Connection: r.ConnectionID, Source: r.Source, Outcome: r.Outcome, N: r.N, Bytes: r.Bytes})
		}
		return nil
	})
	return out, err
}

// The states of a finding for the list (R-STOR-111): open is unresolved and not acknowledged, acknowledged is unresolved and
// acknowledged (an acknowledgement never resolves, Q-STOR-530).
const (
	FindingOpen         = "open"
	FindingAcknowledged = "acknowledged"
	FindingResolved     = "resolved"
	FindingAll          = "all"
)

// FindingItems is how many referencing items a finding of the list names (Q-STOR-531).
const FindingItems = 3

// FindingQuery selects one page of findings.
type FindingQuery struct {
	// From is the period's start: a finding unresolved at From or later belongs to it.
	From time.Time
	// Connection narrows to one connection (empty: every one); State is one of the Finding… states.
	Connection, State string
	// AfterDetected and AfterID are the last finding of the previous page; AfterID 0 for the first.
	AfterDetected time.Time
	AfterID       int64
	Limit         int
}

// Item is an item that references the blob of a finding, read now.
type Item struct {
	Project, Repository, Path string
	Trashed                   bool
}

// Finding is one finding of the list (R-STOR-108, R-STOR-111). Resolved and Acknowledged are zero while they are not; AcknowledgedBy is
// the person's id and name, empty when the person no longer exists.
type Finding struct {
	ID                     int64
	Digest                 blob.Digest
	Connection, Status     string
	Source, Reason         string
	StoredSize, BlobSize   int64
	Unavailable            bool
	Detected, Resolved     time.Time
	Resolution             string
	Acknowledged           time.Time
	AcknowledgedBy, ByName string
	Note                   string
	Items                  []Item
	ItemCount              int64
}

// State is the finding's state for the list.
func (f Finding) State() string {
	switch {
	case !f.Resolved.IsZero():
		return FindingResolved
	case !f.Acknowledged.IsZero():
		return FindingAcknowledged
	}
	return FindingOpen
}

type findingRow = q.HistoryListFindingsRow

func findingOf(r findingRow) Finding {
	return Finding{
		ID: r.ID, Digest: blob.Digest(r.Sha256), Connection: r.ConnectionID, Status: r.Status, Source: r.Source, Reason: r.Reason,
		StoredSize: r.StoredSize, BlobSize: r.BlobSize, Unavailable: r.Unavailable, Detected: micro(r.DetectedUs), Resolved: micro(r.ResolvedUs),
		Resolution: r.Resolution, Acknowledged: micro(r.AcknowledgedUs), AcknowledgedBy: r.AcknowledgedBy, ByName: r.AcknowledgedByName,
		Note: r.AcknowledgedNote, Items: []Item{},
	}
}

// ListFindings reads one page of findings, newest first, with the first FindingItems items behind each and their number.
func ListFindings(ctx context.Context, db *postgres.DB, fq FindingQuery) ([]Finding, error) {
	var out []Finding
	err := db.Read(ctx, func(c q.DBTX) error {
		qs := q.New(c)
		state := fq.State
		if state == "" {
			state = FindingAll
		}
		rows, err := qs.HistoryListFindings(ctx, q.HistoryListFindingsParams{
			FromUnix: unix(fq.From), ConnectionID: fq.Connection, State: state, AfterID: fq.AfterID, AfterUs: fq.AfterDetected.UnixMicro(),
			PageSize: int32(fq.Limit), //nolint:gosec // a page size of the API
		})
		if err != nil {
			return err
		}
		out = make([]Finding, 0, len(rows))
		for _, r := range rows {
			out = append(out, findingOf(r))
		}
		return withItems(ctx, qs, out)
	})
	return out, err
}

// withItems fills the items of a page of findings: one read for the page's blobs, one for their counts.
func withItems(ctx context.Context, qs *q.Queries, page []Finding) error {
	if len(page) == 0 {
		return nil
	}
	index := map[blob.Digest][]int{}
	var digests [][]byte
	for i, f := range page {
		if _, ok := index[f.Digest]; !ok {
			d := f.Digest
			digests = append(digests, d[:])
		}
		index[f.Digest] = append(index[f.Digest], i)
	}
	items, err := qs.DamagedBlobItems(ctx, q.DamagedBlobItemsParams{Digests: digests, MaxItems: FindingItems})
	if err != nil {
		return err
	}
	for _, r := range items {
		for _, i := range index[blob.Digest(r.Sha256)] {
			page[i].Items = append(page[i].Items, Item{Project: r.ProjectKey, Repository: r.RepoKey, Path: r.Path, Trashed: r.Trashed})
		}
	}
	counts, err := qs.DamagedBlobItemCounts(ctx, digests)
	if err != nil {
		return err
	}
	for _, r := range counts {
		for _, i := range index[blob.Digest(r.Sha256)] {
			page[i].ItemCount = r.Items
		}
	}
	return nil
}

// ErrNoFinding is the answer of Acknowledge for an id no finding has; ErrFindingResolved for a resolved finding.
var (
	ErrNoFinding       = errors.New("history: no finding has this id")
	ErrFindingResolved = errors.New("history: the finding is resolved")
)

// Acknowledge records in tx that a person knows of an unresolved finding (R-STOR-111, Q-STOR-530): the time, the person's id and the
// note; a second acknowledgement replaces the first, and none resolves. It answers the finding as the list shows it.
func Acknowledge(ctx context.Context, tx meta.Tx, id int64, by, note string) (Finding, error) {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return Finding{}, errors.New("history: the transaction is not one of internal/meta/postgres")
	}
	qs := q.New(ptx)
	ids, err := qs.HistoryAcknowledgeFinding(ctx, q.HistoryAcknowledgeFindingParams{ID: id, AcknowledgedBy: by, Note: note})
	if err != nil {
		return Finding{}, err
	}
	if len(ids) == 0 {
		resolved, err := qs.HistoryFindingResolved(ctx, id)
		switch {
		case err != nil:
			return Finding{}, err
		case len(resolved) == 0:
			return Finding{}, ErrNoFinding
		}
		return Finding{}, ErrFindingResolved
	}
	rows, err := qs.HistoryFindingByID(ctx, id)
	if err != nil {
		return Finding{}, err
	}
	if len(rows) == 0 {
		return Finding{}, ErrNoFinding
	}
	page := []Finding{findingOf(findingRow(rows[0]))}
	if err := withItems(ctx, qs, page); err != nil {
		return Finding{}, err
	}
	return page[0], nil
}
