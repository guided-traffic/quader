package v1

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/api/edge"
	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/mirror/discover"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/remotecuration/queue"
)

// Downloads kept in sync (remote-curation.md R-CUR-027): the list of a repository's syncs, their set-up, a run now, the pause, the
// resume and the removal. A sync is the stored definition of a bulk that the task remote-downloads-dispatch starts again; the
// holders of manage and the system role admin set it up (R-CUR-005, Q-CUR-241), decided here as every write of the queue is.

// mayKeepInSync is R-CUR-005's "set up, pause, resume, run or remove a sync": manage on the repository, or the system role admin.
func mayKeepInSync(ctx context.Context, acc *edge.Access, key qname.Repo) error {
	if downloadManager(ctx, acc, key) {
		return nil
	}
	return errs.E(errs.Forbidden, errs.CodeForbidden, "Keeping downloads of "+strconv.Quote(key.String())+" in sync needs manage on it.")
}

// SyncBounds are a sync's bounds as the API answers them and as the event of its run names them.
func SyncBounds(sy queue.Sync) any {
	return boundsOf(queue.Bulk{
		MaxVersions: sy.MaxVersions, MaxBytes: sy.MaxBytes, IncludePrereleases: sy.IncludePrereleases, FromVersion: sy.FromVersion,
		ToVersion: sy.ToVersion, ToInclusive: sy.ToInclusive,
	})
}

// syncView is a sync as the API answers it (R-CUR-027): its newest run is a bulk with the counts of its rows, null when it has none.
func (s *Server) syncView(ctx context.Context, acc *edge.Access, t downloadTarget, sy queue.Sync, last *queue.Bulk, rows map[string]int64) map[string]any {
	state, next := "active", any(sy.NextRunAt.UTC())
	if sy.Paused {
		state, next = "paused", nil
	}
	var run any
	if last != nil {
		run = s.bulkView(ctx, acc, t, *last, rows)
	}
	return map[string]any{
		"id": sy.ID, "level": sy.Level, "subject": sy.Subject, "bounds": SyncBounds(sy), "state": state, "nextRunAt": next,
		"lastRunAt": nullTime(sy.LastRunAt), "lastOutcome": nullString(string(sy.LastOutcome)), "lastBulk": run,
		"createdBy": s.starter(ctx, acc, t, sy.CreatedBy), "createdAt": sy.CreatedAt.UTC(),
	}
}

// syncError maps the syncs' refusals to the API's codes; the answer names the sync or the run that stands in the way.
func (s *Server) syncError(ctx context.Context, acc *edge.Access, t downloadTarget, err error) error {
	var se *queue.SyncedError
	var tm *queue.TooManySyncsError
	var re *queue.RunningError
	switch {
	case errors.As(err, &se):
		return errs.E(errs.Conflict, errs.CodeAlreadySynced,
			"The repository keeps "+string(se.Sync.Level)+" "+strconv.Quote(se.Sync.Subject)+" in sync already: sync "+se.Sync.ID+".",
			errs.WithExtension("sync", se.Sync.ID))
	case errors.As(err, &tm):
		return errs.E(errs.Unprocessable, errs.CodeTooManySyncs,
			"The repository keeps "+strconv.Itoa(tm.Max)+" downloads in sync, as many as it may (proxy.downloads.max_syncs_per_repository).",
			errs.WithExtension("max", tm.Max))
	case errors.As(err, &re):
		return errs.E(errs.Conflict, errs.CodeSyncRunning,
			"The sync's previous run has downloads that wait or run: bulk "+re.Bulk.ID+". Run it again once that run has ended.",
			errs.WithExtension("bulk", s.bulkView(ctx, acc, t, re.Bulk, nil)))
	case errors.Is(err, queue.ErrNotFound):
		return errs.E(errs.NotFound, errs.CodeNotFound, "The sync does not exist in this repository.")
	}
	return err
}

