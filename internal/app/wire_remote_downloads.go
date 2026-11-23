package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/formathost"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/proxy"
	"github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/queue"
)

// The download queue of the remote repositories (remote-curation.md R-CUR-010 to R-CUR-013, docs/developer/remote-curation.md R4): the
// queue's service over PostgreSQL, built at its first use in every role (a web replica inserts rows and answers the API, a worker
// runs them); the job remote-download, one attempt of one row on the queue downloads; the task remote-downloads-dispatch on the queue
// maintenance; the format host's port for a probe's row (R-CUR-013) and the remote engine's for a client's failed fetch (Q-CUR-19).

const (
	// downloadRunTimeout bounds one attempt (R-REPO-065's bound of one download, R-CUR-011).
	downloadRunTimeout = 30 * time.Minute
	// defaultDownloadsDispatchSchedule is the schedule of the task remote-downloads-dispatch: every minute (R-CUR-020).
	defaultDownloadsDispatchSchedule = "* * * * *"
)

// downloadsState is the App's part of the queue: the service and its metrics, built once.
type downloadsState struct {
	downloadsOnce sync.Once
	downloadsSvc  *queue.Service
	downloadsErr  error
}

// downloads returns the queue's service of the process.
func (a *App) downloads() (*queue.Service, error) {
	a.downloadsOnce.Do(func() { a.downloadsSvc, a.downloadsErr = a.buildDownloads() })
	return a.downloadsSvc, a.downloadsErr
}

func (a *App) buildDownloads() (*queue.Service, error) {
	db := a.database()
	if db == nil {
		return nil, errors.New("app: the download queue needs the database")
	}
	store := queue.NewPGStore(db, jobEnqueuer{a})
	o := queue.Options{
		Store: store, Maintenance: store, Settings: a.downloadSettings, Planner: downloadPlanner{a}, Jobs: jobCanceller{a},
		Log: a.log.With("component", "remote-downloads"),
	}
	if a.tele != nil {
		names := func(id int64) string {
			r := a.Repositories()
			if r == nil {
				return ""
			}
			repo, err := r.ByID(context.Background(), id)
			if err != nil {
				return ""
			}
			return repo.Name.String()
		}
		m, err := queue.NewMetrics(a.tele.metrics.Registry(), a.tele.metrics.Repos().Value, names)
		if err != nil {
			return nil, err
		}
		a.tele.metrics.Repos().OnForget(m.Forget)
		o.Metrics = m
	}
	return queue.New(o)
}

// downloadSettings are the bounds of proxy.downloads (R-CUR-020).
func (a *App) downloadSettings() queue.Settings {
	d := a.cfg.Proxy.Downloads
	return queue.Settings{
		PerRepository: d.PerRepository, MaxWaiting: d.MaxWaitingPerRepository, MaxAttempts: d.MaxAttempts,
		DoneRetention: d.DoneRetention, FailedRetention: d.FailedRetention, ClientFailures: d.ClientFailuresPerRepository,
	}
}

// probeMetrics are the queue's counters, which the format host counts the decisions on probes with; nil without them.
func (a *App) probeMetrics() formathost.ProbeCounter {
	svc, err := a.downloads()
	if err != nil || svc == nil {
		return nil
	}
	m := svc.Metrics()
	if m == nil {
		return nil // a nil *queue.Metrics in the interface would not be nil
	}
	return m
}

// jobEnqueuer enqueues the attempt of a row (queue.Enqueuer) on the process's job queue, read at the call.
type jobEnqueuer struct{ a *App }

func (e jobEnqueuer) Enqueue(ctx context.Context, tx meta.Tx, spec jobs.EnqueueSpec) (jobs.Result, error) {
	qu := e.a.Queue()
	if qu == nil {
		return jobs.Result{}, errors.New("app: this replica has no job queue")
	}
	return qu.Enqueue(ctx, tx, spec)
}

// jobCanceller cancels the job of a running row (queue.JobCanceller) through the process's job queue.
type jobCanceller struct{ a *App }

