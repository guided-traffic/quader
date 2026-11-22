package blob

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The connections' records in PostgreSQL (R-STOR-051, R-STOR-052): the first sight of a connection, its fixed properties,
// the retired ids, the capabilities of its start check, and forget-connection. These statements run against PostgreSQL only;
// the unit tests' simulation of the blob rows does not reach them.

// ConnectionAddedFunc is called in the transaction that recorded the first sight of a connection: the audit event
// system.storage.connection_added hangs here (R-STOR-051).
type ConnectionAddedFunc func(ctx context.Context, tx meta.Tx, conn string) error

// WithConnectionAdded sets the hook of a connection's first sight.
func WithConnectionAdded(f ConnectionAddedFunc) Option { return func(s *Store) { s.onAdded = f } }

// RecordConnections records this replica's connections before the store commits (R-STOR-051): the first replica that starts
// with a connection inserts its row with the properties that decide where and how its data lies (bucket, prefix, encrypted);
// a row that holds no copy takes the element's values; a row that holds a copy and differs in one of them refuses the start,
// as a configuration that names a retired id does. The row the migration of the connections left for the blobs that existed
// (an empty bucket, Q-STOR-250) is bound to the element of its id: its bucket and prefix become the element's and the prefix
// goes in front of its copies' keys. The error names the connection and what to do.
func (s *Store) RecordConnections(ctx context.Context) error {
	for _, c := range s.conns.List() {
		if err := s.recordConnection(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) recordConnection(ctx context.Context, c *Connection) error {
	return s.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		inserted, err := qs.InsertStorageConnection(ctx, q.InsertStorageConnectionParams{ID: c.ID(), Bucket: c.Bucket(), Prefix: c.Prefix(), Encrypted: c.Encrypted()})
		if err != nil {
			return err
		}
		rows, err := qs.LockStorageConnection(ctx, c.ID())
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return fmt.Errorf("the row of connection %s vanished while it was recorded", c.ID())
		}
		r := rows[0]
		switch {
		case r.RetiredAt.Valid:
			return refusal("%s id: the connection id %s was retired on %s; choose another id (storage.md R-STOR-051)",
				c.Element(), c.ID(), r.RetiredAt.Time.UTC().Format(time.DateOnly))
		case inserted > 0:
			s.log.Info("an object-store connection was seen for the first time", "connection", c.ID(), "bucket", c.Bucket(), "prefix", c.Prefix())
			return s.connectionAdded(ctx, tx, c.ID())
		case r.Bucket == "":
			return s.bindMigrated(ctx, tx, qs, c)
		case r.Bucket == c.Bucket() && r.Prefix == c.Prefix() && r.Encrypted == c.Encrypted():
			return nil
		}
		holds, err := qs.StorageConnectionHoldsCopy(ctx, c.ID())
		if err != nil {
			return err
		}
		if holds {
			n, err := qs.StorageConnectionCopies(ctx, c.ID())
			if err != nil {
				return err
			}
			return refusal("%s bucket, prefix and encrypted are fixed: the connection holds %d copies in %s/%s. To move the data, add a connection with another id and set drain: true on this one (storage.md R-STOR-051)",
				c.Element(), n, r.Bucket, r.Prefix)
		}
		s.log.Info("a connection without copies takes the bucket and prefix of its element", "connection", c.ID(),
			"bucket", c.Bucket(), "prefix", c.Prefix(), "was_bucket", r.Bucket, "was_prefix", r.Prefix)
		return qs.UpdateStorageConnectionLocation(ctx, q.UpdateStorageConnectionLocationParams{ID: c.ID(), Bucket: c.Bucket(), Prefix: c.Prefix(), Encrypted: c.Encrypted()})
	})
}

// bindMigrated binds the row of the migration of the connections to the element of its id (Q-STOR-250).
func (s *Store) bindMigrated(ctx context.Context, tx meta.Tx, qs *q.Queries, c *Connection) error {
	if err := qs.UpdateStorageConnectionLocation(ctx, q.UpdateStorageConnectionLocationParams{ID: c.ID(), Bucket: c.Bucket(), Prefix: c.Prefix(), Encrypted: c.Encrypted()}); err != nil {
		return err
	}
	var n int64
	if c.Prefix() != "" {
		var err error
		if n, err = qs.PrefixUnboundCopyKeys(ctx, q.PrefixUnboundCopyKeysParams{ID: c.ID(), Prefix: c.Prefix()}); err != nil {
			return err
		}
	}
	s.log.Info("the copies the migration recorded for the blobs that existed are bound to their connection", "connection", c.ID(),
		"bucket", c.Bucket(), "prefix", c.Prefix(), "copies", n)
	return s.connectionAdded(ctx, tx, c.ID())
}

func (s *Store) connectionAdded(ctx context.Context, tx meta.Tx, conn string) error {
	if s.onAdded == nil {
		return nil
	}
	return s.onAdded(ctx, tx, conn)
}

// recordCapabilities stores the capabilities' JSON in the connection's row.
func (s *Store) recordCapabilities(ctx context.Context, conn, data string) error {
	return s.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		return qs.SetStorageConnectionCapabilities(ctx, q.SetStorageConnectionCapabilitiesParams{ID: conn, Capabilities: data})
	})
}

