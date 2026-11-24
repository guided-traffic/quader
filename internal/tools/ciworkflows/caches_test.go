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
	"release.yml/image":             "make image builds in Docker; the job's Go runs only `go env` for the Makefile",
	"release.yml/container-scan":    "make image builds in Docker; the job's Go runs only `go env` for the Makefile",
	"release.yml/workflows":         "builds actionlint, a tool outside go.sum, through go-tool",
	"release.yml/malware-scan":      "ClamAV over the tree; the job's Go runs only `go env` for the Makefile",
	"release.yml/release-tooling":   "npm and Node.js; the job's Go runs only `go env` for the Makefile",
	"performance.yml/k8s-matrix":    "builds kind only, a tool outside go.sum, through go-tool",
	"performance.yml/image":         "make image builds in Docker; the job's Go runs only `go env` for the Makefile",
	"release.yml/client-images":     "builds the forwarder for the key, which imports the standard library alone; the images build in Docker",
	"performance.yml/client-images": "builds the forwarder for the key, which imports the standard library alone; the images build in Docker",
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

// runnerKinds are the cache kinds of a job's runners: "ubuntu-24.04" for the GitHub-hosted runners, "self-hosted" for both
// self-hosted classes (the same runner image). Both put the pinned zstd on PATH (Q-ARCH-1061), so an entry is shared; a kind whose
// zstd step failed falls back to gzip and an entry of its own, which is why the rows stay per kind. A runs-on that is a matrix
// value gives the kinds of every value.
func runnerKinds(j job) []string {
	labels := []string{j.RunsOn}
	if m := regexp.MustCompile(`^\$\{\{ matrix\.([A-Za-z0-9_-]+) \}\}$`).FindStringSubmatch(j.RunsOn); m != nil {
		labels = matrixValues(j.Strategy, m[1])
	}
	var out []string
	for _, l := range labels {
		kind := "self-hosted"
		if strings.HasPrefix(l, "ubuntu-") {
			kind = "ubuntu-24.04"
		}
		if !slices.Contains(out, kind) {
			out = append(out, kind)
		}
	}
	return out
}

// needsOf is the list of a job's needs.
func needsOf(j job) []string {
	var needs []string
	if err := j.Needs.Decode(&needs); err != nil {
		var one string
		if err := j.Needs.Decode(&one); err == nil {
			needs = []string{one}
		}
	}
	return needs
}

// builderRows are the rows of a workflow's job client-images, as "set runner": the literal matrix of the nightly, or for Test
// and Release the rows e2e-scope can emit (each row's JSON in the script of its step "The clients images the legs need").
func builderRows(t *testing.T, w workflow) []string {
	t.Helper()
	b, ok := w.Jobs["client-images"]
	if !ok {
		return nil
	}
	var rows []string
	sets, runners := matrixValues(b.Strategy, "images"), matrixValues(b.Strategy, "runner")
	if len(sets) > 0 && len(sets) == len(runners) {
		for i := range sets {
			rows = append(rows, sets[i]+" "+runners[i])
		}
		return rows
	}
	scope := w.Jobs["e2e-scope"]
	i := stepIndex(scope, func(s step) bool { return s.ID == "images" })
	if i < 0 {
		t.Fatal("the job client-images has no literal matrix and e2e-scope no step images")
	}
	for _, m := range regexp.MustCompile(`\{"images":"([a-z]+)","runner":"([a-z0-9.-]+)"\}`).FindAllStringSubmatch(scope.Steps[i].Run, -1) {
		rows = append(rows, m[1]+" "+m[2])
	}
	return rows
}

// Every kind leg whose sets start client pods loads each clients image through .github/actions/client-images, before it runs
// the set, and tells deploy/dev/e2e.sh not to build the image again (F-158: the build inside a leg installed packages from the
// Ubuntu archive in every run and failed with it). It builds none itself (Q-ARCH-638): its steps leave the input build at its
// default, it needs the job client-images of its workflow, and that job has a row for each of its sets on its runner kind, so
// a run builds each set once per kind and the legs wait for that build instead of making their own beside it.
func TestEveryLegThatUsesAClientsImageGetsItFromTheCache(t *testing.T) {
	t.Parallel()
	clients := clientsOfSets(t)
	legs := 0
	for file, w := range loadWorkflows(t) {
		rows := builderRows(t, w)
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
			if len(need) > 0 && !slices.Contains(needsOf(j), "client-images") {
				t.Errorf("%s job %s loads clients images but does not need the job client-images", file, name)
			}
			for _, c := range need {
				legs++
				where := file + " job " + name + " (" + c + " clients)"
				at := stepIndex(j, func(s step) bool { return isClientImages(s) && s.With["set"] == c })
				if at < 0 {
					t.Errorf("%s: no .github/actions/client-images step with set %s", where, c)
				} else if at > run {
					t.Errorf("%s: the clients image step comes after the step that runs the set", where)
				} else if b := j.Steps[at].With["build"]; b != "" && b != "false" {
					t.Errorf("%s: the leg's step passes build %q; only the job client-images builds", where, b)
				}
				switchName := "QUADER_E2E_" + strings.ToUpper(c) + "_CLIENTS_IMAGE_BUILD"
				if j.Env[switchName] != "0" {
					t.Errorf("%s: env %s is %q; it must be \"0\" so that deploy/dev/e2e.sh does not build the image again", where, switchName, j.Env[switchName])
				}
				for _, kind := range runnerKinds(j) {
					if !slices.Contains(rows, c+" "+kind) {
						t.Errorf("%s: the job client-images has no row %s on %s (rows %v)", where, c, kind, rows)
					}
				}
			}
		}
	}
	if legs == 0 {
		t.Fatal("no leg uses a clients image; the test reads the workflows wrongly")
	}
}

