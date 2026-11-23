package ciworkflows

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The decision which e2e legs of Test and Release a change runs (.github/scripts/e2e-legs.sh; owner, 2026-11-20, Q-ARCH-540 to
// Q-ARCH-543; docs/developer/releasing.md#which-e2e-legs-a-change-runs). The script runs here against a git repository of its own
// in a temporary directory: a base commit with the real release.yml and tools/versions.env, and one commit per case. No network.

// e2eFormats are the formats with a leg of their own; e2eLegs are the legs the script decides, in the order of its outputs.
var (
	e2eFormats = []string{"npm", "nuget", "oci", "deb", "ansible"}
	e2eLegs    = append(slices.Clone(e2eFormats), "bastion", "vuln")
)

// gitEnv keeps the developer's git configuration (signing, hooks, templates) out of the test repository.
func gitEnv(home string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + filepath.Join(home, "gitconfig"),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid",
		"GIT_TEMPLATE_DIR=",
	}
}

type legsRepo struct {
	dir, home, base string
}

func (r legsRepo) git(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = gitEnv(r.home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// newLegsRepo is a repository whose base commit holds the real release.yml and tools/versions.env and a few files of the tree.
func newLegsRepo(t *testing.T) legsRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	r := legsRepo{dir: t.TempDir(), home: t.TempDir()}
	r.git(t, "init", "-q", "-b", "main")
	r.git(t, "config", "commit.gpgsign", "false")
	files := map[string]string{
		".github/workflows/release.yml":     readRepoFile(t, ".github/workflows/release.yml"),
		"tools/versions.env":                readRepoFile(t, "tools/versions.env"),
		"internal/formats/oci/a.go":         "package oci\n",
		"internal/formats/deb/b.go":         "package deb\n",
		"README.md":                         "# test\n",
		"go.mod":                            "module example\n\ngo 1.27\n\nrequire example.org/x v1.0.0\n",
		"go.sum":                            "example.org/x v1.0.0 h1:x\n",
		".github/workflows/performance.yml": "on: schedule\n",
	}
	for p, text := range files {
		write(t, filepath.Join(r.dir, p), text)
	}
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "base")
	r.base = r.git(t, "rev-parse", "HEAD")
	return r
}

// change commits, on top of the base, the files of edit (path to content; an empty content removes the file) and returns the commit.
func (r legsRepo) change(t *testing.T, edit map[string]string) string {
	t.Helper()
	r.git(t, "checkout", "-q", "--detach", r.base)
	for p, text := range edit {
		full := filepath.Join(r.dir, p)
		if text == "" {
			if err := os.Remove(full); err != nil {
				t.Fatal(err)
			}
			continue
		}
		write(t, full, text)
	}
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "change")
	return r.git(t, "rev-parse", "HEAD")
}

type legsRun struct {
	status  int
	log     string
	outputs map[string]string
	lines   int // lines of GITHUB_OUTPUT
}

func (r legsRepo) run(t *testing.T, event, before, head string) legsRun {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	script, err := filepath.Abs(filepath.Join(repoRoot, ".github", "scripts", "e2e-legs.sh"))
	if err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	output := filepath.Join(state, "output")
	cmd := exec.Command(bash, script)
	cmd.Dir = r.dir
	cmd.Env = append(gitEnv(r.home), "TMPDIR="+state, "EVENT="+event, "BEFORE="+before, "HEAD="+head,
		"GITHUB_OUTPUT="+output, "GITHUB_STEP_SUMMARY="+filepath.Join(state, "summary"))
	out, err := cmd.CombinedOutput()
	res := legsRun{log: string(out), outputs: map[string]string{}}
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		res.status = exit.ExitCode()
	case err != nil:
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(output)
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			res.outputs[k] = v
			res.lines++
		}
	}
	return res
}

// running are the legs whose output is true; the script must write every leg exactly once.
func (res legsRun) running(t *testing.T) []string {
	t.Helper()
	if res.status != 0 {
		t.Fatalf("e2e-legs.sh exited %d:\n%s", res.status, res.log)
	}
	if res.lines != len(e2eLegs) {
		t.Errorf("GITHUB_OUTPUT holds %d lines, want one per leg (%d):\n%s", res.lines, len(e2eLegs), res.log)
	}
	var on []string
	for _, leg := range e2eLegs {
		switch res.outputs[leg] {
		case "true":
			on = append(on, leg)
		case "false":
		default:
			t.Errorf("output %s=%q, want true or false", leg, res.outputs[leg])
		}
	}
	return on
}