// syncEvent records one of the syncs' events in the operation's transaction (R-CUR-019).
func (s *Server) syncEvent(ctx context.Context, r *http.Request, tx meta.Tx, t downloadTarget, typ string, sy queue.Sync) error {
	return s.downloadEvent(ctx, r, tx, t, typ, map[string]any{"sync": sy.ID, "level": sy.Level, "subject": sy.Subject, "bounds": SyncBounds(sy)})
}

// ListRemoteDownloadSyncs implements GET …/download-syncs (R-CUR-027).
func (s *Server) ListRemoteDownloadSyncs(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey string, _ gen.ListRemoteDownloadSyncsParams) {
	key := qname.Repo{Project: projectKey, Key: repoKey}
	ctx, svc, acc, t, err := s.downloadSetup(r.Context(), key)
	var lq ListQuery
	if err == nil {
		lq, err = ParseList(r, ListSpec{}, s.o.API.Config)
	}
	f := queue.BulkFilter{RepoID: t.ID, Limit: lq.Limit + 1}
	if err == nil && lq.After != nil {
		f.After, err = bulkCursor(lq.After)
	}
	var syncs []queue.Sync
	var last map[string]queue.Bulk
	var rows map[string]map[string]int64
	if err == nil {
		syncs, last, rows, err = svc.Reader().ListSyncs(ctx, f)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.listRemoteDownloadSyncs"))
		return
	}
	var next *string
	if len(syncs) > lq.Limit {
		syncs = syncs[:lq.Limit]
		end := syncs[len(syncs)-1]
		c := lq.EncodeCursor([]string{end.CreatedAt.UTC().Format(time.RFC3339Nano), end.ID})
		next = &c
		w.Header().Set("Link", "<"+nextURL(r, c)+`>; rel="next"`)
	}
	items := make([]map[string]any, 0, len(syncs))
	for _, sy := range syncs {
		var run *queue.Bulk
		if b, ok := last[sy.ID]; ok {
			run = &b
		}
		var counts map[string]int64
		if run != nil {
			counts = rows[run.ID]
		}
		items = append(items, s.syncView(ctx, acc, t, sy, run, counts))
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"items": items, "nextCursor": next, "intervalSeconds": t.Syncs.IntervalSeconds, "paused": t.Syncs.Paused,
		"maxSyncs": max(s.o.RemoteDownloads.Settings.MaxSyncsPerRepository, 1),
	})
}

// CreateRemoteDownloadSync implements POST …/download-syncs (R-CUR-027): the body of a download ahead of a package or a section.
func (s *Server) CreateRemoteDownloadSync(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey string, _ gen.CreateRemoteDownloadSyncParams) {
	key := qname.Repo{Project: projectKey, Key: repoKey}
	ctx, svc, acc, t, err := s.downloadSetup(r.Context(), key)
	if err == nil && s.idem == nil {
		err = unavailable("The download queue")
	}
	var in downloadRequest
	var b downloadBounds
	var body []byte
	if err == nil {
		in, b, body, err = s.parseDownloadRequest(w, r)
	}
	if err == nil && in.Level == string(queue.LevelVersion) {
		err = rangeField(errs.Invalid, errs.CodeValidation, "/level", errs.CodeValidation,
			"A sync keeps every version of a package or a sub-section current; one version is downloaded once.")
	}
	if err == nil {
		err = mayKeepInSync(ctx, acc, key)
	}
	if err == nil {
		err = downloadRange(t.Format, &b)
	}
	if err == nil && in.Level == string(queue.LevelSection) {
		err = s.sectionOffered(ctx, t, in.Section)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.createRemoteDownloadSync"))
		return
	}
	subject := in.Name
	if in.Level == string(queue.LevelSection) {
		subject = in.Section
	}
	err = s.idem.Do(w, r, body, IdempotentOp{Run: func(ctx context.Context, tx meta.Tx) (Outcome, error) {
		sy, bulk, err := svc.CreateSync(ctx, tx, queue.SyncSpec{
			RepoID: t.ID, Level: queue.Level(in.Level), Subject: subject, MaxVersions: b.MaxVersions, MaxBytes: b.MaxBytes,
			IncludePrereleases: b.IncludePrereleases, Range: b.versionRange(), CreatedBy: callerID(ctx),
		})
		if err != nil {
			return Outcome{}, s.syncError(ctx, acc, t, err)
		}
		if err := s.syncEvent(ctx, r, tx, t, audit.EventRemoteSyncCreated, sy); err != nil {
			return Outcome{}, err
		}
		if err := s.downloadEvent(ctx, r, tx, t, audit.EventRemoteDownloadStarted, map[string]any{
			"level": in.Level, "subject": subject, "bounds": b, "bulk": bulk.ID, "sync": sy.ID, "count": 0,
		}); err != nil {
			return Outcome{}, err
		}
		return Outcome{
			Status: http.StatusCreated,
			Body:   map[string]any{"sync": s.syncView(ctx, acc, t, sy, &bulk, nil), "bulk": s.bulkView(ctx, acc, t, bulk, nil)},
			Header: map[string]string{"Location": downloadsBase(t.Key) + "/download-syncs"},
		}, nil
	}})
	if err != nil {
		httpserver.Problem(w, r, err)
	}
}

