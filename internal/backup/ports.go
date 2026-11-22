package backup

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/guided-traffic/quader/internal/backup/format"
	"github.com/guided-traffic/quader/internal/backup/tables"
	"github.com/guided-traffic/quader/internal/backup/target"
)

// The ports below are the seams of the create pipeline: the pipeline is written against them and tested with fakes, and
// cmd/quader joins the implementations (PostgreSQL in internal/meta/postgres, S3 clients built by internal/blob/s3).

// SnapshotOptions are the parameters of the database phase (R-BAK-005).
type SnapshotOptions struct {
	// Registry classifies the tables; nil means tables.Default().
	Registry *tables.Registry
	// ExcludeRemoteCaches leaves out of the referenced set the blobs that only remote-repository caches reference
	// (R-BAK-008).
	ExcludeRemoteCaches bool
	// Parallel is backup.database.parallel: export connections besides the snapshot connection.
	Parallel int
	// LockTimeout is backup.database.lock_timeout, MigrationWait backup.database.migration_wait.
	LockTimeout, MigrationWait time.Duration
}

// TableInfo is one table of the catalog list read inside the snapshot (R-BAK-004, R-BAK-005). Columns are the exported
// column list: stored generated columns are left out, identity columns are in.
type TableInfo struct {
	Name    string
	Class   tables.Class
	Columns [][2]string
	// Registered is false for a table the registry does not know; it is exported as data and reported under
	// unregisteredTables (R-BAK-004).
	Registered bool
	// EstimatedBytes orders the export, largest first.
	EstimatedBytes int64
}

// SnapshotInfo is what the run reads once inside the snapshot.
type SnapshotInfo struct {
	At            time.Time
	ServerVersion string
	Schema        format.SchemaInfo
	// KeyIDs are the key IDs of the sealed values and canary rows (R-BAK-003).
	KeyIDs []string
	// Tables are the tables to export (class data and blobs), the largest first.
	Tables []TableInfo
}

// Snapshotter opens the snapshot of R-BAK-005: it takes the shared migration lock, commits the 'all' hold through the
// callback, locks every table, exports the snapshot and returns it. The implementation is
// internal/meta/postgres/backup_snapshot.go (the create work package).
type Snapshotter interface {
	// Open runs steps 1 and 2 of R-BAK-007 and R-BAK-005: the shared migration lock, then onLocked (which sets hold 'all'
	// and commits it), then the table locks and the snapshot. It exits through the codes of R-BAK-005: a standby is
	// ExitUsage, a lock or migration wait that timed out ExitTemporary.
	Open(ctx context.Context, opts SnapshotOptions, onLocked func(context.Context) error) (Snapshot, error)
}

// Snapshot is the open snapshot. All its reads see the same state of the database.
type Snapshot interface {
	Info() SnapshotInfo
	// Referenced streams the digests and sizes of every blobs row referenced by at least one row of a data table, ordered
	// by digest (R-BAK-006). It fails with ErrInvariantViolation when a referenced row is in state deleting.
	Referenced(ctx context.Context) (format.BlobIterator, error)
	// ExportTable streams COPY (SELECT ...) TO STDOUT of one table, text format, to w, and returns the row count
	// (R-BAK-005 step 4). It runs on one of the parallel export connections.
	ExportTable(ctx context.Context, t TableInfo, w io.Writer) (rows int64, err error)
	// Sequences reads last_value and is_called of every sequence of the schema (R-BAK-005 step 6).
	Sequences(ctx context.Context) ([]format.Sequence, error)
	// ReleaseMigrationLock releases the shared migration lock once the last table is exported; the snapshot stays open
	// until Close.
	ReleaseMigrationLock(ctx context.Context) error
	// Close ends the snapshot and closes the connections.
	Close(ctx context.Context) error
}

// SystemFacts are the identity facts a backup command reads from system_state (R-BAK-002, R-BAK-009, R-BAK-022).
type SystemFacts struct {
	InstallationID string
	Incarnation    string
	// RestoredFrom is the origin of a restored installation, nil otherwise.
	RestoredFrom *RestoredFrom
	// RestoreInProgress is the marker of a restore that has not finished; SchemaBehind is true when the schema is below
	// the binary's required step. Either makes create and delete exit 75.
	RestoreInProgress bool
	SchemaBehind      bool
}

