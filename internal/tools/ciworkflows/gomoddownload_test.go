package ciworkflows

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The stubs of the module download: timeout logs its arguments and runs the rest of the command; go prints the file
// STUB_DIR/output (what `go mod download -x` would print) and exits with STUB_STATUS, or answers `go env GOPROXY`.
var goDownloadStubs = map[string]string{
	"timeout": `#!/usr/bin/env bash
echo "timeout $*" >> "$STUB_LOG"
shift 3
exec "$@"
`,
	"go": `#!/usr/bin/env bash
if [ "$1" = env ]; then echo "https://proxy.golang.org,direct"; exit 0; fi
echo "go $*" >> "$STUB_LOG"
cat "$STUB_DIR/output"
exit "${STUB_STATUS:-0}"
`,
}

func runGoModDownload(t *testing.T, output string, env ...string) (int, string, []string) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	stubDir := t.TempDir()
	for name, body := range goDownloadStubs {
		if err := os.WriteFile(filepath.Join(stubDir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(state, "output"), []byte(output), 0o600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(state, "calls.log")
	script, err := filepath.Abs(filepath.Join(repoRoot, ".github", "scripts", "go-mod-download.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, script)
	cmd.Env = append([]string{
		"PATH=" + stubDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"TMPDIR=" + state,
		"STUB_DIR=" + state,
		"STUB_LOG=" + log,
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

const (
	getZip  = "https://proxy.golang.org/github.com/cloudflare/circl/@v/v1.6.3.zip"
	getMod  = "https://proxy.golang.org/golang.org/x/sys/@v/v0.50.0.mod"
	getInfo = "https://proxy.golang.org/golang.org/x/sys/@v/v0.50.0.info"
)

// After a hit nothing is fetched, and the download runs bounded: 300 s, then KILL 10 s later.
func TestGoModDownloadAfterAHitFetchesNothing(t *testing.T) {
	t.Parallel()
	status, out, calls := runGoModDownload(t, "")
	want := []string{"timeout -k 10 300 go mod download -x", "go mod download -x"}
	if status != 0 || strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("status %d, calls %q, want 0 and %q:\n%s", status, calls, want, out)
	}
	if !strings.Contains(out, "nothing fetched") {
		t.Errorf("the output does not say that nothing was fetched:\n%s", out)
	}
}

// After a miss the script says how many files came from which host.
func TestGoModDownloadAfterAMissNamesWhatItFetched(t *testing.T) {
	t.Parallel()
	output := "# get " + getMod + "\n# get " + getMod + ": 200 OK (0.051s)\n# get " + getZip + "\n# get " + getZip + ": 200 OK (1.2s)\n"
	status, out, _ := runGoModDownload(t, output)
	if status != 0 || !strings.Contains(out, "fetched 2 files from proxy.golang.org") {
		t.Fatalf("status %d, want 0 and the count and host:\n%s", status, out)
	}
}

// A download that the bound ends fails the step in the bound's time and names the requests that had no answer and the proxy:
// the module of F-156 is named, the one that had arrived is not.
func TestGoModDownloadAHangFailsAndNamesTheRequestWithoutAnAnswer(t *testing.T) {
	t.Parallel()
	output := "# get " + getInfo + "\n# get " + getInfo + ": 200 OK (0.02s)\n# get " + getZip + "\n"
	status, out, calls := runGoModDownload(t, output, "STUB_STATUS=124", "QUADER_GO_DOWNLOAD_SECONDS=120")
	if status != 124 {
		t.Fatalf("status %d, want 124:\n%s", status, out)
	}
	if calls[0] != "timeout -k 10 120 go mod download -x" {
		t.Errorf("the bound of QUADER_GO_DOWNLOAD_SECONDS is not used: %q", calls[0])
	}
	for _, w := range []string{"::error::the download of the Go modules did not finish in 120 s", "GOPROXY=https://proxy.golang.org,direct", "  " + getZip} {
		if !strings.Contains(out, w) {
			t.Errorf("the output lacks %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "  "+getInfo) {
		t.Errorf("a request that was answered is named as unanswered:\n%s", out)
	}
}

// A download that fails by itself fails the step with go's own error lines.
func TestGoModDownloadAFailureFailsWithGosErrors(t *testing.T) {
	t.Parallel()
	output := "go: golang.org/x/sys@v0.50.0: reading " + getZip + ": 404 Not Found\n"
	status, out, _ := runGoModDownload(t, output, "STUB_STATUS=1")
	if status != 1 || !strings.Contains(out, "404 Not Found") || !strings.Contains(out, "::error::go mod download failed with exit 1") {
		t.Fatalf("status %d, want 1, go's error and the summary:\n%s", status, out)
	}
}