// HoldsCopies reports whether a connection holds a copy (the warning of a missing marker, R-STOR-051).
func (s *Store) HoldsCopies(ctx context.Context, conn string) (bool, error) {
	var holds bool
	err := s.db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		holds, err = qs.StorageConnectionHoldsCopy(ctx, conn)
		return err
	})
	return holds, err
}

// ErrConnectionConfigured is forget-connection's refusal while a live replica, or the command's own configuration, has the
// connection (R-STOR-051).
var ErrConnectionConfigured = errors.New("the connection is in the configuration of a live replica")

// ErrNoConnection is forget-connection of an id that has no row.
var ErrNoConnection = errors.New("no connection has this id")

// ForgetReport is what forget-connection did (R-STOR-051).
type ForgetReport struct {
	Copies       int64 `json:"copies"`       // copy rows deleted
	Queued       int64 `json:"queued"`       // queue rows deleted
	BlobsMissing int64 `json:"blobsMissing"` // blobs that had no present copy elsewhere and became missing (or corrupt)
}

// ForgetFunc is called in the last transaction of forget-connection: the audit event system.storage.connection_forgotten.
type ForgetFunc func(ctx context.Context, tx meta.Tx, conn string, rep ForgetReport) error

// forgetPage is how many copies one transaction of forget-connection deletes.
const forgetPage = 500

// ForgetConnection gives up a connection that is gone for good (R-STOR-051): refused with ErrConnectionConfigured while a
// replica whose heartbeat is younger than staleAfter has it in its configuration; otherwise it deletes the connection's copy
// and queue rows, lets the content state of every blob it held follow from the copies that remain (a blob without a present
// copy elsewhere becomes missing, R-STOR-052), sets retired_at, so that the id is never used again, and calls event with the
// counts in the last transaction. It never deletes an object.
func ForgetConnection(ctx context.Context, db DB, conn string, staleAfter time.Duration, event ForgetFunc) (ForgetReport, error) {
	var rep ForgetReport
	err := db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.LockStorageConnection(ctx, conn)
		switch {
		case err != nil:
			return err
		case len(rows) == 0:
			return fmt.Errorf("%w: %s", ErrNoConnection, conn)
		case rows[0].RetiredAt.Valid:
			return fmt.Errorf("connection %s was retired on %s already", conn, rows[0].RetiredAt.Time.UTC().Format(time.DateOnly))
		}
		live, err := qs.StorageConnectionConfiguredLive(ctx, q.StorageConnectionConfiguredLiveParams{ID: conn, StaleSecs: staleAfter.Seconds()})
		if err != nil {
			return err
		}
		if live {
			return fmt.Errorf("%w: %s; remove it from storage.connections and roll the replicas out first", ErrConnectionConfigured, conn)
		}
		return nil
	})
	if err != nil {
		return rep, err
	}
	for {
		var n int
		err := db.InTx(ctx, func(tx meta.Tx) error {
			n = 0
			qs, err := queries(tx)
			if err != nil {
				return err
			}
			page, err := qs.ConnectionCopyPage(ctx, q.ConnectionCopyPageParams{ID: conn, PageSize: forgetPage})
			if err != nil {
				return err
			}
			n = len(page)
			for _, sha := range page {
				var d Digest
				copy(d[:], sha)
				missing, err := forgetCopy(ctx, tx, d, conn)
				if err != nil {
					return err
				}
				rep.Copies++
				if missing {
					rep.BlobsMissing++
				}
			}
			return nil
		})
		if err != nil {
			return rep, err
		}
		if n < forgetPage {
			break
		}
	}
	err = db.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		if rep.Queued, err = qs.DeleteConnectionQueue(ctx, conn); err != nil {
			return err
		}
		if err := qs.RetireStorageConnection(ctx, conn); err != nil {
			return err
		}
		if event != nil {
			return event(ctx, tx, conn, rep)
		}
		return nil
	})
	return rep, err
}

// forgetCopy deletes one copy row of a connection that is forgotten and lets the blob's content state follow; it reports
// whether the blob left ok.
func forgetCopy(ctx context.Context, tx meta.Tx, d Digest, conn string) (bool, error) {
	var rows pgRows
	state, found, err := rows.LockForMark(ctx, tx, d)
	if err != nil {
		return false, err
	}
	qs, err := queries(tx)
	if err != nil {
		return false, err
	}
	if _, err := qs.DeleteBlobCopy(ctx, q.DeleteBlobCopyParams{Sha256: d.Bytes(), ConnectionID: conn}); err != nil {
		return false, err
	}
	if !found || state != "live" {
		return false, nil
	}
	copies, err := rows.Copies(ctx, tx, d)
	if err != nil {
		return false, err
	}
	status := DeriveContentStatus(copies)
	changed, err := rows.SetContentStatus(ctx, tx, d, status)
	return changed && status != StatusOK, err
}

// Refusal is a configuration that the records of the connections refuse (R-STOR-051): a changed fixed property of a
// connection that holds copies, or a retired id. The start stops on it; any other error of RecordConnections is the
// database's and is retried.
type Refusal struct{ msg string }

func (r *Refusal) Error() string { return r.msg }

func refusal(format string, args ...any) error { return &Refusal{msg: fmt.Sprintf(format, args...)} }
