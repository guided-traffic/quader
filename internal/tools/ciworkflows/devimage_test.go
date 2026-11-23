package ciworkflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The image a caller gives a profile script is the image Helm runs (Q-HA-489). up.sh, bastion.sh and restore-ns.sh set the chart's
// image.registry, image.repository and image.tag from QUADER_IMAGE (image_values of deploy/dev/lib.sh, the tag with --set-string),
// and Helm applies every --set-json, then every --set, then every --set-string, whatever their order on the command line. A
// `--set image.tag=iface` among a script's extra arguments therefore lost to the script's own `--set-string image.tag=dev`: the
// interface-mismatch scenario's main installation kept the checkout's image and TestBastion_R_BAS_049_InterfaceMismatch waited six
// minutes for a mismatch that could not come (Test and Release 45187733127 and 45191858474). The scripts run here in a copy of
// deploy/dev with docker, kind, kubectl and helm stubbed, without a cluster or the network.

// devImageStubs record every call in STUB_LOG and fail with 97 while STUB_FAIL is set. docker image inspect prints an ID made of the
// image's name; helm writes its arguments, one per line, to STUB_HELM_ARGS.
var devImageStubs = map[string]string{
	"docker": `#!/usr/bin/env bash
echo "docker $*" >> "$STUB_LOG"
[ -z "${STUB_FAIL:-}" ] || exit 97
if [ "$1 $2" = "image inspect" ]; then printf 'sha256:id-of-%s\n' "${@: -1}"; fi
exit 0
`,
	"kind": `#!/usr/bin/env bash
echo "kind $*" >> "$STUB_LOG"
[ -z "${STUB_FAIL:-}" ] || exit 97
exit 0
`,
	"kubectl": `#!/usr/bin/env bash
echo "kubectl $*" >> "$STUB_LOG"
[ -z "${STUB_FAIL:-}" ] || exit 97
exit 0
`,
	"helm": `#!/usr/bin/env bash
echo "helm $*" >> "$STUB_LOG"
[ -z "${STUB_FAIL:-}" ] || exit 97
printf '%s\n' "$@" > "$STUB_HELM_ARGS"
exit 0
`,
}

type devScriptRun struct {
	err      error
	stderr   string
	calls    []string
	helmArgs []string
}

