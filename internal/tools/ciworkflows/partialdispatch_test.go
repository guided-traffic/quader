package ciworkflows

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// selectTerm is the test of one name against the sets of a dispatch in a job's if.
func selectTerm(name string) string {
	return "contains(format(',{0},', inputs.sets), '," + name + ",')"
}

// A partial dispatch of the nightly starts only the jobs it names (G5-18 of the triage of 2026-11-23; Q-ARCH-597). The decision of a
// leg is a step (select-leg) after the checkout, so before it every job of the workflow took its runner, a large pod for a lane
// job, only to skip its steps. Now every job with that step, but image, which every leg needs, carries a job-level if that runs it
// when the dispatch names no sets, names its job key or one of the sets its legs take, or holds a space (then every leg fails
// with select-leg's error, as before). A job whose if leaves out one of its sets would never run on a dispatch of that set.
func TestAPartialDispatchStartsOnlyTheJobsOfItsSets(t *testing.T) {
	t.Parallel()
	w := loadWorkflows(t)["performance.yml"]
	matrixLeg := regexp.MustCompile(`^\$\{\{ matrix\.([A-Za-z0-9_-]+) \}\}$`)
	checked := 0
	for id, j := range w.Jobs {
		var sel *step
		for i := range j.Steps {
			if j.Steps[i].Uses == "./.github/actions/select-leg" {
				sel = &j.Steps[i]
			}
		}
		if sel == nil || id == "image" {
			continue
		}
		checked++
		if sel.With["job"] != id {
			t.Errorf("job %s asks select-leg with job %q; a dispatch names the job by its key", id, sel.With["job"])
		}
		sets := []string{sel.With["leg"]}
		if m := matrixLeg.FindStringSubmatch(sel.With["leg"]); m != nil {
			sets = matrixValues(j.Strategy, m[1])
		}
		if len(sets) == 0 {
			t.Errorf("job %s: no sets found for its legs", id)
			continue
		}
		for _, want := range []string{"!inputs.sets", "contains(inputs.sets, ' ')", selectTerm(id)} {
			if !strings.Contains(j.If, want) {
				t.Errorf("job %s runs if %q; it lacks %s", id, j.If, want)
			}
		}
		for _, s := range sets {
			if !strings.Contains(j.If, selectTerm(s)) {
				t.Errorf("job %s runs if %q; a dispatch of its set %s would not start it", id, j.If, s)
			}
		}
		named := regexp.MustCompile(`', inputs\.sets\), ',([A-Za-z0-9_-]+),'\)`).FindAllStringSubmatch(j.If, -1)
		for _, n := range named {
			if n[1] != id && !slices.Contains(sets, n[1]) {
				t.Errorf("job %s starts on a dispatch of %s, which none of its legs takes", id, n[1])
			}
		}
	}
	if checked < 20 {
		t.Errorf("%d jobs with select-leg checked; the nightly has more than twenty", checked)
	}
}
