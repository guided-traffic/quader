package v1

import (
	"context"
	"net/http"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/blob/summary"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/jobs"
)

// The storage overview (storage.md R-STOR-107, Q-API-475): the state of the stored content now, for the top of the storage page and the
// admin dashboard. The states are derived by summary.DeriveOverview, once (Q-STOR-505); this file reads the input and maps the result.

// storageScrubTask is the task of the scrub (R-STOR-027), whose last and next run the overview names.
const storageScrubTask = "scrub"

// GetStorageOverview implements GET /api/v1/storage/overview. The caller holds settings:read (declared). The problems of the period
// (R-STOR-111) are derived from the history of the period, 30 days unless the request names another.
func (s *Server) GetStorageOverview(w http.ResponseWriter, r *http.Request, p gen.GetStorageOverviewParams) {
	if s.o.Storage.Overview == nil {
		httpserver.Problem(w, r, unavailable("The storage overview"))
		return
	}
	period, err := parsePeriod(periodName(p.Period))
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	ctx := r.Context()
	now := time.Now()
	in, err := s.o.Storage.Overview(ctx)
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.getStorageOverview"))
		return
	}
	runs, err := s.scrubRuns(ctx)
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.getStorageOverview"))
		return
	}
	in.Scrub = runs
	in.Period = period.Name
	if in.Problems, err = s.periodProblems(r, period, now); err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.getStorageOverview"))
		return
	}
	writeJSON(w, r, http.StatusOK, storageOverviewView(summary.DeriveOverview(in, now)))
}

// scrubRuns is the scrub task's last finished run, its next scheduled run and whether a run is going on, from the task list's own read
// (taskViews); empty where the replica has no task registry.
func (s *Server) scrubRuns(ctx context.Context) (summary.ScrubRuns, error) {
	if s.o.API.Jobs == nil || s.o.API.Store == nil {
		return summary.ScrubRuns{}, nil
	}
	all, err := s.resolveTasks()
	if err != nil {
		return summary.ScrubRuns{}, err
	}
	var scrub []jobs.TaskInfo
	for _, t := range all {
		if t.Name == storageScrubTask {
			scrub = append(scrub, t)
		}
	}
	if len(scrub) == 0 {
		return summary.ScrubRuns{}, nil
	}
	views, err := s.taskViews(ctx, scrub)
	if err != nil || len(views) == 0 {
		return summary.ScrubRuns{}, err
	}
	out := summary.ScrubRuns{NextRunAt: views[0].NextRunAt, Running: views[0].Running}
	if lr := views[0].LastRun; lr != nil {
		out.LastRunAt = lr.FinishedAt
	}
	return out, nil
}

// storageOverviewView is the overview of the API.
func storageOverviewView(o summary.Overview) gen.StorageOverview {
	var out gen.StorageOverview
	out.At, out.FiguresAt = o.At, o.FiguresAt
	c := &out.Connections
	c.State, c.Configured, c.Reachable = gen.StorageOverviewConnectionsState(o.Connections.State), o.Connections.Configured, o.Connections.Reachable
	c.Detached = o.Connections.Detached
	c.Unreachable = make([]gen.StorageOverviewUnreachable, 0, len(o.Connections.Unreachable))
	for _, u := range o.Connections.Unreachable {
		c.Unreachable = append(c.Unreachable, gen.StorageOverviewUnreachable{Id: u.ID, CircuitOpen: u.CircuitOpen, Configured: u.Configured})
	}
	out.Stored.Blobs, out.Stored.Bytes = o.Stored.Count, o.Stored.Bytes
	a := &out.Availability
	a.State, a.Target, a.MinCopies = gen.StorageOverviewAvailabilityState(o.Availability.State), o.Availability.Target, o.Availability.MinCopies
	a.PerBlob = storageCopyCounts(o.Availability.PerBlob)
	a.BelowTarget.Blobs, a.BelowTarget.Bytes = o.Availability.BelowTarget.Blobs, o.Availability.BelowTarget.Bytes
	a.Loss = make([]gen.StorageOverviewLoss, 0, len(o.Availability.Loss))
	for _, l := range o.Availability.Loss {
		a.Loss = append(a.Loss, gen.StorageOverviewLoss{Connection: l.Connection, Blobs: l.Blobs, Bytes: l.Bytes})
	}
	e := &out.Encryption
	e.State, e.EncryptedShare = gen.StorageOverviewEncryptionState(o.Encryption.State), o.Encryption.EncryptedShare
	e.Encrypted, e.Plain, e.ProviderConfigured, e.ProviderApplied = o.Encryption.Encrypted, o.Encryption.Plain, o.Encryption.ProviderConfigured,
		o.Encryption.ProviderApplied
	i := &out.Integrity
	i.State, i.DownloadVerify, i.AuthenticatedReads = gen.StorageOverviewIntegrityState(o.Integrity.State), o.Integrity.DownloadVerify, o.Integrity.AuthenticatedReads
	i.Scrub.Coverage, i.Scrub.Overdue, i.Scrub.Running = o.Integrity.Scrub.Coverage, o.Integrity.Scrub.Overdue, o.Integrity.Scrub.Running
	i.Scrub.LastRunAt, i.Scrub.NextRunAt = utcOrNil(o.Integrity.Scrub.LastRunAt), utcOrNil(o.Integrity.Scrub.NextRunAt)
	i.DamagedNow.Copies, i.DamagedNow.Blobs = o.Integrity.DamagedCopies, o.Integrity.DamagedBlobs
	pr := &out.Problems
	pr.Period, pr.State, pr.Count = gen.StorageOverviewProblemsPeriod(o.Problems.Period), gen.StorageOverviewProblemsState(o.Problems.State), o.Problems.Count
	pr.Open, pr.Acknowledged = o.Problems.Open, o.Problems.Acknowledged
	if wp := o.Problems.Worst; wp != nil {
		pr.Worst = &gen.StorageOverviewWorstProblem{
			Kind: gen.StorageProblemKind(wp.Kind), Connection: wp.Connection, StartedAt: wp.Started.UTC(), Seconds: wp.Seconds,
			Tone: gen.StorageOverviewWorstProblemTone(wp.Tone),
		}
	}
	return out
}

// storageCopyCounts is the copies per blob of the API.
func storageCopyCounts(in []summary.CopyCount) []gen.StorageCopyCount {
	out := make([]gen.StorageCopyCount, 0, len(in))
	for _, c := range in {
		out = append(out, gen.StorageCopyCount{Copies: c.Copies, Blobs: c.Blobs, Bytes: c.Bytes})
	}
	return out
}

// utcOrNil is t in UTC, nil for nil.
func utcOrNil(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
