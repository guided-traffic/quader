package ciworkflows

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The scripts that choose a report or a record by its run (tools/fetch-e2e-reports.sh, tools/publish-coverage-record.sh,
// Q-ARCH-811) run here against a git repository of their own and recorded answers of the Actions API: a curl stub serves the
// list of runs per workflow, the artifacts per run and each artifact's zip from files, and fails the calls named in
// STUB_FAIL. Nothing reaches GitHub; the fixtures are a few hundred bytes each.
const apiStub = `#!/usr/bin/env bash
cfg="$(cat)"
case "$cfg" in *"Authorization: Bearer stub-token"*) ;; *) echo "curl stub: no token on stdin" >&2; exit 2 ;; esac
out="" url=""
while [ $# -gt 0 ]; do
  case $1 in -o) out=$2; shift ;; http*) url=$1 ;; esac
  shift
done
echo "$url" >> "$STUB_LOG"
for f in ${STUB_FAIL:-}; do
  case "$url" in *"$f"*) echo "curl: (22) The requested URL returned error: 503" >&2; exit 22 ;; esac
done
case "$url" in
  */actions/workflows/*/runs*) w="${url#*/actions/workflows/}"; src="$STUB_DIR/runs-${w%%/*}" ;;
  */actions/runs/*/artifacts*) r="${url#*/actions/runs/}"; src="$STUB_DIR/artifacts-${r%%/*}.json" ;;
  */actions/artifacts/*/zip) a="${url#*/actions/artifacts/}"; src="$STUB_DIR/zip-${a%%/*}.zip" ;;
  *) echo "curl stub: unknown URL $url" >&2; exit 2 ;;
esac
[ -e "$src" ] || src="$STUB_DIR/empty.json"
if [ -n "$out" ]; then cp "$src" "$out"; else cat "$src"; fi
`

const stubRepo = "guided-traffic/quader"

// recordedRun is a run of a workflow in the recorded answers.
type recordedRun struct {
	id        int
	sha       string
	age       time.Duration // how long before now the run started
	event     string        // push by default
	conc      string        // conclusion, success by default
	repo      string        // the head repository, this one by default
	artifacts map[string]string
}

// apiFixture writes the recorded answers: runs per workflow file, the artifacts of each run, and a zip per artifact holding
// the given content as test-e2e.json (or as the file the name says, for the record and the badge).
type apiFixture struct {
	dir  string
	next int
}

func newAPIFixture(t *testing.T) *apiFixture {
	t.Helper()
	f := &apiFixture{dir: t.TempDir(), next: 100}
	writeFile(t, filepath.Join(f.dir, "empty.json"), `{"workflow_runs":[],"artifacts":[]}`)
	writeFile(t, filepath.Join(f.dir, "curl"), apiStub)
	if err := os.Chmod(filepath.Join(f.dir, "curl"), 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *apiFixture) runs(t *testing.T, workflow string, runs ...recordedRun) {
	t.Helper()
	var list []map[string]any
	for _, r := range runs {
		event, conc, repo := r.event, r.conc, r.repo
		if event == "" {
			event = "push"
		}
		if conc == "" {
			conc = "success"
		}
		if repo == "" {
			repo = stubRepo
		}
		list = append(list, map[string]any{
			"id": r.id, "head_sha": r.sha, "event": event, "conclusion": conc, "status": "completed",
			"created_at":      time.Now().Add(-r.age).UTC().Format(time.RFC3339),
			"head_repository": map[string]any{"full_name": repo},
		})
		var arts []map[string]any
		for name, content := range r.artifacts {
			f.next++
			arts = append(arts, map[string]any{"id": f.next, "name": name, "expired": false})
			f.zip(t, f.next, name, content)
		}
		writeJSON(t, filepath.Join(f.dir, fmt.Sprintf("artifacts-%d.json", r.id)), map[string]any{"artifacts": arts})
	}
	writeJSON(t, filepath.Join(f.dir, "runs-"+workflow), map[string]any{"workflow_runs": list})
}

func (f *apiFixture) zip(t *testing.T, id int, name, content string) {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	files := map[string]string{"test-e2e.json": content}
	switch name {
	case "coverage-evidence":
		files = map[string]string{"implemented.txt": content, "checklist.md": "# checklist of " + content, "README.md": "# README of " + content}
	case "coverage-badge":
		files = map[string]string{"coverage.json": content}
	}
	for n, c := range files {
		fw, err := w.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(c)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, fmt.Sprintf("zip-%d.zip", id)), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(data))
}

// history is a git repository with the commits c1 to c4 on main, one after the other, and x, a commit beside c3 that is no
// ancestor of c4.
type history struct {
	dir string
	sha map[string]string
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t",
		"GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newHistory(t *testing.T) *history {
	t.Helper()
	for _, tool := range []string{"git", "bash", "jq", "unzip"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on PATH", tool)
		}
	}
	h := &history{dir: t.TempDir(), sha: map[string]string{}}
	gitIn(t, h.dir, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(h.dir, "docs", "coverage", "README.md"), "# Coverage\n")
	gitIn(t, h.dir, "add", ".")
	for _, c := range []string{"c1", "c2", "c3"} {
		gitIn(t, h.dir, "commit", "-q", "--allow-empty", "-m", c)
		h.sha[c] = gitIn(t, h.dir, "rev-parse", "HEAD")
	}
	gitIn(t, h.dir, "checkout", "-q", "-b", "side")
	gitIn(t, h.dir, "commit", "-q", "--allow-empty", "-m", "x")
	h.sha["x"] = gitIn(t, h.dir, "rev-parse", "HEAD")
	gitIn(t, h.dir, "checkout", "-q", "main")
	gitIn(t, h.dir, "commit", "-q", "--allow-empty", "-m", "c4")
	h.sha["c4"] = gitIn(t, h.dir, "rev-parse", "HEAD")
	return h
}

