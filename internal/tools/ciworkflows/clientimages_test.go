package ciworkflows

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The stubs of the clients image script: go writes STUB_FWD as the forwarder binary, docker answers load, image inspect and
// save with the exit codes the test sets (save writes STUB_SAVED into the tar first), and the set's build.sh in the copied tree
// exits with STUB_BUILD. Every call goes to STUB_LOG.
var clientImageStubs = map[string]string{
	"go": `#!/usr/bin/env bash
echo "go $*" >> "$STUB_LOG"
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then out="$2"; shift; fi
  shift
done
[ -n "$out" ] && printf '%s' "$STUB_FWD" > "$out"
exit 0
`,
	"docker": `#!/usr/bin/env bash
echo "docker $*" >> "$STUB_LOG"
case "$1" in
  load) exit "${STUB_LOAD:-0}" ;;
  image) exit "${STUB_INSPECT:-0}" ;;
  save)
    [ "$2" = "-o" ] || exit 2
    printf '%s' "$STUB_SAVED" > "$3"
    exit "${STUB_SAVE:-0}" ;;
esac
exit 3
`,
}

// clientTree copies the script and tools/versions.env into a new tree and gives every set a context directory with a
// Dockerfile and a build.sh stub, so that the tests can change inputs without touching the repository.
func clientTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	copyFile := func(from, to string, mode os.FileMode) {
		raw, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(to, raw, mode); err != nil {
			t.Fatal(err)
		}
	}
	copyFile(filepath.Join(repoRoot, ".github", "scripts", "client-images.sh"), filepath.Join(root, ".github", "scripts", "client-images.sh"), 0o755)
	copyFile(filepath.Join(repoRoot, "tools", "versions.env"), filepath.Join(root, "tools", "versions.env"), 0o644)
	build := "#!/usr/bin/env bash\necho \"build.sh $*\" >> \"$STUB_LOG\"\nexit \"${STUB_BUILD:-0}\"\n"
	for _, dir := range []string{"clients", "nugetclients", "ociclients", "aptclients", "ansibleclients"} {
		d := filepath.Join(root, "test", "e2e", dir)
		write(t, filepath.Join(d, "Dockerfile"), "FROM scratch\n")
		if err := os.WriteFile(filepath.Join(d, "build.sh"), []byte(build), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(root, "test", "e2e", "clients", "fwd", "main.go"), "package main\n")
	return root
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

type clientRun struct {
	status int
	stdout string
	stderr string
	calls  []string
	output string // what the script wrote to GITHUB_OUTPUT
}

func runClientImages(t *testing.T, root string, env []string, args ...string) clientRun {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH")
	}
	stubDir := t.TempDir()
	for name, body := range clientImageStubs {
		if err := os.WriteFile(filepath.Join(stubDir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	state := t.TempDir()
	log := filepath.Join(state, "calls.log")
	out := filepath.Join(state, "github-output")
	cmd := exec.Command(bash, append([]string{filepath.Join(root, ".github", "scripts", "client-images.sh")}, args...)...)
	cmd.Env = append([]string{
		"PATH=" + stubDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + state,
		"TMPDIR=" + state,
		"STUB_LOG=" + log,
		"STUB_FWD=forwarder-1",
		"STUB_SAVED=images",
		"GITHUB_OUTPUT=" + out,
	}, env...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	r := clientRun{stdout: stdout.String(), stderr: stderr.String()}
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
	if raw, err := os.ReadFile(out); err == nil {
		r.output = string(raw)
	}
	return r
}

func key(t *testing.T, root string, env ...string) string {
	t.Helper()
	r := runClientImages(t, root, env, "key", "npm")
	k := strings.TrimSpace(r.stdout)
	if r.status != 0 || !strings.HasPrefix(k, "e2e-clients-npm-linux-") || len(k) < len("e2e-clients-npm-linux-amd64-")+64 {
		t.Fatalf("key: status %d, key %q:\n%s", r.status, k, r.stderr)
	}
	if !strings.Contains(r.output, "key="+k+"\n") {
		t.Errorf("GITHUB_OUTPUT lacks key=%s: %q", k, r.output)
	}
	return k
}

func setPin(t *testing.T, root, name, value string) {
	t.Helper()
	p := filepath.Join(root, "tools", "versions.env")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	found := false
	for i, l := range lines {
		if strings.HasPrefix(l, name+"=") {
			lines[i] = name + "=" + value
			found = true
		}
	}
	if !found {
		t.Fatalf("tools/versions.env has no %s", name)
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The key of a set changes with every input of its images (a pin it reads, a file of its build context, the forwarder binary)
// and with nothing else (another set's pin, node_modules, a file outside the context), Q-ARCH-521.
func TestClientImagesTheKeyChangesWithTheInputsOnly(t *testing.T) {
	t.Parallel()
	root := clientTree(t)
	base := key(t, root)
	if again := key(t, root); again != base {
		t.Fatalf("the same inputs gave two keys: %s and %s", base, again)
	}
	same := func(what string) {
		t.Helper()
		if k := key(t, root); k != base {
			t.Errorf("%s changed the key", what)
		}
	}
	differs := func(what string, k string) {
		t.Helper()
		if k == base {
			t.Errorf("%s did not change the key", what)
		}
	}

	setPin(t, root, "E2E_NUGETCLIENTS_JQ_VERSION", "9.9")
	same("a pin of another set")
	write(t, filepath.Join(root, "test", "e2e", "clients", "node_modules", "x", "index.js"), "x")
	same("a file under node_modules")
	write(t, filepath.Join(root, "README.md"), "x")
	same("a file outside the build context")

	differs("another forwarder binary", key(t, root, "STUB_FWD=forwarder-2"))

	setPin(t, root, "E2E_CLIENTS_JQ_VERSION", "9.9")
	changed := key(t, root)
	differs("a pin the set reads", changed)
	base = changed
	write(t, filepath.Join(root, "test", "e2e", "clients", "Dockerfile"), "FROM scratch\nRUN true\n")
	differs("a changed Dockerfile", key(t, root))
	write(t, filepath.Join(root, "test", "e2e", "clients", "bin", "npm12"), "#!/bin/sh\n")
	differs("a new file of the build context", key(t, root))
}

// The key does not depend on the commit or on where the checkout lies (Q-ARCH-595). Four pushes in a row of 2026-11-23 missed the
// cache, and the legs of one nightly missed each other's entries: go build stamped the commit, its time and the main module's
// pseudo-version into the forwarder, whose hash is an input of the key. Here the script runs with the real go, not the stub, in a
// git repository of its own holding the real forwarder: the key is the same after a commit that changes nothing of the images,
// and in a copy of the tree at another path. A key input that follows the commit fails this test.
func TestClientImagesTheKeyDoesNotDependOnTheCommit(t *testing.T) {
	t.Parallel()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	goEnv := func(name string) string {
		out, err := exec.Command(goBin, "env", name).Output()
		if err != nil {
			t.Fatalf("go env %s: %v", name, err)
		}
		return strings.TrimSpace(string(out))
	}
	// The real go with the developer's caches, so the build costs seconds; the stub directory then holds only docker.
	env := []string{
		"GOCACHE=" + goEnv("GOCACHE"), "GOMODCACHE=" + goEnv("GOMODCACHE"), "GOPATH=" + goEnv("GOPATH"),
		"GOTOOLCHAIN=local", "GOFLAGS=", "GOPROXY=off",
	}
	realKey := func(root string) string {
		t.Helper()
		stubDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(stubDir, "docker"), []byte(clientImageStubs["docker"]), 0o755); err != nil {
			t.Fatal(err)
		}
		state := t.TempDir()
		cmd := exec.Command("bash", filepath.Join(root, ".github", "scripts", "client-images.sh"), "key", "npm")
		cmd.Env = append([]string{
			"PATH=" + stubDir + string(os.PathListSeparator) + filepath.Dir(goBin) + string(os.PathListSeparator) + os.Getenv("PATH"),
			"HOME=" + state, "TMPDIR=" + state, "STUB_LOG=" + filepath.Join(state, "calls.log"),
		}, env...)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("key in %s: %v\n%s", root, err, stderr.String())
		}
		return strings.TrimSpace(string(out))
	}

	root := clientTree(t)
	goMod := readRepoFile(t, "go.mod")
	goLine := ""
	for _, l := range strings.Split(goMod, "\n") {
		if strings.HasPrefix(l, "go ") {
			goLine = l
		}
	}
	write(t, filepath.Join(root, "go.mod"), "module github.com/guided-traffic/quader\n\n"+goLine+"\n")
	write(t, filepath.Join(root, "test", "e2e", "clients", "fwd", "main.go"), readRepoFile(t, filepath.Join("test", "e2e", "clients", "fwd", "main.go")))
	repo := legsRepo{dir: root, home: t.TempDir()}
	repo.git(t, "init", "-q", "-b", "main")
	repo.git(t, "add", "-A")
	repo.git(t, "commit", "-q", "-m", "first")
	first := realKey(root)

	write(t, filepath.Join(root, "README.md"), "a change outside every input\n")
	repo.git(t, "add", "-A")
	repo.git(t, "commit", "-q", "-m", "second")
	if second := realKey(root); second != first {
		t.Errorf("a commit that changes no input of the images changed the key: %s, then %s", first, second)
	}

	moved := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.CopyFS(moved, os.DirFS(root)); err != nil {
		t.Fatal(err)
	}
	if elsewhere := realKey(moved); elsewhere != first {
		t.Errorf("the same tree at another path gave another key: %s, then %s", first, elsewhere)
	}
}

// A tar from the cache is loaded and nothing is built or saved: the hosts the build installs from are not contacted.
func TestClientImagesALoadableTarIsLoadedAndNothingIsBuilt(t *testing.T) {
	t.Parallel()
	root := clientTree(t)
	tar := filepath.Join(t.TempDir(), "nuget.tar")
	write(t, tar, "cached")
	r := runClientImages(t, root, nil, "load-or-build", "nuget", tar)
	want := []string{"docker load -i " + tar, "docker image inspect quader-e2e-nugetclients:dev"}
	if r.status != 0 || strings.Join(r.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("status %d, calls %q, want 0 and %q:\n%s%s", r.status, r.calls, want, r.stdout, r.stderr)
	}
	if r.output != "built=false\n" {
		t.Errorf("GITHUB_OUTPUT %q, want built=false", r.output)
	}
}

// Without a tar the set is built by its build.sh with the names deploy/dev/e2e.sh loads, and saved into the tar.
func TestClientImagesWithoutATarTheSetIsBuiltAndSaved(t *testing.T) {
	t.Parallel()
	root := clientTree(t)
	tar := filepath.Join(t.TempDir(), "sub", "apt.tar")
	r := runClientImages(t, root, nil, "load-or-build", "apt", tar)
	images := "quader-e2e-aptclients-debian13:dev quader-e2e-aptclients-debian12:dev quader-e2e-aptclients-ubuntu2404:dev quader-e2e-aptclients-ubuntu2204:dev"
	want := []string{"build.sh quader-e2e-aptclients-", "docker save -o " + tar + ".part " + images}
	if r.status != 0 || strings.Join(r.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("status %d, calls %q, want 0 and %q:\n%s%s", r.status, r.calls, want, r.stdout, r.stderr)
	}
	if raw, err := os.ReadFile(tar); err != nil || string(raw) != "images" {
		t.Errorf("the tar holds %q (%v), want the saved images", raw, err)
	}
	if r.output != "built=true\n" {
		t.Errorf("GITHUB_OUTPUT %q, want built=true", r.output)
	}
}

// A tar that does not load (a partial or corrupt cache entry) does not fail the leg: it is deleted and the set is built.
func TestClientImagesATarThatDoesNotLoadIsBuiltAgain(t *testing.T) {
	t.Parallel()
	root := clientTree(t)
	for _, failing := range []string{"STUB_LOAD=1", "STUB_INSPECT=1"} {
		tar := filepath.Join(t.TempDir(), "npm.tar")
		write(t, tar, "truncated")
		r := runClientImages(t, root, []string{failing}, "load-or-build", "npm", tar)
		if r.status != 0 {
			t.Fatalf("%s: status %d:\n%s%s", failing, r.status, r.stdout, r.stderr)
		}
		if !strings.Contains(r.stdout, "::warning::") || !strings.Contains(strings.Join(r.calls, "|"), "build.sh quader-e2e-clients:dev") {
			t.Errorf("%s: no warning or no build: calls %q\n%s", failing, r.calls, r.stdout)
		}
		if raw, _ := os.ReadFile(tar); string(raw) != "images" {
			t.Errorf("%s: the tar holds %q, want the newly saved images", failing, raw)
		}
		if r.output != "built=true\n" {
			t.Errorf("%s: GITHUB_OUTPUT %q, want built=true", failing, r.output)
		}
	}
}

// A failed build or save fails the step and leaves no tar, so the cache never receives a partial entry.
func TestClientImagesAFailedBuildOrSaveLeavesNoTar(t *testing.T) {
	t.Parallel()
	root := clientTree(t)
	for _, failing := range []string{"STUB_BUILD=1", "STUB_SAVE=1"} {
		dir := t.TempDir()
		tar := filepath.Join(dir, "oci.tar")
		r := runClientImages(t, root, []string{failing}, "load-or-build", "oci", tar)
		if r.status == 0 {
			t.Errorf("%s: the step passed", failing)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Errorf("%s: %d files left in the tar's directory", failing, len(entries))
		}
		if strings.Contains(r.output, "built=") {
			t.Errorf("%s: GITHUB_OUTPUT %q, want no built=", failing, r.output)
		}
	}
}

func TestClientImagesAnUnknownSetIsAUsageError(t *testing.T) {
	t.Parallel()
	root := clientTree(t)
	for _, args := range [][]string{{"key", "maven"}, {"load-or-build", "npm"}, {"build", "npm", "x.tar"}, {"key"}} {
		if r := runClientImages(t, root, nil, args...); r.status != 2 || len(r.calls) != 0 {
			t.Errorf("%q: status %d, calls %q, want 2 and none", args, r.status, r.calls)
		}
	}
}
