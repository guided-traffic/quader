package app

import (
	"context"
	"errors"
	"sync"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/jobs"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/requests"
	"github.com/guided-traffic/quader/internal/remotecuration/retention"
	"github.com/guided-traffic/quader/internal/repository"
)

// The requests for an allow and the retention of what was fetched for examination (remote-curation.md R-CUR-014 to R-CUR-016,
// docs/developer/remote-curation.md R2b): the requests' service and the retention's, built at their first use in every role over the
// delivery rules' store, the download queue, the item store and the formats' classification; the task remote-examination-cleanup on
// the queue maintenance. The journal's replay of a discard is registered by the retention package itself (journal_replay.go).

// defaultExaminationCleanupSchedule is the schedule of the task remote-examination-cleanup: daily at 04:00 (R-CUR-020).
const defaultExaminationCleanupSchedule = "0 4 * * *"

// requestsState is the App's part of the requests: the two services, built once.
type requestsState struct {
	requestsOnce sync.Once
	requestsSvc  *requests.Service
	retentionSvc *retention.Service
	requestsErr  error
}

// curationRequests returns the requests' and the retention's services of the process.
func (a *App) curationRequests() (*requests.Service, *retention.Service, error) {
	a.requestsOnce.Do(func() { a.requestsSvc, a.retentionSvc, a.requestsErr = a.buildRequests() })
	return a.requestsSvc, a.retentionSvc, a.requestsErr
}

func (a *App) buildRequests() (*requests.Service, *retention.Service, error) {
	db := a.database()
	rules := part[deliveryRulesArea](a.parts).store
	itemStore := part[filesArea](a.parts).items
	seam := part[formatsArea](a.parts).seam
	repos := a.Repositories()
	if db == nil || rules == nil || itemStore == nil || seam == nil || repos == nil {
		return nil, nil, errors.New("app: the requests need the database, the delivery rules, the item store, the formats and the repositories")
	}
	q, err := a.downloads()
	if err != nil {
		return nil, nil, err
	}
	ret, err := retention.New(retention.Options{
		Runner: db, Recorder: a.auditRecorder(), Remover: itemStore, Repos: repos,
		Classify: func(repo repository.Repository, path string) (string, string) {
			c := seam.Classify(repo, repository.Cleaned{Path: path})
			return c.Package, c.Version
		},
	})
	if err != nil {
		return nil, nil, err
	}
	req, err := requests.New(requests.Options{
		Runner: db, Recorder: a.auditRecorder(), Rules: rules, Queue: q, Retention: ret, Settings: a.requestSettings,
	})
	if err != nil {
		return nil, nil, err
	}
	return req, ret, nil
}

// requestSettings are the bounds of proxy.delivery and of a bulk (R-CUR-020).
func (a *App) requestSettings() requests.Settings {
	d, b := a.cfg.Proxy.Delivery, a.cfg.Proxy.Downloads
	return requests.Settings{
		OpenPerPerson: d.OpenRequestsPerPerson, ExamineVersions: d.ExamineVersionsPerRequest,
		MaxVersionsPerBulk: b.MaxVersionsPerBulk, MaxBytesPerBulk: int64(b.MaxBytesPerBulk),
	}
}

// registerExaminationJobs registers the task remote-examination-cleanup with its kind (queue maintenance, one run at a time). It
// removes content, so it is mutating: the scheduler does not start it in read-only maintenance mode (R-OPS-024). The handler finds the
// services at run time.
func (a *App) registerExaminationJobs(r *jobs.Registry) {
	r.RegisterKind(rc.TaskExaminationCleanup, retention.CleanupKindSpec(a.examinationCleanupHandler()))
	spec := jobs.TaskSpec{Schedule: defaultExaminationCleanupSchedule, Mutating: true}
	if c, ok := a.cfg.Maintenance.Tasks[rc.TaskExaminationCleanup]; ok && c.Schedule != "" {
		spec.Schedule, spec.Disabled = c.Schedule, !c.Enabled
	}
	r.RegisterTask(rc.TaskExaminationCleanup, spec)
}

// examinationCleanupHandler runs the task remote-examination-cleanup (R-CUR-016, R-CUR-015 "Withdraw").
func (a *App) examinationCleanupHandler() jobs.Handler {
	return func(ctx context.Context, run *jobs.Run) error {
		_, ret, err := a.curationRequests()
		if err != nil {
			return err
		}
		res, err := ret.Cleanup(ctx, a.cfg.Proxy.Delivery.RequestRetention)
		if err != nil {
			return err
		}
		return run.SetResult(res)
	}
}

// deliveryRequestsAPI are the services of the requests' operations; a process that cannot build them answers them with 503.
func (a *App) deliveryRequestsAPI() v1.DeliveryRequestsServices {
	req, ret, err := a.curationRequests()
	if err != nil {
		a.log.Warn("the requests for an allow are not available; their operations answer 503", "error", err)
		return v1.DeliveryRequestsServices{}
	}
	return v1.DeliveryRequestsServices{Requests: req, Retention: ret}
}
