package queue

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/remotecuration"
)

// Downloads kept in sync (remote-curation.md R-CUR-027, Q-CUR-240 to Q-CUR-250): a sync is the stored definition of a bulk (the
// level package or section, the subject, the range, the newest maxVersions, maxBytes, the pre-releases) that the task
// remote-downloads-dispatch starts again as a bulk of the origin sync every remote.syncs.intervalSeconds of its repository. There is no
// second engine beside the bulk's: a run is a bulk, expanded and run as every bulk is, except that its expansion leaves out what the
// rules do not deliver and what the repository stores as version rows, and its rows' fills ask the rules (Q-CUR-243, Q-CUR-244).

// Sync is a sync of a repository (remote_download_syncs).
type Sync struct {
	ID                 string      `json:"id"`
	RepoID             int64       `json:"repo_id"`
	Level              Level       `json:"level"`
	Subject            string      `json:"subject"`
	MaxVersions        int         `json:"max_versions"`
	MaxBytes           int64       `json:"max_bytes"`
	IncludePrereleases bool        `json:"include_prereleases"`
	FromVersion        string      `json:"from_version"`
	ToVersion          string      `json:"to_version"`
	ToInclusive        bool        `json:"to_inclusive"`
	Paused             bool        `json:"paused"`
	NextRunAt          time.Time   `json:"next_run_at"`
	LastRunAt          *time.Time  `json:"last_run_at"`
	LastOutcome        SyncOutcome `json:"last_outcome"`
	CreatedBy          string      `json:"created_by"`
	CreatedAt          time.Time   `json:"created_at"`
}

// Range is the sync's range of versions.
func (s Sync) Range() Range {
	return Range{From: s.FromVersion, To: s.ToVersion, ToInclusive: s.ToInclusive}
}

// SyncActor is the actor of a sync's run the dispatch starts (R-CUR-019): system:download-sync. A person's **Run now** is the
// person's.
var SyncActor = audit.Actor{Kind: "system", Name: "download-sync"}

// SyncOutcome is what the last look at a sync did (remote_download_syncs.last_outcome).
type SyncOutcome string

// The outcomes: a run started, or why none did.
const (
	// SyncStarted: a run started.
	SyncStarted SyncOutcome = "started"
	// SyncOverlapped: the previous run's rows had not all ended; the run starts once they have.
	SyncOverlapped SyncOutcome = "overlapped"
	// SyncLimited: the upstream's rate limit held the repository's queue; the run starts at the limit's time.
	SyncLimited SyncOutcome = "limited"
	// SyncBounded: the repository's folded bytes reached remote.syncs.maxBytes.
	SyncBounded SyncOutcome = "bounded"
	// SyncDisabled: the repository is disabled, offline or no longer remote.
	SyncDisabled SyncOutcome = "disabled"
	// SyncPausedAll: remote.syncs.paused holds every sync of the repository.
	SyncPausedAll SyncOutcome = "paused"
)

// SyncState is what a sync reads of its repository at a look (R-CUR-027): its composition root reads the repository's snapshot.
type SyncState struct {
	// Found is false for a repository that is gone.
	Found bool
	// Runnable is false while the repository is disabled, offline or no longer remote.
	Runnable bool
	// Interval is remote.syncs.intervalSeconds.
	Interval time.Duration
	// Paused is remote.syncs.paused.
	Paused bool
	// MaxBytes is remote.syncs.maxBytes; 0 bounds nothing.
	MaxBytes int64
}

// SyncRepositories is what the syncs read of their repositories; the composition root implements it over the repository snapshot
// and the content's version rows.
type SyncRepositories interface {
	// SyncState reads the repository's state for a sync's look.
	SyncState(ctx context.Context, repoID int64) (SyncState, error)
	// Fixed answers which of versions of a package the repository stores as version rows, whose content does not change (Q-CUR-244);
	// the versions are compared in the format's canonical form, and the answer names them as asked.
	Fixed(ctx context.Context, repoID int64, name string, versions []string) (map[string]bool, error)
}

// DefaultSyncInterval is remote.syncs.intervalSeconds by default: an hour, as the mirror's interval (R-CUR-020).
const DefaultSyncInterval = time.Hour

// overlapAgain is when a look that found the previous run still running looks again: the next dispatch.
const overlapAgain = time.Minute

// Errors of the syncs' operations; the API maps them to its codes.
var (
	// ErrAlreadySynced: the repository has a sync of the subject (409 already_synced).
	ErrAlreadySynced = errors.New("queue: the subject is kept in sync already")
	// ErrTooManySyncs: the repository has proxy.downloads.max_syncs_per_repository syncs (422 too_many_syncs).
	ErrTooManySyncs = errors.New("queue: the repository has as many syncs as it may")
	// ErrSyncRunning: the sync's previous run has rows that wait or run (409 sync_running).
	ErrSyncRunning = errors.New("queue: the sync's previous run has not ended")
)

