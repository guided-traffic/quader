package ciworkflows

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Every leg of the nightly on the large runners records the jobs of Test and Release that ran beside it (Q-ARCH-1062): a push may
// come at any time, so the leg writes down who shared its runners, before its first upload and whatever became of its test, into a
// file one of its artifacts carries.
func TestEveryLargeLegOfTheNightlyRecordsItsNeighbours(t *testing.T) {
	t.Parallel()
	w := loadWorkflows(t)["performance.yml"]
	legs := 0
	for name, j := range w.Jobs {
		large := j.RunsOn == "large"
		if m := regexp.MustCompile(`^\$\{\{ matrix\.([A-Za-z0-9_-]+) \}\}$`).FindStringSubmatch(j.RunsOn); m != nil {
			large = slices.Contains(matrixValues(j.Strategy, m[1]), "large")
		}
		if !large || name == "client-images" {
			continue
		}
		legs++
		where := "performance.yml job " + name
		record, upload := -1, -1
		for i, s := range j.Steps {
			if record < 0 && strings.Contains(s.Run, "go run ./tools/neighbours") {
				record = i
			}
			if upload < 0 && strings.HasPrefix(s.Uses, "actions/upload-artifact@") {
				upload = i
			}
		}
		if record < 0 {
			t.Errorf("%s runs on the large runners and does not record its neighbours (go run ./tools/neighbours)", where)
			continue
		}
		s := j.Steps[record]
		if upload >= 0 && record > upload {
			t.Errorf("%s records its neighbours after its first upload", where)
		}
		if !strings.HasPrefix(s.If, "always()") {
			t.Errorf("%s: the record runs if %q; it must run whatever became of the test (always())", where, s.If)
		}
		if s.Env["GITHUB_TOKEN"] != "${{ github.token }}" {
			t.Errorf("%s: the record has no token for the Actions API", where)
		}
		if limit, ok := minutes(s.TimeoutMinutes); !ok || limit > 5 {
			t.Errorf("%s: the record is not bounded at a few minutes", where)
		}
		out := regexp.MustCompile(`-out (\S+)`).FindStringSubmatch(s.Run)
		if out == nil {
			t.Errorf("%s: the record is written to no file", where)
			continue
		}
		carried := false
		for _, u := range j.Steps[record:] {
			if !strings.HasPrefix(u.Uses, "actions/upload-artifact@") {
				continue
			}
			for _, p := range strings.Fields(u.With["path"]) {
				if p == out[1] || (strings.HasSuffix(p, "/") && strings.HasPrefix(out[1], p)) {
					carried = true
				}
			}
		}
		if !carried {
			t.Errorf("%s: no artifact of the job carries the record %s", where, out[1])
		}
	}
	if legs < 10 {
		t.Fatalf("only %d large legs found in performance.yml; the test reads the workflow wrongly", legs)
	}
}
