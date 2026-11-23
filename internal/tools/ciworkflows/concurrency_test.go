package ciworkflows

import (
	"testing"

	"go.yaml.in/yaml/v3"
)

// mainRunGroup is the concurrency group of Test and Release (Q-ARCH-594): the run's own id on main, so no run of main shares a group
// with another and none is cancelled or replaced; the ref elsewhere, so a pull request still cancels its own earlier run.
const mainRunGroup = "${{ github.workflow }}-${{ github.ref == 'refs/heads/main' && github.run_id || github.ref }}"

// A push to main no longer cancels the run of the push before it (Q-ARCH-594). With one group per ref and cancel-in-progress, a
// push cancelled the running run of main, and the integration shards, e2e legs and layout checks it had not finished were lost.
// With one group per ref and cancel-in-progress false, GitHub would keep one waiting run and replace it with the next, so of three
// quick pushes the middle one would never run. The run's own group avoids both. A concurrency group on a job would bring the
// replacement back for that job: GitHub cancels a waiting job of a group when a newer one queues, and the newer one is not
// always the newer commit (a Semantic Release of the tip could be cancelled for an older run's, which publishes nothing).
func TestARunOfMainIsNeverCancelledByAnother(t *testing.T) {
	w := loadWorkflows(t)["release.yml"]
	var c struct {
		Group            string `yaml:"group"`
		CancelInProgress string `yaml:"cancel-in-progress"`
	}
	if w.Concurrency.Kind != yaml.MappingNode {
		t.Fatalf("release.yml has no concurrency mapping at the top; want group %q", mainRunGroup)
	}
	if err := w.Concurrency.Decode(&c); err != nil {
		t.Fatal(err)
	}
	if c.Group != mainRunGroup {
		t.Errorf("release.yml concurrency group is %q; want %q: on main the run's own id, so no run of main is cancelled or "+
			"replaced by another, elsewhere the ref", c.Group, mainRunGroup)
	}
	if c.CancelInProgress != "true" {
		t.Errorf("release.yml cancel-in-progress is %q; want true, so a pull request cancels its own earlier run (a run of main "+
			"is alone in its group and has nothing to cancel)", c.CancelInProgress)
	}
	for name, j := range w.Jobs {
		if !j.Concurrency.IsZero() {
			t.Errorf("job %s of release.yml has a concurrency group of its own; a group shared by runs of main lets GitHub cancel "+
				"one run's waiting job for another's (Q-ARCH-594)", name)
		}
	}
}