// SyncedError is [ErrAlreadySynced] with the sync of the subject.
type SyncedError struct{ Sync Sync }

func (e *SyncedError) Error() string { return ErrAlreadySynced.Error() }

// Is makes errors.Is(err, ErrAlreadySynced) hold.
func (e *SyncedError) Is(target error) bool { return target == ErrAlreadySynced }

// TooManySyncsError is [ErrTooManySyncs] with the bound.
type TooManySyncsError struct{ Max int }

func (e *TooManySyncsError) Error() string { return ErrTooManySyncs.Error() }

// Is makes errors.Is(err, ErrTooManySyncs) hold.
func (e *TooManySyncsError) Is(target error) bool { return target == ErrTooManySyncs }

// RunningError is [ErrSyncRunning] with the run that has not ended.
type RunningError struct{ Bulk Bulk }

func (e *RunningError) Error() string { return ErrSyncRunning.Error() }

// Is makes errors.Is(err, ErrSyncRunning) hold.
func (e *RunningError) Is(target error) bool { return target == ErrSyncRunning }

// SyncSpec is what a person sets up: the bulk's definition and who set it up.
type SyncSpec struct {
	RepoID             int64
	Level              Level
	Subject            string
	MaxVersions        int
	MaxBytes           int64
	IncludePrereleases bool
	Range              Range
	CreatedBy          string
}

// syncState reads the repository's state; without the port a sync runs every hour and nothing holds it.
func (s *Service) syncState(ctx context.Context, repoID int64) (SyncState, error) {
	if s.o.Syncs == nil {
		return SyncState{Found: true, Runnable: true, Interval: DefaultSyncInterval}, nil
	}
	st, err := s.o.Syncs.SyncState(ctx, repoID)
	if st.Interval <= 0 {
		st.Interval = DefaultSyncInterval
	}
	return st, err
}

// CreateSync sets up a sync and starts its first run at once (R-CUR-027): one sync per subject ([SyncedError]), at most the
// settings' MaxSyncs per repository ([TooManySyncsError]). tx is the caller's transaction (the audit event of the act).
func (s *Service) CreateSync(ctx context.Context, tx meta.Tx, spec SyncSpec) (Sync, Bulk, error) {
	st, err := s.syncState(ctx, spec.RepoID)
	if err != nil {
		return Sync{}, Bulk{}, err
	}
	var sy Sync
	var b Bulk
	err = s.within(ctx, tx, func(t Tx) error {
		if found, err := t.LockRepository(ctx, spec.RepoID); err != nil || !found {
			return firstErr(err, ErrNotFound)
		}
		if cur, found, err := t.SyncOfSubject(ctx, spec.RepoID, spec.Level, spec.Subject); err != nil {
			return err
		} else if found {
			return &SyncedError{Sync: cur}
		}
		n, err := t.SyncCount(ctx, spec.RepoID)
		if err != nil {
			return err
		}
		if limit := max(s.o.Settings().MaxSyncs, 1); n >= limit {
			return &TooManySyncsError{Max: limit}
		}
		now := s.o.Now()
		created, ok, err := t.InsertSync(ctx, Sync{
			RepoID: spec.RepoID, Level: spec.Level, Subject: spec.Subject, MaxVersions: spec.MaxVersions, MaxBytes: spec.MaxBytes,
			IncludePrereleases: spec.IncludePrereleases, FromVersion: spec.Range.From, ToVersion: spec.Range.To,
			ToInclusive: spec.Range.To == "" || spec.Range.ToInclusive, NextRunAt: now.Add(st.Interval), LastRunAt: &now,
			LastOutcome: SyncStarted, CreatedBy: spec.CreatedBy,
		})
		if err != nil {
			return err
		}
		if !ok {
			cur, _, err := t.SyncOfSubject(ctx, spec.RepoID, spec.Level, spec.Subject)
			return firstErr(err, &SyncedError{Sync: cur})
		}
		sy = created
		b, err = t.InsertBulk(ctx, syncBulk(sy, spec.CreatedBy))
		return err
	})
	return sy, b, err
}

// syncBulk is the bulk of a run of sy; by is the person who started it, "" for the dispatch.
func syncBulk(sy Sync, by string) Bulk {
	return Bulk{
		RepoID: sy.RepoID, Level: sy.Level, Subject: sy.Subject, MaxVersions: sy.MaxVersions, MaxBytes: sy.MaxBytes,
		IncludePrereleases: sy.IncludePrereleases, State: BulkExpanding, StartedBy: by, Origin: remotecuration.OriginSync, SyncID: sy.ID,
		FromVersion: sy.FromVersion, ToVersion: sy.ToVersion, ToInclusive: sy.ToVersion == "" || sy.ToInclusive,
	}
}

