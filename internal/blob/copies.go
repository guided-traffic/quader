package blob

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
)

// The states of a copy (R-STOR-052, Q-STOR-227). A connection without a copy row for a blob does not hold it (a gap the
// reconciliation fills).
const (
	CopyPresent  = "present"  // written and confirmed
	CopyMissing  = "missing"  // the store answered "no such object" for it
	CopyCorrupt  = "corrupt"  // its size differs from stored_size, or a read to its end had another digest
	CopyDeleting = "deleting" // collection or a drain claimed it for deletion
)

// The content states of a blob (R-STOR-003).
const (
	StatusOK      = "ok"
	StatusMissing = "missing"
	StatusCorrupt = "corrupt"
)

// Copy is one copy of a blob: its object on one connection and what is known about it (a row of blob_copies, R-STOR-052).
type Copy struct {
	// Connection is the id of the connection the object lies on.
	Connection string
	// ObjectKey is the object's name on that connection; every reader takes it from here (R-STOR-062).
	ObjectKey string
	// StoredSize is the object's size as stored; every size check of a stored object compares with it (stage 1: the blob's
	// size).
	StoredSize int64
	// StorageKeyID is the storage key that wrapped the object's data key; empty in stage 1.
	StorageKeyID string
	State        string
	// VerifiedAt is when the whole stored object was last shown to be the blob; zero when never.
	VerifiedAt time.Time
	// CheckedAt is when the store last answered for it; zero when never.
	CheckedAt time.Time
	// Retired reports that the copy's connection is retired: its copy counts for nothing (R-STOR-051).
	Retired bool
}

// DeriveContentStatus is a blob's content state as its copies give it (R-STOR-052, Q-STOR-226): ok while one copy on a
// connection that is not retired is present; otherwise corrupt when such a copy is corrupt, else missing. A blob without a
// copy row is missing. A copy on a detached connection counts: detached is not retired.
func DeriveContentStatus(copies []Copy) string {
	corrupt := false
	for _, c := range copies {
		if c.Retired {
			continue
		}
		switch c.State {
		case CopyPresent:
			return StatusOK
		case CopyCorrupt:
			corrupt = true
		}
	}
	if corrupt {
		return StatusCorrupt
	}
	return StatusMissing
}

// CopyWrite is the copy row of an object a commit wrote (R-STOR-053): the connection, the object's name, its stored size and
// whether the write showed the whole object to be the blob.
type CopyWrite struct {
	Connection string
	Key        string
	StoredSize int64
	Verified   bool
}

// copyWrite is an object a commit wrote on one connection, as its copy row records it.
type copyWrite struct {
	conn       *Connection
	key        string
	storedSize int64
	verified   bool
	at         time.Time // the end of the write
}

// ParseCopies reads the copy rows of a blob that a statement of another package aggregated as one JSON array, the item
// lookup of a download (files.sql GetLiveItem, Q-STOR-275): one object per copy with the keys connection, objectKey,
// storedSize, storageKeyId, state, verifiedAt and checkedAt (null when never) and retired. An empty array is a blob without
// copies.
func ParseCopies(data string) ([]Copy, error) {
	var rows []struct {
		Connection   string     `json:"connection"`
		ObjectKey    string     `json:"objectKey"`
		StoredSize   int64      `json:"storedSize"`
		StorageKeyID string     `json:"storageKeyId"`
		State        string     `json:"state"`
		VerifiedAt   *time.Time `json:"verifiedAt"`
		CheckedAt    *time.Time `json:"checkedAt"`
		Retired      bool       `json:"retired"`
	}
	if err := json.Unmarshal([]byte(data), &rows); err != nil {
		return nil, fmt.Errorf("blob: the copies of an item lookup: %w", err)
	}
	out := make([]Copy, 0, len(rows))
	for _, r := range rows {
		c := Copy{Connection: r.Connection, ObjectKey: r.ObjectKey, StoredSize: r.StoredSize, StorageKeyID: r.StorageKeyID, State: r.State, Retired: r.Retired}
		if r.VerifiedAt != nil {
			c.VerifiedAt = *r.VerifiedAt
		}
		if r.CheckedAt != nil {
			c.CheckedAt = *r.CheckedAt
		}
		out = append(out, c)
	}
	return out, nil
}

// Copies returns the copy rows of a blob (an empty list when it has none).
func (s *Store) Copies(ctx context.Context, d Digest) ([]Copy, error) {
	var out []Copy
	err := s.db.InTx(ctx, func(tx meta.Tx) error {
		var err error
		out, err = s.rows.Copies(ctx, tx, d)
		return err
	})
	return out, err
}

// Servable is the copies a read of the blob would try now, in its order (R-STOR-055, R-STOR-062): present, on a connection
// this replica configures, whose circuit is closed on this replica, a draining connection's last. Each carries its
// connection's id, the object's name and its stored size; [Store.Connections] gives the connection, its Encrypted, its
// LinkCheck and its guarded Presign. Stage 3 issues a link only for a copy of this list (Q-STOR-245).
func (s *Store) Servable(ctx context.Context, d Digest) ([]Copy, error) {
	copies, err := s.Copies(ctx, d)
	if err != nil {
		return nil, err
	}
	var out []Copy
	for _, cp := range s.conns.readOrder(copies) {
		if cp.State != CopyPresent {
			continue
		}
		c, _ := s.conns.Get(cp.Connection)
		if _, rejecting := c.Rejecting(); rejecting {
			continue
		}
		out = append(out, cp)
	}
	return out, nil
}

// damage is a change of a blob's content state that a mark caused, reported after its transaction committed.
type damage struct {
	blobChanged bool
	status      string
}

