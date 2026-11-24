package queue

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/remotecuration"
)

// JobCanceller cancels the job of a running row; *jobs.Queue implements it.
type JobCanceller interface {
	Cancel(ctx context.Context, id string) (ok bool, err error)
}

// Options are what a [Service] is built with.
type Options struct {
	Store Store
	// Maintenance is what the dispatch task does besides starting rows; nil runs no dispatch.
	Maintenance Maintenance
	// Settings reads the bounds at every use; nil is [DefaultSettings].
	Settings func() Settings
	// Planner expands a bulk; nil fails every bulk with catalogue_unavailable.
	Planner Planner
	// Jobs cancels the job of a running row; nil cancels only waiting rows.
	Jobs JobCanceller
	// Metrics counts the ends of rows; nil counts nothing.
	Metrics *Metrics
	// Now and Jitter are the clock and the spread of the waits (a number in [0, 1)); nil: the system's.
	Now    func() time.Time
	Jitter func() float64
	Log    *slog.Logger
}

// Service is the download queue of the remote repositories (R-CUR-011).
type Service struct {
	o       Options
	clients *clientThrottle
}

var _ remotecuration.Queue = (*Service)(nil)

// New creates the service.
func New(o Options) (*Service, error) {
	if o.Store == nil {
		return nil, errors.New("queue: no store")
	}
	if o.Settings == nil {
		o.Settings = DefaultSettings
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Jitter == nil {
		o.Jitter = rand.Float64 //nolint:gosec // G404: the spread of a wait, no secret
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	return &Service{o: o, clients: newClientThrottle()}, nil
}

// Reader is the queue's reads (the API's list, one row, one bulk).
func (s *Service) Reader() Reader { return s.o.Store }

// Metrics are the queue's counters; nil without them.
func (s *Service) Metrics() *Metrics { return s.o.Metrics }

// Queued reports whether a version has a waiting or running row, and whether the repository's waiting rows reach their bound: the
// decision on a probe asks before it charges the probe's caller (R-CUR-013).
func (s *Service) Queued(ctx context.Context, repoID int64, name, version string) (active, full bool, err error) {
	rows, err := s.o.Store.Active(ctx, repoID, name, []string{version})
	if err != nil || len(rows) > 0 {
		return len(rows) > 0, false, err
	}
	n, err := s.o.Store.Waiting(ctx, repoID)
	return false, n >= max(s.o.Settings().MaxWaiting, 1), err
}

// Settings are the bounds in force.
func (s *Service) Settings() Settings { return s.o.Settings() }

// Add queues one version (R-CUR-010, R-CUR-013) and starts it at once when its repository has a free place. A version that has a
// waiting or running row is answered by a *[QueuedError] with that row; a repository whose waiting rows reach the bound by a
// *[FullError]. tx is the caller's transaction (the audit event of the act commits with the row); nil runs one of its own.
func (s *Service) Add(ctx context.Context, tx meta.Tx, n NewRow) (Row, error) {
	var out Row
	err := s.within(ctx, tx, func(t Tx) error {
		r, err := s.add(ctx, t, n)
		out = r
		return err
	})
	return out, err
}

func (s *Service) within(ctx context.Context, tx meta.Tx, fn func(Tx) error) error {
	if tx == nil {
		return s.o.Store.InTx(ctx, fn)
	}
	return s.o.Store.Within(ctx, tx, fn)
}

func (s *Service) add(ctx context.Context, t Tx, n NewRow) (Row, error) {
	st := s.o.Settings()
	if found, err := t.LockRepository(ctx, n.RepoID); err != nil || !found {
		return Row{}, firstErr(err, ErrNotFound)
	}
	if active, err := t.Active(ctx, n.RepoID, n.Name, []string{n.Version}); err != nil {
		return Row{}, err
	} else if len(active) > 0 {
		return active[0], &QueuedError{Row: active[0]}
	}
	waiting, _, err := t.Counts(ctx, n.RepoID)
	if err != nil {
		return Row{}, err
	}
	if waiting >= max(st.MaxWaiting, 1) {
		return Row{}, &FullError{Waiting: waiting}
	}
	row, created, err := t.Insert(ctx, Row{
		RepoID: n.RepoID, Name: n.Name, Version: n.Version, Probe: n.Probe, Origin: n.Origin, BulkID: n.BulkID, RequestID: n.RequestID,
		StartedBy: n.StartedBy, Ticket: n.Ticket, MaxAttempts: st.MaxAttempts,
	})
	if err != nil {
		return Row{}, err
	}
	if !created {
		return row, &QueuedError{Row: row}
	}
	started, err := s.fill(ctx, t, n.RepoID)
	if err != nil {
		return Row{}, err
	}
	if j, ok := started[row.ID]; ok {
		row.State, row.JobID, row.Attempts = remotecuration.StateRunning, j, row.Attempts+1
	}
	return row, nil
}

func firstErr(err, otherwise error) error {
	if err != nil {
		return err
	}
	return otherwise
}

// fill starts the waiting rows of a repository whose next attempt came, as many as it has free places, in the order of R-CUR-011.
// The caller holds the repository's lock. It answers the started rows' jobs by row.
func (s *Service) fill(ctx context.Context, t Tx, repoID int64) (map[string]string, error) {
	_, running, err := t.Counts(ctx, repoID)
	if err != nil {
		return nil, err
	}
	now := s.o.Now()
	due, err := t.Due(ctx, repoID, now, freePlaces(s.o.Settings().PerRepository, running))
	if err != nil {
		return nil, err
	}
	started := make(map[string]string, len(due))
	for _, r := range due {
		job, err := t.Start(ctx, r, now)
		if err != nil {
			return nil, err
		}
		started[r.ID] = job
	}
	return started, nil
}

// Start starts the waiting rows of a repository that may run now; the dispatch task and a retry call it.
func (s *Service) Start(ctx context.Context, repoID int64) (int, error) {
	n := 0
	err := s.o.Store.InTx(ctx, func(t Tx) error {
		if found, err := t.LockRepository(ctx, repoID); err != nil || !found {
			return err
		}
		started, err := s.fill(ctx, t, repoID)
		n = len(started)
		return err
	})
	return n, err
}

// Claim reads the row a job runs: ok is false when the row is gone or no longer runs with this job (cancelled, found orphaned and
// started again), and the job then ends without work.
func (s *Service) Claim(ctx context.Context, rowID, jobID string) (Row, bool, error) {
	r, found, err := s.o.Store.GetRow(ctx, rowID, 0)
	if err != nil || !found {
		return Row{}, false, err
	}
	return r, r.State == remotecuration.StateRunning && r.JobID == jobID, nil
}

// Finish writes the end of an attempt of a row (R-CUR-011) and, in the same transaction, starts the repository's next waiting rows
// in the place it frees. repo is the repository's qualified name for the metrics.
func (s *Service) Finish(ctx context.Context, row Row, jobID, repo string, res Result) (settlement, error) {
	now := s.o.Now()
	set := settle(row, res, now, s.o.Jitter())
	err := s.o.Store.InTx(ctx, func(t Tx) error {
		if found, err := t.LockRepository(ctx, row.RepoID); err != nil || !found {
			return err
		}
		ok, err := t.Settle(ctx, row.ID, jobID, set, res, now)
		if err != nil || !ok {
			return err
		}
		_, err = s.fill(ctx, t, row.RepoID)
		return err
	})
	if err != nil {
		return set, err
	}
	if res.Failed && res.Failure != nil {
		s.o.Metrics.failed(repo, res.Failure.Class)
	}
	switch set.State {
	case remotecuration.StateDone:
		s.o.Metrics.ended(repo, row.Origin, string(set.Outcome))
	case remotecuration.StateFailed, remotecuration.StateCancelled:
		s.o.Metrics.ended(repo, row.Origin, string(set.State))
	}
	return set, nil
}

// Active implements [remotecuration.Queue].
func (s *Service) Active(ctx context.Context, repoID, name string, versions []string) (map[string]remotecuration.QueuedDownload, error) {
	id, err := parseRepoID(repoID)
	if err != nil {
		return map[string]remotecuration.QueuedDownload{}, nil //nolint:nilerr // an id of another form names no repository
	}
	rows, err := s.o.Store.Active(ctx, id, name, versions)
	if err != nil {
		return nil, err
	}
	out := make(map[string]remotecuration.QueuedDownload, len(rows))
	for _, r := range rows {
		qd := remotecuration.QueuedDownload{
			ID: r.ID, Version: r.Version, State: r.State, Origin: r.Origin, Attempts: r.Attempts, MaxAttempts: r.MaxAttempts,
		}
		if r.NotBefore != nil {
			qd.NextAttemptAt = *r.NotBefore
		}
		out[r.Version] = qd
	}
	return out, nil
}

// Retry makes a failed or cancelled row wait again with its attempts reset (R-CUR-012) and starts it when a place is free. A row
// of origin client becomes a download ahead of its version by the caller, charged with ticket.
func (s *Service) Retry(ctx context.Context, tx meta.Tx, repoID int64, id, caller string, ticket []byte) (Row, error) {
	var out Row
	err := s.within(ctx, tx, func(t Tx) error {
		if found, err := t.LockRepository(ctx, repoID); err != nil || !found {
			return firstErr(err, ErrNotFound)
		}
		cur, found, err := t.Get(ctx, id, repoID)
		if err != nil || !found {
			return firstErr(err, ErrNotFound)
		}
		if cur.State != remotecuration.StateFailed && cur.State != remotecuration.StateCancelled {
			return ErrNotRetryable
		}
		if active, err := t.Active(ctx, repoID, cur.Name, []string{cur.Version}); err != nil {
			return err
		} else if len(active) > 0 {
			return &QueuedError{Row: active[0]}
		}
		r, ok, err := t.Retry(ctx, id, repoID, s.o.Settings().MaxAttempts, caller, ticket)
		if err != nil || !ok {
			return firstErr(err, ErrNotRetryable)
		}
		out = r
		started, err := s.fill(ctx, t, repoID)
		if j, ok := started[r.ID]; ok {
			out.State, out.JobID, out.Attempts = remotecuration.StateRunning, j, 1
		}
		return err
	})
	return out, err
}

// RetryMatching makes every failed row of a repository that matches class and bulk wait again, at most 10,000 (R-CUR-012).
func (s *Service) RetryMatching(ctx context.Context, tx meta.Tx, repoID int64, class, bulkID string) (int, error) {
	n := 0
	err := s.within(ctx, tx, func(t Tx) error {
		if found, err := t.LockRepository(ctx, repoID); err != nil || !found {
			return firstErr(err, ErrNotFound)
		}
		ids, err := t.RetryMatching(ctx, repoID, class, bulkID, s.o.Settings().MaxAttempts, maxRetryMatching)
		if err != nil {
			return err
		}
		n = len(ids)
		_, err = s.fill(ctx, t, repoID)
		return err
	})
	return n, err
}

// maxRetryMatching bounds one POST downloads/retry (R-CUR-012).
const maxRetryMatching = 10000

// Cancel cancels a row (R-CUR-012): a waiting row at once; a running row by its job, which stops after the file it stores and ends
// the row cancelled. It answers the row as it is after the call.
func (s *Service) Cancel(ctx context.Context, tx meta.Tx, repoID int64, id string) (Row, error) {
	var out Row
	err := s.within(ctx, tx, func(t Tx) error {
		r, found, err := t.Get(ctx, id, repoID)
		if err != nil || !found {
			return firstErr(err, ErrNotFound)
		}
		out = r
		switch r.State {
		case remotecuration.StateWaiting:
			c, ok, err := t.CancelWaiting(ctx, id, repoID)
			if err != nil {
				return err
			}
			if ok {
				out = c
			}
		case remotecuration.StateRunning:
			if s.o.Jobs != nil && r.JobID != "" {
				_, err := s.o.Jobs.Cancel(ctx, r.JobID)
				return err
			}
		}
		return nil
	})
	return out, err
}

// parseRepoID reads a repository's ID as the repository model spells it.
func parseRepoID(s string) (int64, error) {
	var n int64
	if s == "" {
		return 0, errors.New("queue: empty repository id")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("queue: repository id is not a number")
		}
		n = n*10 + int64(c-'0')
		if n < 0 {
			return 0, errors.New("queue: repository id overflows")
		}
	}
	return n, nil
}