// RunSync starts a run of a sync now, for a person (R-CUR-027, **Run now**): [RunningError] while its previous run's rows wait or
// run. The next run is an interval after this one.
func (s *Service) RunSync(ctx context.Context, tx meta.Tx, repoID int64, id, by string) (Sync, Bulk, error) {
	st, err := s.syncState(ctx, repoID)
	if err != nil {
		return Sync{}, Bulk{}, err
	}
	var sy Sync
	var b Bulk
	err = s.within(ctx, tx, func(t Tx) error {
		if found, err := t.LockRepository(ctx, repoID); err != nil || !found {
			return firstErr(err, ErrNotFound)
		}
		cur, found, err := t.LockSync(ctx, repoID, id)
		if err != nil || !found {
			return firstErr(err, ErrNotFound)
		}
		if last, found, err := t.LastRun(ctx, cur.ID); err != nil {
			return err
		} else if found && last.Active() {
			return &RunningError{Bulk: last}
		}
		if b, err = t.InsertBulk(ctx, syncBulk(cur, by)); err != nil {
			return err
		}
		now := s.o.Now()
		cur.LastRunAt, cur.NextRunAt, cur.LastOutcome = &now, now.Add(st.Interval), SyncStarted
		sy = cur
		return t.SyncRan(ctx, cur.ID, now, cur.NextRunAt, SyncStarted)
	})
	if err == nil {
		s.o.Metrics.syncRan(s.repoName(repoID), SyncStarted)
	}
	return sy, b, err
}

// PauseSync pauses (paused true) or resumes a sync (Q-CUR-248). A resumed sync whose next run passed runs at the next dispatch.
func (s *Service) PauseSync(ctx context.Context, tx meta.Tx, repoID int64, id string, paused bool) (Sync, error) {
	var out Sync
	err := s.within(ctx, tx, func(t Tx) error {
		sy, found, err := t.SetSyncPaused(ctx, repoID, id, paused)
		out = sy
		return firstErr(err, notFoundIf(!found))
	})
	return out, err
}

// DeleteSync removes a sync and answers it as it was; a run in progress goes on as a bulk (R-CUR-027).
func (s *Service) DeleteSync(ctx context.Context, tx meta.Tx, repoID int64, id string) (Sync, error) {
	var out Sync
	err := s.within(ctx, tx, func(t Tx) error {
		sy, found, err := t.LockSync(ctx, repoID, id)
		if err != nil || !found {
			return firstErr(err, ErrNotFound)
		}
		out = sy
		return t.DeleteSync(ctx, repoID, id)
	})
	return out, err
}

func notFoundIf(b bool) error {
	if b {
		return ErrNotFound
	}
	return nil
}

// SyncRun is a run the dispatch started: the event of R-CUR-019 is written for it in the run's transaction.
type SyncRun struct {
	Sync Sync
	Bulk Bulk
}

// SyncsResult is what the syncs' part of one dispatch did.
type SyncsResult struct {
	// Looked are the due syncs the dispatch looked at; Started the runs it started.
	Looked, Started int
}

// startSyncs looks at every sync whose next run came (R-CUR-027), each in a transaction of its own that holds its repository's lock
// (Q-CUR-82), and starts its run unless, in this order: the repository is gone (nothing is written), disabled, offline or no longer
// remote (disabled), remote.syncs.paused holds (paused), the previous run's rows have not all ended (overlapped: it looks again at the
// next dispatch), the upstream's rate limit holds the queue (limited: it looks again at the limit's time) or the repository's folded
// bytes reach remote.syncs.maxBytes (bounded). Every other outcome looks again an interval later. A failure of one sync is logged and
// the dispatch goes on.
func (s *Service) startSyncs(ctx context.Context, now time.Time) (SyncsResult, error) {
	var res SyncsResult
	due, err := s.o.Maintenance.DueSyncs(ctx, now)
	if err != nil {
		return res, err
	}
	for _, d := range due {
		st, err := s.syncState(ctx, d.RepoID)
		if err != nil {
			s.o.Log.Warn("the dispatch could not read the repository of a sync", "sync", d.ID, "error", err)
			continue
		}
		outcome, err := s.lookAtSync(ctx, d, st, now)
		if err != nil {
			s.o.Log.Warn("the dispatch could not start a sync's run", "sync", d.ID, "error", err)
			continue
		}
		if outcome == "" {
			continue
		}
		res.Looked++
		if outcome == SyncStarted {
			res.Started++
		}
		s.o.Metrics.syncRan(s.repoName(d.RepoID), outcome)
	}
	return res, nil
}

