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

// The rows of the audit log's screen (operations-and-observability.md R-OPS-055, R-OPS-056, the owner's decision of 2026-11-26,
// ADR-297): the stored events stay one per item (an artifact.downloaded per item served, an artifact.deployed per file stored, an
// artifact.deleted per file removed), and the screen reads them as rows, the items of one act of one caller on one unit as one row: a
// download, a push or a publish, a delete. The grouping is a function of the stored events alone, so the same events always form the
// same rows, read now or a year later, on any replica.

// Of a group's row.
const (
	// DownloadGap is the longest pause between two items of one row: the events of one act on one unit that one caller caused (the key
	// of audit_row_key) belong to one row while each follows the one before by at most this much. An event of a transfer is written
	// when its body was sent, an event of a change when its transaction began, so the pause between two items is the time one item's
	// transfer took while no other ended: a minute holds a layer of several hundred megabytes at ordinary speeds, and two pulls of one
	// image by one caller a minute apart are two rows.
	DownloadGap = time.Minute
	// DownloadPreview is the number of items a row carries; the screen lists them when the row is opened and links to the whole.
	DownloadPreview = 10
	// DownloadReadLimit is the number of items a row's facts are read from: a group of more (one caller fetching one unit without a
	// pause, for hours) says so (Group.More) instead of reading on.
	DownloadReadLimit = 1000
)

// The acts a row of several events is (R-OPS-056): what a person did to a unit, named by the row. The key of a row holds the act, not
// the action, so that the events of different actions of one act join (a push's files and the version it published).
const (
	// ActDownload is a download: the items served (artifact.downloaded).
	ActDownload = "download"
	// ActDataKeys is the data keys handed to a Bastion for a download's items (bastion.data_key.handed), a row beside the download's.
	ActDataKeys = "datakeys"
	// ActUpload is a push, a publish or an upload: the files stored (artifact.deployed) and the version they published
	// (package.published, npm.publish). The row names it by what it holds ([Group.Act]): ActPush, ActPublish or ActUpload.
	ActUpload = "upload"
	// ActPush is an upload whose version was published by a format whose clients push (OCI, NuGet).
	ActPush = "push"
	// ActPublish is an upload that published a version by a format whose clients publish (npm, Ansible Galaxy, Debian).
	ActPublish = "publish"
	// ActDelete is a delete: the files removed (artifact.deleted) and the versions, manifests and packages the format removed with them.
	ActDelete = "delete"
	// ActWrites is the allowed writes of one act (authz.write.allowed): the decisions behind a push, a delete or a fill, a row beside
	// the act's own.
	ActWrites = "writes"
)

// actions of each act whose events a row joins. The statements of the rows (audit.sql) and the act of the function audit_row_key of migration
// 00144 name the same actions; TestR_OPS_056_TheStatementsNameTheGroupedActions holds them together.
var actActions = map[string][]string{
	ActDownload: {EventArtifactDownloaded},
	ActDataKeys: {EventBastionDataKeyHanded},
	ActUpload:   {EventArtifactDeployed, EventPackagePublished, EventNPMPublish},
	ActDelete: {
		EventArtifactDeleted, EventOCIManifestDeleted, EventDebPackageDeleted, EventNPMUnpublish, EventPackageDeleted,
		EventAnsibleVersionDeleted,
	},
	ActWrites: {"authz.write.allowed"},
}

// pushFormats are the formats whose clients push a version rather than publish it (docker push, dotnet nuget push): the word a row of
// their upload is named by (package.published names its format, R-OPS-056).
var pushFormats = []string{"oci", "nuget"}

// fileActions are the actions whose event is one item with its bytes: a row sums their sizes. The other actions of an act (a version
// published, a manifest or a version removed) name a version or a digest and carry no size of their own.
var fileActions = []string{EventArtifactDownloaded, EventBastionDataKeyHanded, EventArtifactDeployed, EventArtifactDeleted}

// GroupedEvents are the event types the rows join per act, unit and caller (R-OPS-055, R-OPS-056), each act's in the order of
// actActions: a download, the data keys handed for it, a push or a publish, a delete and the allowed writes.
func GroupedEvents() []string {
	var out []string
	for _, act := range []string{ActDownload, ActDataKeys, ActUpload, ActDelete, ActWrites} {
		out = append(out, actActions[act]...)
	}
	return out
}

