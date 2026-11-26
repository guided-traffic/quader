//go:build integration

package npm

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_CUR_028_AnUpstreamPackageShowsItsReadmeReadThroughAndNotStored: the lookup of a package no client fetched answers the facts of
// its latest version and that its README may be asked for; the README call answers the package document's README rendered, at no
// further request of the upstream (the lookup handed it over), and stores nothing. A reader of the artifacts who may not read the wiki
// gets the facts, readme false and the README call's 404, and the upstream is not asked for it. Each on a replica of the role api,
// which serves the management API in the chart and has no format host, and on one of the role all (R-CUR-028, R-ARCH-001). About 3 s,
// the stub's corpus.
func TestR_CUR_028_AnUpstreamPackageShowsItsReadmeReadThroughAndNotStored(t *testing.T) {
	r := newRemoteEnv(t, remoteOptions{Valkey: true})
	// dave reads the remote's artifacts by its level internal and has no grant: the wiki is private. The levels are set before the
	// replicas start, so that their grant index holds them.
	r.st.Exec("UPDATE project SET visibility = 'internal' WHERE key = $1", testenv.DefaultProject)
	r.st.Exec("UPDATE repositories SET visibility_artifacts = 'internal', visibility_wiki = 'private' WHERE key = $1", rRemote)
	dave := r.st.Token(r.st.User("dave"), scope.Default())
	p := upstreamPackage(t, 2)
	other := otherPackage(t, 2, p.Name)
	for _, o := range []testenv.ReplicaOptions{{Name: "api-a", Role: "api"}, {Name: "all-1", Role: "all", Endpoints: customersEndpoint}} {
		rep := r.st.Start(t, o)
		t.Run("R-CUR-028 on the role "+o.Role, func(t *testing.T) {
			a := r.st.Do(rep, http.MethodGet, upstreamPath+"/package?name="+url.QueryEscape(p.Name), r.tokA, nil)
			m := a.JSON(t)
			about, _ := m["about"].(map[string]any)
			if a.Status != http.StatusOK || about == nil || m["readme"] != true {
				t.Fatalf("the lookup: %d %.400s", a.Status, a.Body)
			}
			version, _ := about["version"].(string)
			if version == "" {
				t.Fatalf("the facts name no version: %v", about)
			}
			docs := r.upstreamDocs(p.Name)
			a = r.st.Do(rep, http.MethodGet, upstreamPath+"/readme?name="+url.QueryEscape(p.Name)+"&version="+url.QueryEscape(version), r.tokA, nil)
			rd := a.JSON(t)
			if a.Status != http.StatusOK || rd["version"] != version || (rd["state"] != "available" && rd["state"] != "none") {
				t.Fatalf("the README: %d %.400s", a.Status, a.Body)
			}
			if rd["state"] == "available" {
				if body, _ := rd["readme"].(map[string]any); body == nil || body["html"] == "" || body["contentType"] != "text/markdown" {
					t.Fatalf("the README's body: %.400s", a.Body)
				}
			}
			if n := r.upstreamDocs(p.Name); n != docs {
				t.Errorf("the README call asked the upstream %d more times, want none (the lookup handed it over)", n-docs)
			}
			if n := r.st.Number(`SELECT count(*) FROM remote_cache_entries e JOIN repositories r ON r.id = e.repo_id WHERE r.key = $1`, rRemote); n != 0 {
				t.Errorf("the lookup and the README stored %d cache entries", n)
			}

			before := r.upstreamDocs(other.Name)
			a = r.st.Do(rep, http.MethodGet, upstreamPath+"/package?name="+url.QueryEscape(other.Name), dave, nil)
			m = a.JSON(t)
			if a.Status != http.StatusOK || m["about"] == nil || m["readme"] != false {
				t.Fatalf("the lookup of a reader without the wiki: %d %.400s", a.Status, a.Body)
			}
			a = r.st.Do(rep, http.MethodGet, upstreamPath+"/readme?name="+url.QueryEscape(other.Name), dave, nil)
			if a.Status != http.StatusNotFound {
				t.Fatalf("the README for a reader without the wiki: %d %.300s, want 404", a.Status, a.Body)
			}
			if n := r.upstreamDocs(other.Name); n != before+1 {
				t.Errorf("a reader without the wiki: the upstream was asked %d times, want the lookup's one", n-before)
			}
		})
	}
}
