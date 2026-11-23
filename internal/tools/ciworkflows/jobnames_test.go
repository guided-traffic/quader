package ciworkflows

import (
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// requirementCode is a requirement, decision or exit criterion code in either spelling, also after a prefix such as Fuzz: R-ARCH-028,
// FuzzR_NUGET_005, Q-ARCH-457, (E0.1).
var requirementCode = regexp.MustCompile(`(?:^|[^A-Z0-9])[RQ][-_][A-Z]{2,}[-_][0-9]+|\(E[0-9]+(\.[0-9]+)?\)`)

// codeIn is the first code in s, without the character before it, or "".
func codeIn(s string) string {
	m := requirementCode.FindString(s)
	if i := strings.IndexAny(m, "RQ("); i > 0 {
		return m[i:]
	}
	return m
}

// matrixRef is a matrix value a job's name takes in: ${{ matrix.<key> }}.
var matrixRef = regexp.MustCompile(`\$\{\{\s*matrix\.([A-Za-z0-9_-]+)\s*\}\}`)

// matrixValues returns every scalar value the matrix gives key, from its list and from its include entries.
func matrixValues(strategy yaml.Node, key string) []string {
	var s struct {
		Matrix map[string]yaml.Node `yaml:"matrix"`
	}
	if strategy.Decode(&s) != nil {
		return nil
	}
	var out []string
	if n, ok := s.Matrix[key]; ok && n.Kind == yaml.SequenceNode {
		for _, v := range n.Content {
			out = append(out, v.Value)
		}
	}
	if n, ok := s.Matrix["include"]; ok && n.Kind == yaml.SequenceNode {
		for _, entry := range n.Content {
			var m map[string]any
			if entry.Decode(&m) == nil {
				if v, ok := m[key]; ok {
					if str, ok := v.(string); ok {
						out = append(out, str)
					}
				}
			}
		}
	}
	return out
}

// Job names say what they do, without requirement codes (owner, 2026-11-10; CLAUDE.md "Stage: CI and releases"): a job's name, and
// every matrix value it takes in, carries none. The fuzz legs of the closing run were named after targets such as
// FuzzR_NUGET_005_Nuspec, so their job and artifact names carried codes (G3-24 of the triage of 2026-11-23).
func TestNoJobNameCarriesARequirementCode(t *testing.T) {
	t.Parallel()
	for file, w := range loadWorkflows(t) {
		for id, j := range w.Jobs {
			if m := codeIn(j.Name); m != "" {
				t.Errorf("%s job %s is named %q, with the code %s", file, id, j.Name, m)
			}
			for _, ref := range matrixRef.FindAllStringSubmatch(j.Name, -1) {
				for _, v := range matrixValues(j.Strategy, ref[1]) {
					if m := codeIn(v); m != "" {
						t.Errorf("%s job %s is named %q, and its matrix value %q carries the code %s", file, id, j.Name, v, m)
					}
				}
			}
			for _, s := range j.Steps {
				if m := codeIn(s.Name); m != "" && !strings.Contains(s.Name, "${{") {
					t.Errorf("%s job %s step %q carries the code %s", file, id, s.Name, m)
				}
			}
		}
	}
}