// runScript runs a script of tools/ in the history's checkout with the stubbed API.
func runScript(t *testing.T, h *history, f *apiFixture, script string, env []string, args ...string) (int, string, []string) {
	t.Helper()
	path, err := filepath.Abs(filepath.Join(repoRoot, "tools", script))
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(f.dir, "calls.log")
	_ = os.Remove(log)
	cmd := exec.Command("bash", append([]string{path}, args...)...)
	cmd.Dir = h.dir
	cmd.Env = append([]string{
		"PATH=" + f.dir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + f.dir, "TMPDIR=" + t.TempDir(),
		"GITHUB_TOKEN=stub-token", "GITHUB_REPOSITORY=" + stubRepo, "GITHUB_RUN_ID=999", "GITHUB_SHA=" + h.sha["c4"],
		"GITHUB_API_URL=https://api.github.invalid", "STUB_DIR=" + f.dir, "STUB_LOG=" + log,
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
	}, env...)
	out, err := cmd.CombinedOutput()
	status := 0
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		status = exit.ExitCode()
	case err != nil:
		t.Fatal(err)
	}
	var calls []string
	if raw, err := os.ReadFile(log); err == nil {
		calls = strings.Split(strings.TrimSpace(string(raw)), "\n")
	}
	return status, string(out), calls
}

// reportOf is a whole `go test -json` report whose one test carries the marker word, so the test can tell which one was taken.
func reportOf(marker string) string {
	return `{"Action":"start","Package":"e2e"}` + "\n" +
		`{"Action":"pass","Package":"e2e","Test":"Test` + marker + `"}` + "\n" + `{"Action":"pass","Package":"e2e"}` + "\n"
}

// cutReportOf is a report whose package started and never ended: its job was stopped while the set ran.
func cutReportOf(marker string) string {
	return `{"Action":"start","Package":"e2e"}` + "\n" + `{"Action":"pass","Package":"e2e","Test":"Test` + marker + `"}` + "\n"
}

