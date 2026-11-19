package ciworkflows

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot is the repository root, three levels above this package.
var repoRoot = filepath.Join("..", "..", "..")

// The stubs stand in for dpkg-query, sudo, timeout and apt-get. Each call of timeout is one bounded apt-get call; the stub
// writes it to STUB_LOG and decides, per phase (update, download, unpack) and attempt, whether the call hangs (exit 124 after
// printing the file STUB_DIR/hang-output, as timeout reports a call it ended), ends by itself without every source (exit 0
// after STUB_DIR/partial-output, as apt-get update does when a source did not answer), fails (exit 100 after
// STUB_DIR/fail-output) or succeeds. The outputs are files, not variables: macOS awk crashed now and then when the environment held a value with
// newlines.
var stubs = map[string]string{
	"dpkg-query": `#!/usr/bin/env bash
pkg="${!#}"
for p in $STUB_INSTALLED; do
  if [ "$p" = "$pkg" ]; then echo installed; exit 0; fi
done
echo "dpkg-query: no packages found matching $pkg" >&2
exit 1
`,
	"sudo": `#!/usr/bin/env bash
echo "sudo $*" >> "$STUB_LOG"
[ "$1" = "-n" ] || { echo "sudo without -n" >&2; exit 1; }
shift
exec "$@"
`,
	"timeout": `#!/usr/bin/env bash
echo "timeout $*" >> "$STUB_LOG"
shift 3
phase=unpack
case " $* " in
  *" update "*) phase=update ;;
  *" --download-only "*) phase=download ;;
esac
count="$STUB_DIR/count-$phase"
n=$(( $(cat "$count" 2>/dev/null || echo 0) + 1 ))
echo "$n" > "$count"
case " $STUB_HANG " in
  *" $phase:$n "*) cat "$STUB_DIR/hang-output"; exit 124 ;;
esac
case " $STUB_PARTIAL " in
  *" $phase:$n "*) cat "$STUB_DIR/partial-output"; exit 0 ;;
esac
case " $STUB_FAIL " in
  *" $phase "*) cat "$STUB_DIR/fail-output"; exit 100 ;;
esac
exec "$@"
`,
	"apt-get": `#!/usr/bin/env bash
echo "Get:1 http://archive.ubuntu.com/ubuntu noble InRelease [256 kB]"
exit 0
`,
}

// aptRun is the outcome of one run of apt-install.sh against the stubs.
type aptRun struct {
	status int
	output string
	calls  []string // the lines of STUB_LOG: every sudo and timeout call
}

// timeouts returns the timeout calls whose apt-get arguments contain marker.
func (r aptRun) timeouts(marker string) []string {
	var out []string
	for _, c := range r.calls {
		if strings.HasPrefix(c, "timeout ") && strings.Contains(c+" ", " "+marker+" ") {
			out = append(out, c)
		}
	}
	return out
}

type aptCase struct {
	installed   string // packages dpkg-query reports as installed
	hang        string // phase:attempt pairs that hang, for example "update:1"
	hangOutput  string
	partial     string // phase:attempt pairs that end by themselves, exit 0, without every source
	partialOut  string
	fail        string // phases that fail
	failOutput  string
	sources     bool // whether the Ubuntu sources file exists
	stampBefore bool // whether an earlier step of the job read the indexes
	awkCrashes  bool // whether awk dies with a segmentation fault (exit 139)
}