// replaceAfter replaces the first old after the first line equal to anchor.
func replaceAfter(t *testing.T, text, anchor, old, repl string) string {
	t.Helper()
	at := strings.Index(text, "\n"+anchor+"\n")
	if at < 0 {
		t.Fatalf("no line %q", anchor)
	}
	rest := text[at:]
	i := strings.Index(rest, old)
	if i < 0 {
		t.Fatalf("no %q after %q", old, anchor)
	}
	return text[:at] + rest[:i] + repl + rest[i+len(old):]
}

// The cases of a push, each with the legs it runs.
func TestE2ELegsOfAPush(t *testing.T) {
	t.Parallel()
	r := newLegsRepo(t)
	workflow := readRepoFile(t, ".github/workflows/release.yml")
	pins := readRepoFile(t, "tools/versions.env")
	for _, c := range []struct {
		name string
		edit map[string]string
		want []string
	}{
		{"documentation only (such a push starts no run at all)", map[string]string{"docs/developer/releasing.md": "x\n", "docs/spec/formats/oci.md": "x\n"}, nil},
		{"only the OCI format's package", map[string]string{"internal/formats/oci/handlers.go": "package oci\n"}, []string{"oci"}},
		{"only the npm format's package", map[string]string{"internal/formats/npm/handlers.go": "package npm\n"}, []string{"npm"}},
		{"only the HTTP server, shared code", map[string]string{"internal/httpserver/server.go": "package httpserver\n"}, []string{"npm"}},
		{"a new directory under internal/ on no list", map[string]string{"internal/brandnew/thing.go": "package brandnew\n"}, []string{"npm"}},
		{"a path outside every list", map[string]string{"newtopdir/file.txt": "x\n"}, []string{"npm"}},
		{"only web/", map[string]string{"web/src/app/app.ts": "export {};\n", "web/src/app/shared/button.html": "<button></button>\n"}, nil},
		{"a screen the Ansible journey opens", map[string]string{"web/src/app/features/packages/ansible-docs.ts": "export {};\n"}, []string{"ansible"}},
		{"its unit test", map[string]string{"web/src/app/features/packages/ansible-docs.spec.ts": "export {};\n"}, nil},
		{"the web workspace's lockfile, which the Ansible leg installs", map[string]string{"web/package-lock.json": "{}\n"}, []string{"ansible"}},
		{"a format's e2e test", map[string]string{"test/e2e/deb_local_test.go": "package e2e\n"}, []string{"deb"}},
		{"a format's unit test", map[string]string{"internal/formats/oci/a_test.go": "package oci\n"}, nil},
		{"an integration test of a format", map[string]string{"test/integration/ociremote/x_test.go": "package ociremote\n"}, nil},
		{"a format's file in a shared package", map[string]string{"internal/config/nuget.go": "package config\n"}, []string{"nuget"}},
		{"the forwarder of every client image", map[string]string{"test/e2e/clients/fwd/main.go": "package main\n"}, []string{"npm"}},
		{"npm's client image", map[string]string{"test/e2e/clients/Dockerfile": "FROM scratch\n"}, []string{"npm"}},
		{"Ansible's browser journey", map[string]string{"web/e2e/ansible-docs/01-ansible-docs.spec.ts": "x\n"}, []string{"ansible"}},
		{"two formats", map[string]string{"internal/formats/oci/b.go": "package oci\n", "internal/formatdata/deb/c.go": "package deb\n"}, []string{"oci", "deb"}},
		{"a file moved out of a format", map[string]string{"internal/formats/oci/a.go": "", "internal/shared/a.go": "package oci\n"}, []string{"npm", "oci"}},
		{"the Bastion's code", map[string]string{"internal/bastion/engine.go": "package bastion\n"}, []string{"npm", "bastion"}},
		{"the Bastion's chart", map[string]string{"deploy/helm/quader-bastion/values.yaml": "x: 1\n"}, []string{"bastion"}},
		{"the e2e harness", map[string]string{"deploy/dev/e2e.sh": "#!/bin/sh\n"}, []string{"npm", "vuln"}},
		{"the scanning's queries", map[string]string{"internal/meta/postgres/queries/vuln_api.sql": "-- x\n"}, []string{"npm", "vuln"}},
		{"release.yml: a line of the OCI job", map[string]string{
			".github/workflows/release.yml": replaceAfter(t, workflow, "  e2e-oci:", "timeout-minutes: 90", "timeout-minutes: 91"),
		}, []string{"oci"}},
		{"release.yml: the comment above the Debian job", map[string]string{
			".github/workflows/release.yml": strings.Replace(workflow, "  # The Debian (APT) acceptance suite", "  # The Debian (APT) suite", 1),
		}, []string{"deb"}},
		{"release.yml: a line of the unit job", map[string]string{
			".github/workflows/release.yml": replaceAfter(t, workflow, "  unit:", "timeout-minutes: 30", "timeout-minutes: 31"),
		}, nil},
		{"release.yml: the header", map[string]string{
			".github/workflows/release.yml": strings.Replace(workflow, `GOMAXPROCS: "8"`, `GOMAXPROCS: "6"`, 1),
		}, []string{"npm"}},
		{"release.yml: a line of the Bastion job", map[string]string{
			".github/workflows/release.yml": replaceAfter(t, workflow, "  e2e-bastion:", "timeout-minutes: 90", "timeout-minutes: 91"),
		}, []string{"bastion"}},
		{"versions.env: the Docker image of the OCI clients", map[string]string{
			"tools/versions.env": regexp.MustCompile(`(?m)^E2E_OCICLIENTS_DOCKER_IMAGE=.*$`).ReplaceAllString(pins, "E2E_OCICLIENTS_DOCKER_IMAGE=docker:0"),
		}, []string{"oci"}},
		{"versions.env: a pin every leg uses", map[string]string{
			"tools/versions.env": regexp.MustCompile(`(?m)^KIND_VERSION=.*$`).ReplaceAllString(pins, "KIND_VERSION=v0.0.0"),
		}, []string{"npm"}},
		// The set vuln loads the analyser's and ClamAV's images into kind; no other leg runs them (G3-16).
		{"versions.env: the analyser's image", map[string]string{
			"tools/versions.env": regexp.MustCompile(`(?m)^TRIVY_IMAGE=.*$`).ReplaceAllString(pins, "TRIVY_IMAGE=docker.io/aquasec/trivy:0"),
		}, []string{"vuln"}},
		{"versions.env: ClamAV's image", map[string]string{
			"tools/versions.env": regexp.MustCompile(`(?m)^CLAMAV_IMAGE=.*$`).ReplaceAllString(pins, "CLAMAV_IMAGE=docker.io/clamav/clamav-debian:0"),
		}, []string{"vuln"}},
	} {
		head := r.change(t, c.edit)
		res := r.run(t, "push", r.base, head)
		got := res.running(t)
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: legs %v, want %v\nlog:\n%s", c.name, got, c.want, res.log)
		}
	}
}

