// Package queue is the download queue of a remote repository (remote-curation.md R-CUR-010 to R-CUR-013, Q-CUR-17 to Q-CUR-20): its
// rows and bulks in PostgreSQL (remote_download, remote_download_bulks, migration 00114), the order in which waiting rows run and the
// bound of running rows per repository, the retries of a failed attempt, the dispatch task remote-downloads-dispatch, the retention,
// the expansion of a bulk into rows and the rows of clients' failed fetches.
//
// A row's run is one job remote-download of one attempt (remotecuration.KindDownload, MaxAttempts 1): the queue owns the state and
// the retries, the job framework only runs the attempt (Q-CUR-18). The run itself (the plan, the fills, the failure in words) is the
// format host's (internal/formathost); it hands the end of its attempt back to [Service.Finish].
package queue

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/guided-traffic/quader/internal/remotecuration"
)

// Row is a row of a repository's download queue (remote_download, R-CUR-011), as the queue reads it.
type Row struct {
	ID      string                `json:"id"`
	RepoID  int64                 `json:"repo_id"`
	Name    string                `json:"name"`
	Version string                `json:"version"`
	Probe   *Probe                `json:"probe"`
	Origin  remotecuration.Origin `json:"origin"`
	BulkID  string                `json:"bulk_id"`
	// RequestID is the request whose examination or allow queued the row (R2b); "" for none.
	RequestID string `json:"request_id"`
	// StartedBy is the principal who started the row, its bulk or its request; "" for a probe's and a client's row.
	StartedBy string `json:"started_by"`
	// Ticket is the charge of a person's download as the run resumes it (limits.Ticket, R-CUR-010); null for system work.
	Ticket      json.RawMessage         `json:"ticket"`
	State       remotecuration.State    `json:"state"`
	Attempts    int                     `json:"attempts"`
	MaxAttempts int                     `json:"max_attempts"`
	NotBefore   *time.Time              `json:"not_before"`
	JobID       string                  `json:"job_id"`
	Occurrences int                     `json:"occurrences"`
	Files       int                     `json:"files"`
	Stored      int                     `json:"stored"`
	Present     int                     `json:"present"`
	Refused     int                     `json:"refused"`
	Bytes       int64                   `json:"bytes"`
	Outcome     remotecuration.Outcome  `json:"outcome"`
	Failure     *remotecuration.Failure `json:"failure"`
	CreatedAt   time.Time               `json:"created_at"`
	StartedAt   *time.Time              `json:"started_at"`
	UpdatedAt   time.Time               `json:"updated_at"`
	FinishedAt  *time.Time              `json:"finished_at"`
}

// Charged reports whether the row carries a person's charge.
func (r Row) Charged() bool { return len(r.Ticket) > 0 && string(r.Ticket) != "null" }

// Probe is what a run starts from when a probe or a plan named it (the column probe): the files, and the metadata entry with the
// digest of what it held (formats.Probe in plain values).
type Probe struct {
	Files    []string `json:"files"`
	Document string   `json:"document,omitempty"`
	Digest   string   `json:"digest,omitempty"`
}

// Bulk is a bulk download (remote_download_bulks, R-CUR-010): every version of a package, or a sub-section, expanded into rows.
type Bulk struct {
	ID                 string                  `json:"id"`
	RepoID             int64                   `json:"repo_id"`
	Level              Level                   `json:"level"`
	Subject            string                  `json:"subject"`
	MaxVersions        int                     `json:"max_versions"`
	MaxBytes           int64                   `json:"max_bytes"`
	IncludePrereleases bool                    `json:"include_prereleases"`
	State              BulkState               `json:"state"`
	VersionsFound      int                     `json:"versions_found"`
	VersionsQueued     int                     `json:"versions_queued"`
	Cursor             json.RawMessage         `json:"cursor"`
	Failure            *remotecuration.Failure `json:"failure"`
	StartedBy          string                  `json:"started_by"`
	CreatedAt          time.Time               `json:"created_at"`
	FinishedAt         *time.Time              `json:"finished_at"`
	// Origin is the origin of the rows the bulk queues: bulk for a person's bulk, request for the examination of a package a request
	// starts, allow for the newest version an allow downloads (Q-CUR-141).
	Origin remotecuration.Origin `json:"origin"`
	// RequestID is the request whose examination or allow the bulk is; "" for a person's bulk.
	RequestID string `json:"request_id"`
	// Ticket is the asker's charge the rows of a request's examination carry (R-CUR-015); null for system work.
	Ticket json.RawMessage `json:"ticket"`
	// FromVersion, ToVersion and ToInclusive are the bulk's range of versions (R-CUR-010, Q-CUR-190): "" is no bound on that side.
	FromVersion string `json:"from_version"`
	ToVersion   string `json:"to_version"`
	ToInclusive bool   `json:"to_inclusive"`
	// SyncID is the sync whose run the bulk is (origin sync, R-CUR-027); "" for any other bulk and for a run whose sync was removed.
	SyncID string `json:"sync_id"`
	// VersionsRefused counts the versions a sync's run left out because the rules do not deliver them (Q-CUR-243); 0 for another bulk.
	VersionsRefused int `json:"versions_refused"`
}

// Range is the bulk's range of versions.
func (b Bulk) Range() Range {
	return Range{From: b.FromVersion, To: b.ToVersion, ToInclusive: b.ToInclusive}
}

// RowOrigin is the origin of the rows the bulk queues: bulk unless a request, an allow or a sync started it.
func (b Bulk) RowOrigin() remotecuration.Origin {
	switch b.Origin {
	case remotecuration.OriginRequest, remotecuration.OriginAllow, remotecuration.OriginSync:
		return b.Origin
	}
	return remotecuration.OriginBulk
}

