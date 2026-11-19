package ciworkflows

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// The rules of F-156 and F-158 (docs/developer/releasing.md#what-the-jobs-fetch-and-what-is-cached): what does not change
// between runs comes from the GitHub Actions cache or from the job image of the run, and a download that is left is bounded.

// goModulesExempt are the jobs that set up Go without building anything of go.sum, with the reason: they skip the module
// cache, which would cost a restore of the whole download cache for nothing.
var goModulesExempt = map[string]string{
	"release.yml/image":           "make image builds in Docker; the job's Go runs only `go env` for the Makefile",
	"release.yml/container-scan":  "make image builds in Docker; the job's Go runs only `go env` for the Makefile",
	"release.yml/workflows":       "builds actionlint, a tool outside go.sum, through go-tool",
	"release.yml/malware-scan":    "ClamAV over the tree; the job's Go runs only `go env` for the Makefile",
	"release.yml/release-tooling": "npm and Node.js; the job's Go runs only `go env` for the Makefile",
	"performance.yml/k8s-matrix":  "builds kind only, a tool outside go.sum, through go-tool",
	"performance.yml/image":       "make image builds in Docker; the job's Go runs only `go env` for the Makefile",
}

// stepIndex is the index of the first step of the job that match accepts, or -1.
func stepIndex(j job, match func(step) bool) int {
	return slices.IndexFunc(j.Steps, match)
}

func isSetupGo(s step) bool      { return strings.HasPrefix(s.Uses, "actions/setup-go@") }
func isGoModules(s step) bool    { return s.Uses == "./.github/actions/go-modules" }
func isClientImages(s step) bool { return s.Uses == "./.github/actions/client-images" }

// Every job that sets up Go turns the cache of actions/setup-go off (it held what the first job to finish needed, F-156) and,
// unless it builds nothing of go.sum, restores and completes the module cache in the step right after, under the same
// condition, before any step that runs make or go.
func TestEveryGoJobRestoresTheCompleteModuleCache(t *testing.T) {
	t.Parallel()
	for file, w := range loadWorkflows(t) {
		for name, j := range w.Jobs {
			at := stepIndex(j, isSetupGo)
			if at < 0 {
				continue
			}
			where := file + " job " + name
			for _, s := range j.Steps {
				if isSetupGo(s) && s.With["cache"] != "false" {
					t.Errorf("%s step %q: actions/setup-go caches the modules of the first job only; set cache: false and use .github/actions/go-modules", where, s.Name)
				}
			}
			if reason, exempt := goModulesExempt[file+"/"+name]; exempt {
				if stepIndex(j, isGoModules) >= 0 {
					t.Errorf("%s is exempt from the module cache (%s) but uses it; drop one of the two", where, reason)
				}
				continue
			}
			if at+1 >= len(j.Steps) || !isGoModules(j.Steps[at+1]) {
				t.Errorf("%s: the step after %q is not .github/actions/go-modules", where, j.Steps[at].Name)
				continue
			}
			if j.Steps[at+1].If != j.Steps[at].If {
				t.Errorf("%s: the module step runs if %q, the Go setup if %q", where, j.Steps[at+1].If, j.Steps[at].If)
			}
			for i, s := range j.Steps[:at+1] {
				if runsGoOrMake(s.Run) {
					t.Errorf("%s step %d %q runs make or go before the module cache is complete", where, i, s.Name)
				}
			}
		}
	}
}

// The exemptions name real jobs, so a renamed job does not keep a stale exemption that hides nothing.
func TestTheModuleCacheExemptionsNameJobsThatSetUpGo(t *testing.T) {
	t.Parallel()
	w := loadWorkflows(t)
	for key, reason := range goModulesExempt {
		file, name, _ := strings.Cut(key, "/")
		j, ok := w[file].Jobs[name]
		if !ok {
			t.Errorf("exemption %s names no job", key)
			continue
		}
		if stepIndex(j, isSetupGo) < 0 {
			t.Errorf("exemption %s (%s): the job sets up no Go", key, reason)
		}
	}
}

