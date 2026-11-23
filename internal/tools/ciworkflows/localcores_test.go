package ciworkflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// runsCappedTarget: a step that runs a target of the Makefile or the integration runner, both of which cap the cores unless
// LOCAL_CORES is empty.
var runsCappedTarget = regexp.MustCompile(`(^|[\s;&|(])(make|test/integration/(run|locked)\.sh)\s`)

// The cap of the cores of a local gate (LOCAL_CORES, default 6) is lifted in the pipeline: every workflow whose jobs run make or the
// integration runner sets LOCAL_CORES empty for them, so a runner keeps its own GOMAXPROCS and the lint its own concurrency
// (docs/developer/build-and-test.md, "The cap of the cores"). Milliseconds.
func TestEveryJobThatRunsACappedTargetLiftsTheCap(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join(repoRoot, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var w struct {
			Env  map[string]*string `yaml:"env"`
			Jobs map[string]struct {
				Env   map[string]*string `yaml:"env"`
				Steps []struct {
					Name string             `yaml:"name"`
					Run  string             `yaml:"run"`
					Env  map[string]*string `yaml:"env"`
				} `yaml:"steps"`
			} `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(raw, &w); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		lifted := func(envs ...map[string]*string) (bool, bool) {
			for i := len(envs) - 1; i >= 0; i-- { // the innermost env wins
				if v, ok := envs[i]["LOCAL_CORES"]; ok {
					return true, v == nil || *v == ""
				}
			}
			return false, false
		}
		for id, j := range w.Jobs {
			for _, s := range j.Steps {
				if !runsCappedTarget.MatchString(s.Run) {
					continue
				}
				if set, empty := lifted(w.Env, j.Env, s.Env); !set || !empty {
					t.Errorf("%s, job %s, step %q runs a capped target without LOCAL_CORES set empty (set %v)", filepath.Base(f), id, s.Name, set)
				}
			}
		}
	}
}

// make cores prints what a local gate gets: the cap of 6 by default, nothing with LOCAL_CORES empty, and a GOMAXPROCS and a -p the
// caller set stay. Seconds (make parses the Makefile, which asks go for its environment).
func TestTheMakefileCapsTheCoresUnlessLifted(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not on PATH")
	}
	cores := func(env ...string) string {
		t.Helper()
		cmd := exec.Command("make", "-s", "-f", "Makefile", "cores")
		cmd.Dir = repoRoot
		for _, kv := range os.Environ() {
			name, _, _ := strings.Cut(kv, "=")
			switch name {
			case "MAKEFLAGS", "MFLAGS", "MAKELEVEL", "LOCAL_CORES", "GOMAXPROCS", "GOFLAGS":
			default:
				cmd.Env = append(cmd.Env, kv)
			}
		}
		cmd.Env = append(cmd.Env, env...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("make cores %v: %v\n%s", env, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if got, want := cores(), "GOMAXPROCS=6 GOFLAGS=-p=6 lint=--concurrency 6"; got != want {
		t.Errorf("default: %q, want %q", got, want)
	}
	if got, want := cores("LOCAL_CORES="), "GOMAXPROCS= GOFLAGS= lint="; got != want {
		t.Errorf("lifted: %q, want %q", got, want)
	}
	if got, want := cores("GOMAXPROCS=1", "GOFLAGS=-p=2"), "GOMAXPROCS=1 GOFLAGS=-p=2 lint=--concurrency 6"; got != want {
		t.Errorf("the caller's own: %q, want %q", got, want)
	}
}
