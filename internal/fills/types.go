package fills

import (
	"time"

	"github.com/google/uuid"
)

// The bounds of R-OPS-050 and R-OPS-051: the running downloads one replica publishes, the finished downloads the list keeps.
const (
	// MaxPublished is the most running downloads one replica writes into its live row, the longest running first; the rest are counted.
	MaxPublished = 200
	// MaxFinished is the owner's number: the last 100 finished downloads (not configurable).
	MaxFinished = 100
	// FreshIntervals is how many publish intervals old a live row may be for a reader to take it.
	FreshIntervals = 3
	// SweepIntervals is how many publish intervals old a live row is when the sweep ends its downloads interrupted; at least
	// MinSweepAge.
	SweepIntervals = 10
	// MinSweepAge is the least age of a swept row, and the least time between two sweeps of a replica.
	MinSweepAge = 30 * time.Second
)

// The outcomes of a finished download (R-OPS-049).
const (
	OutcomeCompleted   = "completed"
	OutcomeFailed      = "failed"
	OutcomeCancelled   = "cancelled"
	OutcomeInterrupted = "interrupted"
)

// Entry is one running download as a replica publishes it: a member of upstream_download_live.downloads. The JSON names are short and
// read by replicas of other releases; a field is added, never renamed.
type Entry struct {
	ID      string `json:"id"`
	RepoID  int64  `json:"repo"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Path    string `json:"path"`
	// Bytes are the bytes read from the upstream's body at the publication.
	Bytes int64 `json:"bytes"`
	// Total is the announced length, nil when the upstream announced none.
	Total *int64 `json:"total,omitempty"`
	// Speed is the bytes per second of the window (Q-OPS-421), nil in the first second.
	Speed     *int64    `json:"speed,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	// ElapsedMs is the time since the begin at the publication, on the replica's monotonic clock.
	ElapsedMs   int64  `json:"elapsedMs"`
	Origin      string `json:"origin"`
	QueueOrigin string `json:"queueOrigin,omitempty"`
	QueueRow    string `json:"queueRow,omitempty"`
	// By is the principal's id, "" for none.
	By string `json:"by,omitempty"`
}

// Live is one replica's live row.
type Live struct {
	InstanceUID string
	Replica     string
	WrittenAt   time.Time
	// Age is how old the write is on PostgreSQL's clock at the read.
	Age       time.Duration
	Omitted   int
	Downloads []Entry
}

// Finished is an ended download: a row of upstream_download_finished. The JSON names are the row's columns, so a row read as to_jsonb
// and a row written through jsonb_to_recordset have one shape.
type Finished struct {
	ID           string    `json:"id"`
	RepoID       int64     `json:"repo_id"`
	Name         string    `json:"name"`
	Version      string    `json:"version"`
	Path         string    `json:"path"`
	Bytes        int64     `json:"bytes"`
	Total        *int64    `json:"total"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	DurationMs   int64     `json:"duration_ms"`
	Outcome      string    `json:"outcome"`
	FailureClass string    `json:"failure_class,omitempty"`
	FailureCode  string    `json:"failure_code,omitempty"`
	Origin       string    `json:"origin"`
	QueueOrigin  string    `json:"queue_origin,omitempty"`
	QueueRow     *string   `json:"queue_row"`
	By           *string   `json:"by_principal"`
	Replica      string    `json:"replica_name"`
}

// RepoName is a repository as a read resolves it by id.
type RepoName struct {
	Project, Key, Format string
}

// View is what a read of the API gets: every live row with its age, the newest finished downloads, and the names of the repositories and
// principals they name, resolved at the read. A repository or principal that is gone is absent from its map.
type View struct {
	// Now is PostgreSQL's clock at the read.
	Now        time.Time
	Live       []Live
	Finished   []Finished
	Repos      map[int64]RepoName
	Principals map[string]string
	// Limited are the download queues an upstream's rate limit holds at the read (remote-curation.md R-CUR-011, Q-CUR-176).
	Limited []Limited
}

// Limited is a remote repository whose download queue an upstream's rate limit holds: what waits, since when the upstream limits and
// until when no row starts, with the repository and its upstream's address resolved at the read.
type Limited struct {
	Repo RepoName
	// URL is the remote's upstream address: the name the panel says ("Docker Hub", else the host).
	URL          string
	Since, Until time.Time
	// Hinted reports that the upstream named Until.
	Hinted bool
	// Status is the status of the upstream's last limit answer, 0 when none came (a reset header of a 403 is one).
	Status int
	// Waiting are the rows of the queue that wait.
	Waiting int64
}

// Interrupted turns the downloads of a swept live row into finished downloads of the outcome interrupted (R-OPS-051): ended at the row's
// last write, with the bytes and the elapsed time of that write.
func Interrupted(replica string, writtenAt time.Time, entries []Entry) []Finished {
	out := make([]Finished, 0, len(entries))
	for _, e := range entries {
		f := Finished{
			ID: e.ID, RepoID: e.RepoID, Name: e.Name, Version: e.Version, Path: e.Path, Bytes: e.Bytes, Total: e.Total,
			StartedAt: e.StartedAt, FinishedAt: writtenAt, DurationMs: max(e.ElapsedMs, 0), Outcome: OutcomeInterrupted,
			Origin: e.Origin, QueueOrigin: e.QueueOrigin, Replica: replica,
		}
		if e.QueueRow != "" {
			row := e.QueueRow
			f.QueueRow = &row
		}
		if e.By != "" {
			by := e.By
			f.By = &by
		}
		out = append(out, f)
	}
	return out
}

// validUUID reports whether s is a UUID: a principal or a queue row that is not one is not recorded, so that a row of the insert never
// fails its cast.
func validUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}
