package blob

import (
	"context"
	"errors"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// sqlStateAS001 is raised by the blob reference guard when a reference points
// at a blob that is not live (migration 00002).
const sqlStateAS001 = "AS001"

// sqlState returns the SQLSTATE of a database error, empty for other errors.
func sqlState(err error) string {
	var s interface{ SQLState() string }
	if errors.As(err, &s) {
		return s.SQLState()
	}
	return ""
}

// DB is what the store needs of the database layer: a function run in one
// READ COMMITTED transaction on the main pool. *postgres.DB implements it.
type DB interface {
	InTx(ctx context.Context, fn func(meta.Tx) error) error
}

var errForeignTx = errors.New("blob: the transaction is not one of internal/meta/postgres")

// queries returns the sqlc queries on the transaction behind tx. The store does
// not write SQL: its statements live in internal/meta/postgres/queries/blob.sql and blob_copies.sql.
func queries(tx meta.Tx) (*q.Queries, error) {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return nil, errForeignTx
	}
	return q.New(ptx), nil
}

// Rows is the store's access to the blobs table and the copies of a blob for the commit protocol and the download path
// (WithRows replaces it).
// pgRows runs the sqlc queries; the unit tests of the protocol's branches substitute a simulation (the integration tier runs
// the same code against PostgreSQL). The collector has no such seam: it is only tested against the database.
type Rows interface {
	// LockShare is the first statement of the commit protocol: FOR SHARE.
	LockShare(ctx context.Context, tx meta.Tx, d Digest) (state, status string, found bool, err error)
	// InsertLive inserts the live row; inserted is false when a concurrent
	// uploader's row won.
	InsertLive(ctx context.Context, tx meta.Tx, sp *Spooled) (inserted bool, err error)
	// Exists reports whether a row exists, in any state.
	Exists(ctx context.Context, tx meta.Tx, d Digest) (bool, error)
	// Enqueue puts the digest in blob_gc_queue.
	Enqueue(ctx context.Context, tx meta.Tx, d Digest) error
	// Row reads the row of a digest.
	Row(ctx context.Context, tx meta.Tx, d Digest) (Row, bool, error)
	// ClaimRecheck moves checked_at of a live row marked missing whose last recheck is at least interval old, and reports
	// whether it did: the claim of the one look at the store of the interval (Q-STOR-153).
	ClaimRecheck(ctx context.Context, tx meta.Tx, d Digest, interval time.Duration) (claimed bool, err error)

	// Copies reads the copies of a blob with whether their connection is retired (R-STOR-052).
	Copies(ctx context.Context, tx meta.Tx, d Digest) ([]Copy, error)
	// LockForMark locks the blob row for a change of a copy and of the derived content state (FOR NO KEY UPDATE).
	LockForMark(ctx context.Context, tx meta.Tx, d Digest) (state string, found bool, err error)
	// PutCopy records an object a commit wrote: present, inserted or overwriting a row that is not deleting (R-STOR-053).
	PutCopy(ctx context.Context, tx meta.Tx, d Digest, w CopyWrite) error
	// SetCopyState moves a copy in state from to state to and reports whether it did.
	SetCopyState(ctx context.Context, tx meta.Tx, d Digest, conn, from, to string) (bool, error)
	// StampVerified moves a present copy's verified_at to now when it is older than interval (Q-STOR-233).
	StampVerified(ctx context.Context, tx meta.Tx, d Digest, conn string, interval time.Duration) error
	// QueueCopy queues a gap of a connection for the reconciliation (R-STOR-056).
	QueueCopy(ctx context.Context, tx meta.Tx, conn string, d Digest) error
	// SetContentStatus sets a live blob's content state and reports whether it changed.
	SetContentStatus(ctx context.Context, tx meta.Tx, d Digest, status string) (bool, error)
}

type pgRows struct{}

var _ Rows = pgRows{}

func (pgRows) LockShare(ctx context.Context, tx meta.Tx, d Digest) (state, status string, found bool, err error) {
	qs, err := queries(tx)
	if err != nil {
		return "", "", false, err
	}
	rows, err := qs.LockBlobShare(ctx, d.Bytes())
	if err != nil || len(rows) == 0 {
		return "", "", false, err
	}
	return rows[0].State, rows[0].ContentStatus, true, nil
}

func (pgRows) InsertLive(ctx context.Context, tx meta.Tx, sp *Spooled) (bool, error) {
	qs, err := queries(tx)
	if err != nil {
		return false, err
	}
	n, err := qs.InsertLiveBlob(ctx, q.InsertLiveBlobParams{
		Sha256: sp.SHA256.Bytes(), Sha1: sp.SHA1[:], Md5: sp.MD5[:], Sha512: sp.SHA512[:], Size: sp.Size,
	})
	return n > 0, err
}

