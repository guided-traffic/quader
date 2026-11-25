package devprofile

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The made-up account of every case. No real account and no registry is involved.
const (
	testUser  = "user-for-test"
	testToken = "dckr_pat_made-up-for-the-test-0123456789"
)

var script = filepath.Join("..", "..", "..", "deploy", "dev", "upstream-accounts.sh")

// kubectlStub records each call's arguments, one line per call, in STUB_LOG. The create of the Secret prints a marker in place of the
// manifest, and the apply writes what it reads from its standard input to STUB_APPLIED, so a case sees where the manifest went.
const kubectlStub = `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STUB_LOG"
case " $* " in
  *" create secret "*) echo "manifest-of-create" ;;
  *" apply "*) cat > "$STUB_APPLIED" ;;
esac
exit 0
`

type run struct {
	stdout, stderr string
	err            error
	calls          []string
	applied        string
}

// runScript runs the script at path with bash, -x when trace, with kubectl stubbed and no variable of the caller that the script or
// git reads, plus env.
func runScript(t *testing.T, path string, trace bool, env []string, args ...string) run {
	t.Helper()
	stubs := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubs, "kubectl"), []byte(kubectlStub), 0o755); err != nil {
		t.Fatal(err)
	}
	log, applied := filepath.Join(stubs, "calls.log"), filepath.Join(stubs, "applied")
	bashArgs := []string{path}
	if trace {
		bashArgs = []string{"-x", path}
	}
	cmd := exec.Command("bash", append(bashArgs, args...)...)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if name != "PATH" && name != "UPSTREAM_ACCOUNTS_FILE" && !strings.HasPrefix(name, "GIT_") && !strings.HasPrefix(name, "STUB_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "PATH="+stubs+string(os.PathListSeparator)+os.Getenv("PATH"), "STUB_LOG="+log, "STUB_APPLIED="+applied)
	cmd.Env = append(cmd.Env, env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	r := run{err: cmd.Run(), stdout: stdout.String(), stderr: stderr.String()}
	if raw, err := os.ReadFile(log); err == nil {
		r.calls = strings.Split(strings.TrimSpace(string(raw)), "\n")
	}
	if raw, err := os.ReadFile(applied); err == nil {
		r.applied = string(raw)
	}
	return r
}

// writeAccountFile writes an account file as the owner keeps it: mode 0600, KEY=value lines.
func writeAccountFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// filled is an account file with the made-up account, a comment, a line ending of Windows and a quoted token.
func filled() string {
	return "# Docker Hub, read-only token\nDOCKERHUB_USERNAME=" + testUser + "\r\nDOCKERHUB_TOKEN=\"" + testToken + "\"\n"
}

// assertNoValue fails when text holds a value of the account in any form the script could have shown it: as it is, or base64 as a
// Secret's data or a Docker configuration carries it.
func assertNoValue(t *testing.T, what, text string) {
	t.Helper()
	enc := base64.StdEncoding.EncodeToString
	for _, form := range []string{testUser, testToken, enc([]byte(testUser)), enc([]byte(testToken)), enc([]byte(testUser + ":" + testToken))} {
		if strings.Contains(text, form) {
			t.Errorf("%s shows a value of the account (%q):\n%s", what, form, text)
		}
	}
}

// lines are the non-empty lines of text that are not lines of bash's trace.
func lines(text string) []string {
	var out []string
	for l := range strings.SplitSeq(text, "\n") {
		if l != "" && !strings.HasPrefix(l, "+") {
			out = append(out, l)
		}
	}
	return out
}

func assertMode(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s has mode %o, want %o", path, got, want)
	}
}

