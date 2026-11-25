package queue

import (
	"context"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
)

// Store is the persistence of the queue. [PGStore] implements it over PostgreSQL; the unit tests over memory.
type Store interface {
	// InTx runs fn in a transaction of its own.
	InTx(ctx context.Context, fn func(Tx) error) error
	// Within runs fn in the caller's transaction, so that a row commits with what the caller writes (an audit event).
	Within(ctx context.Context, tx meta.Tx, fn func(Tx) error) error
	// Reads outside a transaction.
	Reader
}

// Tx is what one transaction of the queue does.
type Tx interface {
	// LockRepository serializes the starts of a repository's rows; false: the repository is gone.
	LockRepository(ctx context.Context, repoID int64) (bool, error)
	// Counts are the waiting and running rows of a repository.
	Counts(ctx context.Context, repoID int64) (waiting, running int, err error)
	// Active returns the waiting and running rows of the versions of a package.
	Active(ctx context.Context, repoID int64, name string, versions []string) ([]Row, error)
	// Insert adds a waiting row; created is false and the row the active one when the version has a waiting or running row.
	Insert(ctx context.Context, r Row) (row Row, created bool, err error)
	// Due returns at most max waiting rows of the repository whose next attempt came, in the order they run, locked.
	Due(ctx context.Context, repoID int64, now time.Time, max int) ([]Row, error)
	// Start enqueues the job of the row's next attempt and makes the row running with it.
	Start(ctx context.Context, r Row, now time.Time) (jobID string, err error)
	// Get reads one row; repoID 0 reads it in any repository.
	Get(ctx context.Context, id string, repoID int64) (Row, bool, error)
	// Settle writes the end of an attempt, fenced by its job; false: the row is not running with that job any more.
	Settle(ctx context.Context, id, jobID string, s settlement, res Result, now time.Time) (bool, error)
	// InsertBulk adds a bulk in the state expanding.
	InsertBulk(ctx context.Context, b Bulk) (Bulk, error)
	// LockBulk reads an expanding bulk for its expansion's step; false when another step holds it or it ended.
	LockBulk(ctx context.Context, id string) (Bulk, bool, error)
	// UpdateBulk writes a bulk's state, counters, cursor and failure.
	UpdateBulk(ctx context.Context, b Bulk) error
	// BulkBytes are the bytes the rows of a bulk stored.
	BulkBytes(ctx context.Context, id string) (int64, error)
	// Retry makes a failed or cancelled row wait again; a client's row becomes the caller's download (startedBy, ticket).
	Retry(ctx context.Context, id string, repoID int64, maxAttempts int, startedBy string, ticket []byte) (Row, bool, error)
	// RetryMatching makes the failed rows that match wait again, at most max; it answers their ids.
	RetryMatching(ctx context.Context, repoID int64, class, bulkID string, maxAttempts, max int) ([]string, error)
	// CancelWaiting cancels a waiting row; false when the row is not waiting.
	CancelWaiting(ctx context.Context, id string, repoID int64) (Row, bool, error)
	// CancelBulk cancels a bulk and its waiting rows.
	CancelBulk(ctx context.Context, b Bulk) error
	// Limit reads the upstream's rate limit of a repository (remote_download_limits); found is false for none.
	Limit(ctx context.Context, repoID int64) (l Limit, found bool, err error)
	// PutLimit writes the repository's limit.
	PutLimit(ctx context.Context, l Limit) error
	// ClearLimit removes the repository's limit.
	ClearLimit(ctx context.Context, repoID int64) error
	// SyncTx is what a transaction does with the syncs (R-CUR-027).
	SyncTx
	// Meta is the transaction for a statement of another package (the audit event of the act); nil in memory.
	Meta() meta.Tx
}

// Reader is the queue's reads outside a transaction.
type Reader interface {
	// List returns one page of a repository's rows (R-CUR-012).
	List(ctx context.Context, f Filter) ([]Row, error)
	// StateCounts are the rows of a repository (or of one of its bulks) by state.
	StateCounts(ctx context.Context, repoID int64, bulkID string) (Counts, error)
	// Active returns the waiting and running rows of the versions of a package.
	Active(ctx context.Context, repoID int64, name string, versions []string) ([]Row, error)
	// GetRow reads one row; repoID 0 reads it in any repository.
	GetRow(ctx context.Context, id string, repoID int64) (Row, bool, error)
	// GetBulk reads one bulk with its rows' counts by state.
	GetBulk(ctx context.Context, id string, repoID int64) (Bulk, map[string]int64, bool, error)
	// ListBulks returns one page of a repository's bulks, newest first, with the counts of each one's rows by state, by the bulk's ID
	// (Q-CUR-168).
	ListBulks(ctx context.Context, f BulkFilter) ([]Bulk, map[string]map[string]int64, error)
	// Waiting is the count of waiting rows of a repository.
	Waiting(ctx context.Context, repoID int64) (int, error)
	// Limit reads the upstream's rate limit of a repository; found is false for none (Q-CUR-170).
	Limit(ctx context.Context, repoID int64) (l Limit, found bool, err error)
	// ListSyncs returns one page of a repository's syncs, newest first, with the newest run of each by the sync's ID and the counts of
	// that run's rows by state, by the bulk's ID (R-CUR-027).
	ListSyncs(ctx context.Context, f BulkFilter) ([]Sync, map[string]Bulk, map[string]map[string]int64, error)
	// GetSync reads one sync of a repository with its newest run (found false for none) and that run's rows by state.
	GetSync(ctx context.Context, repoID int64, id string) (Sync, *Bulk, map[string]int64, bool, error)
}

