package ciworkflows

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type step struct {
	ID             string            `yaml:"id"`
	Name           string            `yaml:"name"`
	If             string            `yaml:"if"`
	Uses           string            `yaml:"uses"`
	With           map[string]string `yaml:"with"`
	Env            map[string]string `yaml:"env"`
	Run            string            `yaml:"run"`
	TimeoutMinutes yaml.Node         `yaml:"timeout-minutes"`
	// ContinueOnError is the raw value of continue-on-error (true or an expression).
	ContinueOnError string `yaml:"continue-on-error"`
}

type job struct {
	Name           string            `yaml:"name"`
	If             string            `yaml:"if"`
	RunsOn         string            `yaml:"runs-on"`
	Outputs        map[string]string `yaml:"outputs"`
	Needs          yaml.Node         `yaml:"needs"`
	Env            map[string]string `yaml:"env"`
	Strategy       yaml.Node         `yaml:"strategy"`
	Permissions    map[string]string `yaml:"permissions"`
	TimeoutMinutes yaml.Node         `yaml:"timeout-minutes"`
	Concurrency    yaml.Node         `yaml:"concurrency"`
	Steps          []step            `yaml:"steps"`
}

type workflow struct {
	Concurrency yaml.Node      `yaml:"concurrency"`
	Jobs        map[string]job `yaml:"jobs"`
}

func loadWorkflows(t *testing.T) map[string]workflow {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(repoRoot, ".github", "workflows", "*.yml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflow files: %v", err)
	}
	out := map[string]workflow{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var w workflow
		if err := yaml.Unmarshal(raw, &w); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		out[filepath.Base(f)] = w
	}
	return out
}

// minutes is a timeout-minutes value; ok is false when it is absent or an expression.
func minutes(n yaml.Node) (int, bool) {
	if n.Kind != yaml.ScalarNode {
		return 0, false
	}
	v, err := strconv.Atoi(n.Value)
	return v, err == nil
}

// installsPackages: the step installs OS packages, through the script or the build-tools action that calls it.
func installsPackages(s step) bool {
	return strings.Contains(s.Run, "apt-install.sh") || strings.HasPrefix(s.Uses, "./.github/actions/build-tools")
}

// downloads names what a step fetches from the network outside the job's main make target, or "" when it fetches nothing of
// that kind: the package install, the actions that download a toolchain, the Go modules and tools (from the cache or the module
// proxy), the clients and Quader images, the pinned zstd (its source archive on a miss), go install, the ClamAV database and npm ci.
func downloads(s step) string {
	if installsPackages(s) {
		return "OS packages"
	}
	actions := []string{
		"actions/setup-go@", "actions/setup-node@", "azure/setup-helm@", "azure/setup-kubectl@", "./.github/actions/install-buildx",
		"./.github/actions/go-modules", "./.github/actions/go-tool", "./.github/actions/client-images", "./.github/actions/quader-image",
		"./.github/actions/zstd",
	}
	for _, a := range actions {
		if strings.HasPrefix(s.Uses, a) {
			return a
		}
	}
	for _, r := range []string{"go install ", "freshclam", "npm ci"} {
		if strings.Contains(s.Run, r) {
			return r
		}
	}
	return ""
}

// scriptWorstCase is the longest apt-install.sh can take, from its own bounds: two updates, two downloads and the unpacking,
// each followed by the KILL delay.
func scriptWorstCase(t *testing.T) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, ".github", "scripts", "apt-install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	value := func(name string) int {
		m := regexp.MustCompile(`(?m)^` + name + `=(\d+)$`).FindSubmatch(raw)
		if m == nil {
			t.Fatalf("apt-install.sh does not set %s", name)
		}
		v, _ := strconv.Atoi(string(m[1]))
		return v
	}
	k := value("kill_after")
	return 2*(value("update_seconds")+k) + 2*(value("download_seconds")+k) + value("unpack_seconds") + k
}