// runsGoOrMake says whether a step's script runs make or the go command: as a command of its own on a line, or after &&, || or ;.
func runsGoOrMake(run string) bool {
	return regexp.MustCompile(`(?m)(^\s*|&&\s*|\|\|\s*|;\s*)(make|go) [a-z]`).MatchString(run)
}

// No step installs a Go tool with go install itself: .github/actions/go-tool restores it from the cache or builds it within a
// bound, so a job does not ask the module proxy for a tool whose pin did not change (Q-ARCH-525). The make targets that build a
// tool into .bin find the binary go-tool put there.
func TestEveryGoToolComesFromTheToolCache(t *testing.T) {
	t.Parallel()
	needs := map[string]string{"make lint": "golangci-lint", "make gosec": "golangci-lint", "make vuln": "govulncheck", "make cyclo": "gocyclo"}
	for file, w := range loadWorkflows(t) {
		for name, j := range w.Jobs {
			for i, s := range j.Steps {
				if strings.Contains(s.Run, "go install ") {
					t.Errorf("%s job %s step %q runs go install; use .github/actions/go-tool", file, name, s.Name)
				}
				for target, tool := range needs {
					if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `\b`).MatchString(s.Run) {
						continue
					}
					ok := slices.ContainsFunc(j.Steps[:i], func(p step) bool {
						return p.Uses == "./.github/actions/go-tool" && p.With["name"] == tool
					})
					if !ok {
						t.Errorf("%s job %s step %q runs %s without a go-tool step for %s before it", file, name, s.Name, target, tool)
					}
				}
			}
		}
	}
}