// A filled account file becomes the two files of upstream/docker-hub (0600 in directories of 0700), the Docker configuration of the
// kind nodes and the Secret's kubectl calls, and nothing the script prints or traces with bash -x, and no argument of a command it
// runs, holds a value: the values go from sed to a file and from the files to kubectl and base64.
func TestTheAccountFileReachesTheProfilesAndNoTraceShowsAValue(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "credentials", "upstream-registries.env")
	writeAccountFile(t, src, filled())
	out := filepath.Join(dir, "state", "upstream-accounts")

	r := runScript(t, script, true, []string{"UPSTREAM_ACCOUNTS_FILE=" + src}, "files", out, "none")
	if r.err != nil {
		t.Fatalf("files: %v\n%s%s", r.err, r.stdout, r.stderr)
	}
	assertNoValue(t, "the output and trace of files", r.stdout+r.stderr)
	if got, want := lines(r.stdout+r.stderr), []string{"upstream/docker-hub pulls with the account of " + src}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("files said %q, want %q", got, want)
	}
	for name, want := range map[string]string{"username": testUser, "password": testToken} {
		path := filepath.Join(out, "docker-hub", name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != want {
			t.Errorf("%s holds %d bytes, not the account's %s", path, len(raw), name)
		}
		assertMode(t, path, 0o600)
	}
	assertMode(t, out, 0o700)
	assertMode(t, filepath.Join(out, "docker-hub"), 0o700)

	r = runScript(t, script, true, nil, "dockerconfig", out)
	if r.err != nil {
		t.Fatalf("dockerconfig: %v\n%s", r.err, r.stderr)
	}
	assertNoValue(t, "the trace of dockerconfig", r.stderr)
	var cfg struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &cfg); err != nil {
		t.Fatalf("dockerconfig printed no JSON: %v", err)
	}
	auth, err := base64.StdEncoding.DecodeString(cfg.Auths["https://index.docker.io/v1/"].Auth)
	if err != nil || string(auth) != testUser+":"+testToken || len(cfg.Auths) != 1 {
		t.Errorf("dockerconfig's entry for Docker Hub is not the account's username and token (%d entries)", len(cfg.Auths))
	}

	r = runScript(t, script, true, nil, "secret", "kind-test", "quader", out)
	if r.err != nil {
		t.Fatalf("secret: %v\n%s", r.err, r.stderr)
	}
	calls := strings.Join(r.calls, "\n")
	assertNoValue(t, "the trace of secret", r.stdout+r.stderr)
	assertNoValue(t, "the arguments of kubectl", calls)
	want := []string{
		"--context kind-test -n quader create secret generic quader-upstream-docker-hub --from-file=username=" +
			filepath.Join(out, "docker-hub", "username") + " --from-file=token=" + filepath.Join(out, "docker-hub", "password") +
			" --dry-run=client -o yaml",
		"--context kind-test -n quader apply --server-side --force-conflicts --field-manager=quader-dev-upstream-accounts -f -",
	}
	// The two ends of the pipe start together, so either may record its call first.
	slices.Sort(want)
	if got := slices.Sorted(slices.Values(r.calls)); !slices.Equal(got, want) {
		t.Errorf("kubectl was called with\n%s\nwant\n%s", calls, strings.Join(want, "\n"))
	}
	if r.applied != "manifest-of-create\n" {
		t.Errorf("the apply read %q from its input, want the manifest of the create", r.applied)
	}
	if strings.Contains(calls, "--from-literal") {
		t.Errorf("a value was given to kubectl on its command line:\n%s", calls)
	}
}

// Without an account the profiles say so in one line, keep no files (the files of an earlier account are removed), print no Docker
// configuration and delete the Secret: for a placeholder file, empty values, a file that does not exist, an instance that reads
// none, and a username with a colon.
func TestWithoutAnAccountTheProfilesSayItInOneLineAndKeepNoFiles(t *testing.T) {
	t.Parallel()
	const prefix = "upstream/docker-hub pulls without an account: "
	for name, c := range map[string]struct {
		content string // the file's content; "-" for no file
		mode    string
		why     string
	}{
		"placeholders": {
			content: "DOCKERHUB_USERNAME=REPLACE_WITH_DOCKERHUB_USERNAME\nDOCKERHUB_TOKEN=REPLACE_WITH_DOCKERHUB_READ_ONLY_ACCESS_TOKEN\n",
			mode:    "none", why: "holds no account for it (DOCKERHUB_USERNAME, DOCKERHUB_TOKEN: empty or REPLACE_WITH_...)",
		},
		"empty values":  {content: "DOCKERHUB_USERNAME=\nDOCKERHUB_TOKEN=\n", mode: "none", why: "holds no account for it"},
		"token missing": {content: "DOCKERHUB_USERNAME=" + testUser + "\n", mode: "none", why: "holds no account for it"},
		"no file":       {content: "-", mode: "none", why: "does not exist"},
		"colon":         {content: "DOCKERHUB_USERNAME=" + testUser + ":x\nDOCKERHUB_TOKEN=" + testToken + "\n", mode: "none", why: "holds a colon"},
		"instance reads none": {
			content: "", mode: "none", why: "this instance reads an account only from the file UPSTREAM_ACCOUNTS_FILE names",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			src := filepath.Join(dir, "upstream-registries.env")
			out := filepath.Join(dir, "upstream-accounts")
			// The account of an earlier run, which this run must remove.
			writeAccountFile(t, src, filled())
			if r := runScript(t, script, false, []string{"UPSTREAM_ACCOUNTS_FILE=" + src}, "files", out, "none"); r.err != nil {
				t.Fatalf("files: %v\n%s", r.err, r.stderr)
			}
			env := []string{"UPSTREAM_ACCOUNTS_FILE=" + src}
			switch c.content {
			case "-":
				if err := os.Remove(src); err != nil {
					t.Fatal(err)
				}
			case "":
				env = nil
			default:
				writeAccountFile(t, src, c.content)
			}

			r := runScript(t, script, true, env, "files", out, c.mode)
			if r.err != nil {
				t.Fatalf("files: %v\n%s", r.err, r.stderr)
			}
			assertNoValue(t, "the output and trace of files", r.stdout+r.stderr)
			got := lines(r.stdout + r.stderr)
			if len(got) != 1 || !strings.HasPrefix(got[0], prefix) || !strings.Contains(got[0], c.why) {
				t.Errorf("files said %q, want one line %q...%q", got, prefix, c.why)
			}
			if _, err := os.Stat(filepath.Join(out, "docker-hub")); !os.IsNotExist(err) {
				t.Errorf("the files of upstream/docker-hub are still there (%v)", err)
			}
			if r := runScript(t, script, false, nil, "dockerconfig", out); r.stdout != "" || r.err == nil {
				t.Errorf("dockerconfig printed %q and ended with %v, want nothing and exit 3", r.stdout, r.err)
			} else if exit := (*exec.ExitError)(nil); !errors.As(r.err, &exit) || exit.ExitCode() != 3 {
				t.Errorf("dockerconfig ended with %v, want exit 3", r.err)
			}
			r = runScript(t, script, false, nil, "secret", "kind-test", "quader", out)
			if want := "--context kind-test -n quader delete secret quader-upstream-docker-hub --ignore-not-found"; r.err != nil ||
				strings.Join(r.calls, "\n") != want {
				t.Errorf("secret called kubectl with %q (%v), want %q", r.calls, r.err, want)
			}
		})
	}
}

