//go:build integration

package npm

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// The upstream catalogue of a remote repository (remote-curation.md R-CUR-008, R-CUR-009) against the integration stack and the counting
// npm upstream of the test process, whose search route answers the corpus's names that hold the text. One replica of the role all;
// lead holds manage on npm-remote, alice reads it.

const upstreamPath = downloadsPath + "/upstream"

// catalogueEnv is newRemoteEnv with lead as a Maintainer of npm-remote, the remote's delivery mode, the seeded rules and one replica.
func catalogueEnv(t *testing.T, o remoteOptions, rules ...testenv.Rule) (*renv, *testenv.Replica) {
	t.Helper()
	r := newRemoteEnv(t, o)
	r.st.Policy("npm-remote-managers", []string{"read", "manage"}, []string{rRemote}, r.lead)
	for _, rule := range rules {
		r.st.DeliveryRule(rRemote, rule)
	}
	return r, r.st.Start(t, testenv.ReplicaOptions{Name: "all-1", Role: "all", Endpoints: customersEndpoint})
}

func hitNamed(t testing.TB, page map[string]any, member, name string) map[string]any {
	t.Helper()
	list, _ := page[member].([]any)
	for _, h := range list {
		if m, _ := h.(map[string]any); m["name"] == name {
			return m
		}
	}
	return nil
}

// TestR_CUR_008_TheCatalogueReadsWithoutStoringAndAnswersFromItsCache: a search through the API reaches the upstream once for two
// identical calls; a package document no client fetched is read, answered and not stored. About 2 s, the stub's corpus.
func TestR_CUR_008_TheCatalogueReadsWithoutStoringAndAnswersFromItsCache(t *testing.T) {
	r, rep := catalogueEnv(t, remoteOptions{Valkey: true})
	p := upstreamPackage(t, 3)
	search := upstreamPath + "/search?q=" + url.QueryEscape(p.Name)
	for i := range 2 {
		a := r.st.Do(rep, http.MethodGet, search, r.tokL, nil)
		if a.Status != http.StatusOK || hitNamed(t, a.JSON(t), "hits", p.Name) == nil {
			t.Fatalf("search %d: %d %s", i+1, a.Status, a.Body)
		}
	}
	if n := r.stub.Counts().Requests(http.MethodGet, "/-/v1/search"); n != 1 {
		t.Errorf("two identical searches reached the upstream %d times, want 1", n)
	}
	for range 2 {
		a := r.st.Do(rep, http.MethodGet, upstreamPath+"/package?name="+url.QueryEscape(p.Name), r.tokA, nil)
		if a.Status != http.StatusOK {
			t.Fatalf("the lookup: %d %s", a.Status, a.Body)
		}
		if vs, _ := a.JSON(t)["versions"].([]any); len(vs) != len(p.Versions) {
			t.Fatalf("the lookup answers %d versions, the upstream has %d", len(vs), len(p.Versions))
		}
	}
	if n := r.upstreamDocs(p.Name); n != 1 {
		t.Errorf("two lookups read the document %d times, want 1", n)
	}
	if n := r.st.Number(`SELECT count(*) FROM remote_cache_entries e JOIN repositories r ON r.id = e.repo_id WHERE r.key = $1`, rRemote); n != 0 {
		t.Errorf("the catalogue stored %d cache entries", n)
	}
	if n := r.st.Number(`SELECT count(*) FROM items i JOIN repositories r ON r.id = i.repo_id WHERE r.key = $1 AND i.kind = 'file'`, rRemote); n != 0 {
		t.Errorf("the catalogue stored %d items", n)
	}
	if a := r.st.Do(rep, http.MethodGet, upstreamPath+"/package?name=no-such-package-anywhere", r.tokA, nil); a.Status != http.StatusNotFound {
		t.Errorf("a package the upstream does not have: %d %s", a.Status, a.Body)
	}
	if a := r.st.Do(rep, http.MethodGet, upstreamPath+"/levels", r.tokA, nil); a.Status != http.StatusBadRequest {
		t.Errorf("the levels of a registry without scopes: %d %s", a.Status, a.Body)
	}
}

