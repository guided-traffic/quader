package catalogue_test

import (
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/catalogue"
	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// docTree is a documentation tree of the builder's shape (R-ANS-036's tree with the catalogue's searchWords, R-BAS-124): one module
// group of the entries, each type, name, short description and search words.
func docTree(t *testing.T, repository string, entries ...[4]string) []byte {
	t.Helper()
	list := []map[string]any{}
	for _, e := range entries {
		list = append(list, map[string]any{"type": e[0], "name": e[1], "shortDescription": e[2], "hasDocs": true, "searchWords": e[3]})
	}
	n := strconv.Itoa(len(list))
	b, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	return []byte(`{"state":"available","repository":"` + repository + `","groups":[{"category":"module","label":"Modules","count":` + n +
		`,"types":[{"type":"module","count":` + n + `,"entries":` + string(b) + `}]}],"files":[]}`)
}

// TestR_BAS_125_TheDocumentationIsSearchedAcrossThePackagesNewestVersions: the reader finds the entries of the newest version of every
// package whose tree the catalogue holds by the main installation's rule (every word of the text begins a word of the entry's search
// words), the name ranks them, the pages follow their cursor, an older version's entries are no hit, a repository whose wiki is not
// public and docs off find nothing, and DocEntries lists what the search could find, for the sitemap.
func TestR_BAS_125_TheDocumentationIsSearchedAcrossThePackagesNewestVersions(t *testing.T) {
	r := newRig(t)
	pub := public("acme/galaxy", "acme/npm-local")
	r.main.SetPublic(pub)
	tools := pkg{id: "pkg_T1", name: "acme.tools", repo: "acme/galaxy", versions: []string{"1.0.0", "2.0.0"}}
	r.publish(tools, leftPad)
	r.main.PutDocument(vpath("pkg_T1", "1.0.0", iface.CatalogueDocs), docTree(t, "acme/galaxy",
		[4]string{"module", "old_module", "Only in the old version", "module old only version"}))
	r.main.PutDocument(vpath("pkg_T1", "2.0.0", iface.CatalogueDocs), docTree(t, "acme/galaxy",
		[4]string{"module", "host_check", "Checks a host", "a checks host"},
		[4]string{"module", "host", "Is a host", "a host is"},
		[4]string{"module", "mount", "Mounts a disk on a host", "disk host mounts on src"},
	))
	r.on(true, true)
	r.tick()

	names := func(p catalogue.DocPage) []string {
		var out []string
		for _, h := range p.Items {
			out = append(out, h.Name)
		}
		return out
	}
	page, err := r.cat.SearchDocs(catalogue.DocQuery{Q: "host"})
	if err != nil || len(page.Items) != 3 || names(page)[0] != "host" || names(page)[1] != "host_check" || names(page)[2] != "mount" {
		t.Fatalf("host: %v %v", names(page), err)
	}
	h := page.Items[0]
	if h.Package != "acme.tools" || h.Version != "2.0.0" || h.Project != "acme" || h.Repository != "galaxy" || h.Format != "npm" || h.Doc() != "module/host" {
		t.Errorf("the hit: %+v", h)
	}
	if p, _ := r.cat.SearchDocs(catalogue.DocQuery{Q: "Mount  SR"}); len(p.Items) != 1 || p.Items[0].Name != "mount" {
		t.Errorf("every word a prefix: %v", names(p))
	}
	if p, _ := r.cat.SearchDocs(catalogue.DocQuery{Q: "ount"}); len(p.Items) != 0 {
		t.Errorf("a word inside a word is no hit: %v", names(p))
	}
	if p, _ := r.cat.SearchDocs(catalogue.DocQuery{Q: "old"}); len(p.Items) != 0 {
		t.Errorf("an older version's entry is a hit: %v", names(p))
	}
	first, _ := r.cat.SearchDocs(catalogue.DocQuery{Q: "host", Limit: 2})
	second, err := r.cat.SearchDocs(catalogue.DocQuery{Q: "host", Limit: 2, After: first.Next})
	if first.Next == "" || err != nil || len(second.Items) != 1 || second.Items[0].Name != "mount" || second.Next != "" {
		t.Errorf("the pages: %v %q, %v %v", names(first), first.Next, names(second), err)
	}
	if p, _ := r.cat.SearchDocs(catalogue.DocQuery{Q: "host", Project: "other"}); len(p.Items) != 0 {
		t.Errorf("another project: %v", names(p))
	}
	for _, q := range []string{"h", " \x00h "} {
		if _, err := r.cat.SearchDocs(catalogue.DocQuery{Q: q}); !errors.Is(err, catalogue.ErrQuery) {
			t.Errorf("%q: %v", q, err)
		}
	}
	if all, err := r.cat.DocEntries(); err != nil || len(all) != 3 {
		t.Errorf("the entries for the sitemap: %+v %v", all, err)
	}
	// A tree that changed changes the revision the sitemap is kept by (R-BAS-122), though the index stays.
	_, before, _ := r.cat.Packages()
	r.main.PutDocument(vpath("pkg_T1", "2.0.0", iface.CatalogueDocs), docTree(t, "acme/galaxy",
		[4]string{"module", "host", "Is a host", "a host is"}))
	r.tick()
	if _, after, _ := r.cat.Packages(); after == before {
		t.Error("a changed documentation tree keeps the sitemap's revision")
	}
	if all, _ := r.cat.DocEntries(); len(all) != 1 {
		t.Errorf("the entries after the tree changed: %+v", all)
	}
	r.main.PutDocument(vpath("pkg_T1", "2.0.0", iface.CatalogueDocs), docTree(t, "acme/galaxy",
		[4]string{"module", "host_check", "Checks a host", "a checks host"},
		[4]string{"module", "host", "Is a host", "a host is"},
		[4]string{"module", "mount", "Mounts a disk on a host", "disk host mounts on src"},
	))
	r.tick()

	// A wiki that is not public: no hit, no entry.
	pub.Repositories[0].Wiki = false
	r.main.SetPublic(pub)
	r.tick()
	if p, _ := r.cat.SearchDocs(catalogue.DocQuery{Q: "host"}); len(p.Items) != 0 {
		t.Errorf("a wiki that is not public: %v", names(p))
	}
	pub.Repositories[0].Wiki = true
	r.main.SetPublic(pub)
	r.tick()
	if p, _ := r.cat.SearchDocs(catalogue.DocQuery{Q: "host"}); len(p.Items) != 3 {
		t.Errorf("the wiki public again: %v", names(p))
	}

	// docs off: nothing, at once.
	r.on(true, false)
	if p, _ := r.cat.SearchDocs(catalogue.DocQuery{Q: "host"}); len(p.Items) != 0 {
		t.Errorf("docs off: %v", names(p))
	}
	if all, _ := r.cat.DocEntries(); len(all) != 0 {
		t.Errorf("docs off lists %d entries", len(all))
	}
}