// The main checkout reads its own .dev/credentials/upstream-registries.env; a git worktree reads none of its own accord, not even a
// file at that place in the worktree, and reads the one UPSTREAM_ACCOUNTS_FILE names (Q-ARCH-1067): a builder's instance or cluster
// never pulls with the owner's account by accident.
func TestAGitWorktreeReadsAnAccountOnlyFromTheFileTheVariableNames(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is needed to make a worktree")
	}
	base := t.TempDir()
	main, wt := filepath.Join(base, "main"), filepath.Join(base, "wt")
	raw, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(main, "deploy", "dev"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(main, "deploy", "dev", "upstream-accounts.sh"), raw, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(dir string, args ...string) {
		t.Helper()
		flags := []string{"-C", dir, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false"}
		cmd := exec.Command("git", append(flags, args...)...)
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "GIT_") {
				cmd.Env = append(cmd.Env, kv)
			}
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git(main, "init", "-q")
	git(main, "add", ".")
	git(main, "commit", "-q", "-m", "script")
	git(main, "worktree", "add", "-q", wt)
	mainFile := filepath.Join(main, ".dev", "credentials", "upstream-registries.env")
	writeAccountFile(t, mainFile, filled())
	writeAccountFile(t, filepath.Join(wt, ".dev", "credentials", "upstream-registries.env"), filled())

	for _, c := range []struct {
		name, checkout string
		env            []string
		want           string
	}{
		{"the main checkout", main, nil, "upstream/docker-hub pulls with the account of " + mainFile},
		{"a worktree", wt, nil, "upstream/docker-hub pulls without an account: this checkout is a git worktree, which reads an account only from the file UPSTREAM_ACCOUNTS_FILE names"},
		{"a worktree with the variable", wt, []string{"UPSTREAM_ACCOUNTS_FILE=" + mainFile}, "upstream/docker-hub pulls with the account of " + mainFile},
	} {
		out := filepath.Join(t.TempDir(), "upstream-accounts")
		r := runScript(t, filepath.Join(c.checkout, "deploy", "dev", "upstream-accounts.sh"), true, c.env, "files", out, "main")
		if r.err != nil {
			t.Fatalf("%s: files: %v\n%s", c.name, r.err, r.stderr)
		}
		assertNoValue(t, c.name+": the output and trace of files", r.stdout+r.stderr)
		if got := lines(r.stdout + r.stderr); len(got) != 1 || got[0] != c.want {
			t.Errorf("%s: files said %q, want %q", c.name, got, c.want)
		}
		_, err := os.Stat(filepath.Join(out, "docker-hub", "password"))
		if has := err == nil; has != strings.Contains(c.want, "pulls with the account") {
			t.Errorf("%s: the files are there: %v, want them only with an account", c.name, has)
		}
	}
}