// RestoredFrom is system_state restored_from (R-BAK-022): the lineage the installation was restored from.
type RestoredFrom struct {
	InstallationID string    `json:"installationId"`
	Incarnation    string    `json:"incarnation"`
	BackupID       string    `json:"backupId"`
	SnapshotAt     time.Time `json:"snapshotAt"`
}

// Facts reads the system facts on a READ COMMITTED connection.
type Facts interface {
	SystemFacts(ctx context.Context) (SystemFacts, error)
}

// ErrBlobNotFound is a blob object that a connection of the source does not have.
var ErrBlobNotFound = errors.New("backup: blob object not found")

// ErrCopyUnavailable wraps a failure of a copy's connection that says nothing about the object: a timeout, a 5xx, throttling,
// an open circuit (R-BAK-030). The run tries the next copy; a blob whose every copy failed and one of them this way is no source
// damage, and the run ends temporarily instead (Q-BAK-71).
var ErrCopyUnavailable = errors.New("backup: the connection of the copy did not answer")

// SourceCopy is one copy of a blob the backup can read (R-BAK-030): a present copy of blob_copies on a connection of the
// backup's own configuration.
type SourceCopy struct {
	// Connection is the connection's id, Key the object's name there (blob_copies.object_key), StoredSize its bytes as stored.
	Connection string
	Key        string
	StoredSize int64
	// Verified is true when the copy's verified_at is set: the whole stored object was shown to be the blob (R-STOR-052).
	Verified bool
	// ServerSide is true when the connection's normalised endpoint is the target's, so a server-side copy can read it
	// (R-BAK-006).
	ServerSide bool
	// Position is the connection's place in the configuration (1 for the first) and Drain its drain flag: the read order of
	// storage.md R-STOR-055.
	Position int
	Drain    bool
}

// BlobSource reads the copies of the source's blobs (R-BAK-006, R-BAK-030), with the read credentials of the backup account
// (backup.source.connections, or each connection's own). Its implementation is [StoreSource].
type BlobSource interface {
	// Copies returns the present copies of each digest on the connections of the backup's configuration, read when the copy
	// phase reaches them (Q-BAK-70). A digest without such a copy is absent from the map.
	Copies(ctx context.Context, digests []format.Digest) (map[format.Digest][]SourceCopy, error)
	// Size is the size of a copy's object (HeadObject), or ErrBlobNotFound; a failure of the connection wraps ErrCopyUnavailable.
	Size(ctx context.Context, c SourceCopy) (int64, error)
	// Open streams length bytes of a copy from offset (a ranged GetObject); length < 0 reads to the end. A missing object is
	// ErrBlobNotFound, a failure of the connection, also while the body is read, wraps ErrCopyUnavailable.
	Open(ctx context.Context, c SourceCopy, offset, length int64) (io.ReadCloser, error)
	// Ref names a copy's object for a server-side copy.
	Ref(c SourceCopy) target.ObjectRef
}

// BlobStateReader reads a blob row at READ COMMITTED, the check that tells source damage from a lost hold (R-BAK-006
// "Source damage").
type BlobStateReader interface {
	// BlobState returns the row's state ("live", "deleting") and content_status, or found == false when the row is gone.
	BlobState(ctx context.Context, d format.Digest) (state, contentStatus string, found bool, err error)
}

// BlobSink writes the first connection without drain of the new installation during a restore (R-BAK-021 step 3, R-BAK-030).
type BlobSink interface {
	// Has reports whether an object of the digest exists with the given size.
	Has(ctx context.Context, d format.Digest, size int64) (bool, error)
	// Put streams the object. The implementation hashes the stream and aborts when it differs from d.
	Put(ctx context.Context, d format.Digest, size int64, r io.Reader) error
	// CopyFrom copies from a pool object server-side. It returns target.ErrServerCopyUnavailable when it cannot.
	CopyFrom(ctx context.Context, src target.ObjectRef, d format.Digest, size int64) error
}
