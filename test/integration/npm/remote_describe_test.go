//go:build integration

package npm

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/npmstub/corpus"
)

// describedPackage is a plain package of the corpus with a deprecated version that has dependencies: the version a test caches, so
// that the description, the licence, the dependencies and the deprecation of its entry are all there to be shown.
func describedPackage(t testing.TB) (*corpus.Package, corpus.Version) {
	t.Helper()
	for _, p := range corpus.Default().Packages {
		if strings.HasPrefix(p.Name, "@") || len(p.Versions) < 2 || len(p.Versions) >= 30 {
			continue
		}
		for _, v := range p.Versions {
			if v.Deprecated != "" && len(v.Dependencies) > 0 {
				return p, v
			}
		}
	}
	t.Fatal("the corpus has no deprecated version with dependencies")
	return nil, corpus.Version{}
}

// TestR_NPM_050_ACachedVersionIsDescribedByItsEntryInTheStoredDocument: a version a remote repository cached has the row of its
// manifest a published one has, from its entry in the stored package document (R-NPM-050), and its README is read from the cached
// tarball (R-NPM-051). The package API answers the description, the licence, the dependencies and the deprecation of the entry, the
// README renders, and a version nobody fetched has no row.
func TestR_NPM_050_ACachedVersionIsDescribedByItsEntryInTheStoredDocument(t *testing.T) {
	r := newRemoteEnv(t, remoteOptions{Valkey: true})
	rep := r.start(1)[0]
	r.st.Start(t, testenv.ReplicaOptions{Name: "worker", Role: "worker"})
	p, v := describedPackage(t)

	doc := r.view(rep, rRemote, p.Name, r.tokA)
	if doc.Status != http.StatusOK {
		t.Fatalf("the package document: %d %.300s", doc.Status, doc.Body)
	}
	if a := r.install(rep, tarballsOf(t, doc)[v.Version], r.tokA); a.Status != http.StatusOK {
		t.Fatalf("the tarball: %d %.300s", a.Status, a.Body)
	}

	ops := r.st.Token(r.lead, scope.Default())
	look := r.st.Do(rep, http.MethodGet, "/api/v1/packages/lookup?format=npm&name="+url.QueryEscape(p.Name), ops, nil)
	if look.Status != http.StatusOK {
		t.Fatalf("lookup: %d %s", look.Status, look.Body)
	}
	id, _ := look.JSON(t)["id"].(string)
	detail := "/api/v1/packages/" + id + "/versions/" + url.PathEscape(v.Version)
	var got map[string]any
	testenv.WaitFor(t, "the row of the cached version", 30*time.Second, func() bool {
		a := r.st.Do(rep, http.MethodGet, detail, ops, nil)
		if a.Status != http.StatusOK {
			t.Fatalf("the version: %d %s", a.Status, a.Body)
		}
		got = a.JSON(t)
		l, _ := got["licenses"].([]any)
		return len(l) > 0
	})
	if l, _ := got["licenses"].([]any); len(l) != 1 || l[0] != "MIT" {
		t.Errorf("licenses %v, want [MIT]", got["licenses"])
	}
	// The package's description is its newest version's: the one the remote cached.
	if pkg := r.st.Do(rep, http.MethodGet, "/api/v1/packages/"+id, ops, nil).JSON(t); pkg["description"] != p.Description {
		t.Errorf("description %v, want the entry's %q", pkg["description"], p.Description)
	}
	md, _ := got["metadata"].(map[string]any)
	deps, _ := md["dependencies"].(map[string]any)
	if len(deps) != len(v.Dependencies) {
		t.Errorf("dependencies %v, want %v", md["dependencies"], v.Dependencies)
	}
	for name, rng := range v.Dependencies {
		if deps[name] != rng {
			t.Errorf("dependency %s is %v, want %q", name, deps[name], rng)
		}
	}
	if md["deprecated"] != v.Deprecated {
		t.Errorf("deprecated %v, want %q", md["deprecated"], v.Deprecated)
	}
	if got["hasReadme"] != true {
		t.Errorf("hasReadme %v, want true: the cached tarball holds README.md", got["hasReadme"])
	}
	readme := r.st.Do(rep, http.MethodGet, detail+"/readme", ops, nil)
	if readme.Status != http.StatusOK || !strings.Contains(string(readme.Body), "Version "+v.Version+" of a synthetic package.") {
		t.Errorf("the README: %d %.300s", readme.Status, readme.Body)
	}

	repoID := r.st.Number(`SELECT id FROM repositories WHERE key = $1`, rRemote)
	if n := r.st.Number(`SELECT count(*) FROM npm_versions nv JOIN package_versions pv ON pv.id = nv.version_id
		JOIN packages pk ON pk.id = pv.package_id WHERE pk.repo_id = $1`, repoID); n != 1 {
		t.Errorf("the remote holds %d rows of npm_versions, want the one of the cached version", n)
	}
	if n := r.st.Number(`SELECT count(*) FROM npm_packages np JOIN packages pk ON pk.id = np.package_id WHERE pk.repo_id = $1`, repoID); n != 0 {
		t.Errorf("the remote holds %d rows of npm_packages: a cache has no dist-tags of its own", n)
	}
}