// A pull request is decided like a push, from what its head changed since the merge base with its base, so that a dependency
// bot's pull requests take the legs their change concerns and not every leg on the shared runners.
func TestE2ELegsOfAPullRequest(t *testing.T) {
	t.Parallel()
	r := newLegsRepo(t)
	workflow := readRepoFile(t, ".github/workflows/release.yml")
	pins := readRepoFile(t, "tools/versions.env")
	for _, c := range []struct {
		name string
		edit map[string]string
		want []string
	}{
		{"only a format's paths", map[string]string{"internal/formats/deb/handlers.go": "package deb\n", "test/e2e/deb_local_test.go": "package e2e\n"}, []string{"deb"}},
		{"only shared code", map[string]string{"internal/mirror/pass.go": "package mirror\n"}, []string{"npm"}},
		{"only documentation", map[string]string{"docs/developer/releasing.md": "x\n"}, nil},
		{"a dependency bump of go.mod and go.sum", map[string]string{"go.mod": "module example\n\ngo 1.27\n\nrequire example.org/x v1.0.1\n", "go.sum": "example.org/x v1.0.1 h1:x\n"}, []string{"npm"}},
		{"a bump of one client pin", map[string]string{
			"tools/versions.env": regexp.MustCompile(`(?m)^E2E_OCICLIENTS_DOCKER_IMAGE=.*$`).ReplaceAllString(pins, "E2E_OCICLIENTS_DOCKER_IMAGE=docker:0"),
		}, []string{"oci"}},
		{"a bump of an action that every job uses", map[string]string{
			".github/workflows/release.yml": strings.ReplaceAll(workflow, "actions/checkout@v7", "actions/checkout@v8"),
		}, e2eLegs},
		{"a bump of an action in the nightly alone", map[string]string{".github/workflows/performance.yml": "on: push\n"}, nil},
	} {
		head := r.change(t, c.edit)
		res := r.run(t, "pull_request", r.base, head)
		got := res.running(t)
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: legs %v, want %v\nlog:\n%s", c.name, got, c.want, res.log)
		}
	}
	// The base branch moved on after the pull request was opened: its own commits are not the pull request's.
	moved := r.change(t, map[string]string{"internal/formats/nuget/later.go": "package nuget\n"})
	head := r.change(t, map[string]string{"internal/formats/oci/b.go": "package oci\n"})
	res := r.run(t, "pull_request", moved, head)
	if got := res.running(t); !slices.Equal(got, []string{"oci"}) {
		t.Errorf("a pull request behind its base: legs %v, want [oci]\n%s", got, res.log)
	}
	if !strings.Contains(res.log, "merge base "+r.base[:12]) {
		t.Errorf("the log does not name the merge base %s:\n%s", r.base[:12], res.log)
	}
}

