package ciworkflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A checkout's development profiles do not meet another checkout's (Q-HA-491, Q-UI-1044). The kind cluster, its edge port and its
// kubeconfig are derived from the checkout (deploy/dev/dev-tag.sh --cluster, deploy/dev/lib.sh), and an instance of the UI
// development profile is claimed under a lock, not chosen (deploy/ui-dev/instance.sh): on 2026-11-23 two builders chose the same
// instance a minute apart and the second recreated the first's containers with its own certificates. The scripts run here in a
// repository of their own with two worktrees, docker stubbed, without a cluster or a container.

// profileRepo is a git repository whose commit holds the scripts, and the paths of its main checkout and its worktrees.
type profileRepo struct {
	home, main string
	worktrees  map[string]string
}

func newProfileRepo(t *testing.T, worktrees ...string) profileRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not on PATH")
	}
	// the scripts print physical paths (pwd after cd, git's), and macOS's temporary directory is reached through a link
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := profileRepo{home: t.TempDir(), main: filepath.Join(base, "main"), worktrees: map[string]string{}}
	for _, f := range []string{"deploy/dev/lib.sh", "deploy/dev/dev-tag.sh", "deploy/ui-dev/instance.sh", "tools/versions.env"} {
		write(t, filepath.Join(r.main, f), readRepoFile(t, f))
	}
	if err := os.Chmod(filepath.Join(r.main, "deploy/dev/dev-tag.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = gitEnv(r.home)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	git(r.main, "init", "-q", "-b", "main")
	git(r.main, "config", "commit.gpgsign", "false")
	git(r.main, "add", "-A")
	git(r.main, "commit", "-q", "-m", "base")
	for _, name := range worktrees {
		dir := filepath.Join(base, name)
		git(r.main, "worktree", "add", "-q", "--detach", dir)
		r.worktrees[name] = dir
	}
	return r
}

type shellRun struct {
	code           int
	stdout, stderr string
}

// sh runs a bash snippet in the checkout dir with the stubs first on PATH, the repository's git environment and the variables of env;
// docker is a stub that writes its arguments to $STUB_LOG.
func (r profileRepo) sh(t *testing.T, dir string, env []string, script string) shellRun {
	t.Helper()
	stubs := filepath.Join(r.home, "bin")
	write(t, filepath.Join(stubs, "docker"), "#!/usr/bin/env bash\necho \"docker $*\" >> \"$STUB_LOG\"\n")
	if err := os.Chmod(filepath.Join(stubs, "docker"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", script)
	cmd.Dir = dir
	cmd.Env = append(gitEnv(r.home), env...)
	cmd.Env[0] = "PATH=" + stubs + string(os.PathListSeparator) + os.Getenv("PATH")
	cmd.Env = append(cmd.Env, "STUB_LOG="+filepath.Join(r.home, "docker.log"))
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return shellRun{code: code, stdout: strings.TrimSpace(out.String()), stderr: errb.String()}
}

func TestDevProfileClusterPortAndKubeconfigArePerCheckout(t *testing.T) {
	r := newProfileRepo(t, "Agent_With.A-Very-Long-Name-That-Is-Cut-At-Fortytwo-Characters-")
	wt := r.worktrees["Agent_With.A-Very-Long-Name-That-Is-Cut-At-Fortytwo-Characters-"]
	show := `. deploy/dev/lib.sh; printf '%s|%s|%s|%s\n' "$CLUSTER" "$DEV_HTTP_PORT" "${KUBECONFIG:-}" "$CTX"`

	if got, want := r.sh(t, r.main, []string{"KUBECONFIG=/k/owner"}, show).stdout, "quader|8080|/k/owner|kind-quader"; got != want {
		t.Errorf("the main checkout: %q, want %q (the owner's and the pipeline's names, the caller's kubeconfig)", got, want)
	}
	res := r.sh(t, wt, []string{"KUBECONFIG=/k/owner"}, show)
	parts := strings.Split(res.stdout, "|")
	if len(parts) != 4 {
		t.Fatalf("the worktree printed %q (%s)", res.stdout, res.stderr)
	}
	cluster := parts[0]
	if cluster != "quader-agent_with.a-very-long-name-that-is-cut-at" && cluster != "quader-agent-with.a-very-long-name-that-is-cut-at" {
		t.Errorf("the worktree's cluster is %q, want quader- and its name in lower case, cut to 49 characters", cluster)
	}
	if len(cluster) > 49 || strings.HasSuffix(cluster, "-") || strings.HasSuffix(cluster, ".") {
		t.Errorf("the worktree's cluster %q is longer than 49 characters or ends in a separator", cluster)
	}
	if parts[1] != "0" {
		t.Errorf("the worktree's edge port is %q, want 0: kind picks a free one", parts[1])
	}
	if want := filepath.Join(wt, ".dev", "kubeconfig"); parts[2] != want {
		t.Errorf("the worktree's kubeconfig is %q, want %q, whatever the caller's KUBECONFIG says", parts[2], want)
	}
	if parts[3] != "kind-"+cluster {
		t.Errorf("the context is %q, want kind-%s", parts[3], cluster)
	}
	if got := r.sh(t, wt, []string{"QUADER_DEV_CLUSTER=mine", "QUADER_DEV_HTTP_PORT=18080", "QUADER_DEV_KUBECONFIG=/k/mine"}, show).stdout; got != "mine|18080|/k/mine|kind-mine" {
		t.Errorf("the variables set each one: %q", got)
	}
	if got := r.sh(t, wt, []string{"QUADER_DEV_TAG=other"}, "deploy/dev/dev-tag.sh --cluster").stdout; got != cluster {
		t.Errorf("dev-tag.sh --cluster under QUADER_DEV_TAG printed %q, want %q: the tag does not name the cluster", got, cluster)
	}
}

func TestUIDevInstancesAreClaimedNotChosen(t *testing.T) {
	r := newProfileRepo(t, "wt-a", "wt-b", "wt-c")
	a, b, c := r.worktrees["wt-a"], r.worktrees["wt-b"], r.worktrees["wt-c"]
	claims := filepath.Join(r.main, ".dev", "ui-dev-claims")
	instance := func(dir string, env ...string) shellRun {
		t.Helper()
		return r.sh(t, dir, env, `. deploy/ui-dev/instance.sh; ui_instance "$PWD" || exit $?; printf '%s %s %s\n' "$UI_INSTANCE" "$UI_PROJECT" "$UI_WEB_PORT"`)
	}
	expect := func(what string, got shellRun, want string) {
		t.Helper()
		if got.code != 0 || got.stdout != want {
			t.Fatalf("%s: exit %d, %q, want %q\n%s", what, got.code, got.stdout, want, got.stderr)
		}
	}
	owner := func(n string) string {
		raw, err := os.ReadFile(filepath.Join(claims, n))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(raw))
	}

	expect("the main checkout without a number", instance(r.main), "1 quader-ui-dev 4200")
	if _, err := os.Stat(claims); err == nil {
		entries, _ := os.ReadDir(claims)
		if len(entries) != 0 {
			t.Fatalf("instance 1 left a claim: %v", entries)
		}
	}
	expect("the first worktree without a number", instance(a), "2 quader-ui-dev2 4300")
	if owner("2") != a {
		t.Fatalf("the claim of instance 2 holds %q, want %q", owner("2"), a)
	}
	expect("the first worktree again", instance(a), "2 quader-ui-dev2 4300")
	expect("the second worktree without a number", instance(b), "3 quader-ui-dev3 4400")

	if res := instance(b, "UI_INSTANCE=2"); res.code == 0 || !strings.Contains(res.stderr, "claimed by "+a) {
		t.Fatalf("the second worktree named the first's instance: exit %d, %q, want a refusal that names %s\n%s", res.code, res.stdout, a, res.stderr)
	}
	if res := instance(a, "UI_INSTANCE=1"); res.code == 0 || !strings.Contains(res.stderr, "main checkout's") {
		t.Fatalf("a worktree named instance 1: exit %d, want a refusal\n%s", res.code, res.stderr)
	}
	if owner("2") != a || owner("3") != b {
		t.Fatalf("a refusal changed a claim: 2 is %q, 3 is %q", owner("2"), owner("3"))
	}
	expect("the main checkout names a free instance", instance(r.main, "UI_INSTANCE=5"), "5 quader-ui-dev5 4600")

	// The first worktree is removed without its down.sh: its claim is stale, the next checkout takes the instance over and removes
	// the containers and volumes of its compose project first.
	if err := os.RemoveAll(a); err != nil {
		t.Fatal(err)
	}
	res := instance(c)
	expect("a third worktree after the first was removed", res, "2 quader-ui-dev2 4300")
	if !strings.Contains(res.stderr, "no longer exists") {
		t.Errorf("the takeover said nothing:\n%s", res.stderr)
	}
	log, _ := os.ReadFile(filepath.Join(r.home, "docker.log"))
	for _, want := range []string{"ps -aq --filter label=com.docker.compose.project=quader-ui-dev2", "volume ls -q --filter label=com.docker.compose.project=quader-ui-dev2"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("the takeover did not run docker %s; docker was called with:\n%s", want, log)
		}
	}

	// down.sh --purge ends the claim (ui_release), and only the checkout's own.
	if res := r.sh(t, b, []string{"UI_INSTANCE=2"}, `. deploy/ui-dev/instance.sh; UI_INSTANCE=2; ui_release "$PWD"`); res.code != 0 || owner("2") != c {
		t.Fatalf("the second worktree released the third's instance: exit %d, the claim holds %q\n%s", res.code, owner("2"), res.stderr)
	}
	if res := r.sh(t, b, nil, `. deploy/ui-dev/instance.sh; ui_instance "$PWD" && ui_release "$PWD"`); res.code != 0 || owner("3") != "" {
		t.Fatalf("the release of the second worktree's instance: exit %d, the claim holds %q\n%s", res.code, owner("3"), res.stderr)
	}
	expect("the freed instance is claimed again", instance(b), "3 quader-ui-dev3 4400")
}
