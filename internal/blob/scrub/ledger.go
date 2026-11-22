package scrub

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// Due is a copy the scrub reads: present, on one connection, never verified or verified longer ago than the connection's
// period (R-STOR-027).
type Due struct {
	Digest     blob.Digest
	Key        string
	StoredSize int64
	// VerifiedUs is the copy's verified_at in microseconds since the epoch, 0 when it was never verified; CreatedUs its
	// created_at, the identity of the row the read saw.
	VerifiedUs, CreatedUs int64
}

// beforeAnyVerification is the cursor's verified_at before the first page, in microseconds since the epoch: about 1200 BC,
// earlier than any stamp and inside PostgreSQL's range of timestamps.
const beforeAnyVerification = -100_000_000_000_000_000

// Cursor is the position of a connection's walk over its due copies: first the copies never verified, by digest, then the
// verified ones, by verified_at and digest.
type Cursor struct {
	Verified bool
	AfterUs  int64
	After    blob.Digest
	started  bool
}

// Gone is a copy marked missing that the scrub reads once more (Q-STOR-265).
type Gone struct {
	Digest     blob.Digest
	Key        string
	StoredSize int64
	// StateChangedUs is the copy's state_changed_at in microseconds: the identity of the row that was marked.
	StateChangedUs int64
}

// CopyRow is one copy of a blob with what the repair and the recheck compare.
type CopyRow struct {
	blob.Copy
	StateChangedAt time.Time
	// StateChangedUs and CreatedUs are the copy's state_changed_at and created_at in microseconds: the identity of the row.
	StateChangedUs, CreatedUs int64
}

// Ledger is the database side of the scrub: the due copies, the stamps and marks, the copies of a blob and the record of a
// copy shown whole again. [PGLedger] is PostgreSQL; the unit tests use a fake.
type Ledger interface {
	// Due returns up to limit due copies of conn after cur, with the cursor after them; an empty page ends the walk.
	Due(ctx context.Context, conn string, period time.Duration, cur Cursor, limit int) ([]Due, Cursor, error)
	// Verified stamps verified_at and checked_at of the copy the read saw (its created_at in microseconds).
	Verified(ctx context.Context, d blob.Digest, conn string, createdUs int64) error
	// Damaged marks a present copy missing or corrupt with the mark of every reader (Store.MarkCopyDamaged, R-STOR-055) and
	// reports whether the copy stands marked.
	Damaged(ctx context.Context, d blob.Digest, cp blob.Copy, status string) bool
	// Missing returns up to limit copies of conn marked missing whose store last answered longer ago than interval, by digest
	// after the given one (all of them when first is set).
	Missing(ctx context.Context, conn string, interval time.Duration, after blob.Digest, first bool, limit int) ([]Gone, error)
	// Looked moves checked_at of a copy marked missing that a read found still not whole.
	Looked(ctx context.Context, d blob.Digest, conn string) error
	// Copies returns the copies of a blob; found is false when no live blob row has the digest.
	Copies(ctx context.Context, d blob.Digest) (copies []CopyRow, found bool, err error)
	// Whole records that a copy found missing or corrupt was shown whole (from its state, changed at changedUs): present and
	// verified with storedSize, its queue row gone, the blob's state following. It reports false when the row is no longer
	// the one found damaged.
	Whole(ctx context.Context, d blob.Digest, conn, from string, changedUs, storedSize int64) (bool, error)
}

// PGLedger is the ledger over PostgreSQL and the blob store, whose mark of a damaged copy it uses.
type PGLedger struct {
	DB    *postgres.DB
	Store *blob.Store
	Log   *slog.Logger
}

// Due implements [Ledger].
func (l PGLedger) Due(ctx context.Context, conn string, period time.Duration, cur Cursor, limit int) ([]Due, Cursor, error) {
	var out []Due
	err := l.DB.Read(ctx, func(c q.DBTX) error {
		out = out[:0]
		qs := q.New(c)
		if !cur.Verified {
			after := []byte{}
			if cur.started {
				after = cur.After.Bytes()
			}
			rows, err := qs.ScrubDueUnverified(ctx, q.ScrubDueUnverifiedParams{ConnectionID: conn, After: after, PageSize: int32(limit)}) //nolint:gosec // a page size
			if err != nil || len(rows) > 0 {
				for _, r := range rows {
					out = append(out, Due{Digest: blob.Digest(r.Sha256), Key: r.ObjectKey, StoredSize: r.StoredSize, CreatedUs: r.CreatedUs})
				}
				return err
			}
		}
		after, afterUs := []byte{}, int64(beforeAnyVerification)
		if cur.Verified {
			after, afterUs = cur.After.Bytes(), cur.AfterUs
		}
		rows, err := qs.ScrubDueVerified(ctx, q.ScrubDueVerifiedParams{
			ConnectionID: conn, PeriodSeconds: period.Seconds(), AfterUs: afterUs, After: after, PageSize: int32(limit), //nolint:gosec // a page size
		})
		for _, r := range rows {
			out = append(out, Due{Digest: blob.Digest(r.Sha256), Key: r.ObjectKey, StoredSize: r.StoredSize, VerifiedUs: r.VerifiedUs, CreatedUs: r.CreatedUs})
		}
		return err
	})
	if err != nil {
		return nil, cur, err
	}
	next := cur
	if len(out) > 0 {
		last := out[len(out)-1]
		next = Cursor{Verified: last.VerifiedUs != 0, AfterUs: last.VerifiedUs, After: last.Digest, started: true}
	}
	return out, next, nil
}