// TestR_CUR_009_TheRowsCarryTheRepositorysStateAndAReaderSeesWhatIsDelivered: in the mode onlyAllowed, a reader's search answers the
// delivered package with its stored state and withholds the one no rule allows, with the count and its sentence; the Maintainer sees
// both; any reader looks up a package that is not allowed and sees its versions; the capabilities say who may set rules; the verdicts of
// versions not stored say the format's coverage. About 3 s, the stub's corpus and one tarball.
func TestR_CUR_009_TheRowsCarryTheRepositorysStateAndAReaderSeesWhatIsDelivered(t *testing.T) {
	p := upstreamPackage(t, 3)
	q := otherPackage(t, 2, p.Name)
	r, rep := catalogueEnv(t, remoteOptions{Valkey: true, Remote: map[string]any{"delivery": testenv.OnlyAllowed()}},
		testenv.PackageRule(p.Name, "allow"))
	stored := p.Versions[0].Version
	if a := r.tarball(rep, rRemote, p.Name, stored, r.tokA); a.Status != http.StatusOK {
		t.Fatalf("alice's fetch of %s@%s: %d %s", p.Name, stored, a.Status, a.Body)
	}

	t.Run("R-CUR-009 the reader sees the delivered hit with its stored state", func(t *testing.T) {
		a := r.st.Do(rep, http.MethodGet, upstreamPath+"/search?q="+url.QueryEscape(p.Name), r.tokA, nil)
		h := hitNamed(t, a.JSON(t), "hits", p.Name)
		if a.Status != http.StatusOK || h == nil || h["stored"] != "partial" || h["delivery"].(map[string]any)["delivered"] != true {
			t.Fatalf("%d %s", a.Status, a.Body)
		}
	})
	t.Run("R-CUR-009 the reader is not answered a hit no rule allows, and its count says so", func(t *testing.T) {
		a := r.st.Do(rep, http.MethodGet, upstreamPath+"/search?q="+url.QueryEscape(q.Name), r.tokA, nil)
		page := a.JSON(t)
		if a.Status != http.StatusOK || hitNamed(t, page, "hits", q.Name) != nil || page["withheld"].(float64) < 1 || page["withheldNote"] == nil {
			t.Fatalf("%d %s", a.Status, a.Body)
		}
		a = r.st.Do(rep, http.MethodGet, upstreamPath+"/search?q="+url.QueryEscape(q.Name), r.tokL, nil)
		h := hitNamed(t, a.JSON(t), "hits", q.Name)
		if h == nil || h["delivery"].(map[string]any)["reason"] != "notAllowed" || a.JSON(t)["withheld"] != nil {
			t.Fatalf("the Maintainer's search: %d %s", a.Status, a.Body)
		}
	})
	t.Run("R-CUR-009 R-CUR-015 an ask is checked by the catalogue, and the Maintainer sees the open request on the hit", func(t *testing.T) {
		reqs := downloadsPath + "/delivery-requests"
		if a := r.st.Do(rep, http.MethodPost, reqs, r.tokA, map[string]any{"name": "no-such-package-anywhere"}); a.Status != http.StatusNotFound {
			t.Fatalf("an ask for a name the upstream does not have: %d %s, want 404", a.Status, a.Body)
		}
		if a := r.st.Do(rep, http.MethodPost, reqs, r.tokA, map[string]any{"name": q.Name}); a.Status != http.StatusCreated {
			t.Fatalf("alice's ask for %s: %d %s", q.Name, a.Status, a.Body)
		}
		a := r.st.Do(rep, http.MethodGet, upstreamPath+"/search?q="+url.QueryEscape(q.Name), r.tokL, nil)
		h := hitNamed(t, a.JSON(t), "hits", q.Name)
		if req, _ := h["request"].(map[string]any); req == nil || req["askers"] != float64(1) {
			t.Fatalf("the hit's request: %s", a.Body)
		}
		if m := r.st.Do(rep, http.MethodGet, upstreamPath, r.tokL, nil).JSON(t); m["openRequests"] != float64(1) {
			t.Fatalf("the open requests: %v", m)
		}
	})
	t.Run("R-CUR-009 any reader looks a package up by its exact name and sees its versions", func(t *testing.T) {
		a := r.st.Do(rep, http.MethodGet, upstreamPath+"/package?name="+url.QueryEscape(q.Name), r.tokA, nil)
		if vs, _ := a.JSON(t)["versions"].([]any); a.Status != http.StatusOK || len(vs) != len(q.Versions) {
			t.Fatalf("%d %s", a.Status, a.Body)
		}
		a = r.st.Do(rep, http.MethodGet, upstreamPath+"/package?name="+url.QueryEscape(p.Name)+"&filter="+url.QueryEscape(stored), r.tokA, nil)
		vs, _ := a.JSON(t)["versions"].([]any)
		found := false
		for _, v := range vs {
			if m := v.(map[string]any); m["version"] == stored {
				found = m["stored"] == "whole"
			}
		}
		if !found {
			t.Fatalf("the stored version is not marked whole: %s", a.Body)
		}
	})
	t.Run("R-CUR-009 the capabilities say the mode and who may set rules", func(t *testing.T) {
		a := r.st.Do(rep, http.MethodGet, upstreamPath, r.tokA, nil)
		m := a.JSON(t)
		if a.Status != http.StatusOK || m["search"] != "upstream" || m["mode"] != "onlyAllowed" || m["maySetRules"] != false || m["openRequests"] != nil {
			t.Fatalf("%d %s", a.Status, a.Body)
		}
		if m := r.st.Do(rep, http.MethodGet, upstreamPath, r.tokL, nil).JSON(t); m["maySetRules"] != true {
			t.Fatalf("the Maintainer's capabilities: %v", m)
		}
	})
	t.Run("R-CUR-009 the verdicts of versions not stored write nothing and say the coverage", func(t *testing.T) {
		before := r.st.Number(`SELECT count(*) FROM vuln_finding`)
		a := r.st.Do(rep, http.MethodPost, upstreamPath+"/vulnerabilities", r.tokA,
			map[string]any{"name": p.Name, "versions": []string{stored, p.Versions[1].Version}})
		m := a.JSON(t)
		if a.Status != http.StatusOK || len(m["versions"].([]any)) != 2 {
			t.Fatalf("%d %s", a.Status, a.Body)
		}
		if v := m["versions"].([]any)[0].(map[string]any); v["verdict"] != "stored" {
			t.Errorf("the stored version: %v", v)
		}
		if n := r.st.Number(`SELECT count(*) FROM vuln_finding`); n != before {
			t.Errorf("the verdicts wrote %d findings", n-before)
		}
	})
	t.Run("R-CUR-005 a caller without read is not answered", func(t *testing.T) {
		if a := r.st.Do(rep, http.MethodGet, upstreamPath, "", nil); a.Status != http.StatusUnauthorized {
			t.Fatalf("anonymous: %d", a.Status)
		}
	})
}

