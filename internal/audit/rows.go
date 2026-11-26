package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The rows of the audit log's screen (operations-and-observability.md R-OPS-055, the owner's decision of 2026-11-26, ADR-297): the
// stored events stay one artifact.downloaded per item served (R-OPS-054), and the screen reads them as rows, a download of several
// items as one row. The grouping is a function of the stored events alone, so the same events always form the same rows, read now
// or a year later, on any replica.

// Of a download's row.
const (
	// DownloadGap is the longest pause between two items of one download: the events of one unit that one caller fetched (the key of
	// audit_download_key) belong to one row while each follows the one before by at most this much. An event is written when its body
	// was sent, so the pause between two items is the time one item's transfer took while no other ended: a minute holds a layer of
	// several hundred megabytes at ordinary speeds, and two pulls of one image by one caller a minute apart are two rows.
	DownloadGap = time.Minute
	// DownloadPreview is the number of items a row carries; the screen lists them when the row is opened and links to the whole.
	DownloadPreview = 10
	// DownloadReadLimit is the number of items a row's facts are read from: a download of more (one caller fetching one unit without
	// a pause, for hours) says so (Download.More) instead of reading on.
	DownloadReadLimit = 1000
)

// Row is one row of the screen: an event, and where it is the first event of a download, the download (Download non-nil).
type Row struct {
	Event    Entry
	Download *Download
}

// Download is what a row of a download shows (R-OPS-055).
type Download struct {
	// Unit, Package and Version name what was downloaded: the unit of the events (an image's name, a version's name@version), the
	// package of the first item, and the first version any item names (an image's tag), "" where none does.
	Unit, Package, Version string
	// Items is the number of its items, at most [DownloadReadLimit]; More says that the download goes on beyond them.
	Items int
	More  bool
	// Size is the sum of the items' sizes; SizeKnown is false when an item does not carry one.
	Size      int64
	SizeKnown bool
	// Last is the time of its last item read.
	Last time.Time
	// First holds its first items, at most [DownloadPreview].
	First []DownloadItem
}

// DownloadItem is one item of a download: its event's ID and time, the item's path, digest and size, the replica that served it.
type DownloadItem struct {
	ID         string
	OccurredAt time.Time
	Path       string
	SHA256     string
	Size       int64
	SizeKnown  bool
	Replica    string
	// Delivery is how the item was delivered when not by Quader's own bytes: "link", "handover".
	Delivery string
}

// RowsResult is one page of rows; Next is nil on the last page and otherwise the position of its last row's event.
type RowsResult struct {
	Items []Row
	Next  *Cursor
}

// downloadFacts are the members of artifact.downloaded's details that the rows read.
type downloadFacts struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Size     *int64 `json:"size"`
	Unit     string `json:"unit"`
	Package  string `json:"package"`
	Version  string `json:"version"`
	Delivery string `json:"delivery"`
}

func factsOf(raw json.RawMessage) downloadFacts {
	var f downloadFacts
	if len(bytes.TrimSpace(raw)) > 0 {
		_ = json.Unmarshal(raw, &f)
	}
	return f
}

// startsDownload says whether a stored event can begin a download's row: an event of a transfer ([TransferEvents]) that names its
// unit. One written before the events named it is a row of its own.
func startsDownload(e Entry) bool {
	return slices.Contains(TransferEvents(), e.Action) && factsOf(e.Details).Unit != ""
}

// TransferEvents are the event types the rows join per unit (R-OPS-055): the successful transfer of one item of a unit a person asked
// for. A download is one row per action: the items served (artifact.downloaded) and the data keys handed to a Bastion for them
// (bastion.data_key.handed) are two rows of one pull. The statements of the rows name the same two (audit.sql).
func TransferEvents() []string {
	return []string{EventArtifactDownloaded, EventBastionDataKeyHanded}
}