// Verified implements [Ledger].
func (l PGLedger) Verified(ctx context.Context, d blob.Digest, conn string, createdUs int64) error {
	return l.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		_, err = qs.ScrubStampVerified(ctx, q.ScrubStampVerifiedParams{Sha256: d.Bytes(), ConnectionID: conn, CreatedUs: createdUs})
		return err
	})
}

// Damaged implements [Ledger].
func (l PGLedger) Damaged(ctx context.Context, d blob.Digest, cp blob.Copy, status string) bool {
	return l.Store.MarkCopyDamaged(ctx, d, cp, status)
}

// Missing implements [Ledger].
func (l PGLedger) Missing(ctx context.Context, conn string, interval time.Duration, after blob.Digest, first bool, limit int) ([]Gone, error) {
	var out []Gone
	from := after.Bytes()
	if first {
		from = []byte{}
	}
	err := l.DB.Read(ctx, func(c q.DBTX) error {
		rows, err := q.New(c).ScrubMissingDue(ctx, q.ScrubMissingDueParams{
			ConnectionID: conn, IntervalSeconds: interval.Seconds(), After: from, PageSize: int32(limit), //nolint:gosec // a page size
		})
		out = out[:0]
		for _, r := range rows {
			out = append(out, Gone{Digest: blob.Digest(r.Sha256), Key: r.ObjectKey, StoredSize: r.StoredSize, StateChangedUs: r.StateChangedUs})
		}
		return err
	})
	return out, err
}

// Looked implements [Ledger].
func (l PGLedger) Looked(ctx context.Context, d blob.Digest, conn string) error {
	return l.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		return qs.ScrubTouchMissing(ctx, q.ScrubTouchMissingParams{Sha256: d.Bytes(), ConnectionID: conn})
	})
}

// Copies implements [Ledger].
func (l PGLedger) Copies(ctx context.Context, d blob.Digest) ([]CopyRow, bool, error) {
	var out []CopyRow
	found := false
	err := l.DB.Read(ctx, func(c q.DBTX) error {
		out, found = out[:0], false
		qs := q.New(c)
		blobs, err := qs.BlobStateSize(ctx, d.Bytes())
		if err != nil || len(blobs) == 0 || blobs[0].State != "live" {
			return err
		}
		found = true
		rows, err := qs.ScrubCopies(ctx, d.Bytes())
		for _, r := range rows {
			cp := CopyRow{
				Copy:           blob.Copy{Connection: r.ConnectionID, ObjectKey: r.ObjectKey, StoredSize: r.StoredSize, State: r.State, Retired: r.Retired},
				StateChangedUs: r.StateChangedUs, CreatedUs: r.CreatedUs,
			}
			// a NULL timestamp is the zero time (this tree imports no driver type, R-ARCH-002)
			if r.VerifiedAt.Valid {
				cp.VerifiedAt = r.VerifiedAt.Time
			}
			if r.CheckedAt.Valid {
				cp.CheckedAt = r.CheckedAt.Time
			}
			if r.StateChangedAt.Valid {
				cp.StateChangedAt = r.StateChangedAt.Time
			}
			out = append(out, cp)
		}
		return err
	})
	return out, found, err
}

// Whole implements [Ledger]: one transaction holds the blob's lock of a mark (FOR NO KEY UPDATE, the blob must be live), sets
// the copy present and verified when it is still the row found damaged, deletes its queue row and lets the blob's content
// state follow from its copies (R-STOR-052).
func (l PGLedger) Whole(ctx context.Context, d blob.Digest, conn, from string, changedUs, storedSize int64) (bool, error) {
	var changed, healed bool
	err := l.DB.InTx(ctx, func(tx meta.Tx) error {
		changed, healed = false, false
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		states, err := qs.LockBlobForMark(ctx, d.Bytes())
		if err != nil || len(states) == 0 || states[0] != "live" {
			return err
		}
		n, err := qs.ScrubRecordWhole(ctx, q.ScrubRecordWholeParams{
			StoredSize: storedSize, Sha256: d.Bytes(), ConnectionID: conn, FromState: from, StateChangedUs: changedUs,
		})
		if err != nil || n == 0 {
			return err
		}
		changed = true
		if err := qs.ScrubDeleteQueued(ctx, q.ScrubDeleteQueuedParams{ConnectionID: conn, Sha256: d.Bytes()}); err != nil {
			return err
		}
		rows, err := qs.BlobCopies(ctx, d.Bytes())
		if err != nil {
			return err
		}
		copies := make([]blob.Copy, 0, len(rows))
		for _, r := range rows {
			copies = append(copies, blob.Copy{Connection: r.ConnectionID, State: r.State, Retired: r.Retired})
		}
		m, err := qs.SetBlobContentStatus(ctx, q.SetBlobContentStatusParams{Sha256: d.Bytes(), Status: blob.DeriveContentStatus(copies)})
		healed = m > 0
		return err
	})
	if err == nil && healed && l.Log != nil {
		l.Log.Info("a blob has a present copy again", "sha256", d.Hex(), "connection", conn)
	}
	return changed, err
}

func queries(tx meta.Tx) (*q.Queries, error) {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return nil, fmt.Errorf("scrub: the transaction is not one of internal/meta/postgres")
	}
	return q.New(ptx), nil
}
