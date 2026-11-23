package ciworkflows

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// tools/nightly-load-guard.sh says before a push whether load legs of the nightly run (decision 22 of the reading of 2026-11-23): a
// push of code beside them cost them their numbers (F-164). It reads the runs and their jobs through gh, stubbed here with recorded
// answers.

func runNightlyGuard(t *testing.T, runs, jobs string, fail bool) (int, string) {
	t.Helper()
	dir := t.TempDir()
	stub := "#!/usr/bin/env bash\n" +
		"[ -z \"$STUB_FAIL\" ] || { echo 'HTTP 401: Bad credentials' >&2; exit 4; }\n" +
		"case \"$1 $2\" in\n" +
		"  'run list') printf '%s' \"$STUB_RUNS\" ;;\n" +
		"  'run view') printf '%s' \"$STUB_JOBS\" ;;\n" +
		"esac\n"
	write(t, filepath.Join(dir, "gh"), stub)
	if err := os.Chmod(filepath.Join(dir, "gh"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", filepath.Join(repoRoot, "tools", "nightly-load-guard.sh"))
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GH=" + filepath.Join(dir, "gh"), "STUB_RUNS=" + runs, "STUB_JOBS=" + jobs}
	if fail {
		cmd.Env = append(cmd.Env, "STUB_FAIL=1")
	}
	out, err := cmd.CombinedOutput()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return code, string(out)
}

func TestNightlyLoadGuardNamesTheLoadLegsThatRun(t *testing.T) {
	run := "45230000001 https://github.com/example/quader/actions/runs/45230000001\n"

	code, out := runNightlyGuard(t, run, "e2e (npm-ha)\ne2e (oci)\nLatency\ne2e (drain-load-b)\ne2e (vuln)\ne2e (ha-faults)\n", false)
	if code != 1 {
		t.Fatalf("exit %d with load legs running, want 1:\n%s", code, out)
	}
	for _, leg := range []string{"e2e (npm-ha)", "Latency", "e2e (drain-load-b)", "e2e (ha-faults)"} {
		if !strings.Contains(out, "the load leg: "+leg) {
			t.Errorf("the load leg %s is not named:\n%s", leg, out)
		}
	}
	for _, leg := range []string{"e2e (oci)", "e2e (vuln)"} {
		if strings.Contains(out, "the load leg: "+leg) {
			t.Errorf("%s is no load leg and was named:\n%s", leg, out)
		}
	}

	if code, out := runNightlyGuard(t, run, "e2e (oci)\ne2e (deb)\nclients images (npm, large)\n", false); code != 0 || !strings.Contains(out, "no load leg") {
		t.Errorf("exit %d with only format legs running, want 0:\n%s", code, out)
	}
	if code, out := runNightlyGuard(t, "", "", false); code != 0 {
		t.Errorf("exit %d with no nightly in progress, want 0:\n%s", code, out)
	}
	if code, out := runNightlyGuard(t, run, "", true); code != 2 || !strings.Contains(out, "Bad credentials") {
		t.Errorf("exit %d when gh fails, want 2 with gh's words:\n%s", code, out)
	}
}
