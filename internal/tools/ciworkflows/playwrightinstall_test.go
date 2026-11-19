package ciworkflows

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runPlaywrightInstall runs .github/scripts/playwright-install.sh with npx stubbed: the stub writes each call to a log and
// exits with dryRunStatus for the dry run of the dependency install, with installStatus for an install.
func runPlaywrightInstall(t *testing.T, dryRunStatus, installStatus string, args ...string) (int, []string, string) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	stub := `#!/usr/bin/env bash
echo "npx $*" >> "$STUB_LOG"
case " $* " in
  *" install-deps --dry-run "*) exit "$STUB_DRY_RUN" ;;
esac
exit "$STUB_INSTALL"
`
	if err := os.WriteFile(filepath.Join(dir, "npx"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs(filepath.Join(repoRoot, ".github", "scripts", "playwright-install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, append([]string{script}, args...)...)
	cmd.Env = []string{
		"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"STUB_LOG=" + log,
		"STUB_DRY_RUN=" + dryRunStatus,
		"STUB_INSTALL=" + installStatus,
	}
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
	return status, calls, string(out)
}

// A runner that has the browser's system libraries fetches the browser only: no --with-deps, so no read of the package indexes.
func TestPlaywrightInstallWithTheLibrariesFetchesTheBrowserOnly(t *testing.T) {
	t.Parallel()
	status, calls, out := runPlaywrightInstall(t, "0", "0", "chromium")
	want := []string{"npx playwright install-deps --dry-run chromium", "npx playwright install chromium"}
	if status != 0 || strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("status %d, calls %q, want 0 and %q:\n%s", status, calls, want, out)
	}
}

// A runner that lacks a library installs as before, libraries and browser in Playwright's own call.
func TestPlaywrightInstallWithoutTheLibrariesInstallsThem(t *testing.T) {
	t.Parallel()
	status, calls, out := runPlaywrightInstall(t, "1", "0", "chromium")
	want := []string{"npx playwright install-deps --dry-run chromium", "npx playwright install --with-deps chromium"}
	if status != 0 || strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("status %d, calls %q, want 0 and %q:\n%s", status, calls, want, out)
	}
}

// A browser download that fails fails the step, on either path.
func TestPlaywrightInstallAFailedInstallFailsTheStep(t *testing.T) {
	t.Parallel()
	for _, dryRun := range []string{"0", "1"} {
		if status, calls, out := runPlaywrightInstall(t, dryRun, "7", "chromium"); status != 7 || len(calls) != 2 {
			t.Errorf("dry run %s: status %d, calls %q, want 7 after two calls:\n%s", dryRun, status, calls, out)
		}
	}
}

func TestPlaywrightInstallWithoutABrowserIsAUsageError(t *testing.T) {
	t.Parallel()
	if status, calls, _ := runPlaywrightInstall(t, "0", "0"); status != 2 || len(calls) != 0 {
		t.Fatalf("status %d, calls %q, want 2 and none", status, calls)
	}
}

// No workflow step installs a browser itself: a bare "playwright install --with-deps" brings the read of the package indexes back
// that the script avoids on a runner that has the libraries.
func TestEveryBrowserInstallGoesThroughTheScript(t *testing.T) {
	t.Parallel()
	found := 0
	for file, w := range loadWorkflows(t) {
		for name, j := range w.Jobs {
			for _, s := range j.Steps {
				if strings.Contains(s.Run, "playwright install") {
					t.Errorf("%s, job %s, step %q runs playwright install itself; call .github/scripts/playwright-install.sh", file, name, s.Name)
				}
				found += strings.Count(s.Run, "scripts/playwright-install.sh ")
			}
		}
	}
	if found == 0 {
		t.Error("no workflow step calls .github/scripts/playwright-install.sh; the test looks for the wrong name")
	}
}
