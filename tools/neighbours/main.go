// Command neighbours names the jobs of Test and Release that ran on the self-hosted runners while a leg of the nightly ran
// (Q-ARCH-1062, docs/developer/releasing.md#a-code-push-during-a-nightly). A push of code may come at any time (the owner's rule),
// and its kind legs share the runner nodes with the nightly's load legs: ten of them beside a load leg made it six to nine times
// slower (F-164). So the leg itself writes down who ran beside it, and its numbers are read with that record: a latency miss with
// a push's legs beside it is noted and opens nothing.
//
// Run as a step of the leg after its test step, with the job's token:
//
//	go run ./tools/neighbours -step 'make e2e SET=' -out .out/e2e/neighbours.md
//
// The window is the test step's (the first step of this job whose name starts with -step and has ended), else the job's own from
// its start to now. The job is found in its run by RUNNER_NAME. The neighbours are the jobs of every run of -workflow whose time
// on a runner overlaps the window, on a runner with the label large (the kind legs) or self-hosted (the rest; both classes share
// the nodes); a GitHub-hosted job shares no machine and is left out. The record goes to -out, to the job summary and, when a kind
// leg ran beside, to a notice on the run. Every failure of the API is a sentence in the record and exit 0: the record never fails
// a leg.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// apiJob is a job of the Actions API, the fields read here.
type apiJob struct {
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	RunnerName  string     `json:"runner_name"`
	Labels      []string   `json:"labels"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
	Steps       []struct {
		Name        string     `json:"name"`
		StartedAt   *time.Time `json:"started_at"`
		CompletedAt *time.Time `json:"completed_at"`
	} `json:"steps"`
}

type apiRun struct {
	ID        int64     `json:"id"`
	Status    string    `json:"status"`
	HTMLURL   string    `json:"html_url"`
	HeadSHA   string    `json:"head_sha"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// neighbour is one job beside the window.
type neighbour struct {
	Run     apiRun
	Job     string
	Class   string // large or self-hosted
	From    time.Time
	To      time.Time
	Overlap time.Duration
	Running bool
}

// client reads the Actions API of one repository.
type client struct {
	api, repo, token string
	http             *http.Client
}

func (c client) get(path string, query url.Values, into any) error {
	u := strings.TrimSuffix(c.api, "/") + "/repos/" + c.repo + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s: %s", path, resp.Status, strings.TrimSpace(string(body[:min(len(body), 200)])))
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	return nil
}

// maxPages bounds every listing: a run has fewer than 100 jobs, and a window of a few hours sees fewer than 500 runs.
const maxPages = 5

func (c client) jobs(runID int64) ([]apiJob, error) {
	var all []apiJob
	for page := 1; page <= maxPages; page++ {
		var r struct {
			Jobs []apiJob `json:"jobs"`
		}
		q := url.Values{"per_page": {"100"}, "page": {fmt.Sprint(page)}, "filter": {"latest"}}
		if err := c.get(fmt.Sprintf("/actions/runs/%d/jobs", runID), q, &r); err != nil {
			return nil, err
		}
		all = append(all, r.Jobs...)
		if len(r.Jobs) < 100 {
			break
		}
	}
	return all, nil
}

func (c client) runs(workflow string, since time.Time) ([]apiRun, error) {
	var all []apiRun
	for page := 1; page <= maxPages; page++ {
		var r struct {
			Runs []apiRun `json:"workflow_runs"`
		}
		q := url.Values{"per_page": {"100"}, "page": {fmt.Sprint(page)}, "created": {">=" + since.UTC().Format(time.RFC3339)}}
		if err := c.get("/actions/workflows/"+url.PathEscape(workflow)+"/runs", q, &r); err != nil {
			return nil, err
		}
		all = append(all, r.Runs...)
		if len(r.Runs) < 100 {
			break
		}
	}
	return all, nil
}

// lookBack is how long before the window a run of the workflow may have been created and still run in it: a run of Test and
// Release takes about an hour, its jobs wait for runners, and a job may take its 75 minutes.
const lookBack = 4 * time.Hour

// window finds this job in its run and returns the window of its test step, or of the job itself until now.
func window(c client, runID int64, runner, stepPrefix string, now time.Time) (from, to time.Time, what string, err error) {
	jobs, err := c.jobs(runID)
	if err != nil {
		return from, to, "", err
	}
	for _, j := range jobs {
		if j.RunnerName != runner || j.Status != "in_progress" || j.StartedAt == nil {
			continue
		}
		if stepPrefix != "" {
			for _, s := range j.Steps {
				if strings.HasPrefix(s.Name, stepPrefix) && s.StartedAt != nil && s.CompletedAt != nil {
					return *s.StartedAt, *s.CompletedAt, fmt.Sprintf("the step %q of the job %q", s.Name, j.Name), nil
				}
			}
		}
		return *j.StartedAt, now, fmt.Sprintf("the job %q from its start", j.Name), nil
	}
	return from, to, "", fmt.Errorf("no job of run %d in progress on the runner %q", runID, runner)
}

// class is the runner class of a job, or "" for a GitHub-hosted runner.
func class(labels []string) string {
	has := func(l string) bool {
		for _, x := range labels {
			if x == l {
				return true
			}
		}
		return false
	}
	switch {
	case has("large"):
		return "large"
	case has("self-hosted"):
		return "self-hosted"
	}
	return ""
}

// beside lists the jobs of the workflow's runs that ran on a self-hosted runner within [from, to].
func beside(c client, workflow string, from, to, now time.Time) ([]neighbour, error) {
	runs, err := c.runs(workflow, from.Add(-lookBack))
	if err != nil {
		return nil, err
	}
	var out []neighbour
	for _, r := range runs {
		if r.CreatedAt.After(to) || (r.Status == "completed" && r.UpdatedAt.Before(from)) {
			continue
		}
		jobs, err := c.jobs(r.ID)
		if err != nil {
			return nil, err
		}
		for _, j := range jobs {
			k := class(j.Labels)
			if k == "" || j.StartedAt == nil {
				continue
			}
			end, running := now, true
			if j.CompletedAt != nil {
				end, running = *j.CompletedAt, false
			}
			a, b := maxTime(from, *j.StartedAt), minTime(to, end)
			if !b.After(a) {
				continue
			}
			out = append(out, neighbour{Run: r, Job: j.Name, Class: k, From: *j.StartedAt, To: end, Overlap: b.Sub(a), Running: running})
		}
	}
	sort.Slice(out, func(i, k int) bool {
		if out[i].Class != out[k].Class {
			return out[i].Class == "large"
		}
		return out[i].From.Before(out[k].From)
	})
	return out, nil
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// report is the record in Markdown, and the one line a notice carries ("" when no kind leg ran beside).
func report(workflowName, what string, from, to time.Time, ns []neighbour) (string, string) {
	var b strings.Builder
	fmt.Fprintf(&b, "### Jobs of %s beside this leg\n\n", workflowName)
	fmt.Fprintf(&b, "Window: %s, %s to %s UTC (%s).\n\n", what, from.UTC().Format("15:04:05"), to.UTC().Format("15:04:05"),
		to.Sub(from).Round(time.Second))
	var large, small int
	var largeMin, smallMin float64
	runs := map[int64]bool{}
	for _, n := range ns {
		runs[n.Run.ID] = true
		if n.Class == "large" {
			large++
			largeMin += n.Overlap.Minutes()
		} else {
			small++
			smallMin += n.Overlap.Minutes()
		}
	}
	summary := fmt.Sprintf("neighbours: %d kind legs on large (%.1f runner minutes inside the window) and %d other jobs on self-hosted (%.1f) of %d runs of %s",
		large, largeMin, small, smallMin, len(runs), workflowName)
	b.WriteString(summary + ".\n\n")
	notice := ""
	if large > 0 {
		notice = fmt.Sprintf("%d kind legs of %s ran on the large runners beside this leg (%.1f runner minutes inside its window): its times are not the product's alone (F-164)",
			large, workflowName, largeMin)
		b.WriteString("Its times were measured beside them and are not the product's alone: a miss is read with these rows and the " +
			"recorder's samples (docs/developer/e2e.md#reading-a-stall), and a miss with a kind leg beside it opens nothing.\n\n")
	} else if small == 0 {
		b.WriteString("No job of it ran on the self-hosted runners in the window.\n\n")
	}
	if len(ns) > 0 {
		b.WriteString("| Run | Job | Runner | On the runner (UTC) | Inside the window |\n|---|---|---|---|---|\n")
		for _, n := range ns {
			until := n.To.UTC().Format("15:04:05")
			if n.Running {
				until = "still running"
			}
			fmt.Fprintf(&b, "| [%d](%s) (%s) | %s | %s | %s to %s | %.1f min |\n", n.Run.ID, n.Run.HTMLURL, short(n.Run.HeadSHA), n.Job,
				n.Class, n.From.UTC().Format("15:04:05"), until, n.Overlap.Minutes())
		}
	}
	return b.String(), notice
}

func short(sha string) string {
	if len(sha) > 9 {
		return sha[:9]
	}
	return sha
}

// run is the command without its process: the record, the notice and nothing that fails.
func run(env func(string) string, args []string, stdout io.Writer, now time.Time) error {
	fs := flag.NewFlagSet("neighbours", flag.ContinueOnError)
	workflow := fs.String("workflow", "release.yml", "the file name of the workflow whose jobs are the neighbours")
	workflowName := fs.String("name", "Test and Release", "the workflow's name, for the record")
	step := fs.String("step", "", "the start of the name of this job's test step, whose time is the window; empty: the job's own")
	out := fs.String("out", "", "the file the record is written to (its directory is created)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var record, notice string
	runID, err := parseID(env("GITHUB_RUN_ID"))
	c := client{api: env("GITHUB_API_URL"), repo: env("GITHUB_REPOSITORY"), token: env("GITHUB_TOKEN"), http: &http.Client{Timeout: 30 * time.Second}}
	if c.api == "" {
		c.api = "https://api.github.com"
	}
	if err == nil && (c.repo == "" || env("RUNNER_NAME") == "") {
		err = errors.New("GITHUB_REPOSITORY or RUNNER_NAME is not set")
	}
	var from, to time.Time
	var what string
	if err == nil {
		from, to, what, err = window(c, runID, env("RUNNER_NAME"), *step, now)
	}
	var ns []neighbour
	if err == nil {
		ns, err = beside(c, *workflow, from, to, now)
	}
	if err != nil {
		record = fmt.Sprintf("### Jobs of %s beside this leg\n\nNot known: %v. The leg's times are read without this record.\n", *workflowName, err)
	} else {
		record, notice = report(*workflowName, what, from, to, ns)
	}
	_, _ = io.WriteString(stdout, record)
	if notice != "" {
		_, _ = fmt.Fprintf(stdout, "::notice title=Jobs of %s beside this leg::%s\n", *workflowName, notice)
	}
	if *out != "" {
		if err := writeFile(*out, record); err != nil {
			_, _ = fmt.Fprintf(stdout, "::warning::the record could not be written to %s: %v\n", *out, err)
		}
	}
	if summary := env("GITHUB_STEP_SUMMARY"); summary != "" {
		f, err := os.OpenFile(filepath.Clean(summary), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // G304: the runner names its job summary file
		if err == nil {
			_, _ = io.WriteString(f, record+"\n")
			_ = f.Close()
		}
	}
	return nil
}

func parseID(s string) (int64, error) {
	var id int64
	if _, err := fmt.Sscan(s, &id); err != nil || id <= 0 {
		return 0, fmt.Errorf("GITHUB_RUN_ID %q is not a run", s)
	}
	return id, nil
}

func writeFile(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o600)
}

func main() {
	if err := run(os.Getenv, os.Args[1:], os.Stdout, time.Now()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
