package ciworkflows

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

// The ignore file of each scan: the container scan of the built image gates the release; the scan of the images the chart runs
// and Quader does not build reports and gates nothing (Q-ARCH-596).
var scanIgnoreFiles = map[string]string{
	"container-scan":      ".trivyignore.yaml",
	"upstream-image-scan": ".trivyignore-upstream.yaml",
}

// TestAcceptedFindingsAreScopedAndEnd holds the form of both ignore files: an accepted finding names one identifier, the paths in
// the image it is accepted at, the day it ends and why it does not apply. A file-wide entry or one without an end would switch a
// finding off for the whole image for good; the container scan itself fails again on the day an entry ends.
func TestAcceptedFindingsAreScopedAndEnd(t *testing.T) {
	for _, name := range []string{".trivyignore.yaml", ".trivyignore-upstream.yaml"} {
		raw, err := os.ReadFile(filepath.Join(repoRoot, name))
		if err != nil {
			t.Fatal(err)
		}
		var file map[string][]struct {
			ID        string   `yaml:"id"`
			Paths     []string `yaml:"paths"`
			ExpiredAt string   `yaml:"expired_at"`
			Statement string   `yaml:"statement"`
		}
		if err := yaml.Unmarshal(raw, &file); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for kind, entries := range file {
			for _, e := range entries {
				what := name + " " + kind + " " + e.ID
				if strings.TrimSpace(e.ID) == "" {
					t.Errorf("%s %s: an entry without an identifier", name, kind)
				}
				if len(e.Paths) == 0 {
					t.Errorf("%s: no paths: the finding would be accepted in the whole image", what)
				}
				for _, p := range e.Paths {
					if p == "" || strings.ContainsAny(p, "*?") {
						t.Errorf("%s: path %q is empty or a pattern; name the file", what, p)
					}
				}
				if _, err := time.Parse("2006-01-02", e.ExpiredAt); err != nil {
					t.Errorf("%s: expired_at %q is not a day (YYYY-MM-DD): the entry would never end", what, e.ExpiredAt)
				}
				if len(strings.Fields(e.Statement)) < 8 {
					t.Errorf("%s: the statement does not say why the finding does not apply", what)
				}
			}
		}
	}
}

// TestBothScanStepsReadTheAcceptedFindings: every step of release.yml that runs the scan action reads the ignore file of its job,
// and only the two scan jobs run it. In the container scan the table scan fails the job and the SARIF scan writes the report; a
// report that lists what the gate accepted would send its reader after a finding that is decided.
func TestBothScanStepsReadTheAcceptedFindings(t *testing.T) {
	w := loadWorkflows(t)["release.yml"]
	for name, j := range w.Jobs {
		want, scanJob := scanIgnoreFiles[name]
		steps := 0
		for _, s := range j.Steps {
			if !strings.HasPrefix(s.Uses, "aquasecurity/trivy-action@") {
				continue
			}
			steps++
			if !scanJob {
				t.Errorf("job %s step %q runs the scan action; only %v do", name, s.Name, slices.Sorted(maps.Keys(scanIgnoreFiles)))
				continue
			}
			if got := s.Env["TRIVY_IGNOREFILE"]; got != want {
				t.Errorf("job %s step %q reads TRIVY_IGNOREFILE %q, want %q", name, s.Name, got, want)
			}
		}
		if scanJob && steps == 0 {
			t.Errorf("job %s runs no scan", name)
		}
	}
	for name := range scanIgnoreFiles {
		if _, ok := w.Jobs[name]; !ok {
			t.Errorf("release.yml has no job %s", name)
		}
	}
}

// pinnedImage is an image line of a chart's values: a reference by digest.
var pinnedImage = regexp.MustCompile(`(?m)^\s*image:\s*([^\s#]+@sha256:[a-f0-9]{64})`)

// Every image a chart runs by digest is scanned (H-1003, H-1109, Q-ARCH-596): each such value of the charts' values.yaml is a pin of
// tools/versions.env, and the job upstream-image-scan scans that pin by name. The job holds no release: Semantic Release does not
// need it, and no scan of it fails on a finding (exit-code 0), because a finding in an upstream build is fixed by its publisher and
// a raise of the pin, not by a change of Quader. A third-party image added to a chart without its scan fails here.
func TestTheImagesTheChartRunsAreScannedAndHoldNoRelease(t *testing.T) {
	w := loadWorkflows(t)["release.yml"]
	j, ok := w.Jobs["upstream-image-scan"]
	if !ok {
		t.Fatal("release.yml has no job upstream-image-scan")
	}
	release := w.Jobs["semantic-release"]
	var needs []string
	if err := release.Needs.Decode(&needs); err != nil {
		t.Fatalf("the needs of semantic-release: %v", err)
	}
	if slices.Contains(needs, "upstream-image-scan") {
		t.Error("semantic-release needs upstream-image-scan; a finding in an image Quader does not build must not hold a release")
	}
	if _, ok := minutes(j.TimeoutMinutes); !ok {
		t.Error("job upstream-image-scan has no timeout-minutes")
	}
	scanned := map[string]bool{}
	envRef := regexp.MustCompile(`^\$\{\{ env\.([A-Z0-9_]+) \}\}$`)
	for _, s := range j.Steps {
		if !strings.HasPrefix(s.Uses, "aquasecurity/trivy-action@") {
			continue
		}
		if s.With["exit-code"] != "0" {
			t.Errorf("step %q fails on a finding (exit-code %q); the scan reports and gates nothing", s.Name, s.With["exit-code"])
		}
		if s.With["skip-setup-trivy"] != "true" {
			t.Errorf("step %q installs its own Trivy; the scans run the pinned analyser of make trivy", s.Name)
		}
		if _, ok := minutes(s.TimeoutMinutes); !ok {
			t.Errorf("step %q has no timeout-minutes", s.Name)
		}
		m := envRef.FindStringSubmatch(s.With["image-ref"])
		if m == nil {
			t.Errorf("step %q scans %q; scan a pin of tools/versions.env as ${{ env.<PIN> }}", s.Name, s.With["image-ref"])
			continue
		}
		scanned[m[1]] = true
	}

	pins := map[string]string{}
	for _, line := range strings.Split(readRepoFile(t, filepath.Join("tools", "versions.env")), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok && !strings.HasPrefix(k, "#") {
			pins[v] = k
		}
	}
	seen := 0
	for _, values := range []string{"deploy/helm/quader/values.yaml", "deploy/helm/quader-bastion/values.yaml"} {
		for _, m := range pinnedImage.FindAllStringSubmatch(readRepoFile(t, values), -1) {
			seen++
			pin, ok := pins[m[1]]
			if !ok {
				t.Errorf("%s runs %s, which is no pin of tools/versions.env", values, m[1])
				continue
			}
			if !scanned[pin] {
				t.Errorf("%s runs %s (%s), which job upstream-image-scan does not scan", values, pin, m[1])
			}
		}
	}
	if seen == 0 {
		t.Error("no image by digest found in the charts' values; the test reads nothing")
	}
}