// rederive sets the blob's content state to what its copies give (R-STOR-052) inside tx, which holds the blob's row lock
// (LockForMark), and reports whether it changed and to what.
func (s *Store) rederive(ctx context.Context, tx meta.Tx, d Digest) (damage, error) {
	copies, err := s.rows.Copies(ctx, tx, d)
	if err != nil {
		return damage{}, err
	}
	status := DeriveContentStatus(copies)
	changed, err := s.rows.SetContentStatus(ctx, tx, d, status)
	return damage{blobChanged: changed, status: status}, err
}

// report counts and logs a change of a blob's content state and calls the damage hook (the audit event of a detection,
// R-STOR-027). A copy's damage that leaves the blob ok is a metric and a log line only.
func (s *Store) report(ctx context.Context, d Digest, dm damage) {
	if !dm.blobChanged {
		return
	}
	if dm.status == StatusOK {
		s.log.Info("a blob has a present copy again", "sha256", d.Hex())
		return
	}
	s.metrics.damage(dm.status)
	msg := "stored object failed verification"
	if dm.status == StatusMissing {
		msg = "no connection holds the object of a blob"
	}
	s.log.Error(msg, "sha256", d.Hex(), "status", dm.status)
	if s.onDamage != nil {
		s.onDamage(ctx, d, dm.status)
	}
}

// markCopy records evidence about one copy (R-STOR-055): a present copy becomes missing or corrupt, the connection's repair is
// queued (R-STOR-056), and the blob's content state follows from its copies. Only a live blob's present copy changes, so a
// repair or a concurrent detection is not overwritten. It reports whether the copy changed.
func (s *Store) markCopy(ctx context.Context, d Digest, conn, status string) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	var changed bool
	var dm damage
	err := s.db.InTx(ctx, func(tx meta.Tx) error {
		changed, dm = false, damage{}
		state, found, err := s.rows.LockForMark(ctx, tx, d)
		if err != nil || !found || state != "live" {
			return err
		}
		if changed, err = s.rows.SetCopyState(ctx, tx, d, conn, CopyPresent, status); err != nil || !changed {
			return err
		}
		if err := s.rows.QueueCopy(ctx, tx, conn, d); err != nil {
			return err
		}
		dm, err = s.rederive(ctx, tx, d)
		return err
	})
	if err != nil {
		s.log.Error("could not mark a copy damaged", "sha256", d.Hex(), "connection", conn, "status", status, "error", err)
		return false
	}
	if changed {
		s.metrics.copyDamaged(conn, status)
		s.log.Warn("a copy of a blob is damaged", "sha256", d.Hex(), "connection", conn, "status", status)
		s.report(ctx, d, dm)
	}
	return changed
}

// copyBack sets a copy marked missing present again after the store answered for its object with the copy's stored size
// (the look after a mark, Q-STOR-151, and the recheck of a missing blob, Q-STOR-153), and lets the blob's state follow. It
// reports whether the copy changed.
func (s *Store) copyBack(ctx context.Context, d Digest, conn string) bool {
	var changed bool
	var dm damage
	err := s.db.InTx(ctx, func(tx meta.Tx) error {
		changed, dm = false, damage{}
		state, found, err := s.rows.LockForMark(ctx, tx, d)
		if err != nil || !found || state != "live" {
			return err
		}
		if changed, err = s.rows.SetCopyState(ctx, tx, d, conn, CopyMissing, CopyPresent); err != nil || !changed {
			return err
		}
		dm, err = s.rederive(ctx, tx, d)
		return err
	})
	if err != nil {
		s.log.Error("could not set a copy present whose object is there again", "sha256", d.Hex(), "connection", conn, "error", err)
		return false
	}
	if changed {
		s.log.Info("the object of a copy that was missing is there again", "sha256", d.Hex(), "connection", conn)
		s.report(ctx, d, dm)
	}
	return changed
}

// stampVerified records that a full read through Quader matched the digest: the copy's verified_at and checked_at move
// when verified_at is older than a day, so the downloads of a hot blob serve as its scrub at one write per copy and day
// (R-STOR-055, Q-STOR-233).
func (s *Store) stampVerified(ctx context.Context, d Digest, cp Copy) {
	if !cp.VerifiedAt.IsZero() && s.now().Sub(cp.VerifiedAt) < VerifiedStampInterval {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := s.db.InTx(ctx, func(tx meta.Tx) error {
		return s.rows.StampVerified(ctx, tx, d, cp.Connection, VerifiedStampInterval)
	}); err != nil {
		s.log.Warn("could not stamp a verified copy", "sha256", d.Hex(), "connection", cp.Connection, "error", err)
	}
}

// VerifiedStampInterval is how old a copy's verified_at must be before a verified download stamps it again (Q-STOR-233).
const VerifiedStampInterval = 24 * time.Hour

// recordWrites inserts the copy rows of the objects a commit wrote and a queue row for every other connection of this
// replica that holds every blob (R-STOR-053), inside the commit's link transaction.
func (s *Store) recordWrites(ctx context.Context, tx meta.Tx, d Digest, writes []copyWrite) error {
	wrote := map[string]bool{}
	for _, w := range writes {
		if err := s.rows.PutCopy(ctx, tx, d, CopyWrite{Connection: w.conn.ID(), Key: w.key, StoredSize: w.storedSize, Verified: w.verified}); err != nil {
			return err
		}
		wrote[w.conn.ID()] = true
	}
	for _, c := range s.conns.Targets() {
		if wrote[c.ID()] {
			continue
		}
		if err := s.rows.QueueCopy(ctx, tx, c.ID(), d); err != nil {
			return err
		}
	}
	return nil
}