// ActOf is the act whose row an event of the action joins, "" for an action that is no part of one.
func ActOf(action string) string {
	for act, actions := range actActions {
		if slices.Contains(actions, action) {
			return act
		}
	}
	return ""
}

// Row is one row of the screen: an event, and where it is the first event of a group, the group (Group non-nil).
type Row struct {
	Event Entry
	Group *Group
}

// Group is what a row of several events of one act shows (R-OPS-055, R-OPS-056).
type Group struct {
	// Act is what the row is: ActDownload, ActDataKeys, ActPush, ActPublish, ActUpload, ActDelete or ActWrites.
	Act string
	// Unit, Package and Version name what the act was on: the unit of the events (an image's name, a version's name@version, a
	// package that was deleted, a folder of files), the package of the first item, and the first version any item names (an image's
	// tag), "" where none does. Versions is the number of distinct versions its items name: a delete of a package of five versions is
	// one row naming five.
	Unit, Package, Version string
	Versions               int
	// Items is the number of its items, at most [DownloadReadLimit]; More says that the group goes on beyond them.
	Items int
	More  bool
	// Size is the sum of the sizes of its files; SizeKnown is false when a file does not carry one, or when it holds no file.
	Size      int64
	SizeKnown bool
	// Last is the time of its last item read.
	Last time.Time
	// First holds its first items, at most [DownloadPreview].
	First []GroupItem
}

// GroupItem is one item of a group: its event's ID, time and action, the item's path, digest, version and size, the replica that
// wrote it.
type GroupItem struct {
	ID         string
	OccurredAt time.Time
	Action     string
	Path       string
	SHA256     string
	Version    string
	Size       int64
	SizeKnown  bool
	Replica    string
	// Delivery is how a downloaded item was delivered when not by Quader's own bytes: "link", "handover".
	Delivery string
}

// RowsResult is one page of rows; Next is nil on the last page and otherwise the position of its last row's event.
type RowsResult struct {
	Items []Row
	Next  *Cursor
}