// Every leg runs on a dispatch, on a push without a commit to compare with, on a pull request whose base is not in the clone, and
// when the script fails.
func TestE2ELegsAllRunWithoutAChangeToJudge(t *testing.T) {
	t.Parallel()
	r := newLegsRepo(t)
	head := r.change(t, map[string]string{"internal/formats/oci/b.go": "package oci\n"})
	for _, c := range []struct {
		name, event, before, head, says string
	}{
		{"a dispatch", "workflow_dispatch", "", head, "runs every e2e leg"},
		{"a pull request whose base is not in the clone", "pull_request", "1234567890123456789012345678901234567890", head, "no commit to compare with"},
		{"the first push of a branch", "push", "0000000000000000000000000000000000000000", head, "no commit to compare with"},
		{"a push without before", "push", "", head, "no commit to compare with"},
		{"a before that is not in the clone", "push", "1234567890123456789012345678901234567890", head, "no commit to compare with"},
		{"a head that is not in the clone", "push", r.base, "1234567890123456789012345678901234567890", "::warning::"},
	} {
		res := r.run(t, c.event, c.before, c.head)
		if got := res.running(t); !slices.Equal(got, e2eLegs) {
			t.Errorf("%s: legs %v, want every leg\n%s", c.name, got, res.log)
		}
		if !strings.Contains(res.log, c.says) {
			t.Errorf("%s: the log does not say %q:\n%s", c.name, c.says, res.log)
		}
	}
}

// Every pattern of every list matches a file of the tree, so a list cannot hold a pattern that never starts its leg (the old
// list of the vuln leg had two: queries/vuln_ anchored at the root, internal/api/v1/vuln_api).
func TestE2ELegsEveryPatternMatchesAFile(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	ls := exec.Command("git", "ls-files")
	ls.Dir = repoRoot
	raw, err := ls.Output()
	if err != nil {
		t.Skipf("the tree is not a git checkout: %v", err)
	}
	files := strings.Split(strings.TrimSpace(string(raw)), "\n")
	lists := exec.Command("bash", filepath.Join(repoRoot, ".github", "scripts", "e2e-legs.sh"), "--lists")
	out, err := lists.Output()
	if err != nil {
		t.Fatalf("e2e-legs.sh --lists: %v", err)
	}
	n := 0
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		list, pattern, ok := strings.Cut(l, "\t")
		if !ok {
			t.Fatalf("a line of --lists without a tab: %q", l)
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			t.Errorf("list %s: pattern %q: %v", list, pattern, err)
			continue
		}
		n++
		if !slices.ContainsFunc(files, re.MatchString) {
			t.Errorf("list %s: pattern %q matches no file of the tree", list, pattern)
		}
	}
	if n < 50 {
		t.Fatalf("only %d patterns; --lists is read wrongly", n)
	}
}

