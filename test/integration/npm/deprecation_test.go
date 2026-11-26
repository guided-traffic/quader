//go:build integration

package npm

import (
	"strings"
	"testing"
)

// TestR_API_085_AnNPMVersionIsDeprecatedThroughTheManagementAPIAndServed: the management operation deprecates one version of a local
// repository with a message (R-API-085, R-NPM-018). The package document serves versions[v].deprecated at once, as npm install reads it
// for its warning, the version list marks it, a repeat changes nothing, and the DELETE clears it. A holder of deploy who does not own the
// name is refused as npm deprecate would be (Q-NPM-9), and npm's refusal of NuGet's reasons says why. Cost: two tiny publishes and a
// dozen requests against one replica, under a second.
func TestR_API_085_AnNPMVersionIsDeprecatedThroughTheManagementAPIAndServed(t *testing.T) {
	e := newEnv(t)
	a := e.replica("web-a")
	e.mustPublish(a, "npm-local", e.tokA, pkg{Name: "@acme/old", Version: "1.0.0"})
	e.mustPublish(a, "npm-local", e.tokA, pkg{Name: "@acme/old", Version: "1.1.0"})
	const op = "/api/v1/projects/it/repositories/npm-local/packages/@acme%2Fold/deprecation"
	body := map[string]any{"version": "1.0.0", "message": "use 1.1.0"}

	if ans := e.st.Do(a, "PUT", op, e.tokB, body); ans.Status != 403 {
		t.Errorf("a holder of deploy who does not own the name: %d %s", ans.Status, ans.Body)
	}
	if ans := e.st.Do(a, "PUT", op, e.tokA, map[string]any{"version": "1.0.0", "message": "x", "reasons": []string{"Legacy"}}); ans.Status != 422 ||
		!strings.Contains(string(ans.Body), "message only") {
		t.Errorf("reasons: %d %s", ans.Status, ans.Body)
	}
	if ans := e.st.Do(a, "PUT", op, e.tokA, map[string]any{"version": "9.9.9", "message": "x"}); ans.Status != 404 {
		t.Errorf("a version that is not published: %d %s", ans.Status, ans.Body)
	}
	rev := e.revSeq("npm-local", "@acme/old")
	for range 2 {
		if ans := e.st.Do(a, "PUT", op, e.tokA, body); ans.Status != 200 || !strings.Contains(string(ans.Body), `"message":"use 1.1.0"`) {
			t.Fatalf("%d %s", ans.Status, ans.Body)
		}
	}
	if e.revSeq("npm-local", "@acme/old") != rev+1 || e.events("npm.deprecate", "@acme/old", "") != 1 {
		t.Error("the revision did not rise once, or npm.deprecate is not there once")
	}
	doc := e.get(a, "npm-local", "@acme%2Fold", e.tokC)
	var d struct {
		Versions map[string]struct {
			Deprecated string `json:"deprecated"`
		} `json:"versions"`
	}
	if err := jsonUnmarshal(doc.Body, &d); err != nil || d.Versions["1.0.0"].Deprecated != "use 1.1.0" || d.Versions["1.1.0"].Deprecated != "" {
		t.Errorf("the package document: %v %s", err, doc.Body)
	}
	id := e.st.Do(a, "GET", "/api/v1/packages/lookup?format=npm&name=@acme/old", e.tokC, nil).JSON(t)["id"].(string)
	if vs := e.st.Do(a, "GET", "/api/v1/packages/"+id+"/versions", e.tokC, nil); strings.Count(string(vs.Body), `"deprecation":{"message":"use 1.1.0","scope":"version"}`) != 1 {
		t.Errorf("the version list: %s", vs.Body)
	}
	if ans := e.st.Do(a, "DELETE", op+"?version=1.0.0", e.tokA, nil); ans.Status != 204 {
		t.Fatalf("clear: %d %s", ans.Status, ans.Body)
	}
	doc = e.get(a, "npm-local", "@acme%2Fold", e.tokC)
	if strings.Contains(string(doc.Body), `"deprecated"`) {
		t.Errorf("the clear is not served: %s", doc.Body)
	}
}