// Active reports whether the bulk's expansion goes on or its rows have not all ended.
func (b Bulk) Active() bool { return b.State == BulkExpanding || b.State == BulkQueued }

// Level is the level of a download ahead (R-CUR-010).
type Level string

// The levels: one version, every version of a package, a sub-section.
const (
	LevelVersion Level = "version"
	LevelPackage Level = "package"
	LevelSection Level = "section"
)

// BulkState is the state of a bulk (remote_download_bulks.state).
type BulkState string

// The states of a bulk: expanding while rows are added, queued once every row is added, then done; bounded when a bound cut the
// expansion; cancelled; failed when the expansion could not go on.
const (
	BulkExpanding BulkState = "expanding"
	BulkQueued    BulkState = "queued"
	BulkDone      BulkState = "done"
	BulkBounded   BulkState = "bounded"
	BulkCancelled BulkState = "cancelled"
	BulkFailed    BulkState = "failed"
)

// Settings are the bounds of the queue (proxy.downloads, R-CUR-020), read at every use so that a changed configuration applies at
// the next start.
type Settings struct {
	// PerRepository is the bound of running rows of one repository.
	PerRepository int
	// MaxWaiting is the bound of waiting rows of one repository.
	MaxWaiting int
	// MaxAttempts is the attempts of a row whose failure a later attempt can overcome.
	MaxAttempts int
	// DoneRetention and FailedRetention keep finished rows.
	DoneRetention, FailedRetention time.Duration
	// ClientFailures is the bound of rows of clients' failed fetches per repository.
	ClientFailures int
	// MaxSyncs is the bound of syncs per repository (R-CUR-027).
	MaxSyncs int
}

// DefaultSettings are the configuration's defaults (internal/config/remote_curation.go).
func DefaultSettings() Settings {
	return Settings{
		PerRepository: 4, MaxWaiting: 10000, MaxAttempts: 5, DoneRetention: 168 * time.Hour, FailedRetention: 720 * time.Hour,
		ClientFailures: 1000, MaxSyncs: 100,
	}
}

// NewRow is what a caller queues: one version of one package of a repository.
type NewRow struct {
	RepoID    int64
	Name      string
	Version   string
	Probe     *Probe
	Origin    remotecuration.Origin
	BulkID    string
	RequestID string
	StartedBy string
	Ticket    json.RawMessage
}

// Result is the end of one attempt as the run reports it to [Service.Finish].
type Result struct {
	// Outcome is the outcome of a run that ended without a failure: completed, bounded, refused (every file refused by a gate before
	// the fill) or skipped (the repository is gone, no longer remote, disabled or offline, or the probe's switch was turned off).
	Outcome remotecuration.Outcome
	// Files, Present and Refused are the counts of this attempt; Stored and Bytes what it stored, added to the row's.
	Files, Stored, Present, Refused int
	Bytes                           int64
	// Failure is the failure of the attempt, or the last refusal of a file of a run that went on (its step gate); nil for none.
	Failure *remotecuration.Failure
	// Failed reports that the attempt failed: Failure says why, and its class whether a later attempt is made.
	Failed bool
	// Cancelled reports that the run stopped because the row was cancelled while it ran.
	Cancelled bool
	// Examination is the version the attempt stored while the rules did not deliver it: the end of the attempt writes its examination
	// row (R-CUR-016 "What is examination"); nil for none.
	Examination *Examination
}

// Examination is a version a run of the queue stored while the delivery rules did not deliver it (R-CUR-014, R-CUR-016): its name
// and version in the format's canonical forms and the repository paths of the files the run stored or found for it, which a discard
// and the retention remove (Q-CUR-140).
type Examination struct {
	Name, Version string
	Files         []string
}

// Errors of the queue's operations; the API maps them to its codes.
var (
	// ErrFull: the repository has proxy.downloads.max_waiting_per_repository waiting rows (429 download_queue_full).
	ErrFull = errors.New("queue: the repository's queue is full")
	// ErrNotRetryable: the row is in a state a retry does not take (409 not_retryable).
	ErrNotRetryable = errors.New("queue: the row is neither failed nor cancelled")
	// ErrAlreadyQueued: the version has a waiting or running row already (409 already_queued).
	ErrAlreadyQueued = errors.New("queue: the version is queued already")
	// ErrNotFound: no such row or bulk in the repository.
	ErrNotFound = errors.New("queue: not found")
)

// FullError is [ErrFull] with the count of waiting rows it met.
type FullError struct{ Waiting int }

func (e *FullError) Error() string { return ErrFull.Error() }

// Is makes errors.Is(err, ErrFull) hold.
func (e *FullError) Is(target error) bool { return target == ErrFull }

// QueuedError is [ErrAlreadyQueued] with the row that waits or runs.
type QueuedError struct{ Row Row }

func (e *QueuedError) Error() string { return ErrAlreadyQueued.Error() }

// Is makes errors.Is(err, ErrAlreadyQueued) hold.
func (e *QueuedError) Is(target error) bool { return target == ErrAlreadyQueued }

// originRank is the order in which waiting rows of a repository run (R-CUR-011): a person's, a request's and an allow's (and a
// client's after its retry) before a bulk's, a sync's and a probe's (Q-CUR-247); then by creation. RemoteDownloadDue orders by the
// same rank.
func originRank(o remotecuration.Origin) int {
	switch o {
	case remotecuration.OriginBulk, remotecuration.OriginSync, remotecuration.OriginProbe:
		return 1
	}
	return 0
}