// The carried report of a leg is the one made for the nearest commit, this one or an ancestor, never the one created last; a
// run of a commit beside this one, a cancelled run, a pull request's run and a fork's run are never sources; a report that is
// not whole is passed over for the next; a leg whose reports are all older than the bound is not carried; a report of this run
// that is not whole is set aside and its leg carried.
func TestFetchE2EReportsChoosesByCommit(t *testing.T) {
	t.Parallel()
	h := newHistory(t)
	f := newAPIFixture(t)
	f.runs(t, "release.yml",
		recordedRun{id: 1, sha: h.sha["c3"], age: 2 * time.Hour, artifacts: map[string]string{
			"e2e-oci-report-oci": reportOf("OciOfC3"), "e2e-deb-report-deb": cutReportOf("DebOfC3"),
		}},
		recordedRun{id: 2, sha: h.sha["c2"], age: time.Hour, artifacts: map[string]string{
			"e2e-oci-report-oci": reportOf("OciOfC2"), "e2e-deb-report-deb": reportOf("DebOfC2"),
		}},
		recordedRun{id: 3, sha: h.sha["x"], age: 10 * time.Minute, artifacts: map[string]string{"e2e-oci-report-oci": reportOf("OciOfX")}},
		recordedRun{id: 4, sha: h.sha["c3"], conc: "cancelled", artifacts: map[string]string{"e2e-npm-report-npm": reportOf("Cancelled")}},
		recordedRun{id: 5, sha: h.sha["c3"], event: "pull_request", artifacts: map[string]string{"e2e-npm-report-npm": reportOf("PullRequest")}},
		recordedRun{id: 6, sha: h.sha["c3"], repo: "someone/quader", artifacts: map[string]string{"e2e-npm-report-npm": reportOf("Fork")}},
		recordedRun{id: 7, sha: h.sha["c1"], age: 200 * time.Hour, artifacts: map[string]string{"e2e-ansible-report-ansible": reportOf("Old")}},
		recordedRun{id: 999, sha: h.sha["c4"], artifacts: map[string]string{"e2e-report-push": reportOf("Self")}},
	)
	f.runs(t, "performance.yml",
		recordedRun{id: 11, sha: h.sha["c1"], age: 5 * time.Hour, event: "schedule", artifacts: map[string]string{"test-performance": reportOf("Latency")}})

	here := filepath.Join(t.TempDir(), "reports-e2e")
	writeFile(t, filepath.Join(here, "e2e-report-push", "test-e2e.json"), reportOf("PushOfThisRun"))
	writeFile(t, filepath.Join(here, "e2e-npm-report-npm", "test-e2e.json"), cutReportOf("NpmOfThisRun"))
	dest := filepath.Join(t.TempDir(), "carried")
	status, out, calls := runScript(t, h, f, "fetch-e2e-reports.sh", nil, here, dest)
	if status != 0 {
		t.Fatalf("exit %d:\n%s", status, out)
	}
	carried := func(name string) string {
		data, err := os.ReadFile(filepath.Join(dest, name, "test-e2e.json"))
		if err != nil {
			return ""
		}
		return string(data)
	}
	for name, want := range map[string]string{
		"e2e-oci-report-oci":         "OciOfC3", // c3 is nearer than c2, whose run was created later; x is no ancestor
		"e2e-deb-report-deb":         "DebOfC2", // the report of c3 is cut
		"test-performance":           "Latency", // the nightly is a source like Test and Release
		"e2e-npm-report-npm":         "",        // this run's is cut, and no run that is a source has one
		"e2e-ansible-report-ansible": "",        // older than the bound
		"e2e-report-push":            "",        // this run has it
	} {
		got := carried(name)
		if want == "" && got != "" || want != "" && !strings.Contains(got, want) {
			t.Errorf("%s carried %q, want %q\n%s", name, got, want, out)
		}
	}
	for _, line := range []string{
		"carried: e2e-oci-report-oci from run 1 (commit " + h.sha["c3"][:9] + ", 1 commits before this one",
		"not whole: e2e-deb-report-deb of run 1",
		"carried: e2e-deb-report-deb from run 2 (commit " + h.sha["c2"][:9] + ", 2 commits before this one",
		"not whole: e2e-npm-report-npm of this run",
		"no whole report: e2e-npm-report-npm",
		"too old: e2e-ansible-report-ansible",
		"carried reports: 3",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("the output lacks %q:\n%s", line, out)
		}
	}
	if _, err := os.Stat(filepath.Join(here+".cut", "e2e-npm-report-npm", "test-e2e.json")); err != nil {
		t.Errorf("this run's cut report was not set aside: %v", err)
	}
	if _, err := os.Stat(filepath.Join(here, "e2e-report-push", "test-e2e.json")); err != nil {
		t.Errorf("this run's whole report was moved: %v", err)
	}
	for _, c := range calls {
		for _, run := range []string{"/runs/3/", "/runs/4/", "/runs/5/", "/runs/6/", "/runs/999/"} {
			if strings.Contains(c, run) {
				t.Errorf("the script listed the artifacts of a run that is no source: %s", c)
			}
		}
	}
}

