package fills

import (
	"time"

	"github.com/google/uuid"
)

// The bounds of R-OPS-050 and R-OPS-051: the running downloads one replica publishes, the finished downloads the list keeps. A download is
// a unit: what a person asked for (R-OPS-049), whatever number of files it took.
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
	// MergeGap is the most time between two records of one client's unit key (on two replicas, or one after the other on one) that
	// are still one download (R-OPS-049, Q-OPS-429): a pull whose layers several replicas served is one row, the same image pulled
	// again an hour later is a second.
	MergeGap = 10 * time.Second
	// maxResolved is the most units a publication asks the version of (Resolver).
	maxResolved = 50
)

// The outcomes of a finished download (R-OPS-049).
const (
	OutcomeCompleted   = "completed"
	OutcomeFailed      = "failed"
	OutcomeCancelled   = "cancelled"
	OutcomeInterrupted = "interrupted"
)

// Entry is one running download as a replica publishes it: a member of upstream_download_live.downloads. The JSON names are short.
type Entry struct {
	ID string `json:"id"`
	// Unit is the unit's key (proxy.DownloadStart.Unit, "p|<id>" for a unit of one file): the read merges the entries and rows of one
	// key across replicas.
	Unit    string `json:"unit"`
	RepoID  int64  `json:"repo"`
	Name    string `json:"name"`
	Version string `json:"version"`
	// Path is the path of the unit's first file.
	Path string `json:"path"`
	// Parts is the number of the unit's files so far.
	Parts int `json:"parts"`
	// Bytes are the bytes read from the upstream's bodies of the unit's files at the publication.
	Bytes int64 `json:"bytes"`
	// Total is the unit's size where it is known (R-OPS-049 "The facts of a unit"), nil otherwise.
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
	// resolve says the unit's version is still to be resolved (Resolver); not published.
	resolve bool
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

// Finished is an ended download: a row of upstream_download_finished, one unit of one replica. The JSON names are the row's columns, so
// a row read as to_jsonb and a row written through jsonb_to_recordset have one shape.
type Finished struct {
	ID           string    `json:"id"`
	Unit         string    `json:"unit"`
	RepoID       int64     `json:"repo_id"`
	Name         string    `json:"name"`
	Version      string    `json:"version"`
	Path         string    `json:"path"`
	Parts        int       `json:"parts"`
	Bytes        int64     `json:"bytes"`
	Total        *int64    `json:"total"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	DurationMs   int64     `json:"duration_ms"`
	Outcome      string    `json:"outcome"`
	FailureClass string    `json:"failure_class,omitempty"`
	FailureCode  string    `json:"failure_code,omitempty"`
	// FailurePath is the path of the file whose failure failed the unit.
	FailurePath string  `json:"failure_path,omitempty"`
	Origin      string  `json:"origin"`
	QueueOrigin string  `json:"queue_origin,omitempty"`
	QueueRow    *string `json:"queue_row"`
	By          *string `json:"by_principal"`
	Replica     string  `json:"replica_name"`
	// resolve says the unit's version is still to be resolved (Resolver); not stored.
	resolve bool
}

// RepoName is a repository as a read resolves it by id.
type RepoName struct {
	Project, Key, Format string
}

// View is what a read of the API gets: every live row with its age, the finished rows of the newest units, and the names of the
// repositories and principals they name, resolved at the read. A repository or principal that is gone is absent from its map.
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
			ID: e.ID, Unit: unitKey(e.Unit, e.ID), RepoID: e.RepoID, Name: e.Name, Version: e.Version, Path: e.Path, Parts: max(e.Parts, 1),
			Bytes: e.Bytes, Total: e.Total, StartedAt: e.StartedAt, FinishedAt: writtenAt, DurationMs: max(e.ElapsedMs, 0),
			Outcome: OutcomeInterrupted, Origin: e.Origin, QueueOrigin: e.QueueOrigin, Replica: replica,
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

// unitKey is a unit's key; a unit of one file without a key of its own is keyed by its id.
func unitKey(key, id string) string {
	if key == "" {
		return "p|" + id
	}
	return key
}

// validUUID reports whether s is a UUID: a principal or a queue row that is not one is not recorded, so that a row of the insert never
// fails its cast.
func validUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}