// Rows returns one page of the screen's rows (R-OPS-055), newest first by each row's first event: the filter and the paging of
// [Store.Query], a page of Page.Limit rows however many events its downloads hold. A download's row is never cut by a page: its
// items are read from its first event. The filters of the range's end and of the trace apply to the items as to the events; a
// download that began before the range's start shows its items within the range, from its first one there.
func (s *Store) Rows(ctx context.Context, f Filter, p Page) (RowsResult, error) {
	a, err := s.args(f, p)
	if err != nil {
		return RowsResult{}, err
	}
	var heads []Entry
	var members []q.ListAuditDownloadMembersRow
	err = s.db.Read(ctx, func(db q.DBTX) error {
		qs := q.New(db)
		got, err := qs.ListAuditRowsNewest(ctx, q.ListAuditRowsNewestParams{
			FromAt: a.from, ToAt: a.to, Action: a.exact, ActionPrefix: a.prefix, Category: f.Category, Outcome: string(f.Outcome),
			Actor: f.Actor, ActorID: a.actorID, Repository: f.Repository, ResourceType: f.ResourceType, ResourceID: f.ResourceID,
			TraceID: f.TraceID, HasCursor: a.hasCursor, CursorAt: a.cursorAt, CursorID: a.cursorID,
			GapSeconds: int32(DownloadGap / time.Second), RowLimit: int32(a.limit + 1), //nolint:gosec // limit <= MaxLimit
		})
		if err != nil {
			return err
		}
		heads = make([]Entry, 0, len(got))
		for _, r := range got {
			heads = append(heads, fromRows(r))
		}
		// The row after the page only says that there is a next page; it is not walked.
		starts := startsOf(heads[:min(len(heads), a.limit)])
		if starts == "" {
			return nil
		}
		members, err = qs.ListAuditDownloadMembers(ctx, q.ListAuditDownloadMembersParams{
			Starts: starts, GapSeconds: int32(DownloadGap / time.Second), ToAt: a.to, TraceID: f.TraceID,
			MemberLimit: DownloadReadLimit,
		})
		return err
	})
	if err != nil {
		return RowsResult{}, fmt.Errorf("audit rows: %w", err)
	}
	out := RowsResult{}
	if len(heads) > a.limit {
		heads = heads[:a.limit]
		last := heads[a.limit-1]
		out.Next = &Cursor{OccurredAt: last.OccurredAt, ID: last.ID}
	}
	byOrd := map[int32][]q.ListAuditDownloadMembersRow{}
	for _, m := range members {
		byOrd[m.Ord] = append(byOrd[m.Ord], m)
	}
	out.Items = make([]Row, 0, len(heads))
	for i, h := range heads {
		row := Row{Event: h}
		if startsDownload(h) {
			row.Download = downloadOf(h, byOrd[int32(i)]) //nolint:gosec // i < MaxLimit
		}
		out.Items = append(out.Items, row)
	}
	return out, nil
}

