package ciworkflows

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type step struct {
	Name           string            `yaml:"name"`
	Uses           string            `yaml:"uses"`
	With           map[string]string `yaml:"with"`
	Run            string            `yaml:"run"`
	TimeoutMinutes yaml.Node         `yaml:"timeout-minutes"`
}

type job struct {
	Needs          yaml.Node         `yaml:"needs"`
	Permissions    map[string]string `yaml:"permissions"`
	TimeoutMinutes yaml.Node         `yaml:"timeout-minutes"`
	Steps          []step            `yaml:"steps"`
}

type workflow struct {
	Jobs map[string]job `yaml:"jobs"`
}

func loadWorkflows(t *testing.T) map[string]workflow {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(repoRoot, ".github", "workflows", "*.yml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflow files: %v", err)
	}
	out := map[string]workflow{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var w workflow
		if err := yaml.Unmarshal(raw, &w); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		out[filepath.Base(f)] = w
	}
	return out
}

// minutes is a timeout-minutes value; ok is false when it is absent or an expression.
func minutes(n yaml.Node) (int, bool) {
	if n.Kind != yaml.ScalarNode {
		return 0, false
	}
	v, err := strconv.Atoi(n.Value)
	return v, err == nil
}

// installsPackages: the step installs OS packages, through the script or the build-tools action that calls it.
func installsPackages(s step) bool {
	return strings.Contains(s.Run, "apt-install.sh") || strings.HasPrefix(s.Uses, "./.github/actions/build-tools")
}

// downloads names what a step fetches from the network outside the job's main make target, or "" when it fetches nothing of
// that kind: the package install, the actions that download a toolchain, go install, the ClamAV database and npm ci.
func downloads(s step) string {
	if installsPackages(s) {
		return "OS packages"
	}
	for _, a := range []string{"actions/setup-go@", "actions/setup-node@", "azure/setup-helm@", "azure/setup-kubectl@", "./.github/actions/install-buildx"} {
		if strings.HasPrefix(s.Uses, a) {
			return a
		}
	}
	for _, r := range []string{"go install ", "freshclam", "npm ci"} {
		if strings.Contains(s.Run, r) {
			return r
		}
	}
	return ""
}

// scriptWorstCase is the longest apt-install.sh can take, from its own bounds: two updates, two downloads and the unpacking,
// each followed by the KILL delay.
func scriptWorstCase(t *testing.T) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, ".github", "scripts", "apt-install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	value := func(name string) int {
		m := regexp.MustCompile(`(?m)^` + name + `=(\d+)$`).FindSubmatch(raw)
		if m == nil {
			t.Fatalf("apt-install.sh does not set %s", name)
		}
		v, _ := strconv.Atoi(string(m[1]))
		return v
	}
	k := value("kill_after")
	return 2*(value("update_seconds")+k) + 2*(value("download_seconds")+k) + value("unpack_seconds") + k
}

// No file under .github calls apt-get, apt or aptitude itself: only apt-install.sh does, so the next call site cannot bring
// back a package index that hangs a job.
func TestNothingUnderGithubRunsAptGetItself(t *testing.T) {
	bare := regexp.MustCompile(`\bapt-get\b|\baptitude\b|\bapt +(install|update|upgrade)\b`)
	helper := filepath.Join(repoRoot, ".github", "scripts", "apt-install.sh")
	err := filepath.WalkDir(filepath.Join(repoRoot, ".github"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == helper {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if bare.MatchString(line) {
				t.Errorf("%s:%d calls apt itself; install OS packages with .github/scripts/apt-install.sh: %s", path, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The script lives in the checkout, so every step that installs packages comes after actions/checkout in its job.
func TestEveryPackageInstallFollowsTheCheckout(t *testing.T) {
	for file, w := range loadWorkflows(t) {
		for name, j := range w.Jobs {
			checkedOut := false
			for _, s := range j.Steps {
				if strings.HasPrefix(s.Uses, "actions/checkout@") {
					checkedOut = true
				}
				if installsPackages(s) && !checkedOut {
					t.Errorf("%s job %s step %q installs packages before actions/checkout", file, name, s.Name)
				}
			}
		}
	}
}

// The jobs that gate Semantic Release bound every step that downloads, so a host that does not answer fails the step in
// minutes, visibly, instead of holding the job until its limit; a package step's bound lies above the script's own worst
// case (the script reports first) and below the job's limit.
func TestTheGatingJobsBoundTheirDownloads(t *testing.T) {
	w := loadWorkflows(t)["release.yml"]
	release, ok := w.Jobs["semantic-release"]
	if !ok {
		t.Fatal("release.yml has no job semantic-release")
	}
	var needs []string
	if err := release.Needs.Decode(&needs); err != nil || len(needs) == 0 {
		t.Fatalf("the needs of semantic-release: %v %v", needs, err)
	}
	worst := scriptWorstCase(t)
	for _, name := range append(slices.Clone(needs), "semantic-release") {
		j, ok := w.Jobs[name]
		if !ok {
			t.Errorf("semantic-release needs %s, which release.yml does not define", name)
			continue
		}
		jobLimit, jobLimitKnown := minutes(j.TimeoutMinutes)
		for _, s := range j.Steps {
			what := downloads(s)
			if what == "" {
				continue
			}
			where := fmt.Sprintf("job %s step %q (%s)", name, s.Name, what)
			limit, ok := minutes(s.TimeoutMinutes)
			if !ok {
				t.Errorf("%s has no timeout-minutes", where)
				continue
			}
			if installsPackages(s) && limit*60 <= worst {
				t.Errorf("%s: timeout-minutes %d is not above the %d s apt-install.sh can take", where, limit, worst)
			}
			if jobLimitKnown && limit >= jobLimit {
				t.Errorf("%s: timeout-minutes %d is not below the job's %d", where, limit, jobLimit)
			}
		}
	}
}
