package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/fills"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// fakeUpstreamDownloads is the in-memory store of the downloads from upstreams: the view a read gets.
type fakeUpstreamDownloads struct{ v fills.View }

func (f fakeUpstreamDownloads) ReadUpstreamDownloads(context.Context) (fills.View, error) {
	return f.v, nil
}

const (
	udPriya = "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a20"
	udSam   = "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a21"
	udGone  = "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a22"
	udRow   = "01937d4d-1a2b-7c3d-9e4f-5a6b7c8d9e0f"
)

// upstreamDownloadView holds three live rows (a fresh one of a worker with one download of a removed repository and 140 left out, a
// fresh one of packages, one whose replica stopped writing a minute ago) and three finished downloads (one of a removed repository).
func upstreamDownloadView() fills.View {
	t0 := time.Date(2026, 11, 24, 8, 40, 12, 0, time.UTC)
	total, speed := int64(65536), int64(8192)
	row, by := udRow, udSam
	return fills.View{
		Now: t0,
		Live: []fills.Live{
			{InstanceUID: "a", Replica: "quader-worker-1", WrittenAt: t0.Add(-time.Second), Age: time.Second, Omitted: 140, Downloads: []fills.Entry{
				{ID: "01937d4e-0000-7000-8000-000000000002", RepoID: 1, Name: "library/ubuntu", Version: "26.10", Path: "v2/library/ubuntu/blobs/sha256:1a",
					Bytes: 16384, Total: &total, Speed: &speed, StartedAt: t0.Add(-10 * time.Second), ElapsedMs: 9000,
					Origin: "queue", QueueOrigin: "person", QueueRow: udRow, By: udPriya},
				{ID: "01937d4e-0000-7000-8000-000000000003", RepoID: 9, Name: "gone", Path: "gone-1.tgz", StartedAt: t0.Add(-5 * time.Second), Origin: "client"},
			}},
			{InstanceUID: "b", Replica: "quader-packages-1", WrittenAt: t0, Downloads: []fills.Entry{
				{ID: "01937d4e-0000-7000-8000-000000000001", RepoID: 2, Name: "@angular/core", Version: "22.0.1", Path: "@angular/core/-/core-22.0.1.tgz",
					Bytes: 100, StartedAt: t0.Add(-20 * time.Second), ElapsedMs: 20000, Origin: "client", By: udGone},
			}},
			{InstanceUID: "c", Replica: "quader-worker-2", WrittenAt: t0.Add(-time.Minute), Age: time.Minute, Omitted: 3, Downloads: []fills.Entry{
				{ID: "01937d4e-0000-7000-8000-000000000004", RepoID: 1, Name: "stalled", StartedAt: t0.Add(-2 * time.Minute), Origin: "mirror"},
			}},
		},
		Finished: []fills.Finished{
			{ID: "01937d4c-0000-7000-8000-000000000001", RepoID: 2, Name: "lodash", Version: "4.17.21", Path: "lodash/-/lodash-4.17.21.tgz", Bytes: 3000,
				Total: &total, StartedAt: t0.Add(-3 * time.Second), FinishedAt: t0.Add(-time.Second), DurationMs: 1500, Outcome: fills.OutcomeCompleted,
				Origin: "client", By: &by, Replica: "quader-packages-1"},
			{ID: "01937d4c-0000-7000-8000-000000000002", RepoID: 1, Name: "library/alpine", Path: "v2/library/alpine/blobs/sha256:2b", Bytes: 10,
				StartedAt: t0.Add(-4 * time.Second), FinishedAt: t0.Add(-2 * time.Second), Outcome: fills.OutcomeFailed, FailureClass: "integrity",
				FailureCode: "upstream_integrity_mismatch", Origin: "queue", QueueOrigin: "bulk", QueueRow: &row, Replica: "quader-worker-1"},
			{ID: "01937d4c-0000-7000-8000-000000000003", RepoID: 9, Name: "gone", Outcome: fills.OutcomeCompleted, Origin: "mirror", Replica: "quader-worker-1"},
		},
		Repos:      map[int64]fills.RepoName{1: {Project: "upstream", Key: "docker-hub", Format: "oci"}, 2: {Project: "web", Key: "npm-remote", Format: "npm"}},
		Principals: map[string]string{udPriya: "priya", udSam: "sam"},
	}
}