// TestR_CUR_009_TheCatalogueAnswersOnAReplicaOfTheRoleAPI: the role api has the engine of the client path and no format host; the
// catalogue's lent upstream took a second engine there, whose caches and metrics were registered twice, and every catalogue call of the
// role panicked (Q-CUR-163, found by the journey of the tab Upstream on the kind cluster, whose management API is the role api). One
// replica of the role api, a lookup and a search. About 1 s, the stub's corpus.
func TestR_CUR_009_TheCatalogueAnswersOnAReplicaOfTheRoleAPI(t *testing.T) {
	r := newRemoteEnv(t, remoteOptions{Valkey: true})
	r.st.Policy("npm-remote-managers", []string{"read", "manage"}, []string{rRemote}, r.lead)
	rep := r.st.Start(t, testenv.ReplicaOptions{Name: "api-1", Role: "api"})
	p := upstreamPackage(t, 2)
	for _, path := range []string{"/package?name=" + url.QueryEscape(p.Name), "/search?q=" + url.QueryEscape(p.Name)} {
		if a := r.st.Do(rep, http.MethodGet, upstreamPath+path, r.tokL, nil); a.Status != http.StatusOK {
			t.Fatalf("%s on the role api: %d %.300s", path, a.Status, a.Body)
		}
	}
}