// runDevScript copies deploy/dev and tools/versions.env into a new tree and runs the script there with the stubs first on PATH and
// no QUADER_ variable of the caller's environment but those given.
func runDevScript(t *testing.T, fail bool, env []string, script string, args ...string) devScriptRun {
	t.Helper()
	root := t.TempDir()
	entries, err := os.ReadDir(filepath.Join(repoRoot, "deploy", "dev"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "dev", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(root, "deploy", "dev", e.Name()), string(raw))
		if err := os.Chmod(filepath.Join(root, "deploy", "dev", e.Name()), info.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(root, "tools", "versions.env"), readRepoFile(t, "tools/versions.env"))
	stubs := t.TempDir()
	for name, body := range devImageStubs {
		if err := os.WriteFile(filepath.Join(stubs, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	log, helmArgs := filepath.Join(stubs, "calls.log"), filepath.Join(stubs, "helm.args")
	cmd := exec.Command(filepath.Join(root, "deploy", "dev", script), args...)
	cmd.Dir = root
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); name != "PATH" && !strings.HasPrefix(name, "QUADER_") && !strings.HasPrefix(name, "STUB_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "PATH="+stubs+string(os.PathListSeparator)+os.Getenv("PATH"), "QUADER_DEV_TAG=probe",
		"STUB_LOG="+log, "STUB_HELM_ARGS="+helmArgs)
	if fail {
		cmd.Env = append(cmd.Env, "STUB_FAIL=1")
	}
	cmd.Env = append(cmd.Env, env...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	r := devScriptRun{err: cmd.Run(), stderr: stderr.String()}
	if raw, err := os.ReadFile(log); err == nil {
		r.calls = strings.Split(strings.TrimSpace(string(raw)), "\n")
	}
	if raw, err := os.ReadFile(helmArgs); err == nil {
		r.helmArgs = strings.Split(strings.TrimSpace(string(raw)), "\n")
	}
	return r
}

// The image given as QUADER_IMAGE is the image the Bastion's chart runs: the one value of image.tag that reaches helm is its tag, and
// the pod annotation that rolls the pods carries its ID. The interface-mismatch scenario upgrades the Bastion this way.
func TestAProfileScriptRunsTheImageItsCallerGave(t *testing.T) {
	t.Parallel()
	r := runDevScript(t, false, []string{"QUADER_IMAGE=guidedtraffic/quader:iface"}, "bastion.sh", "install", "bucket")
	if r.err != nil {
		t.Fatalf("bastion.sh install bucket: %v\n%s", r.err, r.stderr)
	}
	pair := func(flag, value string) bool {
		for i := 0; i+1 < len(r.helmArgs); i++ {
			if r.helmArgs[i] == flag && r.helmArgs[i+1] == value {
				return true
			}
		}
		return false
	}
	for _, want := range [][2]string{
		{"--set", "image.registry=docker.io"},
		{"--set", "image.repository=guidedtraffic/quader"},
		{"--set-string", "image.tag=iface"},
		{"--set-string", "podAnnotations.quader-dev/image-id=sha256:id-of-guidedtraffic/quader:iface"},
	} {
		if !pair(want[0], want[1]) {
			t.Errorf("helm did not receive %s %s:\n%s", want[0], want[1], strings.Join(r.helmArgs, "\n"))
		}
	}
	tags := slices.DeleteFunc(slices.Clone(r.helmArgs), func(a string) bool { return !strings.Contains(a, "image.tag=") })
	if len(tags) != 1 {
		t.Errorf("helm received %d values of image.tag, want the one of QUADER_IMAGE: %q", len(tags), tags)
	}
}

// An image value among a script's extra helm arguments is refused before any tool runs, with the variable named and QUADER_IMAGE as the
// way to give another image: Helm would apply it before the script's own --set-string and drop it without a word. Other extra values
// pass, among them the pull policy of the image and the pod annotation the Bastion's long set rolls the pods with.
func TestAnImageAmongTheExtraHelmArgumentsIsRefused(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		script string
		args   []string
		name   string
		extra  string
	}{
		{"up.sh", nil, "QUADER_HELM_EXTRA", "--set image.tag=iface"},
		{"up.sh", nil, "QUADER_HELM_EXTRA", "--set config.x=1,image.repository=library/quader-x"},
		{"bastion.sh", []string{"install", "bucket"}, "QUADER_BASTION_HELM_EXTRA", "--set image.tag=iface"},
		{"bastion.sh", []string{"install", "bucket"}, "QUADER_BASTION_HELM_EXTRA", "--set-string=image.tag=iface"},
		{"restore-ns.sh", []string{"install", "b1"}, "QUADER_RESTORE_HELM_EXTRA", "--set-json image={\"tag\":\"iface\"}"},
	} {
		t.Run(c.script+" "+c.extra, func(t *testing.T) {
			t.Parallel()
			r := runDevScript(t, true, []string{c.name + "=" + c.extra}, c.script, c.args...)
			if r.err == nil {
				t.Fatalf("%s accepted %s=%q", c.script, c.name, c.extra)
			}
			if !strings.Contains(r.stderr, c.name) || !strings.Contains(r.stderr, "QUADER_IMAGE") {
				t.Errorf("%s did not refuse %s=%q naming the variable and QUADER_IMAGE:\n%s", c.script, c.name, c.extra, r.stderr)
			}
			if len(r.calls) != 0 {
				t.Errorf("%s ran tools before the refusal: %q", c.script, r.calls)
			}
		})
	}
	t.Run("bastion.sh other values", func(t *testing.T) {
		t.Parallel()
		extra := "--set-string podAnnotations.e2e-roll=r1 --set image.pullPolicy=Always"
		r := runDevScript(t, false, []string{"QUADER_BASTION_HELM_EXTRA=" + extra}, "bastion.sh", "install", "bucket")
		if r.err != nil {
			t.Fatalf("bastion.sh install bucket with %q: %v\n%s", extra, r.err, r.stderr)
		}
		if got := strings.Join(r.helmArgs, " "); !strings.HasSuffix(got, extra) {
			t.Errorf("helm did not receive the extra arguments last: %s", got)
		}
	})
}