// R-OPS-052: the two operations answer the holders of content:metadata-read (the administrator, the auditor) in the document's shape and
// nobody else; a stale live row, its downloads and its count are left out; elapsedSeconds adds the age of the write; a removed
// repository's entries are left out of the list but counted as running; the summary counts the listed and the left-out downloads.
func TestR_OPS_052_TheDownloadsFromUpstreams(t *testing.T) {
	view := upstreamDownloadView()
	callers := map[string]sigCaller{
		"admin":            {name: "admin", caps: authz.RoleCapabilities(authz.RoleAdmin)},
		"auditor":          {name: "auditor", caps: authz.RoleCapabilities(authz.RoleAuditor)},
		"security-admin":   {name: "security-admin", caps: authz.RoleCapabilities(authz.RoleSecurityAdmin)},
		"repository-admin": {name: "repository-admin", caps: authz.RoleCapabilities(authz.RoleRepositoryAdmin)},
		"owner":            {name: "owner", roles: map[string]string{"web": "owner"}},
		"member":           {name: "member", roles: map[string]string{"web": "reader"}},
		"anonymous":        {name: "anonymous", anon: true},
	}
	want := map[string]int{"admin": 200, "auditor": 200, "security-admin": 403, "repository-admin": 403, "owner": 403, "member": 403, "anonymous": 401}
	if r := requirementOf(t, "listUpstreamDownloads"); r.Permission != string(authz.ContentMetadataRead) {
		t.Errorf("list: %+v", r)
	}
	if r := requirementOf(t, "getUpstreamDownloadSummary"); r.Permission != string(authz.ContentMetadataRead) {
		t.Errorf("summary: %+v", r)
	}
	for name, c := range callers {
		for _, path := range []string{"/api/v1/system/upstream-downloads", "/api/v1/system/upstream-downloads/summary"} {
			t.Run("R-OPS-052 "+name+" reads "+path+": "+http.StatusText(want[name]), func(t *testing.T) {
				s := upstreamDownloadSetup(t, c, view)
				if w := s.get(path); w.Code != want[name] {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
			})
		}
	}

	t.Run("R-OPS-052 the list holds the fresh downloads of existing repositories in the order of their begin, and the last finished", func(t *testing.T) {
		s := upstreamDownloadSetup(t, callers["auditor"], view)
		h := newValidator(t).Handler(t, s.handler)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/system/upstream-downloads", nil))
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%d %q %s", w.Code, w.Header().Get("Cache-Control"), w.Body.String())
		}
		var got gen.UpstreamDownloadList
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		// Two fresh rows: three downloads and 140 left out; the stale row of quader-worker-2 and its three left out are not counted.
		if got.RunningCount != 143 || got.PollSeconds != 2 || len(got.Running) != 2 {
			t.Fatalf("count %d poll %v running %+v", got.RunningCount, got.PollSeconds, got.Running)
		}
		first, second := got.Running[0], got.Running[1]
		if first.Name != "@angular/core" || first.By != nil || first.Repository.Key != "npm-remote" || first.ElapsedSeconds != 20 {
			t.Errorf("the oldest first, the removed principal as nobody: %+v", first)
		}
		if second.Name != "library/ubuntu" || second.ElapsedSeconds != 10 || second.Total == nil || *second.Total != 65536 ||
			second.BytesPerSecond == nil || *second.BytesPerSecond != 8192 || second.QueueOrigin == nil || *second.QueueOrigin != gen.UpstreamDownloadQueuePerson ||
			second.QueueRow == nil || second.QueueRow.String() != udRow || second.By == nil || second.By.Name != "priya" || second.Replica != "quader-worker-1" {
			t.Errorf("the queued download, elapsed 9 s at the write plus 1 s of age: %+v", second)
		}
		if len(got.Finished) != 2 {
			t.Fatalf("finished %+v", got.Finished)
		}
		done, failed := got.Finished[0], got.Finished[1]
		if done.Outcome != gen.UpstreamDownloadCompleted || done.DurationSeconds != 1.5 || done.BytesPerSecond == nil || *done.BytesPerSecond != 2000 ||
			done.Failure != nil || done.By == nil || done.By.Name != "sam" {
			t.Errorf("completed: %+v", done)
		}
		if failed.Outcome != gen.UpstreamDownloadFailed || failed.Failure == nil || failed.Failure.Class != gen.DownloadClassIntegrity ||
			failed.Failure.Code != "upstream_integrity_mismatch" || failed.BytesPerSecond != nil || failed.Total != nil {
			t.Errorf("failed, duration 0 without an average: %+v", failed)
		}
	})

	t.Run("R-OPS-052 the summary counts what the list counts", func(t *testing.T) {
		s := upstreamDownloadSetup(t, callers["admin"], view)
		w := s.get("/api/v1/system/upstream-downloads/summary")
		var got gen.UpstreamDownloadSummary
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.Running != 143 || got.PollSeconds != 2 {
			t.Fatalf("%v %s", err, w.Body.String())
		}
	})

	t.Run("R-OPS-052 a replica without the reader answers 503", func(t *testing.T) {
		s := upstreamDownloadSetup(t, callers["admin"], view, func(o *Options) { o.UpstreamDownloads = nil })
		if w := s.get("/api/v1/system/upstream-downloads"); w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%d", w.Code)
		}
	})
}

func upstreamDownloadSetup(t *testing.T, c sigCaller, v fills.View, opts ...func(*Options)) setup {
	t.Helper()
	authn := func(*http.Request) (httpserver.Principal, error) {
		return httpserver.Principal{Kind: httpserver.KindUser, ID: "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10", Name: c.name}, nil
	}
	if c.anon {
		authn = nil
	}
	return newSetup(t, func(o *Options) {
		o.Access = sigAccess{c}
		o.UpstreamDownloads, o.UpstreamDownloadInterval = fakeUpstreamDownloads{v}, 2*time.Second
		for _, f := range opts {
			f(o)
		}
	}, authn)
}
