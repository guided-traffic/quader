package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/formathost"
	"github.com/guided-traffic/quader/internal/jobs"
)

// A probe starts the download (repositories-and-proxy.md R-REPO-065, walk item W-25): the format host decides on a probe and queues
// the job remote-background-download through the queue below; a worker runs it through the mirror host, charged with the ticket the
// probe's decision took (usage-and-limits.md R-LIM-037).

const (
	// backgroundDownloadAttempts and backgroundDownloadTimeout bound the job: a run that waits out the upstream's rate limit or an
	// open circuit returns for a retry, and 30 minutes stop one that hangs.
	backgroundDownloadAttempts = 5
	backgroundDownloadTimeout  = 30 * time.Minute
)

// backgroundDownloads is the queue of the background downloads as the format host asks it (formathost.BackgroundDownloads).
type backgroundDownloads struct{ a *App }

var _ formathost.BackgroundDownloads = backgroundDownloads{}

func (b backgroundDownloads) queue() (*jobs.Queue, error) {
	qu := b.a.Queue()
	if qu == nil {
		return nil, errors.New("app: this replica has no job queue")
	}
	return qu, nil
}

// Busy implements [formathost.BackgroundDownloads].
func (b backgroundDownloads) Busy(ctx context.Context, key string, repoID int64) (bool, int, error) {
	qu, err := b.queue()
	if err != nil {
		return false, 0, err
	}
	active, err := qu.Active(ctx, key)
	if err != nil || active {
		return active, 0, err
	}
	n, err := qu.CountActive(ctx, formathost.BackgroundDownloadKind, strconv.FormatInt(repoID, 10))
	return false, n, err
}

// Enqueue implements [formathost.BackgroundDownloads].
func (b backgroundDownloads) Enqueue(ctx context.Context, d formathost.BackgroundDownload) (bool, error) {
	qu, err := b.queue()
	if err != nil {
		return false, err
	}
	res, err := qu.EnqueueNow(ctx, jobs.EnqueueSpec{
		Kind: formathost.BackgroundDownloadKind, DedupKey: d.Key(), Args: d,
		TargetKind: "repository", TargetKey: strconv.FormatInt(d.RepoID, 10),
	})
	return res.Created, err
}

// probeMetrics are the counters of the decisions on probes and of the background downloads, one family per process (R-REPO-065).
func (a *App) probeMetrics() *formathost.ProbeMetrics {
	a.probeMetricsOnce.Do(func() {
		if a.tele == nil {
			return
		}
		m, err := formathost.NewProbeMetrics(a.tele.metrics.Registry())
		if err != nil {
			a.log.Warn("the metrics of the background downloads are not registered", "error", err)
			return
		}
		a.probeMetricsValue = m
	})
	return a.probeMetricsValue
}

// registerBackgroundDownloadJobs registers the kind remote-background-download: queue default, one job per version of a repository at
// a time across replicas (exclusive on BackgroundDownload.Key), retried five times.
func (a *App) registerBackgroundDownloadJobs(r *jobs.Registry) {
	r.RegisterKind(formathost.BackgroundDownloadKind, jobs.KindSpec{
		Queue: "default", DedupMode: jobs.Exclusive, MaxAttempts: backgroundDownloadAttempts, Timeout: backgroundDownloadTimeout,
		Handler: a.backgroundDownloadHandler(),
	})
}

// backgroundDownloadHandler runs one attempt of a background download on the mirror host of the process.
func (a *App) backgroundDownloadHandler() jobs.Handler {
	return func(ctx context.Context, run *jobs.Run) error {
		var d formathost.BackgroundDownload
		if err := json.Unmarshal(run.Args, &d); err != nil {
			return jobs.Permanent(fmt.Errorf("background download: the arguments do not parse: %w", err))
		}
		area := part[mirrorEngineArea](a.parts)
		if area.b == nil {
			return errors.New("background download: this process has no remote engine for background fetches")
		}
		mh, err := area.b.get()
		if err != nil {
			return err
		}
		res, err := mh.RunBackgroundDownload(ctx, d, run.ID, formathost.BackgroundDownloadOptions{
			Limits: part[downloadLimitsArea](a.parts).engine, MaxBytes: int64(a.cfg.Proxy.MaxArtifactBytes), Metrics: a.probeMetrics(),
		})
		if err != nil {
			return err
		}
		return run.SetResult(res)
	}
}

// probeState is the App's part of the background downloads.
type probeState struct {
	probeMetricsOnce  sync.Once
	probeMetricsValue *formathost.ProbeMetrics
}