// No file under .github calls apt-get, apt or aptitude itself: only apt-install.sh does, so the next call site cannot bring
// back a package index that hangs a job.
func TestNothingUnderGithubRunsAptGetItself(t *testing.T) {
	bare := regexp.MustCompile(`\bapt-get\b|\baptitude\b|\bapt +(install|update|upgrade)\b`)
	helper := filepath.Join(repoRoot, ".github", "scripts", "apt-install.sh")
	err := filepath.WalkDir(filepath.Join(repoRoot, ".github"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == helper {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if bare.MatchString(line) {
				t.Errorf("%s:%d calls apt itself; install OS packages with .github/scripts/apt-install.sh: %s", path, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The script lives in the checkout, so every step that installs packages comes after actions/checkout in its job.
func TestEveryPackageInstallFollowsTheCheckout(t *testing.T) {
	for file, w := range loadWorkflows(t) {
		for name, j := range w.Jobs {
			checkedOut := false
			for _, s := range j.Steps {
				if strings.HasPrefix(s.Uses, "actions/checkout@") {
					checkedOut = true
				}
				if installsPackages(s) && !checkedOut {
					t.Errorf("%s job %s step %q installs packages before actions/checkout", file, name, s.Name)
				}
			}
		}
	}
}

// The jobs that gate Semantic Release bound every step that downloads, so a host that does not answer fails the step in
// minutes, visibly, instead of holding the job until its limit; a package step's bound lies above the script's own worst
// case (the script reports first) and below the job's limit.
func TestTheGatingJobsBoundTheirDownloads(t *testing.T) {
	w := loadWorkflows(t)["release.yml"]
	release, ok := w.Jobs["semantic-release"]
	if !ok {
		t.Fatal("release.yml has no job semantic-release")
	}
	var needs []string
	if err := release.Needs.Decode(&needs); err != nil || len(needs) == 0 {
		t.Fatalf("the needs of semantic-release: %v %v", needs, err)
	}
	worst := scriptWorstCase(t)
	for _, name := range append(slices.Clone(needs), "semantic-release") {
		j, ok := w.Jobs[name]
		if !ok {
			t.Errorf("semantic-release needs %s, which release.yml does not define", name)
			continue
		}
		jobLimit, jobLimitKnown := minutes(j.TimeoutMinutes)
		for _, s := range j.Steps {
			what := downloads(s)
			if what == "" {
				continue
			}
			where := fmt.Sprintf("job %s step %q (%s)", name, s.Name, what)
			limit, ok := minutes(s.TimeoutMinutes)
			if !ok {
				t.Errorf("%s has no timeout-minutes", where)
				continue
			}
			if installsPackages(s) && limit*60 <= worst {
				t.Errorf("%s: timeout-minutes %d is not above the %d s apt-install.sh can take", where, limit, worst)
			}
			if jobLimitKnown && limit >= jobLimit {
				t.Errorf("%s: timeout-minutes %d is not below the job's %d", where, limit, jobLimit)
			}
		}
	}
}

// The layout checks of the UI run in Test and Release on every push (owner, 2026-11-19; R-UI-176, Q-ARCH-510): their fixtures and the
// states of the dialogs once, in the job ui-layout-fixtures, and every screen of the render set in the legs of ui-layout, each the
// part SHARD=i/n with i the leg's number and n the number of legs (Q-ARCH-1071). A part is every n-th pair of screen and kind of user
// (web/e2e/renders/paths.ts), so the legs together render each pair once exactly when the matrix numbers them 1 to n and holds nothing
// else, since strategy.job-total counts every combination; ui-layout-fixtures compares the parts' lists with the whole pass's for that
// number of legs (make ui-renders-parts-check). No leg narrows its part further, no leg cancels the others, no job has a
// condition that skips it. Until the first stable release they hold no release back: Semantic Release needs neither job
// (Q-ARCH-457). With the first stable release this test changes with the gate. Milliseconds.
func TestTheUILayoutChecksRunEveryScreenAndHoldNoRelease(t *testing.T) {
	t.Parallel()
	w := loadWorkflows(t)["release.yml"]
	fixtures, ok := w.Jobs["ui-layout-fixtures"]
	if !ok {
		t.Fatal("release.yml has no job ui-layout-fixtures")
	}
	legs, ok := w.Jobs["ui-layout"]
	if !ok {
		t.Fatal("release.yml has no job ui-layout")
	}
	for id, j := range map[string]job{"ui-layout-fixtures": fixtures, "ui-layout": legs} {
		if j.If != "" {
			t.Errorf("job %s runs only if %q; it runs on every push", id, j.If)
		}
	}
	var checks, states bool
	for _, s := range fixtures.Steps {
		checks = checks || strings.Contains(s.Run, "make ui-layout-test")
		states = states || strings.Contains(s.Run, "make ui-states")
	}
	if !checks || !states {
		t.Errorf("job ui-layout-fixtures runs the fixtures: %v, the dialog states: %v; want both", checks, states)
	}
	for _, s := range legs.Steps {
		if strings.Contains(s.Run, "make ui-layout-test") || strings.Contains(s.Run, "make ui-states") {
			t.Errorf("job ui-layout step %q runs %q in every leg; the fixtures and the dialog states run once, in ui-layout-fixtures", s.Name, s.Run)
		}
	}

	var strategy struct {
		FailFast *bool                `yaml:"fail-fast"`
		Matrix   map[string]yaml.Node `yaml:"matrix"`
	}
	if err := legs.Strategy.Decode(&strategy); err != nil {
		t.Fatalf("the strategy of job ui-layout: %v", err)
	}
	if strategy.FailFast == nil || *strategy.FailFast {
		t.Error("job ui-layout does not set fail-fast: false; a red leg would cancel the others and hide their findings")
	}
	for key := range strategy.Matrix {
		if key != "shard" {
			t.Errorf("the matrix of job ui-layout has %q; it holds the part's number alone, or strategy.job-total is not the number of parts", key)
		}
	}
	var parts []string
	if n, ok := strategy.Matrix["shard"]; ok && n.Kind == yaml.SequenceNode {
		for _, v := range n.Content {
			parts = append(parts, v.Value)
		}
	}
	if len(parts) < 2 {
		t.Fatalf("job ui-layout runs the parts %v; want a matrix of parts", parts)
	}
	for i, v := range parts {
		if v != strconv.Itoa(i+1) {
			t.Errorf("job ui-layout runs the parts %v; want 1 to %d in order", parts, len(parts))
			break
		}
	}

	// The check of the parts against the whole pass runs once, for the number of legs the matrix has.
	check := "make ui-renders-parts-check PARTS=" + strconv.Itoa(len(parts))
	if !slices.ContainsFunc(fixtures.Steps, func(s step) bool { return strings.TrimSpace(s.Run) == check }) {
		t.Errorf("job ui-layout-fixtures does not run %q; the matrix of ui-layout has %d legs", check, len(parts))
	}

	const render = "make ui-renders SHARD=${{ matrix.shard }}/${{ strategy.job-total }}"
	var renders int
	for _, s := range legs.Steps {
		if !strings.Contains(s.Run, "make ui-renders") {
			continue
		}
		renders++
		if strings.TrimSpace(s.Run) != render {
			t.Errorf("job ui-layout step %q runs %q; want %q", s.Name, strings.TrimSpace(s.Run), render)
		}
		for _, narrow := range []string{"GREP", "UI_RENDERS_"} {
			for k := range s.Env {
				if strings.Contains(k, narrow) {
					t.Errorf("job ui-layout step %q narrows its part with %s", s.Name, k)
				}
			}
		}
	}
	if renders != 1 {
		t.Errorf("job ui-layout runs make ui-renders in %d steps; want one", renders)
	}
	if _, ok := legs.Env["UI_RENDERS_WIDTHS"]; ok {
		t.Error("job ui-layout sets UI_RENDERS_WIDTHS; every leg renders the three widths")
	}
	// Each leg's evidence under a name of its own: two legs that uploaded under one name would fail the second upload.
	for _, s := range legs.Steps {
		if strings.HasPrefix(s.Uses, "actions/upload-artifact@") && !strings.Contains(s.With["name"], "${{ matrix.shard }}") {
			t.Errorf("job ui-layout step %q uploads %q, a name every leg shares", s.Name, s.With["name"])
		}
	}

	release := w.Jobs["semantic-release"]
	var needs []string
	if err := release.Needs.Decode(&needs); err != nil {
		t.Fatalf("the needs of semantic-release: %v", err)
	}
	for _, id := range []string{"ui-layout-fixtures", "ui-layout"} {
		if slices.Contains(needs, id) {
			t.Errorf("semantic-release needs %s; until the first stable release it gates no release (Q-ARCH-457)", id)
		}
	}
}
