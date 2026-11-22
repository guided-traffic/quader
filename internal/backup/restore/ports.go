package restore

import (
	"context"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/backup"
	"github.com/guided-traffic/quader/internal/backup/format"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// Session is the restore's one database connection, which holds the migration lock (R-BAK-021 step 1).
// *postgres.RestoreSession implements it.
type Session interface {
	meta.Runner
	Read(ctx context.Context, fn func(q.DBTX) error) error
	DBTX() q.DBTX

	Lock(ctx context.Context, wait time.Duration) error
	Close(ctx context.Context)
	Schema() string
	Inspect(ctx context.Context) (postgres.RestoreInspection, error)
	ServerCheck(ctx context.Context) error
	Steps() ([]postgres.RestoreStep, error)
	SetAnchor(ctx context.Context, backupID string) error
	MigrateTo(ctx context.Context, m postgres.RestoreMigration) (postgres.Result, error)
	Load(ctx context.Context, plan postgres.LoadPlan, src postgres.LoadSource) (postgres.LoadResult, error)
	Fix(ctx context.Context, fn func(meta.Tx) error) error
	Finish(ctx context.Context, fn func(meta.Tx) error) error
	EnsureAuditPartitions(ctx context.Context) error
	PendingSteps(ctx context.Context) ([]postgres.StepInfo, error)
	LiveBlobs(ctx context.Context, fn func(digest []byte) error) error
	SecretCanaries(ctx context.Context) (map[string]string, error)
}

var _ Session = (*postgres.RestoreSession)(nil)

// BlobStore is the new installation's object store as the restore uses it: the sink of step 3 plus the listing of check 5
// and the probe of check 7. internal/blob/s3.BlobSink implements it.
type BlobStore interface {
	backup.BlobSink
	// FirstForeign returns the first key below the blob prefix that is neither a probe object nor the connection's marker
	// <prefix>/_connection, and with allowBlobs not below sha256/ either; empty when there is none (R-BAK-020 check 5).
	FirstForeign(ctx context.Context, allowBlobs bool) (string, error)
	// Marker reads the connection's marker <prefix>/_connection: found is false when there is none, an object there that is
	// no marker is blob.ErrNotMarker (storage.md R-STOR-051, R-BAK-030 check 5).
	Marker(ctx context.Context) (m blob.Marker, found bool, err error)
	// Probe writes and deletes <prefix>/_probe/<name> (R-BAK-020 check 7).
	Probe(ctx context.Context, name string) error
}

// Connection is one connection of the restoring installation's configuration as the restore uses it (R-BAK-030): its id, its
// bucket and prefix as resolved from the configuration (the fix-up records them), its drain flag and its store. The first
// connection without drain is the sink of step 3; every connection's prefix is checked (check 5) and probed (check 7).
type Connection struct {
	ID             string
	Bucket, Prefix string
	Drain          bool
	Store          BlobStore
}

// JournalEnv is what the journal needs to be built for one restore.
type JournalEnv struct {
	// Runner is the restore's session: the replay handlers apply in transactions on it, as the owner.
	Runner meta.Runner
	// Sources are manifest.source.connections, below whose prefixes the journal lives (R-BAK-030).
	Sources []format.ConnectionRef
	// CredentialsFile is --journal-credentials-file, empty to use the blob-store credentials.
	CredentialsFile string
	// Recorder writes the audit events of the replay, in transactions of the restore's session.
	Recorder audit.Recorder
}

// Journal is the revocation journal of the source as the restore needs it.
type Journal interface {
	// Reachable lists the journal prefix with the credentials of the restore (R-BAK-020 check 8).
	Reachable(ctx context.Context) error
	journal.Replayer
}