// syncAct runs one act on a sync in an idempotent operation, for the holders of manage and admin.
func (s *Server) syncAct(w http.ResponseWriter, r *http.Request, key qname.Repo, op string,
	act func(ctx context.Context, tx meta.Tx, svc *queue.Service, acc *edge.Access, t downloadTarget) (Outcome, error),
) {
	ctx, svc, acc, t, err := s.downloadSetup(r.Context(), key)
	if err == nil && s.idem == nil {
		err = unavailable("The download queue")
	}
	if err == nil {
		err = mayKeepInSync(ctx, acc, key)
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, op))
		return
	}
	err = s.idem.Do(w, r, nil, IdempotentOp{Run: func(ctx context.Context, tx meta.Tx) (Outcome, error) {
		out, err := act(ctx, tx, svc, acc, t)
		if err != nil {
			return Outcome{}, s.syncError(ctx, acc, t, err)
		}
		return out, nil
	}})
	if err != nil {
		httpserver.Problem(w, r, err)
	}
}

// RunRemoteDownloadSync implements POST …/download-syncs/{syncId}/run (R-CUR-027, Run now).
func (s *Server) RunRemoteDownloadSync(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey, syncID string, _ gen.RunRemoteDownloadSyncParams) {
	s.syncAct(w, r, qname.Repo{Project: projectKey, Key: repoKey}, "api.runRemoteDownloadSync",
		func(ctx context.Context, tx meta.Tx, svc *queue.Service, acc *edge.Access, t downloadTarget) (Outcome, error) {
			sy, bulk, err := svc.RunSync(ctx, tx, t.ID, syncID, callerID(ctx))
			if err != nil {
				return Outcome{}, err
			}
			if err := s.downloadEvent(ctx, r, tx, t, audit.EventRemoteDownloadStarted, map[string]any{
				"level": sy.Level, "subject": sy.Subject, "bounds": SyncBounds(sy), "bulk": bulk.ID, "sync": sy.ID, "count": 0,
			}); err != nil {
				return Outcome{}, err
			}
			return Outcome{
				Status: http.StatusAccepted,
				Body:   map[string]any{"sync": s.syncView(ctx, acc, t, sy, &bulk, nil), "bulk": s.bulkView(ctx, acc, t, bulk, nil)},
				Header: map[string]string{"Location": downloadsBase(t.Key) + "/download-bulks/" + bulk.ID},
			}, nil
		})
}

// PauseRemoteDownloadSync implements POST …/download-syncs/{syncId}/pause (R-CUR-027).
func (s *Server) PauseRemoteDownloadSync(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey, syncID string, _ gen.PauseRemoteDownloadSyncParams) {
	s.pauseSync(w, r, qname.Repo{Project: projectKey, Key: repoKey}, syncID, true)
}

// ResumeRemoteDownloadSync implements POST …/download-syncs/{syncId}/resume (R-CUR-027).
func (s *Server) ResumeRemoteDownloadSync(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey, syncID string, _ gen.ResumeRemoteDownloadSyncParams) {
	s.pauseSync(w, r, qname.Repo{Project: projectKey, Key: repoKey}, syncID, false)
}

