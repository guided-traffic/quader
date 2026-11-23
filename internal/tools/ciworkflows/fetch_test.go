package ciworkflows

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// runFetch runs .github/scripts/fetch.sh with the real timeout and no pause between attempts. The command is a bash script
// that counts its runs in a file and exits with the status of its run (STATUSES, one per run, "hang" sleeps beyond the bound).
func runFetch(t *testing.T, seconds, attempts, statuses string) (int, string, int) {
	t.Helper()
	needTimeout(t)
	state := t.TempDir()
	count := filepath.Join(state, "count")
	cmdScript := filepath.Join(state, "cmd.sh")
	write(t, cmdScript, `n=$(( $(cat "$COUNT" 2>/dev/null || echo 0) + 1 )); echo "$n" > "$COUNT"
s=$(echo "$STATUSES" | cut -d' ' -f"$n"); [ "$s" = hang ] && exec sleep 30; exit "${s:-0}"
`)
	script, err := filepath.Abs(filepath.Join(repoRoot, ".github", "scripts", "fetch.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script, "the test fetch", seconds, attempts, "bash", cmdScript)
	cmd.Env = append(os.Environ(), "QUADER_FETCH_PAUSE=0", "COUNT="+count, "STATUSES="+statuses)
	out, err := cmd.CombinedOutput()
	status := 0
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		status = exit.ExitCode()
	case err != nil:
		t.Fatal(err)
	}
	runs := 0
	if raw, err := os.ReadFile(count); err == nil {
		runs, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
	}
	return status, string(out), runs
}

// A fetch that succeeds at once prints nothing of the script's and runs once.
func TestFetchASuccessRunsOnceAndSaysNothing(t *testing.T) {
	t.Parallel()
	status, out, runs := runFetch(t, "5", "3", "0")
	if status != 0 || runs != 1 || out != "" {
		t.Errorf("status %d, %d runs, output %q; want 0, 1 and nothing", status, runs, out)
	}
}

// A fetch that fails is run again, and a later success ends it with status 0 and a line per failed attempt.
func TestFetchAFailureIsRunAgain(t *testing.T) {
	t.Parallel()
	status, out, runs := runFetch(t, "5", "3", "22 0")
	if status != 0 || runs != 2 {
		t.Fatalf("status %d, %d runs; want 0 and 2:\n%s", status, runs, out)
	}
	if want := "fetch: the test fetch, attempt 1 of 3: exit 22; trying again in 0 s"; !strings.Contains(out, want) {
		t.Errorf("the output lacks %q:\n%s", want, out)
	}
	if strings.Contains(out, "::error::") {
		t.Errorf("a fetch that succeeded printed an error:\n%s", out)
	}
}

// A fetch that hangs is ended by the bound, which a byte now and then does not reset (2026-11-23: an index read for 25 minutes),
// and the last attempt's failure fails the script with one line that names the fetch, the bound and every attempt's end.
func TestFetchAHangIsEndedAndTheLastFailureIsNamed(t *testing.T) {
	t.Parallel()
	status, out, runs := runFetch(t, "1", "2", "hang 7")
	if status != 1 || runs != 2 {
		t.Fatalf("status %d, %d runs; want 1 and 2:\n%s", status, runs, out)
	}
	want := "::error::fetch: the test fetch failed in 2 attempts of at most 1 s each (1: no end within 1 s; 2: exit 7)\n"
	if !strings.HasSuffix(out, want) {
		t.Errorf("the output does not end with %q:\n%s", want, out)
	}
}

func TestFetchWrongArgumentsAreAUsageError(t *testing.T) {
	t.Parallel()
	needTimeout(t)
	script := filepath.Join(repoRoot, ".github", "scripts", "fetch.sh")
	for _, args := range [][]string{{"x", "10", "3"}, {"x", "ten", "3", "true"}, {"x", "10", "0", "true"}, {"x", "0", "3", "true"}} {
		err := exec.Command("bash", append([]string{script}, args...)...).Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 2 {
			t.Errorf("%q: %v, want exit 2", args, err)
		}
	}
}

// Every fetch of a clients image's build goes through the two scripts of the build context `ci` (Q-ARCH-639): apt-get through
// apt-install.sh, curl, wget, npm, pip and the like through fetch.sh, so that a stalled mirror ends an attempt within its bound
// and the build names the fetch. A RUN that calls one of those tools by itself, or a Dockerfile that mounts `ci` while its
// build.sh does not pass that context, fails this test.
func TestEveryFetchOfAClientsImageBuildIsBounded(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join(repoRoot, "test", "e2e", "*", "Dockerfile*"))
	if err != nil || len(files) < 5 {
		t.Fatalf("found %d Dockerfiles of clients images: %v", len(files), err)
	}
	// A command that fetches, at the start of a command (after a line start, &&, ||, ;, | or a continued line).
	bare := regexp.MustCompile(`(?m)(^|&&|\|\||;|\|)\s*(apt-get|apt|curl|wget|npm (ci|install|i)|pnpm (install|add)|yarn (install|add)|pip3? install|dotnet (restore|tool install)|git (clone|fetch)|go (install|get|mod download))\s`)
	mounted := 0
	for _, f := range files {
		rel, _ := filepath.Rel(repoRoot, f)
		text := readRepoFile(t, rel)
		for _, run := range dockerRuns(text) {
			cmd := run
			// The commands the two scripts run are their arguments, not fetches of the RUN itself.
			for _, s := range []string{"/ci/fetch.sh", "/ci/apt-install.sh"} {
				if i := strings.Index(cmd, s); i >= 0 {
					if !strings.Contains(run, "--mount=type=bind,from=ci,target=/ci") {
						t.Errorf("%s: a RUN calls %s without mounting the build context ci: %q", rel, s, run)
					}
					mounted++
					cmd = removeScriptCall(cmd, s)
				}
			}
			if m := bare.FindStringSubmatch(cmd); m != nil {
				t.Errorf("%s: a RUN calls %s itself; run it through /ci/fetch.sh or /ci/apt-install.sh: %q", rel, m[2], run)
			}
		}
		if strings.Contains(text, "from=ci") {
			build := readRepoFile(t, filepath.Join(filepath.Dir(rel), "build.sh"))
			if !strings.Contains(build, `--build-context "ci=$build/ci"`) || !strings.Contains(build, `image_ci_context "$build/ci"`) {
				t.Errorf("%s mounts the build context ci, which its build.sh does not fill and pass", rel)
			}
		}
	}
	if mounted < 6 {
		t.Errorf("%d fetch script calls found in the Dockerfiles; the test reads them wrongly", mounted)
	}
}

// dockerRuns returns the RUN instructions of a Dockerfile, continued lines joined, comments left out.
func dockerRuns(text string) []string {
	var runs []string
	var cur strings.Builder
	in := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !in {
			if !strings.HasPrefix(trimmed, "RUN ") {
				continue
			}
			in = true
			cur.Reset()
			trimmed = strings.TrimPrefix(trimmed, "RUN ")
		}
		cur.WriteString(strings.TrimSuffix(trimmed, "\\") + "\n")
		if !strings.HasSuffix(trimmed, "\\") {
			runs = append(runs, cur.String())
			in = false
		}
	}
	return runs
}

// removeScriptCall cuts the call of a fetch script out of a RUN: from the script's path to the end of that command (the next
// && or ; at the start of a line of the joined RUN), so that the arguments it runs are not read as the RUN's own commands.
func removeScriptCall(run, script string) string {
	i := strings.Index(run, script)
	rest := run[i:]
	end := len(rest)
	for _, sep := range []string{"\n&&", "\n;", "; \n", ";\n"} {
		if j := strings.Index(rest, sep); j >= 0 && j < end {
			end = j
		}
	}
	return run[:i] + rest[end:]
}
