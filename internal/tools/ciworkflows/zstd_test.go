package ciworkflows

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The pinned zstd of the Actions cache (Q-ARCH-1061, .github/actions/zstd): actions/cache writes zstd when `zstd` is on PATH and
// gzip otherwise, and the compression is part of an entry's version, so a job that reaches the cache without the pin writes an
// entry of its runner kind alone.

// cacheStep says whether a step reads or writes the GitHub Actions cache: the composite actions that use it, actions/cache itself,
// and actions/setup-node with a cache.
func cacheStep(s step) bool {
	switch s.Uses {
	case "./.github/actions/go-modules", "./.github/actions/go-tool", "./.github/actions/client-images", "./.github/actions/install-buildx":
		return true
	}
	if strings.HasPrefix(s.Uses, "actions/cache@") || strings.HasPrefix(s.Uses, "actions/cache/") {
		return true
	}
	return strings.HasPrefix(s.Uses, "actions/setup-node@") && s.With["cache"] != ""
}

func isZstd(s step) bool { return s.Uses == "./.github/actions/zstd" }

// Every job puts the pinned zstd on PATH before its first step that uses the cache, under a condition that holds whenever that
// step runs (none, or one the cache step's own condition contains), and bounds it at 5 minutes; no job has the step twice.
func TestEveryCacheStepRunsAfterThePinnedZstd(t *testing.T) {
	t.Parallel()
	seen := 0
	for file, w := range loadWorkflows(t) {
		for name, j := range w.Jobs {
			where := file + " job " + name
			zstd := -1
			for i, s := range j.Steps {
				if isZstd(s) {
					if zstd >= 0 {
						t.Errorf("%s puts zstd on PATH twice (steps %d and %d)", where, zstd, i)
					}
					zstd = i
					if limit, ok := minutes(s.TimeoutMinutes); !ok || limit != 5 {
						t.Errorf("%s step %q: timeout-minutes is not 5", where, s.Name)
					}
					continue
				}
				if !cacheStep(s) {
					continue
				}
				seen++
				if zstd < 0 {
					t.Errorf("%s step %q uses the Actions cache before the pinned zstd is on PATH (.github/actions/zstd)", where, s.Name)
					break
				}
				if cond := j.Steps[zstd].If; cond != "" && !strings.Contains(s.If, cond) {
					t.Errorf("%s: the zstd step runs if %q, the cache step %q if %q", where, cond, s.Name, s.If)
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("no step of any workflow uses the Actions cache: the rule above matched nothing")
	}
}

// No composite action under .github/actions uses the cache without the pin being on PATH already: the jobs put it there, and an
// action that called the cache in a job without the step would write gzip unnoticed. The rule above holds for the actions'
// callers; this one holds that the list of cache-using composite actions above is complete.
func TestTheCacheActionsAreTheOnesTheZstdRuleKnows(t *testing.T) {
	t.Parallel()
	dirs, err := filepath.Glob(filepath.Join(repoRoot, ".github", "actions", "*", "action.yml"))
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no composite actions: %v", err)
	}
	for _, f := range dirs {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(filepath.Dir(f))
		usesCache := strings.Contains(string(raw), "uses: actions/cache")
		known := cacheStep(step{Uses: "./.github/actions/" + name})
		if name == "zstd" {
			continue
		}
		if usesCache && !known {
			t.Errorf(".github/actions/%s uses the Actions cache but cacheStep does not name it, so no job is held to put zstd on PATH first", name)
		}
		if !usesCache && known {
			t.Errorf(".github/actions/%s no longer uses the Actions cache; drop it from cacheStep", name)
		}
	}
}

// fakeZstdSource is a source archive whose make target zstd-release writes a program that reports version.
func fakeZstdSource(t *testing.T, dir, version, makefile string) (string, string) {
	t.Helper()
	path := filepath.Join(dir, "zstd-"+version+".tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := []byte(makefile)
	if err := tw.WriteHeader(&tar.Header{Name: "zstd-" + version + "/Makefile", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	for _, c := range []interface{ Close() error }{tw, gz, f} {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return path, hex.EncodeToString(sum[:])
}

type zstdRun struct {
	out, githubPath, githubOutput string
}

func runZstdInstall(t *testing.T, work, version, digest, source string) zstdRun {
	t.Helper()
	ghPath := filepath.Join(work, "github-path")
	ghOut := filepath.Join(work, "github-output")
	script, err := filepath.Abs(filepath.Join(repoRoot, ".github", "actions", "zstd", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script)
	cmd.Dir = work
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + work, "GITHUB_WORKSPACE=" + work,
		"WANT=" + version, "DIGEST=" + digest, "DIR=.bin/zstd-" + version,
		"GITHUB_PATH=" + ghPath, "GITHUB_OUTPUT=" + ghOut, "QUADER_ZSTD_SOURCE=" + source,
	}
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		t.Fatalf("install.sh exited %d; it never fails a job:\n%s", ee.ExitCode(), out)
	} else if err != nil {
		t.Fatal(err)
	}
	read := func(p string) string {
		raw, err := os.ReadFile(p)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return string(raw)
	}
	return zstdRun{out: string(out), githubPath: read(ghPath), githubOutput: read(ghOut)}
}

const fakeZstdMakefile = "zstd-release:\n\tmkdir -p programs\n\tprintf '#!/bin/sh\\necho 9.9.9\\n' > programs/zstd\n\tchmod +x programs/zstd\n"

// A miss builds the program from the source archive whose SHA-256 is the pin, puts it and the names tar calls beside it on PATH,
// and asks the cache to save it; the next job restores it and builds nothing; a wrong digest or a failed build leaves the job on
// what the runner has, with a warning, and never fails it. Unit, milliseconds, a fake source archive of one Makefile.
func TestThePinnedZstdIsBuiltFromTheVerifiedSourceOnceAndNeverFailsAJob(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not on PATH")
	}

	work := t.TempDir()
	source, digest := fakeZstdSource(t, t.TempDir(), "9.9.9", fakeZstdMakefile)
	r := runZstdInstall(t, work, "9.9.9", digest, source)
	if !strings.Contains(r.githubOutput, "built=true") {
		t.Fatalf("a miss did not build:\n%s\n%s", r.out, r.githubOutput)
	}
	dir := filepath.Join(work, ".bin", "zstd-9.9.9")
	if strings.TrimSpace(r.githubPath) != dir {
		if real, err := filepath.EvalSymlinks(dir); err != nil || strings.TrimSpace(r.githubPath) != real {
			t.Errorf("GITHUB_PATH holds %q, want %s", r.githubPath, dir)
		}
	}
	for _, name := range []string{"zstd", "zstdmt", "unzstd"} {
		out, err := exec.Command(filepath.Join(dir, name), "--quiet", "--version").Output()
		if err != nil || strings.TrimSpace(string(out)) != "9.9.9" {
			t.Errorf("%s does not run the built program: %q %v", name, out, err)
		}
	}

	// The next job of the week: the cache restored the directory, nothing is built or saved.
	again := runZstdInstall(t, work, "9.9.9", digest, filepath.Join(work, "no-such-archive"))
	if !strings.Contains(again.githubOutput, "built=false") || strings.Contains(again.out, "::warning::") {
		t.Errorf("a restored program was not used as it is:\n%s\n%s", again.out, again.githubOutput)
	}

	// A source archive that is not the pinned one is never built.
	other := t.TempDir()
	bad := runZstdInstall(t, other, "9.9.9", strings.Repeat("0", 64), source)
	if !strings.Contains(bad.out, "::warning::") || !strings.Contains(bad.out, "tools/versions.env pins "+strings.Repeat("0", 64)) ||
		bad.githubPath != "" || !strings.Contains(bad.githubOutput, "built=false") {
		t.Errorf("a wrong digest was not refused with a warning:\n%s\n%q %q", bad.out, bad.githubPath, bad.githubOutput)
	}
	if _, err := os.Stat(filepath.Join(other, ".bin", "zstd-9.9.9", "zstd")); err == nil {
		t.Error("the program of a refused archive was installed")
	}

	// A build that fails, or one that makes a program of another version, leaves the job as it was.
	broken, brokenDigest := fakeZstdSource(t, t.TempDir(), "9.9.9", "zstd-release:\n\texit 3\n")
	if r := runZstdInstall(t, t.TempDir(), "9.9.9", brokenDigest, broken); !strings.Contains(r.out, "make zstd-release failed") || r.githubPath != "" {
		t.Errorf("a failed build was not a warning:\n%s", r.out)
	}
	wrong, wrongDigest := fakeZstdSource(t, t.TempDir(), "9.9.8", strings.ReplaceAll(fakeZstdMakefile, "9.9.9", "9.9.7"))
	if r := runZstdInstall(t, t.TempDir(), "9.9.8", wrongDigest, wrong); !strings.Contains(r.out, "does not report 9.9.8") || r.githubPath != "" {
		t.Errorf("a program of another version was put on PATH:\n%s", r.out)
	}
}

// The pin is one version and one digest of the release's source archive, read by the action from tools/versions.env.
func TestTheZstdPinHasItsDigest(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(repoRoot, "tools", "versions.env"))
	if err != nil {
		t.Fatal(err)
	}
	pins := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok && !strings.HasPrefix(k, "#") {
			pins[k] = v
		}
	}
	if v := pins["ZSTD_VERSION"]; !strings.HasPrefix(v, "1.") || strings.HasPrefix(v, "v") {
		t.Errorf("ZSTD_VERSION=%q: want the release's number without the v of its tag", v)
	}
	if d := pins["ZSTD_SHA256"]; len(d) != 64 || strings.Trim(d, "0123456789abcdef") != "" {
		t.Errorf("ZSTD_SHA256=%q is not a SHA-256", d)
	}
}