func (s *Server) pauseSync(w http.ResponseWriter, r *http.Request, key qname.Repo, syncID string, paused bool) {
	op, event := "api.resumeRemoteDownloadSync", audit.EventRemoteSyncResumed
	if paused {
		op, event = "api.pauseRemoteDownloadSync", audit.EventRemoteSyncPaused
	}
	s.syncAct(w, r, key, op, func(ctx context.Context, tx meta.Tx, svc *queue.Service, acc *edge.Access, t downloadTarget) (Outcome, error) {
		sy, err := svc.PauseSync(ctx, tx, t.ID, syncID, paused)
		if err != nil {
			return Outcome{}, err
		}
		if err := s.syncEvent(ctx, r, tx, t, event, sy); err != nil {
			return Outcome{}, err
		}
		_, last, rows, _, err := svc.Reader().GetSync(ctx, t.ID, syncID)
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{Status: http.StatusOK, Body: s.syncView(ctx, acc, t, sy, last, rows)}, nil
	})
}

// DeleteRemoteDownloadSync implements DELETE …/download-syncs/{syncId} (R-CUR-027): a run in progress goes on as a bulk.
func (s *Server) DeleteRemoteDownloadSync(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey, syncID string, _ gen.DeleteRemoteDownloadSyncParams) {
	s.syncAct(w, r, qname.Repo{Project: projectKey, Key: repoKey}, "api.deleteRemoteDownloadSync",
		func(ctx context.Context, tx meta.Tx, svc *queue.Service, _ *edge.Access, t downloadTarget) (Outcome, error) {
			sy, err := svc.DeleteSync(ctx, tx, t.ID, syncID)
			if err != nil {
				return Outcome{}, err
			}
			if err := s.syncEvent(ctx, r, tx, t, audit.EventRemoteSyncDeleted, sy); err != nil {
				return Outcome{}, err
			}
			return Outcome{Status: http.StatusNoContent}, nil
		})
}

// syncPreview is what keeping the previewed package or sub-section in sync would do (R-CUR-027): whether the rules deliver the
// package, the repository's interval and pause, and the sync of the same subject; the first run's count is filled in by the
// preview where it counts the versions (firstRun). nil for one version.
func (s *Server) syncPreview(ctx context.Context, t downloadTarget, in downloadRequest) (map[string]any, error) {
	if in.Level == string(queue.LevelVersion) {
		return nil, nil
	}
	level, subject, delivered := queue.LevelPackage, in.Name, true
	if in.Level == string(queue.LevelSection) {
		level, subject = queue.LevelSection, in.Section
	} else {
		delivered = s.downloadDecider().Package(ctx, strconv.FormatInt(t.ID, 10), in.Name).Delivered
	}
	out := map[string]any{"versions": nil, "delivered": delivered, "intervalSeconds": t.Syncs.IntervalSeconds, "paused": t.Syncs.Paused, "existing": nil}
	if svc := s.o.RemoteDownloads.Queue; svc != nil {
		sy, found, err := svc.SyncOf(ctx, t.ID, level, subject)
		if err != nil {
			return nil, err
		}
		if found {
			out["existing"] = sy.ID
		}
	}
	return out, nil
}

// firstRun counts the versions a sync's first run would queue of the versions the preview picked (Q-CUR-243, Q-CUR-244): those the
// rules deliver that the repository does not store as version rows.
func (s *Server) firstRun(ctx context.Context, t downloadTarget, name string, picked []discover.VersionFacts) (int, error) {
	repoID := strconv.FormatInt(t.ID, 10)
	delivered := make([]string, 0, len(picked))
	for _, v := range picked {
		if s.downloadDecider().Decide(ctx, repoID, name, v.Version).Delivered {
			delivered = append(delivered, v.Version)
		}
	}
	svc := s.o.RemoteDownloads.Queue
	if svc == nil {
		return len(delivered), nil
	}
	fixed, err := svc.Fixed(ctx, t.ID, name, delivered)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, v := range delivered {
		if !fixed[v] {
			n++
		}
	}
	return n, nil
}