// lookAtSync is one look of the dispatch at a due sync; "" when there was nothing to look at (gone, locked, no longer due).
func (s *Service) lookAtSync(ctx context.Context, d Sync, st SyncState, now time.Time) (SyncOutcome, error) {
	var outcome SyncOutcome
	err := s.o.Store.InTx(ctx, func(t Tx) error {
		if found, err := t.LockRepository(ctx, d.RepoID); err != nil || !found || !st.Found {
			return err
		}
		sy, ok, err := t.LockDueSync(ctx, d.ID, now)
		if err != nil || !ok {
			return err
		}
		next := now.Add(st.Interval)
		outcome, next, err = s.syncCheck(ctx, t, sy, st, now, next)
		if err != nil {
			return err
		}
		if outcome == SyncStarted {
			b, err := t.InsertBulk(ctx, syncBulk(sy, ""))
			if err != nil {
				return err
			}
			if s.o.OnSyncRun != nil {
				if err := s.o.OnSyncRun(ctx, t.Meta(), SyncRun{Sync: sy, Bulk: b}); err != nil {
					return err
				}
			}
		}
		return t.SyncRan(ctx, sy.ID, now, next, outcome)
	})
	return outcome, err
}

// syncCheck applies the checks of R-CUR-027 in their order and answers the outcome and the time of the next look.
func (s *Service) syncCheck(ctx context.Context, t Tx, sy Sync, st SyncState, now, next time.Time) (SyncOutcome, time.Time, error) {
	switch {
	case !st.Runnable:
		return SyncDisabled, next, nil
	case st.Paused:
		return SyncPausedAll, next, nil
	}
	if last, found, err := t.LastRun(ctx, sy.ID); err != nil {
		return "", next, err
	} else if found && last.Active() {
		return SyncOverlapped, now.Add(overlapAgain), nil
	}
	if l, found, err := t.Limit(ctx, sy.RepoID); err != nil {
		return "", next, err
	} else if found && l.Holds(now) {
		return SyncLimited, l.Until, nil
	}
	if st.MaxBytes > 0 {
		used, err := t.StoredBytes(ctx, sy.RepoID)
		if err != nil {
			return "", next, err
		}
		if used >= st.MaxBytes {
			return SyncBounded, next, nil
		}
	}
	return SyncStarted, next, nil
}

// repoName is the repository's qualified name for the metrics, by the metrics' own lookup.
func (s *Service) repoName(id int64) string {
	if s.o.Metrics == nil || s.o.Metrics.name == nil {
		return strconv.FormatInt(id, 10)
	}
	return s.o.Metrics.name(id)
}

// syncFilter is what a step of a sync's run leaves out of a package's versions (Q-CUR-243, Q-CUR-244): the versions the rules do not
// deliver (refused) and those the repository stores as version rows (fixed). Another bulk leaves out nothing.
type syncFilter struct {
	refused map[string]bool
	fixed   map[string]bool
}

// filterOf reads the filter of a package for a step of b; nil for a bulk that is no sync's run.
func (s *Service) filterOf(ctx context.Context, b Bulk, name string, vs []PlannedVersion) (*syncFilter, error) {
	if b.Origin != remotecuration.OriginSync || len(vs) == 0 {
		return nil, nil
	}
	f := &syncFilter{refused: map[string]bool{}, fixed: map[string]bool{}}
	repo := strconv.FormatInt(b.RepoID, 10)
	versions := make([]string, 0, len(vs))
	for _, v := range vs {
		if s.o.Decider != nil && !s.o.Decider.Decide(ctx, repo, name, v.Version).Delivered {
			f.refused[v.Version] = true
			continue
		}
		versions = append(versions, v.Version)
	}
	if s.o.Syncs != nil && len(versions) > 0 {
		fixed, err := s.o.Syncs.Fixed(ctx, b.RepoID, name, versions)
		if err != nil {
			return nil, err
		}
		f.fixed = fixed
	}
	return f, nil
}

// SyncOf reads the sync of a subject of a repository; found is false for none (the preview names it, R-CUR-027).
func (s *Service) SyncOf(ctx context.Context, repoID int64, level Level, subject string) (sy Sync, found bool, err error) {
	err = s.o.Store.InTx(ctx, func(t Tx) (e error) {
		sy, found, e = t.SyncOfSubject(ctx, repoID, level, subject)
		return e
	})
	return sy, found, err
}

// Fixed answers which of versions of a package the repository stores as version rows, which a sync's run leaves out (Q-CUR-244);
// without the port, none.
func (s *Service) Fixed(ctx context.Context, repoID int64, name string, versions []string) (map[string]bool, error) {
	if s.o.Syncs == nil || len(versions) == 0 {
		return map[string]bool{}, nil
	}
	return s.o.Syncs.Fixed(ctx, repoID, name, versions)
}
