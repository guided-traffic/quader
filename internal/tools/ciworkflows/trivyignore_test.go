package ciworkflows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

// TestAcceptedFindingsAreScopedAndEnd holds the form of .trivyignore.yaml: an accepted finding of the container scan names one
// identifier, the paths in the image it is accepted at, the day it ends and why it does not apply. A file-wide entry or one
// without an end would switch a finding off for the whole image for good; the scan itself fails again on the day an entry ends.
func TestAcceptedFindingsAreScopedAndEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".trivyignore.yaml"))
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
		t.Fatal(err)
	}
	for kind, entries := range file {
		for _, e := range entries {
			name := kind + " " + e.ID
			if strings.TrimSpace(e.ID) == "" {
				t.Errorf("%s: an entry without an identifier", kind)
			}
			if len(e.Paths) == 0 {
				t.Errorf("%s: no paths: the finding would be accepted in the whole image", name)
			}
			for _, p := range e.Paths {
				if p == "" || strings.ContainsAny(p, "*?") {
					t.Errorf("%s: path %q is empty or a pattern; name the file", name, p)
				}
			}
			if _, err := time.Parse("2006-01-02", e.ExpiredAt); err != nil {
				t.Errorf("%s: expired_at %q is not a day (YYYY-MM-DD): the entry would never end", name, e.ExpiredAt)
			}
			if len(strings.Fields(e.Statement)) < 8 {
				t.Errorf("%s: the statement does not say why the finding does not apply", name)
			}
		}
	}
}

// TestBothScanStepsReadTheAcceptedFindings: the table scan fails the job and the SARIF scan writes the report; a report that
// lists what the gate accepted would send its reader after a finding that is decided.
func TestBothScanStepsReadTheAcceptedFindings(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	steps := strings.Count(text, "uses: aquasecurity/trivy-action@")
	reads := strings.Count(text, "TRIVY_IGNOREFILE: .trivyignore.yaml")
	if steps == 0 || steps != reads {
		t.Fatalf("%d steps run the scan action, %d of them read .trivyignore.yaml", steps, reads)
	}
}
