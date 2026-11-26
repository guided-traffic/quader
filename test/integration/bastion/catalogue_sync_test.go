//go:build integration

package bastion

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/catalogue"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// The public catalogue at the Bastion (R-BAS-109 to R-BAS-112; W-35's package P7) against the main installation of the suite with
// the role all, which builds the catalogue (P6): two Bastions, one on a bucket and one on a volume, mirror it; a publish reaches
// both within the builder's debounce and a poll; with the link cut they serve on with their last confirmation, a replica of the
// bucket restarted during the cut serves the stored state, one whose volume was removed answers not ready; a level lowered at the
// main installation is hidden at the Bastion soon after the builder's run. Two npm packages of bytes.

// listed reports whether the replica's catalogue lists a package of the name.
func listed(t *testing.T, r *replica, name string) bool {
	t.Helper()
	page, err := r.srv.Catalogue().Search(catalogue.Query{Q: name})
	if err != nil {
		return false
	}
	for _, e := range page.Items {
		if e.Name == name {
			return true
		}
	}
	return false
}

// TestR_BAS_109_TheBastionMirrorsTheCatalogueAndServesOnWhileTheLinkIsCut: the scenario of the package P7 in one world.
func TestR_BAS_109_TheBastionMirrorsTheCatalogueAndServesOnWhileTheLinkIsCut(t *testing.T) {
	w := newWorld(t, worldOptions{
		Role: "all", Egress: []string{"127.0.0.1/32"},
		Switches: func(string) iface.Switches { return iface.Switches{GUI: true, Docs: true} },
	})
	w.anonymousPolicy()
	// A poll of 2 s (the default is 10 s), so the case waits for the builder's debounce more than for the poll.
	gui := "  gui:\n    poll_interval: 2s\n"
	bucket := w.newBastion(t, bastionOptions{Backend: "bucket", CutsLink: true, Settings: gui}).wait()
	volume := w.newBastion(t, bastionOptions{CutsLink: true, Settings: gui}).wait()
	b, v := bucket.reps[0], volume.reps[0]

	// A publish reaches both within the builder's debounce (5 s), its run and a poll.
	start := time.Now()
	w.publishNPM("plain-lib", "1.0.0")
	testenv.WaitFor(t, "plain-lib at both Bastions", 30*time.Second, func() bool { return listed(t, b, "plain-lib") && listed(t, v, "plain-lib") })
	t.Logf("the publish reached both Bastions after %s", time.Since(start).Round(100*time.Millisecond))
	entry, ok, err := b.srv.Catalogue().Package(mustFind(t, b, "plain-lib"))
	if err != nil || !ok {
		t.Fatalf("the package: %v %v", ok, err)
	}
	if d, ok, err := b.srv.Catalogue().Document(context.Background(), iface.CataloguePath{Kind: iface.CatalogueVersion, Package: entry.ID, Version: "1.0.0"}.String()); err != nil || !ok || d.Size == 0 {
		t.Fatalf("the version's document: %v %v", ok, err)
	}
	if st := b.srv.Catalogue().State(); !st.Ready || !st.Kept || v.srv.Catalogue().State().Kept {
		t.Fatalf("the states: bucket %+v, volume %+v", st, v.srv.Catalogue().State())
	}

	// The link cut: both serve on with the time of their last confirmation.
	w.front.SetDown(true)
	failed := func(r *replica) bool {
		return sumMetric(r.metrics(t), metrics.CatalogueSyncTotal, `outcome="failed"`) > 0
	}
	testenv.WaitFor(t, "a failed round at both Bastions", 20*time.Second, func() bool { return failed(b) && failed(v) })
	confirmed := b.srv.Catalogue().ConfirmedAt()
	time.Sleep(2500 * time.Millisecond) // more than a poll
	if got := b.srv.Catalogue().ConfirmedAt(); !got.Equal(confirmed) || !listed(t, b, "plain-lib") || !listed(t, v, "plain-lib") {
		t.Fatalf("with the link cut: confirmedAt %s (was %s), listed %v %v", got, confirmed, listed(t, b, "plain-lib"), listed(t, v, "plain-lib"))
	}

	// A replica of the bucket restarted during the cut serves the stored state; one whose volume was removed is not ready.
	b.stop(t)
	nb := bucket.start(t, bucket.dir, bucket.o, 1)
	if st := nb.srv.Catalogue().State(); !st.Ready || st.ConfirmedAt.IsZero() || st.ConfirmedAt.After(confirmed) || !listed(t, nb, "plain-lib") {
		t.Fatalf("the bucket's replica restarted during the cut: %+v", st)
	}
	v.stop(t)
	if err := os.RemoveAll(volume.sh.volume); err != nil {
		t.Fatal(err)
	}
	nv := volume.start(t, volume.dir, volume.o, 1)
	if _, _, err := nv.srv.Catalogue().Document(context.Background(), "state"); !errors.Is(err, catalogue.ErrNotReady) {
		t.Fatalf("the volume's replica restarted without its volume: %v", err)
	}

	// The link back: the new replicas confirm; a level lowered to private is hidden soon after the builder's run.
	w.front.SetDown(false)
	testenv.WaitFor(t, "the restarted replicas confirmed", 30*time.Second, func() bool {
		return nb.srv.Catalogue().ConfirmedAt().After(confirmed) && nv.srv.Catalogue().State().Ready && listed(t, nv, "plain-lib")
	})
	start = time.Now()
	w.mustAPI("PUT", "/api/v1/projects/"+testenv.DefaultProject+"/visibility", 200, map[string]any{"level": "private"}, "If-Match", "*")
	testenv.WaitFor(t, "plain-lib hidden at both Bastions", 30*time.Second, func() bool { return !listed(t, nb, "plain-lib") && !listed(t, nv, "plain-lib") })
	t.Logf("the lowered level was hidden at both Bastions after %s", time.Since(start).Round(100*time.Millisecond))
}

// mustFind is the catalogue ID of a listed package.
func mustFind(t *testing.T, r *replica, name string) string {
	t.Helper()
	page, err := r.srv.Catalogue().Search(catalogue.Query{Q: name})
	if err != nil || len(page.Items) == 0 {
		t.Fatalf("%s is not listed: %v", name, err)
	}
	return page.Items[0].ID
}