// startsOf is the JSON array of the download starts among heads, each with its position in heads; "" when there is none.
func startsOf(heads []Entry) string {
	type start struct {
		Ord int    `json:"ord"`
		ID  string `json:"id"`
		At  string `json:"at"`
	}
	var out []start
	for i, h := range heads {
		if startsDownload(h) {
			out = append(out, start{Ord: i, ID: h.ID, At: h.OccurredAt.UTC().Format(time.RFC3339Nano)})
		}
	}
	if len(out) == 0 {
		return ""
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// downloadOf builds a download's row from its first event and the items the walk read (the first of them is the event itself).
func downloadOf(head Entry, members []q.ListAuditDownloadMembersRow) *Download {
	hf := factsOf(head.Details)
	d := &Download{Unit: hf.Unit, Package: hf.Package, Last: head.OccurredAt, SizeKnown: true}
	if len(members) == 0 { // the walk found the event no more (it cannot, the event stays): the row is the event alone
		members = []q.ListAuditDownloadMembersRow{{ID: head.ID, OccurredAt: head.OccurredAt, Path: head.Path, Replica: head.Replica, Details: head.Details}}
	}
	if len(members) > DownloadReadLimit {
		members, d.More = members[:DownloadReadLimit], true
	}
	for _, m := range members {
		it := itemOf(m)
		d.Items++
		d.Last = it.OccurredAt
		if it.SizeKnown {
			d.Size += it.Size
		} else {
			d.SizeKnown = false
		}
		if mf := factsOf(m.Details); d.Version == "" && mf.Version != "" {
			d.Version = mf.Version
		}
		if len(d.First) < DownloadPreview {
			d.First = append(d.First, it)
		}
	}
	return d
}

func itemOf(m q.ListAuditDownloadMembersRow) DownloadItem {
	f := factsOf(m.Details)
	it := DownloadItem{ID: m.ID, OccurredAt: m.OccurredAt.UTC(), Path: m.Path, SHA256: f.SHA256, Replica: m.Replica, Delivery: f.Delivery}
	if it.Path == "" {
		it.Path = f.Path
	}
	if f.Size != nil {
		it.Size, it.SizeKnown = *f.Size, true
	}
	return it
}

// DownloadItemsResult is one page of a download's items, in the order they were served; Next is nil on the last page.
type DownloadItemsResult struct {
	Items []Entry
	Next  *Cursor
}

// DownloadItems returns one page of the items of the download whose first event is id (R-OPS-055), oldest first: the events the
// row's walk reads, every one of them, without the range and the filters of the screen. An ID that is no first event of a download
// is not found. Page.After is the last item of the page before; Page.Oldest is implied.
func (s *Store) DownloadItems(ctx context.Context, id string, p Page) (DownloadItemsResult, error) {
	head, err := s.Get(ctx, id)
	if err != nil {
		if errs.KindOf(err) == errs.NotFound {
			return DownloadItemsResult{}, errNoDownload()
		}
		return DownloadItemsResult{}, err
	}
	limit := p.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	limit = min(limit, MaxLimit)
	from, skip := head, 0
	if p.After != nil {
		from, skip = Entry{ID: strings.ToLower(p.After.ID), OccurredAt: p.After.OccurredAt}, 1
		if _, ok := parseUUID(from.ID); !ok || from.OccurredAt.Before(head.OccurredAt) {
			return DownloadItemsResult{}, errBadItemCursor()
		}
	}
	gap := int32(DownloadGap / time.Second)
	starts, _ := json.Marshal([]map[string]any{{"ord": 0, "id": from.ID, "at": from.OccurredAt.UTC().Format(time.RFC3339Nano)}})
	var got []q.ListAuditDownloadMembersRow
	err = s.db.Read(ctx, func(db q.DBTX) error {
		qs := q.New(db)
		key, err := qs.AuditDownloadStartKey(ctx, q.AuditDownloadStartKeyParams{
			At: head.OccurredAt.UTC().Format(time.RFC3339Nano), ID: head.ID, GapSeconds: gap,
		})
		if err != nil || key == "" {
			return err
		}
		got, err = qs.ListAuditDownloadMembers(ctx, q.ListAuditDownloadMembersParams{
			Starts: string(starts), GapSeconds: gap, ToAt: "infinity", MemberLimit: int32(limit + skip), //nolint:gosec // limit <= MaxLimit
		})
		if err == nil && (len(got) == 0 || got[0].Key != key) {
			// The cursor names no event of the download's key: a cursor of another download, or a made-up one.
			return errBadItemCursor()
		}
		return err
	})
	switch {
	case errs.KindOf(err) == errs.Invalid:
		return DownloadItemsResult{}, err
	case err != nil:
		return DownloadItemsResult{}, fmt.Errorf("audit download items: %w", err)
	case got == nil:
		return DownloadItemsResult{}, errNoDownload()
	}
	got = got[min(skip, len(got)):]
	out := DownloadItemsResult{Items: make([]Entry, 0, len(got))}
	for _, m := range got {
		out.Items = append(out.Items, fromMember(m))
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		last := out.Items[limit-1]
		out.Next = &Cursor{OccurredAt: last.OccurredAt, ID: last.ID}
	}
	return out, nil
}

func errBadItemCursor() error {
	return errs.E(errs.Invalid, errs.CodeInvalid, "the cursor position is not an item of this download")
}

func errNoDownload() error { return errs.E(errs.NotFound, errs.CodeNotFound, "no such download") }