func runAptInstall(t *testing.T, c aptCase, packages ...string) (aptRun, string) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	stubDir := t.TempDir()
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(stubDir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if c.awkCrashes {
		if err := os.WriteFile(filepath.Join(stubDir, "awk"), []byte("#!/usr/bin/env bash\nexit 139\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	state := t.TempDir()
	runnerTemp := t.TempDir()
	stamp := filepath.Join(runnerTemp, "apt-install-indexes-read")
	if c.stampBefore {
		if err := os.WriteFile(stamp, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sources := filepath.Join(state, "ubuntu.sources")
	if c.sources {
		if err := os.WriteFile(sources, []byte("Types: deb\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, text := range map[string]string{"hang-output": c.hangOutput, "partial-output": c.partialOut, "fail-output": c.failOutput} {
		if err := os.WriteFile(filepath.Join(state, name), []byte(text+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(state, "calls.log")
	script, err := filepath.Abs(filepath.Join(repoRoot, ".github", "scripts", "apt-install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, append([]string{script}, packages...)...)
	cmd.Env = []string{
		"PATH=" + stubDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + state,
		"TMPDIR=" + state,
		"RUNNER_TEMP=" + runnerTemp,
		"QUADER_APT_SOURCES=" + sources,
		"STUB_DIR=" + state,
		"STUB_LOG=" + log,
		"STUB_INSTALLED=" + c.installed,
		"STUB_HANG=" + c.hang,
		"STUB_PARTIAL=" + c.partial,
		"STUB_FAIL=" + c.fail,
	}
	out, err := cmd.CombinedOutput()
	r := aptRun{output: string(out)}
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		r.status = exit.ExitCode()
	case err != nil:
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(log); err == nil {
		r.calls = strings.Split(strings.TrimSpace(string(raw)), "\n")
	}
	return r, stamp
}

func mustContain(t *testing.T, r aptRun, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(r.output, w) {
			t.Errorf("the output lacks %q:\n%s", w, r.output)
		}
	}
}

// A runner image that carries the packages costs nothing: no sudo, no apt-get, no network.
func TestAptInstallInstalledPackagesTouchNothing(t *testing.T) {
	t.Parallel()
	r, stamp := runAptInstall(t, aptCase{installed: "libatomic1 jq", sources: true}, "libatomic1", "jq")
	if r.status != 0 || len(r.calls) != 0 {
		t.Fatalf("status %d, calls %q, want 0 and none:\n%s", r.status, r.calls, r.output)
	}
	mustContain(t, r, "already installed")
	if _, err := os.Stat(stamp); err == nil {
		t.Error("the indexes were marked as read, but nothing read them")
	}
}

// The healthy path: the update reads the Ubuntu archive only, then the missing package (not the installed one) is downloaded
// and unpacked, each call under its bound and with apt's own timeout. The update retries no request itself (it has to end
// inside its bound to keep what it read), the download retries each three times, the unpacking fetches nothing.
func TestAptInstallBoundsEveryCallAndInstallsOnlyWhatIsMissing(t *testing.T) {
	t.Parallel()
	r, stamp := runAptInstall(t, aptCase{installed: "jq", sources: true}, "jq", "libatomic1")
	if r.status != 0 {
		t.Fatalf("status %d:\n%s", r.status, r.output)
	}
	retries := map[string]string{"update": "Acquire::Retries=0", "--download-only": "Acquire::Retries=3"}
	for marker, bound := range map[string]string{"update": "-k 10 60 ", "--download-only": "-k 10 90 ", "--no-download": "-k 10 120 "} {
		calls := r.timeouts(marker)
		if len(calls) != 1 {
			t.Fatalf("%d calls with %s, want 1: %q", len(calls), marker, r.calls)
		}
		if !strings.HasPrefix(calls[0], "timeout "+bound+"apt-get ") {
			t.Errorf("the %s call is not bounded by %q: %s", marker, bound, calls[0])
		}
		for _, opt := range []string{"Acquire::http::Timeout=20", "Acquire::https::Timeout=20"} {
			if !strings.Contains(calls[0], opt) {
				t.Errorf("the %s call lacks %s: %s", marker, opt, calls[0])
			}
		}
		if want := retries[marker]; (want == "" && strings.Contains(calls[0], "Acquire::Retries")) || !strings.Contains(calls[0], want) {
			t.Errorf("the %s call should carry %q as its retries: %s", marker, want, calls[0])
		}
		if marker != "update" && (!strings.HasSuffix(calls[0], " libatomic1") || strings.Contains(calls[0], " jq")) {
			t.Errorf("the %s call should name libatomic1 only: %s", marker, calls[0])
		}
	}
	update := r.timeouts("update")[0]
	for _, opt := range []string{"Dir::Etc::sourcelist=", "Dir::Etc::sourceparts=-", "APT::Get::List-Cleanup=0"} {
		if !strings.Contains(update, opt) {
			t.Errorf("the update does not keep to the Ubuntu sources (%s): %s", opt, update)
		}
	}
	if _, err := os.Stat(stamp); err != nil {
		t.Error("a successful update did not mark the indexes as read for the later steps of the job")
	}
	mustContain(t, r, "apt-install: installed libatomic1")
}

// Without the Ubuntu sources file the update reads every source of the image.
func TestAptInstallWithoutTheUbuntuSourcesFileReadsEverySource(t *testing.T) {
	t.Parallel()
	r, _ := runAptInstall(t, aptCase{}, "jq")
	if r.status != 0 {
		t.Fatalf("status %d:\n%s", r.status, r.output)
	}
	if u := r.timeouts("update"); len(u) != 1 || strings.Contains(u[0], "Dir::Etc::") {
		t.Errorf("update calls %q, want one over every source", u)
	}
}

// A later step of the same job does not read the indexes again.
func TestAptInstallReadsTheIndexesOncePerJob(t *testing.T) {
	t.Parallel()
	r, _ := runAptInstall(t, aptCase{sources: true, stampBefore: true}, "jq")
	if r.status != 0 || len(r.timeouts("update")) != 0 || len(r.timeouts("--no-download")) != 1 {
		t.Fatalf("status %d, calls %q, want no update and one install:\n%s", r.status, r.calls, r.output)
	}
}

// The hang of Test and Release 44384406557: the update stalls once on the Ubuntu archive. The bound ends it, a second update
// runs and the install goes on; the warning names the host apt gave up on.
func TestAptInstallAnUpdateThatHangsOnceIsReadAgain(t *testing.T) {
	t.Parallel()
	r, stamp := runAptInstall(t, aptCase{
		sources:    true,
		hang:       "update:1",
		hangOutput: "Get:1 http://archive.ubuntu.com/ubuntu noble InRelease [256 kB]\nGet:2 http://security.ubuntu.com/ubuntu noble-security InRelease [126 kB]\nIgn:1 http://archive.ubuntu.com/ubuntu noble InRelease\nGet:9 http://archive.ubuntu.com/ubuntu noble-updates InRelease [126 kB]",
	}, "build-essential")
	if r.status != 0 || len(r.timeouts("update")) != 2 || len(r.timeouts("--no-download")) != 1 {
		t.Fatalf("status %d, calls %q, want two updates and an install:\n%s", r.status, r.calls, r.output)
	}
	mustContain(t, r, "::warning::apt-get update did not finish in 60 s (waiting on archive.ubuntu.com); reading the indexes once more")
	if _, err := os.Stat(stamp); err != nil {
		t.Error("the second, successful update did not mark the indexes as read")
	}
}

// An update that hangs twice is given up with a warning; the install is tried from the indexes present, and when it then
// cannot find the package, the error says that the indexes are incomplete. Without a failed request apt's output does not
// say which request hung, so every host it started a request to is named.
func TestAptInstallAnUpdateThatHangsTwiceFailsTheInstallFast(t *testing.T) {
	t.Parallel()
	r, stamp := runAptInstall(t, aptCase{
		sources:    true,
		hang:       "update:1 update:2",
		hangOutput: "Get:1 http://archive.ubuntu.com/ubuntu noble InRelease [256 kB]\nGet:2 http://security.ubuntu.com/ubuntu noble-security InRelease [126 kB]",
		fail:       "download",
		failOutput: "Reading package lists...\nE: Unable to locate package build-essential",
	}, "build-essential")
	if r.status != 1 || len(r.timeouts("update")) != 2 || len(r.timeouts("--download-only")) != 1 || len(r.timeouts("--no-download")) != 0 {
		t.Fatalf("status %d, calls %q, want 1 after two updates and one download:\n%s", r.status, r.calls, r.output)
	}
	mustContain(t, r,
		"::warning::apt-get update did not finish in 60 s (waiting on one of archive.ubuntu.com security.ubuntu.com), the second time; installing from the indexes present",
		"::error::apt-get could not fetch build-essential (E: Unable to locate package build-essential; the package indexes are incomplete, see the update above)",
	)
	if _, err := os.Stat(stamp); err == nil {
		t.Error("an update that never finished marked the indexes as read")
	}
}

// The failure of HA and Performance 44453781574 (nuget-ha), first half: one source is silent, apt gives it up inside the
// bound and ends with exit 0 and the indexes of the other source. The script reads once more, on a new connection, and a
// complete second update marks the indexes as read.
func TestAptInstallAnUpdateWithoutEverySourceIsReadAgain(t *testing.T) {
	t.Parallel()
	r, stamp := runAptInstall(t, aptCase{
		sources:    true,
		partial:    "update:1",
		partialOut: "Get:1 http://archive.ubuntu.com/ubuntu noble InRelease [256 kB]\nErr:2 http://security.ubuntu.com/ubuntu noble-security InRelease\n  Connection failed [IP: 185.125.190.82 80]\nW: Some index files failed to download. They have been ignored, or old ones used instead.",
	}, "build-essential")
	if r.status != 0 || len(r.timeouts("update")) != 2 || len(r.timeouts("--no-download")) != 1 {
		t.Fatalf("status %d, calls %q, want two updates and an install:\n%s", r.status, r.calls, r.output)
	}
	mustContain(t, r, "::warning::apt-get update could not read security.ubuntu.com; reading the indexes once more")
	if _, err := os.Stat(stamp); err != nil {
		t.Error("the second, complete update did not mark the indexes as read")
	}
}

// Second half: the source stays silent. The install goes on from the indexes of the source that answered, which the old
// script lost with the update it ended; the indexes are not marked as read, so a later step of the job asks again.
func TestAptInstallAnUpdateWithoutEverySourceTwiceInstallsFromTheRest(t *testing.T) {
	t.Parallel()
	r, stamp := runAptInstall(t, aptCase{
		sources:    true,
		partial:    "update:1 update:2",
		partialOut: "Get:1 http://archive.ubuntu.com/ubuntu noble InRelease [256 kB]\nErr:2 http://security.ubuntu.com/ubuntu noble-security InRelease\n  Connection failed [IP: 185.125.190.82 80]",
	}, "build-essential")
	if r.status != 0 || len(r.timeouts("update")) != 2 || len(r.timeouts("--no-download")) != 1 {
		t.Fatalf("status %d, calls %q, want two updates and an install:\n%s", r.status, r.calls, r.output)
	}
	mustContain(t, r,
		"::warning::apt-get update could not read security.ubuntu.com, the second time; installing from the indexes present",
		"apt-install: installed build-essential",
	)
	if _, err := os.Stat(stamp); err == nil {
		t.Error("an update that did not read every source marked the indexes as read")
	}
}

// An update that fails by itself (a source answering 503) is read once more as well, because the update retries no request
// itself; after the second failure the install goes on from the indexes it read.
func TestAptInstallAFailedUpdateIsAWarning(t *testing.T) {
	t.Parallel()
	r, _ := runAptInstall(t, aptCase{
		fail:       "update",
		failOutput: "Err:7 https://ppa.launchpadcontent.net/git-core/ppa/ubuntu noble InRelease\n  503  Service Unavailable\nE: Failed to fetch https://ppa.launchpadcontent.net/git-core/ppa/ubuntu/dists/noble/InRelease  503  Service Unavailable",
	}, "jq")
	if r.status != 0 || len(r.timeouts("update")) != 2 || len(r.timeouts("--no-download")) != 1 {
		t.Fatalf("status %d, calls %q, want two updates and an install:\n%s", r.status, r.calls, r.output)
	}
	mustContain(t, r,
		"::warning::apt-get update failed (E: Failed to fetch https://ppa.launchpadcontent.net/git-core/ppa/ubuntu/dists/noble/InRelease  503  Service Unavailable); reading the indexes once more",
		"::warning::apt-get update failed (E: Failed to fetch https://ppa.launchpadcontent.net/git-core/ppa/ubuntu/dists/noble/InRelease  503  Service Unavailable), the second time; installing from the indexes present",
	)
}

// A download that hangs twice fails the step in bounded time and names the packages and the mirror.
func TestAptInstallADownloadThatHangsTwiceFailsAndNamesTheMirror(t *testing.T) {
	t.Parallel()
	r, _ := runAptInstall(t, aptCase{
		sources:    true,
		hang:       "download:1 download:2",
		hangOutput: "Get:1 http://archive.ubuntu.com/ubuntu noble/main amd64 make amd64 4.3-4.1build2 [180 kB]\nIgn:1 http://archive.ubuntu.com/ubuntu noble/main amd64 make amd64 4.3-4.1build2",
	}, "make", "jq")
	if r.status != 1 || len(r.timeouts("--download-only")) != 2 || len(r.timeouts("--no-download")) != 0 {
		t.Fatalf("status %d, calls %q, want 1 after two downloads and no unpacking:\n%s", r.status, r.calls, r.output)
	}
	mustContain(t, r,
		"::warning::the download of make jq did not finish in 90 s (waiting on archive.ubuntu.com); trying once more",
		"::error::the download of make jq did not finish in 90 s twice (waiting on archive.ubuntu.com). Re-run the job.",
	)
}

// A download that fails by itself is not repeated: apt has retried each request already.
func TestAptInstallAFailedDownloadFailsAndNamesTheMirror(t *testing.T) {
	t.Parallel()
	r, _ := runAptInstall(t, aptCase{
		sources:    true,
		fail:       "download",
		failOutput: "Err:1 http://archive.ubuntu.com/ubuntu noble/main amd64 make amd64 4.3-4.1build2\n  Could not connect to archive.ubuntu.com:80 (185.125.190.81), connection timed out\nE: Failed to fetch http://archive.ubuntu.com/ubuntu/pool/main/m/make-dfsg/make_4.3-4.1build2_amd64.deb  Could not connect to archive.ubuntu.com:80",
	}, "make")
	if r.status != 1 || len(r.timeouts("--download-only")) != 1 {
		t.Fatalf("status %d, calls %q, want 1 after one download:\n%s", r.status, r.calls, r.output)
	}
	mustContain(t, r, "::error::apt-get could not fetch make (E: Failed to fetch http://archive.ubuntu.com/ubuntu/pool/main/m/make-dfsg/make_4.3-4.1build2_amd64.deb  Could not connect to archive.ubuntu.com:80; no answer from archive.ubuntu.com). Re-run the job if a mirror was down.")
}

// The helpers that word a message never decide the exit status: with awk crashing, a failed download still ends the step with
// exit 1 and apt's error line, only without the hosts.
func TestAptInstallACrashWhileWordingTheErrorKeepsTheExitStatus(t *testing.T) {
	t.Parallel()
	r, _ := runAptInstall(t, aptCase{
		sources:    true,
		awkCrashes: true,
		fail:       "download",
		failOutput: "Err:1 http://archive.ubuntu.com/ubuntu noble/main amd64 make amd64 4.3-4.1build2\nE: Failed to fetch make_4.3-4.1build2_amd64.deb  Could not connect to archive.ubuntu.com:80",
	}, "make")
	if r.status != 1 {
		t.Fatalf("status %d, want 1:\n%s", r.status, r.output)
	}
	mustContain(t, r, "::error::apt-get could not fetch make (E: Failed to fetch make_4.3-4.1build2_amd64.deb  Could not connect to archive.ubuntu.com:80). Re-run the job if a mirror was down.")
}

func TestAptInstallWithoutPackagesIsAUsageError(t *testing.T) {
	t.Parallel()
	r, _ := runAptInstall(t, aptCase{})
	if r.status != 2 || len(r.calls) != 0 {
		t.Fatalf("status %d, calls %q, want 2 and none:\n%s", r.status, r.calls, r.output)
	}
}
