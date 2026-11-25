package v1

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/fills"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// UpstreamDownloadReader reads the downloads from upstreams as PostgreSQL holds them (operations-and-observability.md R-OPS-052): every
// live row with its age, the newest finished downloads, and the names they need. *fills.Store implements it.
type UpstreamDownloadReader interface {
	ReadUpstreamDownloads(ctx context.Context) (fills.View, error)
}

// ListUpstreamDownloads implements GET /api/v1/system/upstream-downloads (R-OPS-052); content:metadata-read is enforced before it runs. A
// replica without the reader answers 503.
func (s *Server) ListUpstreamDownloads(w http.ResponseWriter, r *http.Request) {
	v, ok := s.readUpstreamDownloads(w, r)
	if !ok {
		return
	}
	writeJSON(w, r, http.StatusOK, upstreamDownloadList(v, s.o.UpstreamDownloadInterval))
}

// GetUpstreamDownloadSummary implements GET /api/v1/system/upstream-downloads/summary (R-OPS-052): the count of the list's runningCount.
func (s *Server) GetUpstreamDownloadSummary(w http.ResponseWriter, r *http.Request) {
	v, ok := s.readUpstreamDownloads(w, r)
	if !ok {
		return
	}
	list := upstreamDownloadList(v, s.o.UpstreamDownloadInterval)
	writeJSON(w, r, http.StatusOK, gen.UpstreamDownloadSummary{Running: list.RunningCount, PollSeconds: list.PollSeconds})
}

func (s *Server) readUpstreamDownloads(w http.ResponseWriter, r *http.Request) (fills.View, bool) {
	if s.o.UpstreamDownloads == nil {
		httpserver.Problem(w, r, errs.E(errs.Unavailable, errs.CodeUnavailable, "The downloads from upstreams cannot be read on this replica."))
		return fills.View{}, false
	}
	v, err := s.o.UpstreamDownloads.ReadUpstreamDownloads(r.Context())
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.listUpstreamDownloads"))
		return fills.View{}, false
	}
	return v, true
}

// upstreamDownloadList builds the answer from a read (R-OPS-050 to R-OPS-052): a live row older than three publish intervals is left
// out (its replica stalled or stopped); the elapsed time is the one at the write plus the age of the write; an entry whose repository is
// gone is left out; running entries in the order of their begin, finished ones as read (newest first); the queues an upstream's rate
// limit holds as read, the longest held first, with the upstream's name (remote-curation.md Q-CUR-176).
func upstreamDownloadList(v fills.View, interval time.Duration) gen.UpstreamDownloadList {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	out := gen.UpstreamDownloadList{
		AsOf: v.Now.UTC(), PollSeconds: interval.Seconds(),
		Running: []gen.UpstreamDownloadRunning{}, Finished: []gen.UpstreamDownloadFinished{}, Waiting: []gen.UpstreamDownloadWaiting{},
	}
	for _, l := range v.Limited {
		w := gen.UpstreamDownloadWaiting{
			Repository: gen.UpstreamDownloadRepository{Project: l.Repo.Project, Key: l.Repo.Key, Format: l.Repo.Format},
			Waiting:    l.Waiting, Upstream: upstreamName(l.Repo.Format, l.URL), Since: l.Since, Until: l.Until, Hinted: l.Hinted,
		}
		if l.Status != 0 {
			st := l.Status
			w.Status = &st
		}
		out.Waiting = append(out.Waiting, w)
	}
	fresh := fills.FreshIntervals * interval
	for _, l := range v.Live {
		if l.Age > fresh {
			continue
		}
		out.RunningCount += int64(l.Omitted)
		for _, e := range l.Downloads {
			out.RunningCount++
			repo, ok := v.Repos[e.RepoID]
			if !ok {
				continue
			}
			out.Running = append(out.Running, runningDownload(e, l, repo, v.Principals))
		}
	}
	sort.SliceStable(out.Running, func(i, j int) bool { return out.Running[i].StartedAt.Before(out.Running[j].StartedAt) })
	for _, f := range v.Finished {
		repo, ok := v.Repos[f.RepoID]
		if !ok {
			continue
		}
		out.Finished = append(out.Finished, finishedDownload(f, repo, v.Principals))
	}
	return out
}

func runningDownload(e fills.Entry, l fills.Live, repo fills.RepoName, names map[string]string) gen.UpstreamDownloadRunning {
	d := gen.UpstreamDownloadRunning{
		Id: uuidOf(e.ID), Repository: gen.UpstreamDownloadRepository{Project: repo.Project, Key: repo.Key, Format: repo.Format},
		Name: e.Name, Version: e.Version, Path: e.Path, Bytes: e.Bytes, Total: e.Total, BytesPerSecond: e.Speed,
		StartedAt: e.StartedAt.UTC(), ElapsedSeconds: (time.Duration(e.ElapsedMs)*time.Millisecond + l.Age).Seconds(),
		Origin: gen.UpstreamDownloadOrigin(e.Origin), QueueRow: uuidPtr(e.QueueRow), By: principalOf(e.By, names), Replica: l.Replica,
	}
	if e.QueueOrigin != "" {
		qo := gen.UpstreamDownloadQueueOrigin(e.QueueOrigin)
		d.QueueOrigin = &qo
	}
	return d
}

func finishedDownload(f fills.Finished, repo fills.RepoName, names map[string]string) gen.UpstreamDownloadFinished {
	d := gen.UpstreamDownloadFinished{
		Id: uuidOf(f.ID), Repository: gen.UpstreamDownloadRepository{Project: repo.Project, Key: repo.Key, Format: repo.Format},
		Name: f.Name, Version: f.Version, Path: f.Path, Bytes: f.Bytes, Total: f.Total,
		StartedAt: f.StartedAt.UTC(), FinishedAt: f.FinishedAt.UTC(), DurationSeconds: float64(f.DurationMs) / 1000,
		Outcome: gen.UpstreamDownloadFinishedOutcome(f.Outcome), Origin: gen.UpstreamDownloadOrigin(f.Origin), Replica: f.Replica,
	}
	if f.DurationMs > 0 {
		avg := f.Bytes * 1000 / f.DurationMs
		d.BytesPerSecond = &avg
	}
	if f.Outcome == fills.OutcomeFailed {
		d.Failure = &gen.UpstreamDownloadFailure{Class: gen.RemoteDownloadFailureClass(f.FailureClass), Code: f.FailureCode}
	}
	if f.QueueOrigin != "" {
		qo := gen.UpstreamDownloadQueueOrigin(f.QueueOrigin)
		d.QueueOrigin = &qo
	}
	if f.QueueRow != nil {
		d.QueueRow = uuidPtr(*f.QueueRow)
	}
	if f.By != nil {
		d.By = principalOf(*f.By, names)
	}
	return d
}

// principalOf names a principal by its current name; nil for none, a removed one, anonymous and authenticated (absent from names).
func principalOf(id string, names map[string]string) *gen.UpstreamDownloadPrincipal {
	name, ok := names[id]
	if id == "" || !ok {
		return nil
	}
	return &gen.UpstreamDownloadPrincipal{Id: uuidOf(id), Name: name}
}

func uuidPtr(s string) *uuid.UUID {
	if s == "" {
		return nil
	}
	u, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return &u
}