func (c jobCanceller) Cancel(ctx context.Context, id string) (bool, error) {
	qu := c.a.Queue()
	if qu == nil {
		return false, errors.New("app: this replica has no job queue")
	}
	_, ok, err := qu.Cancel(ctx, id)
	return ok, err
}

// downloadPlanner is the queue's planner (queue.Planner) on the mirror host of the process, which only a worker builds.
type downloadPlanner struct{ a *App }

func (p downloadPlanner) host() (*formathost.MirrorHost, error) {
	area := part[mirrorEngineArea](p.a.parts)
	if area.b == nil {
		return nil, errors.New("app: this process has no remote engine for background fetches")
	}
	return area.b.get()
}

func (p downloadPlanner) Versions(ctx context.Context, repoID int64, name string, pre bool, maxVersions int) ([]queue.PlannedVersion, error) {
	h, err := p.host()
	if err != nil {
		return nil, err
	}
	return h.Versions(ctx, repoID, name, pre, maxVersions)
}

func (p downloadPlanner) Packages(ctx context.Context, repoID int64, section, cursor string) ([]string, string, bool, error) {
	h, err := p.host()
	if err != nil {
		return nil, "", false, err
	}
	return h.Packages(ctx, repoID, section, cursor)
}

// probeDownloads is the format host's port of the queue (formathost.Downloads): a probe's row (R-CUR-013).
type probeDownloads struct{ a *App }

var _ formathost.Downloads = probeDownloads{}

func (p probeDownloads) Queued(ctx context.Context, repoID int64, name, version string) (bool, bool, error) {
	svc, err := p.a.downloads()
	if err != nil {
		return false, false, err
	}
	return svc.Queued(ctx, repoID, name, version)
}

func (p probeDownloads) AddProbe(ctx context.Context, d formathost.ProbeDownload) (bool, bool, error) {
	svc, err := p.a.downloads()
	if err != nil {
		return false, false, err
	}
	ticket, err := json.Marshal(d.Ticket)
	if err != nil {
		return false, false, err
	}
	if !d.Ticket.Charged() {
		ticket = nil
	}
	_, err = svc.Add(ctx, nil, queue.NewRow{
		RepoID: d.RepoID, Name: d.RowName(), Version: d.Version, Origin: remotecuration.OriginProbe, Ticket: ticket,
		Probe: &queue.Probe{Files: d.Files, Document: d.Document, Digest: d.Digest},
	})
	var qe *queue.QueuedError
	var fe *queue.FullError
	switch {
	case errors.As(err, &qe):
		return false, false, nil
	case errors.As(err, &fe):
		return false, true, nil
	case err != nil:
		return false, false, err
	}
	return true, false, nil
}

// clientFailures is the remote engine's recorder of clients' failed fetches (proxy.ClientFailures, Q-CUR-19).
type clientFailures struct{ a *App }

var _ proxy.ClientFailures = clientFailures{}

func (c clientFailures) ClientFailure(ctx context.Context, repoID int64, name, version, file string, f remotecuration.Failure) {
	svc, err := c.a.downloads()
	if err != nil {
		return
	}
	svc.ClientFailure(ctx, repoID, name, version, file, f)
}

// registerDownloadJobs registers the kind remote-download (queue downloads, exclusive per row, one attempt, 30 minutes) and the task
// remote-downloads-dispatch with its kind (queue maintenance, exclusive). The handlers find the queue's service and the mirror host at
// run time; only a worker and the role all claim their jobs.
func (a *App) registerDownloadJobs(r *jobs.Registry) {
	r.RegisterKind(remotecuration.KindDownload, jobs.KindSpec{
		Queue: remotecuration.QueueDownloads, DedupMode: jobs.Exclusive, MaxAttempts: 1, Timeout: downloadRunTimeout,
		Handler: a.downloadHandler(),
	})
	r.RegisterKind(remotecuration.TaskDispatch, jobs.KindSpec{
		Queue: "maintenance", DedupMode: jobs.Exclusive, MaxAttempts: 1, Handler: a.downloadsDispatchHandler(),
	})
	spec := jobs.TaskSpec{Schedule: defaultDownloadsDispatchSchedule}
	if c, ok := a.cfg.Maintenance.Tasks[remotecuration.TaskDispatch]; ok && c.Schedule != "" {
		spec.Schedule, spec.Disabled = c.Schedule, !c.Enabled
	}
	r.RegisterTask(remotecuration.TaskDispatch, spec)
}