// The legs take their run from the job e2e-scope: each needs it and the job image, runs unless the decision said false (an empty
// output, as from a failed decision, runs the leg), and has no scope step of its own; e2e-scope runs on the standard runners and
// outputs every leg; the coverage evidence still runs when a leg was skipped; Semantic Release needs neither the decision nor a leg.
func TestTheE2ELegsTakeTheirRunFromTheDecision(t *testing.T) {
	t.Parallel()
	w := loadWorkflows(t)["release.yml"]
	scope, ok := w.Jobs["e2e-scope"]
	if !ok {
		t.Fatal("release.yml has no job e2e-scope")
	}
	if scope.RunsOn != "self-hosted" {
		t.Errorf("e2e-scope runs on %q; it belongs on the standard runners (self-hosted)", scope.RunsOn)
	}
	if !slices.ContainsFunc(scope.Steps, func(s step) bool { return strings.Contains(s.Run, ".github/scripts/e2e-legs.sh") }) {
		t.Error("e2e-scope does not run .github/scripts/e2e-legs.sh")
	}
	needsOf := func(name string, j job) []string {
		var needs []string
		if err := j.Needs.Decode(&needs); err != nil {
			var one string
			if err := j.Needs.Decode(&one); err != nil {
				t.Fatalf("the needs of %s: %v", name, err)
			}
			needs = []string{one}
		}
		return needs
	}
	for _, leg := range e2eLegs {
		name := "e2e-" + leg
		if want := "${{ steps.decide.outputs." + leg + " }}"; scope.Outputs[leg] != want {
			t.Errorf("e2e-scope output %s is %q, want %q", leg, scope.Outputs[leg], want)
		}
		j, ok := w.Jobs[name]
		if !ok {
			t.Errorf("release.yml has no job %s", name)
			continue
		}
		needs := needsOf(name, j)
		if !slices.Contains(needs, "e2e-scope") || !slices.Contains(needs, "image") {
			t.Errorf("%s needs %v, want e2e-scope and image", name, needs)
		}
		for _, part := range []string{"!cancelled()", "needs.image.result == 'success'", "needs.e2e-scope.outputs." + leg + " != 'false'"} {
			if !strings.Contains(j.If, part) {
				t.Errorf("%s runs if %q, which lacks %q", name, j.If, part)
			}
		}
		for _, s := range j.Steps {
			if s.ID == "scope" || strings.Contains(s.If, "steps.scope") {
				t.Errorf("%s step %q still decides its scope itself", name, s.Name)
			}
		}
	}
	evidence := w.Jobs["coverage-evidence"]
	for _, part := range []string{"!cancelled()", "'failure'", "'cancelled'"} {
		if !strings.Contains(evidence.If, part) {
			t.Errorf("coverage-evidence runs if %q, which lacks %q: a skipped leg would skip it", evidence.If, part)
		}
	}
	needs := needsOf("semantic-release", w.Jobs["semantic-release"])
	for _, leg := range append([]string{"scope"}, e2eLegs...) {
		if slices.Contains(needs, "e2e-"+leg) {
			t.Errorf("semantic-release needs e2e-%s; until the first stable release no e2e leg gates it (Q-ARCH-457)", leg)
		}
	}
}

// Every format's set runs in the nightly, in a job that latency waits for (owner, 2026-11-20: what only another format's client
// shows of a change of shared code is found there).
func TestEveryFormatSetRunsInTheNightly(t *testing.T) {
	t.Parallel()
	w := loadWorkflows(t)["performance.yml"]
	latency := w.Jobs["latency"]
	var waits []string
	if err := latency.Needs.Decode(&waits); err != nil {
		t.Fatalf("the needs of latency: %v", err)
	}
	for _, set := range e2eFormats {
		found := ""
		for name, j := range w.Jobs {
			if slices.Contains(setsOfJob(j), set) {
				found = name
			}
		}
		switch {
		case found == "":
			t.Errorf("no job of performance.yml runs the set %s", set)
		case !slices.Contains(waits, found):
			t.Errorf("latency does not wait for %s, which runs the set %s", found, set)
		}
	}
}
