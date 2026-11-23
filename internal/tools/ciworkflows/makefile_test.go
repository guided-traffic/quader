package ciworkflows

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// makeDryRun is what `make -n <args>` would run in the repository, as a make of its own (a run under `make test` passes its
// flags and level down otherwise).
func makeDryRun(t *testing.T, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not on PATH")
	}
	cmd := exec.Command("make", append([]string{"-n", "-f", "Makefile"}, args...)...)
	cmd.Dir = repoRoot
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if name != "MAKEFLAGS" && name != "MFLAGS" && name != "MAKELEVEL" && !strings.HasPrefix(name, "LINT_") && name != "PKGS" {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make -n %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// goTestLine is the first line of a dry run that runs go test, or "".
func goTestLine(out string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "go test ") {
			return l
		}
	}
	return ""
}

// The linter has one time limit, passed by make (G2-29, G3-26), and a second run on the machine waits for the first instead of
// ending at once with "parallel golangci-lint is running" (G4-13): make lint and make gosec pass --timeout and
// --allow-serial-runners, say first when another run holds the lock, and .golangci.yml sets no timeout of its own.
func TestTheLinterHasOneLimitAndWaitsForAnotherRun(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"lint", "gosec"} {
		out := makeDryRun(t, target, "LINT_TIMEOUT=17m")
		run := ""
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, "golangci-lint") && strings.Contains(l, " run ") {
				run = l
			}
		}
		if run == "" {
			t.Errorf("make %s runs no golangci-lint run:\n%s", target, out)
			continue
		}
		for _, want := range []string{"--timeout 17m", "--allow-serial-runners"} {
			if !strings.Contains(run, want) {
				t.Errorf("make %s runs %q; it lacks %s", target, run, want)
			}
		}
		if strings.Contains(run, "--allow-parallel-runners") {
			t.Errorf("make %s drops golangci-lint's lock: %q", target, run)
		}
		if !strings.Contains(out, "pgrep -f") {
			t.Errorf("make %s does not say that it waits for another run", target)
		}
	}
	var cfg struct {
		Run map[string]any `yaml:"run"`
	}
	if err := yaml.Unmarshal([]byte(readRepoFile(t, ".golangci.yml")), &cfg); err != nil {
		t.Fatal(err)
	}
	if v, ok := cfg.Run["timeout"]; ok {
		t.Errorf(".golangci.yml sets run.timeout %v beside LINT_TIMEOUT of the Makefile; the limit lives in one place", v)
	}
}

// The unit run before a push (make unit; G2-19 of the triage of 2026-11-23): the packages of make test, the environment a runner
// has, no race detector and no coverage. A plain go test read the host's AWS configuration and passed where CI failed
// (TestBackupCommands, run 42387599073), so the environment is the one definition make test uses, not a copy.
func TestTheUnitTargetRunsWithTheRunnersEnvironment(t *testing.T) {
	t.Parallel()
	unit := goTestLine(makeDryRun(t, "unit", "PKGS=ciworkflows-pkgs"))
	full := goTestLine(makeDryRun(t, "test", "PKGS=ciworkflows-pkgs"))
	if unit == "" || full == "" {
		t.Fatalf("no go test line: unit %q, test %q", unit, full)
	}
	for _, want := range []string{
		"-u AWS_REGION", "-u AWS_DEFAULT_REGION", "-u AWS_PROFILE", "-u AWS_ACCESS_KEY_ID", "-u AWS_SECRET_ACCESS_KEY",
		"AWS_CONFIG_FILE=/nonexistent", "AWS_SHARED_CREDENTIALS_FILE=/nonexistent", "AWS_EC2_METADATA_DISABLED=true",
		"-count=1", "-timeout ", "ciworkflows-pkgs",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("make unit runs %q; it lacks %q", unit, want)
		}
	}
	envOf := func(line string) string {
		before, _, _ := strings.Cut(line, "go test ")
		return strings.TrimSpace(before)
	}
	if envOf(unit) == "" || !strings.HasSuffix(envOf(full), envOf(unit)) {
		t.Errorf("make unit runs with %q, make test with %q; both read UNIT_ENV", envOf(unit), envOf(full))
	}
	for _, not := range []string{"-race", "-cover", "-json"} {
		if strings.Contains(unit, not) {
			t.Errorf("make unit runs %q; the run before a push has no %s", unit, not)
		}
	}
}

// Every fuzz run of the Makefile bounds the shrinking of a new input (G3-25): Go's default of a minute per input stalled a target
// whose seeds are tens of kilobytes (175 executions in two minutes against 565,507 with five seconds), so each go test -fuzz
// passes -fuzzminimizetime $(FUZZMINIMIZETIME), the one setting.
func TestEveryFuzzRunBoundsTheShrinkingOfAnInput(t *testing.T) {
	t.Parallel()
	runs := 0
	for i, l := range strings.Split(readRepoFile(t, "Makefile"), "\n") {
		if !strings.Contains(l, "go test ") || !strings.Contains(l, " -fuzz ") {
			continue
		}
		runs++
		if !strings.Contains(l, "-fuzzminimizetime $(FUZZMINIMIZETIME)") {
			t.Errorf("Makefile:%d fuzzes without -fuzzminimizetime $(FUZZMINIMIZETIME): %s", i+1, strings.TrimSpace(l))
		}
	}
	if runs < 3 {
		t.Errorf("%d fuzz runs found in the Makefile; fuzz-npm, fuzz-nuget and fuzz-stored-format are three", runs)
	}
}

// make image labels what it builds and removes the untagged images of its earlier builds, and only those (G2-07): a build under a
// tag an earlier build holds leaves that one untagged, and nothing removed them. The prune may fail without failing the build,
// which the gating job image runs.
func TestTheImageTargetRemovesTheUntaggedImagesItLeft(t *testing.T) {
	t.Parallel()
	out := makeDryRun(t, "image", "IMAGE=ciworkflows:probe", "VERSION=dev")
	label := ""
	for _, l := range strings.Split(out, "\n") {
		if i := strings.Index(l, "--label "); i >= 0 {
			label = strings.Fields(l[i+len("--label "):])[0]
		}
	}
	if label == "" {
		t.Fatalf("make image labels nothing it builds:\n%s", out)
	}
	prune := "docker image prune --force --filter dangling=true --filter label=" + label
	if !strings.Contains(out, prune) {
		t.Errorf("make image does not run %q:\n%s", prune, out)
	}
	if !strings.Contains(readRepoFile(t, "Makefile"), "\t-docker image prune --force --filter dangling=true --filter label=$(IMAGE_BUILD_LABEL)") {
		t.Error("the prune of make image can fail the build; it is a recipe line with a leading '-'")
	}
}