// clientsOfSets reads deploy/dev/e2e.sh: for each e2e set, the clients image sets of .github/scripts/client-images.sh its
// client pods use (NPM_CLIENTS=1 and so on in the set's line of the case statement).
func clientsOfSets(t *testing.T) map[string][]string {
	t.Helper()
	f, err := os.Open(filepath.Join(repoRoot, "deploy", "dev", "e2e.sh"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	line := regexp.MustCompile(`^  ([a-z0-9|-]+)\) (.*;;)\s*$`)
	vars := []struct{ name, set string }{{"NPM_CLIENTS", "npm"}, {"NUGET_CLIENTS", "nuget"}, {"OCI_CLIENTS", "oci"}, {"APT_CLIENTS", "apt"}, {"ANSIBLE_CLIENTS", "ansible"}}
	out := map[string][]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		m := line.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		var need []string
		for _, v := range vars {
			if regexp.MustCompile(`\b` + v.name + `=1\b`).MatchString(m[2]) {
				need = append(need, v.set)
			}
		}
		for _, s := range strings.Split(m[1], "|") {
			out[s] = need
		}
	}
	if len(out["nuget"]) == 0 || len(out["ci"]) != 2 {
		t.Fatalf("the case statement of deploy/dev/e2e.sh did not parse: nuget %v, ci %v", out["nuget"], out["ci"])
	}
	return out
}

// setsOfJob names the e2e sets a job runs: the set of each matrix row (a literal list or the JSON of a fromJSON expression) and
// any literal `make e2e SET=<set>`.
func setsOfJob(j job) []string {
	var text strings.Builder
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n == nil {
			return
		}
		text.WriteString(n.Value + "\n")
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(&j.Strategy)
	for _, s := range j.Steps {
		text.WriteString(s.Run + "\n")
	}
	found := map[string]bool{}
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`"set":"([a-z0-9-]+)"`),
		regexp.MustCompile(`make e2e SET=([a-z0-9-]+)`),
	} {
		for _, m := range re.FindAllStringSubmatch(text.String(), -1) {
			found[m[1]] = true
		}
	}
	// A plain matrix: the value after a key "set".
	var pairs func(n *yaml.Node)
	pairs = func(n *yaml.Node) {
		if n == nil {
			return
		}
		for i := 0; i+1 < len(n.Content); i++ {
			if n.Kind == yaml.MappingNode && i%2 == 0 && n.Content[i].Value == "set" {
				found[n.Content[i+1].Value] = true
			}
		}
		for _, c := range n.Content {
			pairs(c)
		}
	}
	pairs(&j.Strategy)
	out := make([]string, 0, len(found))
	for s := range found {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// Every kind leg whose sets start client pods gets each clients image through .github/actions/client-images, before it runs
// the set, and tells deploy/dev/e2e.sh not to build the image again (F-158: the build inside a leg installed packages from the
// Ubuntu archive in every run and failed with it).
func TestEveryLegThatUsesAClientsImageGetsItFromTheCache(t *testing.T) {
	t.Parallel()
	clients := clientsOfSets(t)
	legs := 0
	for file, w := range loadWorkflows(t) {
		for name, j := range w.Jobs {
			run := stepIndex(j, func(s step) bool { return strings.Contains(s.Run, "make e2e SET=") })
			if run < 0 {
				continue
			}
			var need []string
			for _, set := range setsOfJob(j) {
				for _, c := range clients[set] {
					if !slices.Contains(need, c) {
						need = append(need, c)
					}
				}
			}
			for _, c := range need {
				legs++
				where := file + " job " + name + " (" + c + " clients)"
				at := stepIndex(j, func(s step) bool { return isClientImages(s) && s.With["set"] == c })
				if at < 0 {
					t.Errorf("%s: no .github/actions/client-images step with set %s", where, c)
				} else if at > run {
					t.Errorf("%s: the clients image step comes after the step that runs the set", where)
				}
				switchName := "QUADER_E2E_" + strings.ToUpper(c) + "_CLIENTS_IMAGE_BUILD"
				if j.Env[switchName] != "0" {
					t.Errorf("%s: env %s is %q; it must be \"0\" so that deploy/dev/e2e.sh does not build the image again", where, switchName, j.Env[switchName])
				}
			}
		}
	}
	if legs == 0 {
		t.Fatal("no leg uses a clients image; the test reads the workflows wrongly")
	}
}

// No workflow step builds a clients image itself or runs a bare docker build: the build of a clients image goes through
// .github/scripts/client-images.sh, which only builds when the cache has no image of the same inputs.
func TestNoJobBuildsAClientsImageItself(t *testing.T) {
	t.Parallel()
	bare := regexp.MustCompile(`make [a-z]+-clients-image|test/e2e/[a-z]+/build\.sh|docker (buildx )?build\b`)
	for file, w := range loadWorkflows(t) {
		for name, j := range w.Jobs {
			for _, s := range j.Steps {
				if m := bare.FindString(s.Run); m != "" {
					t.Errorf("%s job %s step %q runs %q; build clients images through .github/actions/client-images", file, name, s.Name, m)
				}
			}
		}
	}
}

// The kind legs deploy the Quader image the job `image` of their run built (Q-ARCH-522): they need that job, load its artifact
// and build no image of the Containerfile themselves. The upgrade leg's second image (make image-marked) is not that image.
func TestTheKindLegsLoadTheQuaderImageOfTheRun(t *testing.T) {
	t.Parallel()
	for file, w := range loadWorkflows(t) {
		img, hasImageJob := w.Jobs["image"]
		uploads := hasImageJob && slices.ContainsFunc(img.Steps, func(s step) bool {
			return strings.HasPrefix(s.Uses, "actions/upload-artifact@") && s.With["name"] == "quader-image"
		})
		for name, j := range w.Jobs {
			if stepIndex(j, func(s step) bool { return strings.Contains(s.Run, "make dev-up") }) < 0 {
				continue
			}
			where := file + " job " + name
			for _, s := range j.Steps {
				if regexp.MustCompile(`(?m)\bmake image( |$)`).MatchString(s.Run) {
					t.Errorf("%s step %q builds the Quader image; load the one of the run with .github/actions/quader-image", where, s.Name)
				}
			}
			if stepIndex(j, func(s step) bool { return s.Uses == "./.github/actions/quader-image" }) < 0 {
				t.Errorf("%s runs make dev-up without loading the Quader image of the run", where)
				continue
			}
			var needs []string
			if err := j.Needs.Decode(&needs); err != nil {
				var one string
				if err := j.Needs.Decode(&one); err == nil {
					needs = []string{one}
				}
			}
			if !slices.Contains(needs, "image") {
				t.Errorf("%s loads the Quader image of the run but does not need the job image (needs %v)", where, needs)
			}
			if !uploads {
				t.Errorf("%s: the job image uploads no artifact quader-image for %s", file, name)
			}
		}
	}
}