// SyncTx is what one transaction of the queue does with the syncs (remote_download_syncs, R-CUR-027).
type SyncTx interface {
	// InsertSync adds a sync; created is false when the repository has a sync of the subject already.
	InsertSync(ctx context.Context, s Sync) (out Sync, created bool, err error)
	// SyncOfSubject reads the sync of a subject; found is false for none.
	SyncOfSubject(ctx context.Context, repoID int64, level Level, subject string) (Sync, bool, error)
	// SyncCount is the count of a repository's syncs.
	SyncCount(ctx context.Context, repoID int64) (int, error)
	// LockSync reads one sync of a repository, locked; found is false for none.
	LockSync(ctx context.Context, repoID int64, id string) (Sync, bool, error)
	// LockDueSync reads a sync whose next run came, locked; false when another transaction holds it, it is paused or not due.
	LockDueSync(ctx context.Context, id string, now time.Time) (Sync, bool, error)
	// SyncRan writes what a look at a sync did: now, the next run's time and the outcome.
	SyncRan(ctx context.Context, id string, now, next time.Time, outcome SyncOutcome) error
	// SetSyncPaused pauses or resumes a sync; found is false for none.
	SetSyncPaused(ctx context.Context, repoID int64, id string, paused bool) (Sync, bool, error)
	// DeleteSync removes a sync; its runs stay as bulks.
	DeleteSync(ctx context.Context, repoID int64, id string) error
	// LastRun reads the newest run of a sync; found is false for none.
	LastRun(ctx context.Context, syncID string) (Bulk, bool, error)
	// StoredBytes are the repository's folded bytes (R-STOR-003), which remote.syncs.maxBytes bounds (Q-CUR-246).
	StoredBytes(ctx context.Context, repoID int64) (int64, error)
}

// Maintenance is what the dispatch task does outside a repository's start.
type Maintenance interface {
	// Orphans sets every running row whose job ended waiting again without counting its attempt and answers their repositories.
	Orphans(ctx context.Context, now time.Time) ([]int64, error)
	// DueRepositories are the repositories with a waiting row whose next attempt came.
	DueRepositories(ctx context.Context, now time.Time) ([]int64, error)
	// ExpandingBulks are the bulks whose expansion goes on.
	ExpandingBulks(ctx context.Context) ([]Bulk, error)
	// SettleBulks ends every queued bulk whose rows all ended.
	SettleBulks(ctx context.Context) (int, error)
	// Prune removes finished rows after their retention and the bulks that have none left; one batch.
	Prune(ctx context.Context, doneBefore, failedBefore time.Time) (int64, error)
	// Gauge are the rows by repository and state.
	Gauge(ctx context.Context) ([]GaugeRow, error)
	// ClientFailure records a client's failed fetch of a file; trim bounds the client rows of the repository.
	ClientFailure(ctx context.Context, r Row, file string, trim int) error
	// PruneLimits removes the limits that ended before now of the repositories that have no waiting or running row left.
	PruneLimits(ctx context.Context, now time.Time) (int64, error)
	// DueSyncs are the syncs whose next run came, at most 100, the longest due first (R-CUR-027).
	DueSyncs(ctx context.Context, now time.Time) ([]Sync, error)
	// SyncGauge are the syncs by repository and state (active, paused).
	SyncGauge(ctx context.Context) ([]GaugeRow, error)
}

// Filter selects a page of the list (R-CUR-012).
type Filter struct {
	RepoID                             int64
	State, Class, Origin, BulkID, Name string
	// After is the last row of the previous page; nil for the first.
	After *Row
	Limit int
}

// BulkFilter selects a page of a repository's bulks (GET download-bulks).
type BulkFilter struct {
	RepoID int64
	// After is the last bulk of the previous page (its creation time and ID); nil for the first.
	After *Bulk
	Limit int
}

// Counts are a repository's rows by state (the counts of R-CUR-012).
type Counts struct {
	Waiting, Running, Failed, Done int64
}

// GaugeRow is a repository's count of rows in a state.
type GaugeRow struct {
	RepoID int64
	State  string
	N      int64
}

// rank is the list's order of a state: running, waiting, the rest.
func rank(s string) int {
	switch s {
	case "running":
		return 0
	case "waiting":
		return 1
	}
	return 2
}
