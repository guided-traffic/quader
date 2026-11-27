package catalogue_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/catalogue"
	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// The reader of the project view (R-BAS-130, R-BAS-131): the indexes of a project and of a repository the builder keeps beside the
// catalogue's own, against the rig of catalogue_test.go. Pages of a few hundred bytes; each test runs in milliseconds.

// scopedPages keeps the indexes of the project acme and of its two repositories: left-pad's 1.0.0 is in npm-local, the virtual
// npm-all over it holds 2.0.0 besides, so each scope's latest version and count are its own. The repository's index also names a
// package the catalogue's own index does not hold, which no search may list.
func (r *rig) scopedPages() {
	r.t.Helper()
	summary := func(id, name, latest string, count int) map[string]any {
		return map[string]any{
			"id": id, "format": "npm", "name": name, "key": name, "latestVersion": latest, "versionCount": count,
			"repositories": []string{"acme/npm-local"}, "updatedAt": "2026-11-0" + latest[:1] + "T00:00:00Z",
		}
	}
	page := func(items ...map[string]any) []byte {
		if items == nil {
			items = []map[string]any{}
		}
		return mustJSON(r.t, map[string]any{"items": items})
	}
	r.main.PutDocument("projects/acme/index/0", page(summary("pkg_A1", "left-pad", "2.0.0", 2)))
	r.main.PutDocument("repositories/acme/npm-local/index/0", page(summary("pkg_A1", "left-pad", "1.0.0", 1), summary("pkg_GHOST", "ghost", "1.0.0", 1)))
	r.main.PutDocument("repositories/acme/npm-all/index/0", page(summary("pkg_A1", "left-pad", "2.0.0", 2)))
}

// latest is the latest version of each item of a search, by name.
func latest(t *testing.T, p catalogue.Page) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, e := range p.Items {
		if !json.Valid(e.Summary) {
			t.Fatalf("the summary of %s is no JSON: %s", e.Name, e.Summary)
		}
		out[e.Name] = e.LatestVersion
	}
	return out
}

func (r *rig) search(q catalogue.Query) catalogue.Page {
	r.t.Helper()
	p, err := r.cat.Search(q)
	if err != nil {
		r.t.Fatalf("%+v: %v", q, err)
	}
	return p
}

// TestR_BAS_130_TheSearchReadsTheNarrowestIndex: a search of exactly one repository reads that repository's index, whose latest
// version and count are its own (also for a virtual repository whose summaries name only the local one under it); a search of a
// project reads the project's; one without either reads the catalogue's own. A scope's entry the catalogue's own index does not hold
// is no public package and is not listed. The indexes are served as documents while their scope is public.
func TestR_BAS_130_TheSearchReadsTheNarrowestIndex(t *testing.T) {
	r := newRig(t)
	r.main.SetPublic(public("acme/npm-local", "acme/npm-all"))
	r.publish(leftPad)
	r.scopedPages()
	r.on(true, true)
	r.tick()

	for _, c := range []struct {
		q    catalogue.Query
		want string
	}{
		{catalogue.Query{}, "1.0.0"},
		{catalogue.Query{Repositories: []string{"acme/npm-local"}}, "1.0.0"},
		{catalogue.Query{Repositories: []string{"acme/npm-all"}}, "2.0.0"},
		{catalogue.Query{Project: "acme", Repositories: []string{"acme/npm-all"}}, "2.0.0"},
		{catalogue.Query{Project: "acme"}, "2.0.0"},
		// Two repositories: the project's index, filtered by them.
		{catalogue.Query{Project: "acme", Repositories: []string{"acme/npm-local", "acme/npm-all"}}, "2.0.0"},
	} {
		got := latest(t, r.search(c.q))
		if len(got) != 1 || got["left-pad"] != c.want {
			t.Errorf("%+v lists %v, want left-pad at %s", c.q, got, c.want)
		}
	}
	if got := r.search(catalogue.Query{Project: "other", Repositories: []string{"acme/npm-local"}}); len(got.Items) != 0 {
		t.Errorf("a repository of another project lists %v", got.Items)
	}
	if got := r.search(catalogue.Query{Repositories: []string{"acme/npm-local"}, Q: "ghost"}); len(got.Items) != 0 {
		t.Errorf("a package the catalogue does not show is listed from a repository's index: %v", got.Items)
	}
	for _, path := range []string{"projects/acme/index/0", "repositories/acme/npm-local/index/0"} {
		if _, ok := r.doc(path); !ok {
			t.Errorf("%s is not served", path)
		}
	}
	// A repository that leaves the public set takes its index with it; the project's search keeps the rest.
	r.main.SetPublic(public("acme/npm-local"))
	r.tick()
	if _, ok := r.doc("repositories/acme/npm-all/index/0"); ok {
		t.Error("the index of a repository outside the public set is served")
	}
	if got := latest(t, r.search(catalogue.Query{Repositories: []string{"acme/npm-local"}})); got["left-pad"] != "1.0.0" {
		t.Errorf("the repository's search after the other left: %v", got)
	}
}

// TestR_BAS_131_TheReaderHidesTheRepositoryIndexes: the switch hideRepositories hides every repository's index at once, before a
// round: its document is not served, a search of one repository reads the project's index filtered by it, the state says the switch,
// and the sitemap's revision moves. The project's index stays. A restarted replica that applies no configuration yet keeps the stored
// switch; off again shows the indexes at once.
func TestR_BAS_131_TheReaderHidesTheRepositoryIndexes(t *testing.T) {
	r := newRig(t)
	r.main.SetPublic(public("acme/npm-local", "acme/npm-all"))
	r.publish(leftPad)
	r.scopedPages()
	r.on(true, true)
	r.tick()
	_, shown, err := r.cat.Packages()
	if err != nil {
		t.Fatal(err)
	}

	r.cat.SetSwitches(iface.Switches{GUI: true, Docs: true, HideRepositories: true}, true)
	if !r.cat.State().HideRepositories {
		t.Error("the state does not say the switch")
	}
	if _, ok := r.doc("repositories/acme/npm-local/index/0"); ok {
		t.Error("a repository's index is served while the repositories are hidden")
	}
	if _, ok := r.doc("projects/acme/index/0"); !ok {
		t.Error("the project's index is hidden with the repositories'")
	}
	if got := latest(t, r.search(catalogue.Query{Repositories: []string{"acme/npm-local"}})); got["left-pad"] != "2.0.0" {
		t.Errorf("hidden, a repository's search does not read the project's index: %v", got)
	}
	if _, hidden, _ := r.cat.Packages(); hidden == shown {
		t.Error("the sitemap's revision stays when the switch changes")
	}

	r.tick()
	r.restart(filepath.Join(r.dir, "volume"))
	if !r.cat.Load(context.Background()) {
		t.Fatal("the stored state is not loaded")
	}
	if !r.cat.State().HideRepositories {
		t.Error("a restarted replica without its configuration forgets the stored switch")
	}
	if _, ok := r.doc("repositories/acme/npm-local/index/0"); ok {
		t.Error("a restarted replica serves a hidden repository's index")
	}
	r.on(true, true)
	if body, ok := r.doc("repositories/acme/npm-local/index/0"); !ok || !strings.Contains(body, `"latestVersion":"1.0.0"`) {
		t.Errorf("off again, the repository's index = %q %v", body, ok)
	}
}
