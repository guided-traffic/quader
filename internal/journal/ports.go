package journal

import (
	"context"
	"time"
)

// Store is the object store of one connection the journal lives on (storage.md R-STOR-059): its bucket below its prefix. The
// writer uses the runtime credential (put only); the reader uses the blob-store credential or the journal credential of a
// restore (R-BAK-015 "Journal reader"). The implementations are adapters over a connection's client: ship.ConnectionStore over
// its guard for the shipper, s3.JournalStore over an S3 client for a reader.
type Store interface {
	// Put writes an object. Writing the same key again with the same bytes is harmless (R-HA-054).
	Put(ctx context.Context, key string, body []byte) error
	// List calls fn for every key below prefix in lexicographic order. With bucket versioning and a credential that may
	// list versions, an implementation configured with versions lists the keys of delete-marked objects as well, so a
	// delete marker cannot hide an entry.
	List(ctx context.Context, prefix string, fn func(key string) error) error
	// Get reads an object.
	Get(ctx context.Context, key string) ([]byte, error)
}

// AllVersions is an optional capability of a Store listed with versions: GetAll returns every distinct content the key ever
// held that is not a delete marker, oldest first. quader only ever rewrites a key with the same bytes, so more than one
// content is somebody else's write; the reader replays each (R-HA-054).
type AllVersions interface {
	GetAll(ctx context.Context, key string) ([][]byte, error)
}

// Outbox is revocation_outbox (R-HA-054), the transactional queue the audit writer fills, with revocation_outbox_shipped, the
// connections that hold the object of a row that is not yet on every connection (storage.md R-STOR-059).
type Outbox interface {
	// Claim returns up to limit rows with an id above after (empty for the first) in id order, each with the connections that
	// already hold its object.
	Claim(ctx context.Context, after string, limit int) ([]OutboxRow, error)
	// MarkShipped records that connections hold the objects of rows that stay because another connection does not yet.
	MarkShipped(ctx context.Context, marks []Shipped) error
	// Delete removes rows whose object every connection holds; their marks go with them.
	Delete(ctx context.Context, ids []string) error
	// Pending returns the number of rows and the age of the oldest.
	Pending(ctx context.Context) (rows int64, oldest time.Duration, err error)
}

// Shipped is a connection that holds the object of an outbox row.
type Shipped struct {
	ID         string
	Connection string
}

// Shipper writes the outbox rows to the store of every connection without drain: true (R-HA-054 "Outbox and shipping",
// storage.md R-STOR-059): a worker job runs it when its signal fires and at least every backup.revocation_journal.ship_interval.
// A change never waits for it; during an outage of a connection that connection's backlog grows and ships afterwards, and the
// others do not wait for it. Ship writes the rows in id order, one object per row and connection, and deletes a row once every
// connection holds its object; it returns the number of rows deleted. It never deletes an object.
type Shipper interface {
	Ship(ctx context.Context) (int, error)
}

// Reader lists the journal entries a replay applies.
type Reader interface {
	// Entries returns the objects with at > since, sorted by at and id (R-HA-054 "Replay run"). The caller passes
	// snapshotAt minus backup.revocation_journal.replay_margin. An object that cannot be decoded is returned in Skipped
	// with its key and the reason, and the replay reports it; it never stops the replay.
	Entries(ctx context.Context, since time.Time) (entries []Object, skipped []Unreadable, err error)
}

// Unreadable is a journal object that could not be read or decoded.
type Unreadable struct {
	Key    string
	Reason string
}