// record is what the branch coverage holds after a run of the publish script: the record, the badge and the head's trailers.
type record struct {
	implemented, badge, message, parents string
}

func branchRecord(t *testing.T, bare string) record {
	t.Helper()
	read := func(path string) string {
		cmd := exec.Command("git", "show", "coverage:"+path)
		cmd.Dir = bare
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return string(out)
	}
	return record{
		implemented: read("docs/coverage/implemented.txt"),
		badge:       read(".github/badges/coverage.json"),
		message:     gitIn(t, bare, "log", "-1", "--format=%B", "coverage"),
		parents:     gitIn(t, bare, "log", "-1", "--format=%P", "coverage"),
	}
}

func badgeOf(pct string) string {
	return `{"schemaVersion":1,"label":"coverage","message":"` + pct + `","color":"green"}`
}

// The record and the badge are published onto the branch coverage from the nearest run of this commit or an ancestor that wrote
// them, never from a newer commit's run; the push is a fast-forward, never forced; a second run with nothing newer writes
// nothing; a badge alone moves the badge; an older record than the branch's is not published; a refused push is decided again;
// a push refused three times and an API that does not answer end with a warning and exit 0; main is never written.
func TestPublishCoverageRecordFromTheNearestRun(t *testing.T) {
	t.Parallel()
	h := newHistory(t)
	bare := t.TempDir()
	gitIn(t, bare, "init", "-q", "--bare")
	gitIn(t, bare, "config", "receive.denyNonFastForwards", "true")
	gitIn(t, h.dir, "remote", "add", "origin", bare)
	gitIn(t, h.dir, "push", "-q", "origin", "main")
	mainBefore := gitIn(t, bare, "rev-parse", "main")

	f := newAPIFixture(t)
	f.runs(t, "release.yml",
		recordedRun{id: 1, sha: h.sha["c3"], age: 2 * time.Hour, artifacts: map[string]string{"coverage-evidence": "R-A-001\n", "coverage-badge": badgeOf("80%")}},
		recordedRun{id: 2, sha: h.sha["c2"], age: time.Hour, artifacts: map[string]string{"coverage-evidence": "R-A-002\n", "coverage-badge": badgeOf("70%")}},
		recordedRun{id: 3, sha: h.sha["x"], age: time.Minute, artifacts: map[string]string{"coverage-evidence": "R-X-001\n", "coverage-badge": badgeOf("99%")}},
	)
	status, out, _ := runScript(t, h, f, "publish-coverage-record.sh", nil)
	if status != 0 {
		t.Fatalf("exit %d:\n%s", status, out)
	}
	got := branchRecord(t, bare)
	if got.implemented != "R-A-001\n" || !strings.Contains(got.badge, "80%") {
		t.Fatalf("the branch holds %q and %q, want the record and the badge of run 1 (c3):\n%s", got.implemented, got.badge, out)
	}
	for _, want := range []string{
		"chore(coverage): the evidence record of " + h.sha["c3"][:9], "[skip ci]",
		"Record-Commit: " + h.sha["c3"], "Record-Run: 1", "Badge-Commit: " + h.sha["c3"], "Released-Commit: " + h.sha["c4"],
	} {
		if !strings.Contains(got.message, want) {
			t.Errorf("the commit message lacks %q:\n%s", want, got.message)
		}
	}
	if got.parents != h.sha["c3"] {
		t.Errorf("the first commit of the branch has the parents %q, want the record's commit", got.parents)
	}
	if checklist := gitIn(t, bare, "show", "coverage:docs/coverage/checklist.md"); checklist != "# checklist of R-A-001" {
		t.Errorf("checklist.md on the branch: %q", checklist)
	}
	first := gitIn(t, bare, "rev-parse", "coverage")

	t.Run("nothing newer writes nothing", func(t *testing.T) {
		status, out, _ := runScript(t, h, f, "publish-coverage-record.sh", nil)
		if status != 0 || !strings.Contains(out, "nothing newer to publish") || gitIn(t, bare, "rev-parse", "coverage") != first {
			t.Fatalf("exit %d, the branch moved or the script did not say so:\n%s", status, out)
		}
	})

	// c4's run wrote the badge alone; the record stays that of c3.
	f.runs(t, "release.yml",
		recordedRun{id: 4, sha: h.sha["c4"], age: time.Minute, artifacts: map[string]string{"coverage-badge": badgeOf("81%")}},
		recordedRun{id: 1, sha: h.sha["c3"], age: 2 * time.Hour, artifacts: map[string]string{"coverage-evidence": "R-A-001\n", "coverage-badge": badgeOf("80%")}},
	)
	status, out, _ = runScript(t, h, f, "publish-coverage-record.sh", nil)
	got = branchRecord(t, bare)
	if status != 0 || got.implemented != "R-A-001\n" || !strings.Contains(got.badge, "81%") || got.parents != first {
		t.Fatalf("exit %d; the branch holds %q, %q with the parents %q; want the record of c3, the badge of c4 on top of %s:\n%s",
			status, got.implemented, got.badge, got.parents, first, out)
	}
	if !strings.Contains(got.message, "Record-Commit: "+h.sha["c3"]) || !strings.Contains(got.message, "Badge-Commit: "+h.sha["c4"]) {
		t.Errorf("the trailers do not name the record of c3 and the badge of c4:\n%s", got.message)
	}
	second := gitIn(t, bare, "rev-parse", "coverage")

	t.Run("an older record than the branch's is not published", func(t *testing.T) {
		f2 := newAPIFixture(t)
		f2.runs(t, "release.yml", recordedRun{id: 2, sha: h.sha["c2"], artifacts: map[string]string{"coverage-evidence": "R-A-002\n", "coverage-badge": badgeOf("70%")}})
		status, out, _ := runScript(t, h, f2, "publish-coverage-record.sh", nil)
		if status != 0 || gitIn(t, bare, "rev-parse", "coverage") != second {
			t.Fatalf("exit %d; the branch moved to an older record:\n%s", status, out)
		}
	})

	t.Run("an API that does not answer ends with a warning and exit 0", func(t *testing.T) {
		status, out, _ := runScript(t, h, f, "publish-coverage-record.sh", []string{"STUB_FAIL=/runs?"})
		if status != 0 || !strings.Contains(out, "::warning::") || gitIn(t, bare, "rev-parse", "coverage") != second {
			t.Fatalf("exit %d:\n%s", status, out)
		}
	})

	// A refused push: the hook refuses the first attempts, as a push of another job in between would.
	hook := filepath.Join(bare, "hooks", "pre-receive")
	count := filepath.Join(t.TempDir(), "count")
	refuse := func(times int) {
		writeFile(t, hook, fmt.Sprintf("#!/bin/sh\nn=$(cat %q 2>/dev/null || echo 0); echo $((n+1)) > %q; [ \"$n\" -ge %d ]\n", count, count, times))
		if err := os.Chmod(hook, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(count)
	}
	f.runs(t, "release.yml",
		recordedRun{id: 5, sha: h.sha["c4"], age: time.Minute, artifacts: map[string]string{"coverage-evidence": "R-A-004\n", "coverage-badge": badgeOf("82%")}})

	refuse(3)
	status, out, _ = runScript(t, h, f, "publish-coverage-record.sh", nil)
	if status != 0 || !strings.Contains(out, "::warning::the record and the badge were not published") || gitIn(t, bare, "rev-parse", "coverage") != second {
		t.Fatalf("three refused pushes: exit %d, or the branch moved:\n%s", status, out)
	}

	refuse(1)
	status, out, _ = runScript(t, h, f, "publish-coverage-record.sh", nil)
	got = branchRecord(t, bare)
	if status != 0 || got.implemented != "R-A-004\n" || !strings.Contains(out, "refused (attempt 1)") || !strings.Contains(out, "published to coverage") {
		t.Fatalf("one refused push: exit %d, the branch holds %q:\n%s", status, got.implemented, out)
	}
	if got.parents != second+" "+h.sha["c4"] {
		t.Errorf("the parents are %q, want the branch's head and the record's commit", got.parents)
	}
	if after := gitIn(t, bare, "rev-parse", "main"); after != mainBefore {
		t.Errorf("main moved from %s to %s", mainBefore, after)
	}
}

// The wiring of Q-ARCH-811: the evidence job writes the record from the reports with the whole history checked out and without
// comparing a committed list; it requires the tiers that run on every push and no leg; Semantic Release needs neither coverage
// job, publishes after the release in a step that cannot fail it, and holds contents: read and actions: read and no more (the
// push uses the App's token); no release plugin commits files to main.
func TestTheRecordIsPublishedAfterTheReleaseAndHoldsNothing(t *testing.T) {
	t.Parallel()
	w := loadWorkflows(t)["release.yml"]
	evidence := w.Jobs["coverage-evidence"]
	for _, tier := range []string{"unit", "web", "integration", "chart"} {
		if !strings.Contains(evidence.If, "needs."+tier+".result == 'success'") {
			t.Errorf("coverage-evidence runs if %q, which does not require %s to have passed", evidence.If, tier)
		}
	}
	var checkout, record string
	for _, s := range evidence.Steps {
		if strings.HasPrefix(s.Uses, "actions/checkout@") {
			checkout = s.With["fetch-depth"]
		}
		if strings.Contains(s.Run, "go run ./internal/tools/coverage") {
			record = s.Run
		}
	}
	if checkout != "0" {
		t.Errorf("coverage-evidence checks out fetch-depth %q; the carried reports are chosen by the history (0)", checkout)
	}
	if strings.Contains(record, "-claims") || !strings.Contains(record, "-previous") {
		t.Errorf("the record step compares a committed list or does not compare with the branch's record:\n%s", record)
	}

	release := w.Jobs["semantic-release"]
	var needs []string
	if err := release.Needs.Decode(&needs); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"coverage-evidence", "coverage-report"} {
		if slices.Contains(needs, n) {
			t.Errorf("semantic-release needs %s; the release never waits for the record (Q-ARCH-811)", n)
		}
	}
	if len(release.Permissions) != 2 || release.Permissions["contents"] != "read" || release.Permissions["actions"] != "read" {
		t.Errorf("semantic-release holds %v, want contents: read and actions: read", release.Permissions)
	}
	releaseAt, publishAt := -1, -1
	for i, s := range release.Steps {
		if strings.Contains(s.Run, "npx semantic-release") {
			releaseAt = i
		}
		if strings.Contains(s.Run, "tools/publish-coverage-record.sh") {
			publishAt = i
			if s.ContinueOnError != "true" {
				t.Errorf("the publish step has continue-on-error %q; it must not fail the job after the release", s.ContinueOnError)
			}
			if s.Env["GITHUB_TOKEN"] != "${{ github.token }}" {
				t.Errorf("the publish step calls the API with %q, want the job's own token", s.Env["GITHUB_TOKEN"])
			}
		}
	}
	if releaseAt < 0 || publishAt <= releaseAt {
		t.Errorf("the publish step is at %d, the release at %d: the record is published after the release", publishAt, releaseAt)
	}

	raw, err := os.ReadFile(filepath.Join(repoRoot, ".releaserc.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"@semantic-release/git"`) {
		t.Error(".releaserc.json names @semantic-release/git; the release commits nothing to main (Q-ARCH-811)")
	}
}