func (pgRows) Exists(ctx context.Context, tx meta.Tx, d Digest) (bool, error) {
	qs, err := queries(tx)
	if err != nil {
		return false, err
	}
	rows, err := qs.BlobExists(ctx, d.Bytes())
	return len(rows) > 0, err
}

func (pgRows) Enqueue(ctx context.Context, tx meta.Tx, d Digest) error {
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	return qs.EnqueueBlobForGC(ctx, d.Bytes())
}

func (pgRows) Row(ctx context.Context, tx meta.Tx, d Digest) (Row, bool, error) {
	qs, err := queries(tx)
	if err != nil {
		return Row{}, false, err
	}
	rows, err := qs.GetBlob(ctx, d.Bytes())
	if err != nil || len(rows) == 0 {
		return Row{}, false, err
	}
	g := rows[0]
	r := Row{State: g.State, ContentStatus: g.ContentStatus, CreatedAt: g.CreatedAt}
	copy(r.SHA256[:], g.Sha256)
	copy(r.SHA1[:], g.Sha1)
	copy(r.MD5[:], g.Md5)
	copy(r.SHA512[:], g.Sha512)
	r.Size = g.Size
	return r, true, nil
}

func (pgRows) ClaimRecheck(ctx context.Context, tx meta.Tx, d Digest, interval time.Duration) (bool, error) {
	qs, err := queries(tx)
	if err != nil {
		return false, err
	}
	n, err := qs.ClaimMissingRecheck(ctx, q.ClaimMissingRecheckParams{Sha256: d.Bytes(), IntervalSeconds: interval.Seconds()})
	return n > 0, err
}

func (pgRows) Copies(ctx context.Context, tx meta.Tx, d Digest) ([]Copy, error) {
	qs, err := queries(tx)
	if err != nil {
		return nil, err
	}
	rows, err := qs.BlobCopies(ctx, d.Bytes())
	if err != nil {
		return nil, err
	}
	out := make([]Copy, 0, len(rows))
	for _, r := range rows {
		c := Copy{
			Connection: r.ConnectionID, ObjectKey: r.ObjectKey, StoredSize: r.StoredSize, StorageKeyID: r.StorageKeyID, State: r.State,
			Retired: r.Retired,
		}
		// a NULL timestamp is the zero time (this package imports no driver type, R-ARCH-002)
		if r.VerifiedAt.Valid {
			c.VerifiedAt = r.VerifiedAt.Time
		}
		if r.CheckedAt.Valid {
			c.CheckedAt = r.CheckedAt.Time
		}
		out = append(out, c)
	}
	return out, nil
}

func (pgRows) LockForMark(ctx context.Context, tx meta.Tx, d Digest) (string, bool, error) {
	qs, err := queries(tx)
	if err != nil {
		return "", false, err
	}
	rows, err := qs.LockBlobForMark(ctx, d.Bytes())
	if err != nil || len(rows) == 0 {
		return "", false, err
	}
	return rows[0], true, nil
}

func (pgRows) PutCopy(ctx context.Context, tx meta.Tx, d Digest, w CopyWrite) error {
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	return qs.PutBlobCopy(ctx, q.PutBlobCopyParams{
		Sha256: d.Bytes(), ConnectionID: w.Connection, ObjectKey: w.Key, StoredSize: w.StoredSize, StorageKeyID: w.StorageKeyID, Verified: w.Verified,
	})
}

func (pgRows) SetCopyState(ctx context.Context, tx meta.Tx, d Digest, conn, from, to string) (bool, error) {
	qs, err := queries(tx)
	if err != nil {
		return false, err
	}
	n, err := qs.SetBlobCopyState(ctx, q.SetBlobCopyStateParams{Sha256: d.Bytes(), ConnectionID: conn, FromState: from, ToState: to})
	return n > 0, err
}

func (pgRows) StampVerified(ctx context.Context, tx meta.Tx, d Digest, conn string, interval time.Duration) error {
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	return qs.StampBlobCopyVerified(ctx, q.StampBlobCopyVerifiedParams{Sha256: d.Bytes(), ConnectionID: conn, IntervalSeconds: interval.Seconds()})
}

func (pgRows) QueueCopy(ctx context.Context, tx meta.Tx, conn string, d Digest) error {
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	return qs.QueueBlobCopy(ctx, q.QueueBlobCopyParams{ConnectionID: conn, Sha256: d.Bytes()})
}

func (pgRows) SetContentStatus(ctx context.Context, tx meta.Tx, d Digest, status string) (bool, error) {
	qs, err := queries(tx)
	if err != nil {
		return false, err
	}
	n, err := qs.SetBlobContentStatus(ctx, q.SetBlobContentStatusParams{Sha256: d.Bytes(), Status: status})
	return n > 0, err
}
