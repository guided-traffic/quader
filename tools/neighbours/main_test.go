package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The record of a nightly leg's neighbours (Q-ARCH-1062): an Actions API served in process with the answers of a night in which a
// push ran beside a load leg. Unit, milliseconds, no network.

var t0 = time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC)

func at(min int) *time.Time { v := t0.Add(time.Duration(min) * time.Minute); return &v }

type fakeAPI struct {
	nightJobs any
	runs      any
	jobsOf    map[string]any
	fail      bool
	queries   []string
}

func (f *fakeAPI) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.queries = append(f.queries, r.URL.Path+"?"+r.URL.RawQuery)
		if r.Header.Get("Authorization") != "Bearer token-of-the-job" {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		if f.fail {
			http.Error(w, `{"message":"Server Error"}`, http.StatusBadGateway)
			return
		}
		var body any
		switch {
		case r.URL.Path == "/repos/example/quader/actions/runs/900/jobs":
			body = f.nightJobs
		case r.URL.Path == "/repos/example/quader/actions/workflows/release.yml/runs":
			body = f.runs
		case strings.HasPrefix(r.URL.Path, "/repos/example/quader/actions/runs/"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/example/quader/actions/runs/"), "/jobs")
			body = f.jobsOf[id]
		}
		if body == nil {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

type j = map[string]any

func job(name, runner string, labels []string, start, end *time.Time, steps ...j) j {
	status := "completed"
	if end == nil {
		status = "in_progress"
	}
	return j{"name": name, "status": status, "runner_name": runner, "labels": labels, "started_at": start, "completed_at": end, "steps": steps}
}

func night() *fakeAPI {
	return &fakeAPI{
		// This leg: the job started at 03:00, its test step ran from 03:10 to 03:40.
		nightJobs: j{"jobs": []j{
			job("e2e (npm-ha)", "large-other", []string{"large"}, at(-30), at(-1)),
			job("e2e (ha-suite)", "large-abc", []string{"large"}, at(0), nil,
				j{"name": "make dev-up", "started_at": at(2), "completed_at": at(9)},
				j{"name": "make e2e SET=ha-suite", "started_at": at(10), "completed_at": at(40)},
				j{"name": "Upload the go test -json report", "started_at": nil, "completed_at": nil}),
		}},
		runs: j{"workflow_runs": []j{
			{"id": 1, "status": "completed", "html_url": "https://example/runs/1", "head_sha": "aaaaaaaaaaaa", "created_at": at(-200), "updated_at": at(-100)},
			{"id": 2, "status": "completed", "html_url": "https://example/runs/2", "head_sha": "bbbbbbbbbbbb", "created_at": at(5), "updated_at": at(70)},
			{"id": 3, "status": "in_progress", "html_url": "https://example/runs/3", "head_sha": "cccccccccccc", "created_at": at(38), "updated_at": at(39)},
			{"id": 4, "status": "queued", "html_url": "https://example/runs/4", "head_sha": "dddddddddddd", "created_at": at(45), "updated_at": at(45)},
		}},
		jobsOf: map[string]any{
			"2": j{"jobs": []j{
				job("e2e (push)", "large-1", []string{"large"}, at(15), at(35)),               // 20 min inside
				job("e2e (npm)", "large-2", []string{"large"}, at(30), at(55)),                // 10 min inside
				job("unit tests", "small-1", []string{"self-hosted"}, at(8), at(20)),          // 10 min inside
				job("e2e (ci)", "GitHub Actions 7", []string{"ubuntu-24.04"}, at(10), at(30)), // GitHub-hosted: no neighbour
				job("lint", "small-2", []string{"self-hosted"}, at(41), at(50)),               // after the window
				job("waiting", "", []string{"large"}, nil, nil),                               // never started
			}},
			"3": j{"jobs": []j{
				job("versions", "small-3", []string{"self-hosted"}, at(39), nil), // still running: 1 min inside
			}},
		},
	}
}

func env(api string) func(string) string {
	vars := map[string]string{
		"GITHUB_API_URL": api, "GITHUB_REPOSITORY": "example/quader", "GITHUB_RUN_ID": "900", "RUNNER_NAME": "large-abc",
		"GITHUB_TOKEN": "token-of-the-job",
	}
	return func(k string) string { return vars[k] }
}

func TestNeighboursNamesThePushsJobsThatRanBesideTheTestStep(t *testing.T) {
	f := night()
	srv := f.server(t)
	out := filepath.Join(t.TempDir(), "e2e", "neighbours.md")
	var stdout bytes.Buffer
	if err := run(env(srv.URL), []string{"-step", "make e2e SET=", "-out", out}, &stdout, *at(50)); err != nil {
		t.Fatal(err)
	}
	text := stdout.String()
	for _, want := range []string{
		`the step "make e2e SET=ha-suite" of the job "e2e (ha-suite)", 03:10:00 to 03:40:00 UTC (30m0s)`,
		"neighbours: 2 kind legs on large (30.0 runner minutes inside the window) and 2 other jobs on self-hosted (11.0) of 2 runs of Test and Release",
		"| [2](https://example/runs/2) (bbbbbbbbb) | e2e (push) | large | 03:15:00 to 03:35:00 | 20.0 min |",
		"| [2](https://example/runs/2) (bbbbbbbbb) | e2e (npm) | large | 03:30:00 to 03:55:00 | 10.0 min |",
		"| unit tests | self-hosted | 03:08:00 to 03:20:00 | 10.0 min |",
		"| versions | self-hosted | 03:39:00 to still running | 1.0 min |",
		"::notice title=Jobs of Test and Release beside this leg::2 kind legs of Test and Release ran on the large runners beside this leg",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the record does not say %q:\n%s", want, text)
		}
	}
	for _, not := range []string{"e2e (ci)", "lint", "waiting", "runs/1)", "runs/4)"} {
		if strings.Contains(text, not) {
			t.Errorf("the record names %q, which ran on no shared runner inside the window:\n%s", not, text)
		}
	}
	raw, err := os.ReadFile(out)
	if err != nil || !strings.Contains(string(raw), "neighbours: 2 kind legs") || strings.Contains(string(raw), "::notice") {
		t.Errorf("the record file holds %q (%v); want the record without the workflow command", raw, err)
	}
	for _, q := range f.queries {
		if strings.Contains(q, "/workflows/release.yml/runs") && !strings.Contains(q, "created=%3E%3D2026-11-23T23%3A10%3A00Z") {
			t.Errorf("the runs are not listed from four hours before the window: %s", q)
		}
	}
}

// Without a test step that has ended, the window is the job's own until now; without a neighbour the record says so and no
// notice is raised.
func TestNeighboursAQuietWindowIsSaidAndRaisesNoNotice(t *testing.T) {
	f := night()
	f.runs = j{"workflow_runs": []j{}}
	srv := f.server(t)
	var stdout bytes.Buffer
	if err := run(env(srv.URL), []string{"-step", "make test-performance"}, &stdout, *at(50)); err != nil {
		t.Fatal(err)
	}
	text := stdout.String()
	if !strings.Contains(text, `the job "e2e (ha-suite)" from its start, 03:00:00 to 03:50:00 UTC`) ||
		!strings.Contains(text, "No job of it ran on the self-hosted runners in the window.") || strings.Contains(text, "::notice") {
		t.Errorf("a quiet window was not recorded as one:\n%s", text)
	}
}

// An API that refuses, a job that cannot be found or a missing variable is a sentence in the record, never an error of the leg.
func TestNeighboursAFailureIsASentenceAndNeverFailsTheLeg(t *testing.T) {
	f := night()
	f.fail = true
	srv := f.server(t)
	out := filepath.Join(t.TempDir(), "neighbours.md")
	var stdout bytes.Buffer
	if err := run(env(srv.URL), []string{"-out", out}, &stdout, *at(50)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Not known: GET /actions/runs/900/jobs: 502 Bad Gateway") {
		t.Errorf("the API's refusal is not named:\n%s", stdout.String())
	}
	if raw, _ := os.ReadFile(out); !strings.Contains(string(raw), "Not known:") {
		t.Errorf("the record file does not hold the sentence: %q", raw)
	}

	f.fail = false
	missing := env(srv.URL)
	stdout.Reset()
	if err := run(func(k string) string {
		if k == "RUNNER_NAME" {
			return "large-gone"
		}
		return missing(k)
	}, nil, &stdout, *at(50)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `Not known: no job of run 900 in progress on the runner "large-gone"`) {
		t.Errorf("a job not found is not named:\n%s", stdout.String())
	}
	stdout.Reset()
	if err := run(func(string) string { return "" }, nil, &stdout, *at(50)); err != nil || !strings.Contains(stdout.String(), "Not known: GITHUB_RUN_ID") {
		t.Errorf("a missing run id: %v\n%s", err, stdout.String())
	}
}