// downloadsDispatchHandler runs the task remote-downloads-dispatch (R-CUR-011).
func (a *App) downloadsDispatchHandler() jobs.Handler {
	return func(ctx context.Context, run *jobs.Run) error {
		svc, err := a.downloads()
		if err != nil {
			return err
		}
		res, err := svc.Dispatch(ctx, proxy.Describe)
		if err != nil {
			return err
		}
		return run.SetResult(res)
	}
}

// downloadHandler runs one attempt of a row (R-CUR-011) on the mirror host of the process and hands its end to the queue. A run the
// job framework releases at shutdown leaves its row running with its job, which another worker runs again; a run whose lease was
// lost leaves it to the dispatch task's orphan rule (R-CUR-021); a cancelled run ends its row cancelled; a run that reached the
// job's timeout ends its attempt with the failure class bound.
func (a *App) downloadHandler() jobs.Handler {
	return func(ctx context.Context, run *jobs.Run) error {
		var args queue.RunArgs
		if err := json.Unmarshal(run.Args, &args); err != nil {
			return jobs.Permanent(fmt.Errorf("remote download: the arguments do not parse: %w", err))
		}
		svc, err := a.downloads()
		if err != nil {
			return err
		}
		row, ok, err := svc.Claim(ctx, args.Row, run.ID)
		if err != nil || !ok {
			return err
		}
		mh, err := downloadPlanner{a}.host()
		if err != nil {
			return err
		}
		res, runErr := mh.RunDownload(ctx, row, run.ID, formathost.DownloadOptions{
			Limits: part[downloadLimitsArea](a.parts).engine, MaxBytes: int64(a.cfg.Proxy.MaxArtifactBytes),
		})
		if runErr != nil {
			switch cause := context.Cause(ctx); {
			case errors.Is(cause, jobs.ErrCancelRequested):
				res.Cancelled = true
			case errors.Is(cause, jobs.ErrTimeout):
				res.Failed = true
				res.Failure = &remotecuration.Failure{
					Class: remotecuration.ClassBound, Step: remotecuration.StepBody, Code: "download_timeout", Attempt: row.Attempts,
					Detail: "The download ran longer than " + downloadRunTimeout.String() + "; what it stored stays and a retry goes on from it.",
					At:     time.Now(),
				}
			default:
				return runErr // released for shutdown or lease lost: the row keeps its job
			}
		}
		repo := ""
		if r := a.Repositories(); r != nil {
			if rp, err := r.ByID(context.WithoutCancel(ctx), row.RepoID); err == nil {
				repo = rp.Name.String()
			}
		}
		if _, err := svc.Finish(context.WithoutCancel(ctx), row, run.ID, repo, res); err != nil {
			return err
		}
		return run.SetResult(res)
	}
}

// remoteDownloadsAPI are the services of the download queue's operations (R-CUR-010, R-CUR-012). The delivery rules' decider is the
// stub that delivers everything until the rules' package is wired (R2): a download ahead does not depend on the rules (R-CUR-014),
// only who may start one and the member delivery of a row do.
func (a *App) remoteDownloadsAPI() v1.RemoteDownloadsServices {
	svc, err := a.downloads()
	if err != nil {
		a.log.Warn("the download queue is not available; its operations answer 503", "error", err)
		return v1.RemoteDownloadsServices{}
	}
	out := v1.RemoteDownloadsServices{Queue: svc, Limits: part[downloadLimitsArea](a.parts).engine, Settings: a.cfg.Proxy.Downloads}
	if p := part[identityArea](a.parts).principals; p != nil {
		out.Principals = p
	}
	return out
}
