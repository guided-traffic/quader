//go:build integration

package nuget

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/nugetstub"
)

// TestR_CUR_028_ANuGetPackagesReadmeIsReadThroughOnTheRolesAPIAndWeb: the tab Upstream's lookup of a package no client fetched names
// its newest registration leaf's facts, and the README call reads the address the service index's ReadmeUriTemplate/6.13.0 names,
// sanitized, once per replica, and stores nothing; a template on a host the remote does not reach is not asked and answers the host;
// a README that fails answers the upstream's failure; while the upstream's rate limit holds the queue the README waits and says until
// when. Each on a replica of the role api, which serves the management API in the chart and has no format host, and on one of the
// role web (R-CUR-028, R-ARCH-001). About 3 s, the stub's corpus.
func TestR_CUR_028_ANuGetPackagesReadmeIsReadThroughOnTheRolesAPIAndWeb(t *testing.T) {
	e := newRemoteEnv(t, remoteOptions{})
	base := "/api/v1/projects/" + testenv.DefaultProject + "/repositories/" + rRemote + "/upstream"
	id := e.st.Number(`SELECT id FROM repositories WHERE key = $1`, rRemote)
	for _, o := range []testenv.ReplicaOptions{{Name: "api-a", Role: "api"}, {Name: "web-a", Role: "web"}} {
		rep := e.st.Start(t, o)
		t.Run("R-CUR-028 on the role "+o.Role, func(t *testing.T) {
			if err := e.stub.SetMode(nugetstub.Mode{}); err != nil {
				t.Fatal(err)
			}
			a := e.st.Do(rep, http.MethodGet, base+"/package?name=contoso.versions", e.tokReader, nil)
			m := a.JSON(t)
			about, _ := m["about"].(map[string]any)
			versions, _ := m["versions"].([]any)
			if a.Status != http.StatusOK || about == nil || m["readme"] != true || len(versions) < 3 {
				t.Fatalf("the lookup: %d %.500s", a.Status, a.Body)
			}
			newest := about["version"].(string)
			readme := func(version string) (int, map[string]any, string) {
				a := e.st.Do(rep, http.MethodGet, base+"/readme?name=contoso.versions&version="+url.QueryEscape(version), e.tokReader, nil)
				return a.Status, a.JSON(t), string(a.Body)
			}
			before := e.stub.Counts().Requests(http.MethodGet, "/v3-flatcontainer/contoso.versions/"+newest+"/readme")
			for range 2 {
				status, rd, body := readme(newest)
				doc, _ := rd["readme"].(map[string]any)
				html, _ := doc["html"].(string)
				if status != http.StatusOK || rd["state"] != "available" || !strings.Contains(html, "contoso.versions "+newest) || strings.Contains(html, "<script") {
					t.Fatalf("the README of %s: %d %.500s", newest, status, body)
				}
			}
			if n := e.stub.Counts().Requests(http.MethodGet, "/v3-flatcontainer/contoso.versions/"+newest+"/readme") - before; n != 1 {
				t.Errorf("two README calls reached the stub %d times, want 1", n)
			}
			if n := e.st.Number(`SELECT count(*) FROM remote_cache_entries WHERE repo_id = $1`, id); n != 0 {
				t.Errorf("the lookup and the README stored %d cache entries", n)
			}

			// A template on a host the remote does not reach: not asked, the host named.
			if err := e.stub.SetMode(nugetstub.Mode{ReadmeHost: "readmes.invalid"}); err != nil {
				t.Fatal(err)
			}
			v1 := versions[1].(map[string]any)["version"].(string)
			if status, rd, body := readme(v1); status != http.StatusOK || rd["state"] != "unreachable" || rd["host"] != "readmes.invalid" {
				t.Errorf("a README on a host the remote does not reach: %d %.400s", status, body)
			}

			// A README that fails: the upstream's failure, not a README.
			if err := e.stub.SetMode(nugetstub.Mode{ReadmeStatus: http.StatusInternalServerError}); err != nil {
				t.Fatal(err)
			}
			v2 := versions[2].(map[string]any)["version"].(string)
			if status, rd, body := readme(v2); status != http.StatusBadGateway || rd["code"] != "upstream_error" {
				t.Errorf("a README that fails: %d %.400s", status, body)
			}

			// The upstream's rate limit holds the queue: the README is not asked and says until when.
			e.st.Exec(`INSERT INTO remote_download_limits (repo_id, since, until, waits, hinted, failure)
				VALUES ($1, now(), now() + interval '10 minutes', 1, true, '{}'::jsonb)`, id)
			if status, rd, body := readme(v2); status != http.StatusOK || rd["state"] != "limited" || rd["until"] == nil {
				t.Errorf("a README while the limit holds: %d %.400s", status, body)
			}
			e.st.Exec(`DELETE FROM remote_download_limits WHERE repo_id = $1`, id)
		})
	}
}
