package scrub

import (
	"context"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The scopes of the list of damaged blobs (R-STOR-027, Q-API-386).
const (
	// ScopeBlobs lists the blobs no connection holds whole (content_status other than ok): the list an administrator acts on.
	ScopeBlobs = "blobs"
	// ScopeCopies lists the blobs with at least one missing or corrupt copy: the self-healing view.
	ScopeCopies = "copies"
)

// ListItems is the number of referencing items an entry of the list carries.
const ListItems = 20

// DamagedQuery selects one page of the list.
type DamagedQuery struct {
	Scope string
	// Connection and Status narrow the list (empty: no narrowing): for ScopeBlobs the blobs with a copy row on the connection and
	// the blob's content state, for ScopeCopies the damaged copy's connection and state.
	Connection, Status string
	// After is the digest of the last entry of the previous page, nil for the first.
	After []byte
	Limit int
}

// DamagedCopy is one copy of a listed blob.
type DamagedCopy struct {
	Connection, State string
	// VerifiedAt and CheckedAt are nil when never.
	VerifiedAt, CheckedAt *time.Time
	StateChangedAt        time.Time
}

// DamagedItem is an item that references a listed blob.
type DamagedItem struct {
	Project, Repository, Path string
	Trashed                   bool
}

// DamagedBlob is one entry of the list.
type DamagedBlob struct {
	Digest        blob.Digest
	Size          int64
	ContentStatus string
	Copies        []DamagedCopy
	// Items are the first ListItems items that reference the blob; ItemCount is the number of all of them.
	Items     []DamagedItem
	ItemCount int64
}

// ListDamaged reads one page of the list of damaged blobs (`GET /api/v1/storage/damaged-blobs`) in four short reads: the
// page, the copies, the items and their counts of its blobs.
func ListDamaged(ctx context.Context, db *postgres.DB, dq DamagedQuery) ([]DamagedBlob, error) {
	var out []DamagedBlob
	err := db.Read(ctx, func(c q.DBTX) error {
		out = out[:0]
		qs := q.New(c)
		after := dq.After
		if after == nil {
			after = []byte{}
		}
		limit := int32(dq.Limit) //nolint:gosec // a page size of the API
		var digests [][]byte
		index := map[blob.Digest]int{}
		add := func(sha []byte, size int64, status string) {
			d := blob.Digest(sha)
			index[d] = len(out)
			digests = append(digests, sha)
			out = append(out, DamagedBlob{Digest: d, Size: size, ContentStatus: status, Copies: []DamagedCopy{}, Items: []DamagedItem{}})
		}
		if dq.Scope == ScopeCopies {
			rows, err := qs.DamagedBlobsByCopy(ctx, q.DamagedBlobsByCopyParams{After: after, Status: dq.Status, ConnectionID: dq.Connection, PageSize: limit})
			if err != nil {
				return err
			}
			for _, r := range rows {
				add(r.Sha256, r.Size, r.ContentStatus)
			}
		} else {
			rows, err := qs.DamagedBlobsByBlob(ctx, q.DamagedBlobsByBlobParams{After: after, Status: dq.Status, ConnectionID: dq.Connection, PageSize: limit})
			if err != nil {
				return err
			}
			for _, r := range rows {
				add(r.Sha256, r.Size, r.ContentStatus)
			}
		}
		if len(digests) == 0 {
			return nil
		}
		return details(ctx, qs, digests, index, out)
	})
	return out, err
}

// details fills the copies, the first items and the item counts of the entries.
func details(ctx context.Context, qs *q.Queries, digests [][]byte, index map[blob.Digest]int, out []DamagedBlob) error {
	copies, err := qs.DamagedBlobCopies(ctx, digests)
	if err != nil {
		return err
	}
	for _, r := range copies {
		e := &out[index[blob.Digest(r.Sha256)]]
		dc := DamagedCopy{Connection: r.ConnectionID, State: r.State}
		// a NULL timestamp is nil (this tree imports no driver type, R-ARCH-002)
		if r.VerifiedAt.Valid {
			t := r.VerifiedAt.Time
			dc.VerifiedAt = &t
		}
		if r.CheckedAt.Valid {
			t := r.CheckedAt.Time
			dc.CheckedAt = &t
		}
		if r.StateChangedAt.Valid {
			dc.StateChangedAt = r.StateChangedAt.Time
		}
		e.Copies = append(e.Copies, dc)
	}
	items, err := qs.DamagedBlobItems(ctx, q.DamagedBlobItemsParams{Digests: digests, MaxItems: ListItems})
	if err != nil {
		return err
	}
	for _, r := range items {
		e := &out[index[blob.Digest(r.Sha256)]]
		e.Items = append(e.Items, DamagedItem{Project: r.ProjectKey, Repository: r.RepoKey, Path: r.Path, Trashed: r.Trashed})
	}
	counts, err := qs.DamagedBlobItemCounts(ctx, digests)
	if err != nil {
		return err
	}
	for _, r := range counts {
		out[index[blob.Digest(r.Sha256)]].ItemCount = r.Items
	}
	return nil
}