// itemFacts are the members of an item's details that the rows read: a file's path, digest and size, the unit, the package and
// the version, a delivery, and what the events of versions name instead of a file (a manifest's digest, a version's files, its format).
type itemFacts struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Digest   string `json:"digest"`
	Size     *int64 `json:"size"`
	Unit     string `json:"unit"`
	Package  string `json:"package"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	Delivery string `json:"delivery"`
	Format   string `json:"format"`
	Files    []struct {
		SHA256 string `json:"sha256"`
	} `json:"files"`
}

func factsOf(raw json.RawMessage) itemFacts {
	var f itemFacts
	if len(bytes.TrimSpace(raw)) > 0 {
		_ = json.Unmarshal(raw, &f)
	}
	return f
}

// digest is the SHA-256 an item names: a file's, a manifest's, or the first file of a version that was published.
func (f itemFacts) digest() string {
	switch {
	case f.SHA256 != "":
		return f.SHA256
	case f.Digest != "":
		return strings.TrimPrefix(f.Digest, "sha256:")
	case len(f.Files) > 0:
		return f.Files[0].SHA256
	}
	return ""
}

// pkg is the package an item names: package, or name where an event of the format names it so (an OCI manifest, an Ansible version).
func (f itemFacts) pkg() string {
	if f.Package != "" {
		return f.Package
	}
	return f.Name
}

// startsGroup says whether a stored event can begin a group's row: an event of an act ([GroupedEvents]) that names its unit. One
// written before the events named it is a row of its own.
func startsGroup(e Entry) bool {
	return ActOf(e.Action) != "" && factsOf(e.Details).Unit != ""
}

// Rows returns one page of the screen's rows (R-OPS-055), newest first by each row's first event: the filter and the paging of
// [Store.Query], a page of Page.Limit rows however many events its groups hold. A group's row is never cut by a page: its items are
// read from its first event. The filters choose the events and a row joins the events they chose: an event of a group is its first
// when no chosen event of its key lies within the pause before it, and the walk of its items takes the chosen events of its key, so a
// filter by action or by kind of resource that keeps one action of an act keeps that action's events as a row. The filters of the
// range's end and of the trace apply to the items as to the events; a group that began before the range's start shows its items
// within the range, from its first one there.
func (s *Store) Rows(ctx context.Context, f Filter, p Page) (RowsResult, error) {
	a, err := s.args(f, p)
	if err != nil {
		return RowsResult{}, err
	}
	var heads []Entry
	var members []q.ListAuditGroupMembersRow
	gap := int32(DownloadGap / time.Second)
	err = s.db.Read(ctx, func(db q.DBTX) error {
		qs := q.New(db)
		got, err := s.rowsNewest(ctx, qs, f, a, gap)
		if err != nil {
			return err
		}
		heads = got
		// The row after the page only says that there is a next page; it is not walked.
		starts := startsOf(heads[:min(len(heads), a.limit)])
		if starts == "" {
			return nil
		}
		members, err = qs.ListAuditGroupMembers(ctx, q.ListAuditGroupMembersParams{
			Starts: starts, GapSeconds: gap, ToAt: a.to, TraceID: f.TraceID, Action: a.exact, ActionPrefix: a.prefix,
			Category: f.Category, Outcome: string(f.Outcome), ResourceType: f.ResourceType, ResourceID: f.ResourceID,
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
	byOrd := map[int32][]q.ListAuditGroupMembersRow{}
	for _, m := range members {
		byOrd[m.Ord] = append(byOrd[m.Ord], m)
	}
	out.Items = make([]Row, 0, len(heads))
	for i, h := range heads {
		row := Row{Event: h}
		if startsGroup(h) {
			row.Group = groupOf(h, byOrd[int32(i)]) //nolint:gosec // i < MaxLimit
		}
		out.Items = append(out.Items, row)
	}
	return out, nil
}

// rowsNewest reads the heads of a page: the single events and the first events of groups, newest first, one more than the page.
func (s *Store) rowsNewest(ctx context.Context, qs *q.Queries, f Filter, a queryArgs, gap int32) ([]Entry, error) {
	got, err := qs.ListAuditRowsNewest(ctx, q.ListAuditRowsNewestParams{
		FromAt: a.from, ToAt: a.to, Action: a.exact, ActionPrefix: a.prefix, Category: f.Category, Outcome: string(f.Outcome),
		ActorName: a.actorName, ActorID: a.actorID, Repository: f.Repository, ResourceType: f.ResourceType, ResourceID: f.ResourceID,
		TraceID: f.TraceID, HasCursor: a.hasCursor, CursorAt: a.cursorAt, CursorID: a.cursorID,
		GapSeconds: gap, RowLimit: int32(a.limit + 1), //nolint:gosec // limit <= MaxLimit
	})
	heads := make([]Entry, 0, len(got))
	for _, r := range got {
		heads = append(heads, fromRows(r))
	}
	return heads, err
}

// startsOf is the JSON array of the group starts among heads, each with its position in heads; "" when there is none.
func startsOf(heads []Entry) string {
	type start struct {
		Ord int    `json:"ord"`
		ID  string `json:"id"`
		At  string `json:"at"`
	}
	var out []start
	for i, h := range heads {
		if startsGroup(h) {
			out = append(out, start{Ord: i, ID: h.ID, At: h.OccurredAt.UTC().Format(time.RFC3339Nano)})
		}
	}
	if len(out) == 0 {
		return ""
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// groupOf builds a group's row from its first event and the items the walk read (the first of them is the event itself).
func groupOf(head Entry, members []q.ListAuditGroupMembersRow) *Group {
	hf := factsOf(head.Details)
	g := &Group{Act: ActOf(head.Action), Unit: hf.Unit, Package: hf.pkg(), Last: head.OccurredAt, SizeKnown: true}
	if len(members) == 0 { // the walk found the event no more (it cannot, the event stays): the row is the event alone
		members = []q.ListAuditGroupMembersRow{{
			ID: head.ID, OccurredAt: head.OccurredAt, Action: head.Action, Path: head.Path,
			Replica: head.Replica, Details: head.Details,
		}}
	}
	if len(members) > DownloadReadLimit {
		members, g.More = members[:DownloadReadLimit], true
	}
	versions := map[string]bool{}
	files := 0
	published := ""
	for _, m := range members {
		it := itemOf(m)
		g.Items++
		g.Last = it.OccurredAt
		if g.Package == "" {
			g.Package = factsOf(m.Details).pkg()
		}
		if slices.Contains(fileActions, it.Action) {
			files++
			if it.SizeKnown {
				g.Size += it.Size
			} else {
				g.SizeKnown = false
			}
		}
		if it.Version != "" {
			if g.Version == "" {
				g.Version = it.Version
			}
			versions[it.Version] = true
		}
		if it.Action == EventNPMPublish || it.Action == EventPackagePublished {
			if published == "" {
				published = firstNonEmpty(factsOf(m.Details).Format, "publish")
			}
		}
		if len(g.First) < DownloadPreview {
			g.First = append(g.First, it)
		}
	}
	if files == 0 {
		g.SizeKnown = false
	}
	g.Versions = len(versions)
	if g.Act == ActUpload {
		g.Act = uploadAct(published)
	}
	return g
}

// uploadAct names an upload by what it published: a push where the format's clients push, a publish where they publish, an upload
// of files that published no version.
func uploadAct(published string) string {
	switch {
	case published == "":
		return ActUpload
	case slices.Contains(pushFormats, published):
		return ActPush
	}
	return ActPublish
}

func itemOf(m q.ListAuditGroupMembersRow) GroupItem {
	f := factsOf(m.Details)
	it := GroupItem{
		ID: m.ID, OccurredAt: m.OccurredAt.UTC(), Action: m.Action, Path: m.Path, SHA256: f.digest(), Version: f.Version,
		Replica: m.Replica, Delivery: f.Delivery,
	}
	if it.Path == "" {
		it.Path = f.Path
	}
	if f.Size != nil {
		it.Size, it.SizeKnown = *f.Size, true
	}
	return it
}

// GroupItemsResult is one page of a group's items, in the order they were written; Next is nil on the last page.
type GroupItemsResult struct {
	Items []Entry
	Next  *Cursor
}

// GroupItems returns one page of the items of the group whose first event is id (R-OPS-055), oldest first: the events of its key that
// the row's walk reads without a filter, every one of them, without the range and the filters of the screen. An ID that is no first
// event of a group is not found. Page.After is the last item of the page before; Page.Oldest is implied.
func (s *Store) GroupItems(ctx context.Context, id string, p Page) (GroupItemsResult, error) {
	head, err := s.Get(ctx, id)
	if err != nil {
		if errs.KindOf(err) == errs.NotFound {
			return GroupItemsResult{}, errNoGroup()
		}
		return GroupItemsResult{}, err
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
			return GroupItemsResult{}, errBadItemCursor()
		}
	}
	gap := int32(DownloadGap / time.Second)
	starts, _ := json.Marshal([]map[string]any{{"ord": 0, "id": from.ID, "at": from.OccurredAt.UTC().Format(time.RFC3339Nano)}})
	var got []q.ListAuditGroupMembersRow
	err = s.db.Read(ctx, func(db q.DBTX) error {
		qs := q.New(db)
		key, err := qs.AuditGroupStartKey(ctx, q.AuditGroupStartKeyParams{
			At: head.OccurredAt.UTC().Format(time.RFC3339Nano), ID: head.ID, GapSeconds: gap,
		})
		if err != nil || key == "" {
			return err
		}
		got, err = qs.ListAuditGroupMembers(ctx, q.ListAuditGroupMembersParams{
			Starts: string(starts), GapSeconds: gap, ToAt: "infinity", MemberLimit: int32(limit + skip), //nolint:gosec // limit <= MaxLimit
		})
		if err == nil && (len(got) == 0 || got[0].Key != key) {
			// The cursor names no event of the group's key: a cursor of another group, or a made-up one.
			return errBadItemCursor()
		}
		return err
	})
	switch {
	case errs.KindOf(err) == errs.Invalid:
		return GroupItemsResult{}, err
	case err != nil:
		return GroupItemsResult{}, fmt.Errorf("audit group items: %w", err)
	case got == nil:
		return GroupItemsResult{}, errNoGroup()
	}
	got = got[min(skip, len(got)):]
	out := GroupItemsResult{Items: make([]Entry, 0, len(got))}
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
	return errs.E(errs.Invalid, errs.CodeInvalid, "the cursor position is not an item of this row")
}

func errNoGroup() error {
	return errs.E(errs.NotFound, errs.CodeNotFound, "no such row of several events")
}