// The job client-images is the one place that builds a clients image (Q-ARCH-638): its step passes build 'true', it runs on
// the runner of its row, it sets up Buildx with the docker driver (the build then takes the base images client-images.sh pulled
// within their bounds), and no other job passes build at all. Test and Release takes its rows from e2e-scope, with every row as
// the fallback when that job gave none.
func TestOnlyTheJobClientImagesBuildsAClientsImage(t *testing.T) {
	t.Parallel()
	builders := 0
	for file, w := range loadWorkflows(t) {
		for name, j := range w.Jobs {
			for _, s := range j.Steps {
				if !isClientImages(s) {
					continue
				}
				if b := s.With["build"]; b != "" && b != "false" && name != "client-images" {
					t.Errorf("%s job %s step %q builds clients images; only the job client-images does", file, name, s.Name)
				}
			}
		}
		b, ok := w.Jobs["client-images"]
		if !ok {
			continue
		}
		builders++
		where := file + " job client-images"
		at := stepIndex(b, func(s step) bool {
			return isClientImages(s) && s.With["build"] == "true" && s.With["set"] == "${{ matrix.images }}"
		})
		if at < 0 {
			t.Errorf("%s has no client-images step with build 'true' and the set of its row", where)
		}
		if b.RunsOn != "${{ matrix.runner }}" {
			t.Errorf("%s runs on %q, not the runner of its row", where, b.RunsOn)
		}
		docker := stepIndex(b, func(s step) bool {
			return strings.HasPrefix(s.Uses, "docker/setup-buildx-action@") && s.With["driver"] == "docker"
		})
		if docker < 0 || (at >= 0 && docker > at) {
			t.Errorf("%s does not set up Buildx with the docker driver before it builds", where)
		}
		if m, ok := minutes(b.Steps[max(at, 0)].TimeoutMinutes); !ok || m > 20 {
			t.Errorf("%s: the build step's limit is %d minutes; a healthy build and one stalled fetch's retries fit in 20", where, m)
		}
		rows := builderRows(t, w)
		if len(rows) == 0 {
			t.Errorf("%s has no rows", where)
		}
		if file == "release.yml" {
			fallback := regexp.MustCompile(`\{"images":"([a-z]+)","runner":"([a-z0-9.-]+)"\}`).FindAllStringSubmatch(b.Strategy.Content[len(b.Strategy.Content)-1].Content[1].Value, -1)
			var all []string
			for _, m := range fallback {
				all = append(all, m[1]+" "+m[2])
			}
			slices.Sort(all)
			slices.Sort(rows)
			if !slices.Equal(all, rows) {
				t.Errorf("%s: the fallback rows %v differ from the rows e2e-scope can give %v", where, all, rows)
			}
			if !slices.Contains(needsOf(b), "e2e-scope") {
				t.Errorf("%s does not need e2e-scope, whose rows it builds", where)
			}
		}
	}
	if builders != 2 {
		t.Errorf("%d workflows have a job client-images, want Test and Release and the nightly", builders)
	}
}

// A leg's step only restores and loads, so its limit is what a restore of the largest tar and a docker load take (the NuGet
// tar, 70 to 200 s to restore on the self-hosted runners, about 30 s to load), not the 30 minutes of a build.
func TestALegsClientsImageStepIsBoundedForALoad(t *testing.T) {
	t.Parallel()
	for file, w := range loadWorkflows(t) {
		for name, j := range w.Jobs {
			if name == "client-images" {
				continue
			}
			for _, s := range j.Steps {
				if !isClientImages(s) {
					continue
				}
				if m, ok := minutes(s.TimeoutMinutes); !ok || m > 10 {
					t.Errorf("%s job %s step %q: limit %v minutes; a load needs at most 10", file, name, s.Name, s.TimeoutMinutes.Value)
				}
				if strings.Contains(s.Name, "build") {
					t.Errorf("%s job %s step %q: a leg builds nothing, its step's name says so", file, name, s.Name)
				}
			}
		}
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
