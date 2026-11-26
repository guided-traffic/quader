//go:build integration

package ansiblelocal

import (
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/packages/view"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_ANS_060_ACollectionIsDeprecatedThroughTheManagementAPIAndServed: the management operation deprecates a whole collection of a
// local repository (R-API-085, R-ANS-060). The collection document serves deprecated: true at once, every version of it is marked in the
// screens' lists with the message the publisher gave, the filter deprecated finds it, a repeat changes nothing, a version is refused with
// the reason, a reader may not, and the DELETE clears it. The operation and the screens' reads go to a replica of the role api
// (Q-ARCH-1070). Cost: one tiny publish, a second replica and a dozen requests, about two seconds.
func TestR_ANS_060_ACollectionIsDeprecatedThroughTheManagementAPIAndServed(t *testing.T) {
	e := newEnv(t)
	e.mustPublish("1.0.0")
	e.mustPublish("1.1.0")
	// The management operation and the screens' reads go to a replica of the role api, which serves no package protocol (R-ARCH-001).
	m := e.st.Start(t, testenv.ReplicaOptions{Name: "api-a", Role: "api"})
	// The publishes wrote who depends on what (api-and-ui.md R-API-086): acme.tools uses community.general, by its newest version, read
	// on the replica of the role api.
	if ans := e.st.Do(m, "GET", "/api/v1/packages/"+view.ID("ansible", "community.general")+"/dependents", e.tokC, nil); ans.Status != 200 ||
		!strings.Contains(string(ans.Body), `"relations":[{"kind":"","range":"\u003e=1.0.0","via":"community.general"}],"version":"1.1.0"`) {
		t.Errorf("the users of community.general: %d %s", ans.Status, ans.Body)
	}
	const op = "/api/v1/projects/" + project + "/repositories/" + repo + "/packages/acme.tools/deprecation"
	if ans := e.st.Do(m, "PUT", op, e.tokC, map[string]any{"message": "x"}); ans.Status != 403 {
		t.Errorf("a reader: %d %s", ans.Status, ans.Body)
	}
	if ans := e.st.Do(m, "PUT", op, e.tokB, map[string]any{"version": "1.0.0"}); ans.Status != 422 || !strings.Contains(string(ans.Body), "whole collection") {
		t.Errorf("a version: %d %s", ans.Status, ans.Body)
	}
	before := e.eventCount("package.metadata_changed")
	for range 2 {
		if ans := e.st.Do(m, "PUT", op, e.tokB, map[string]any{"message": "Use acme.kit."}); ans.Status != 200 ||
			!strings.Contains(string(ans.Body), `"scope":"package"`) {
			t.Fatalf("%d %s", ans.Status, ans.Body)
		}
	}
	if e.eventCount("package.metadata_changed") != before+1 {
		t.Error("the event is not there once")
	}
	doc := e.get(e.tokC, "v3/collections/acme/tools/")
	if !strings.Contains(string(doc.Body), `"deprecated":true`) {
		t.Errorf("the collection document: %d %s", doc.Status, doc.Body)
	}
	id := e.packageID()
	if vs := e.st.Do(m, "GET", "/api/v1/packages/"+id+"/versions", e.tokC, nil); strings.Count(string(vs.Body), `"deprecation":{"message":"Use acme.kit.","scope":"package"}`) != 2 {
		t.Errorf("every version is marked: %s", vs.Body)
	}
	if l := e.st.Do(m, "GET", "/api/v1/packages?format=ansible&deprecated=true", e.tokC, nil); !strings.Contains(string(l.Body), `"name":"acme.tools"`) {
		t.Errorf("the filter: %s", l.Body)
	}
	if ans := e.st.Do(m, "DELETE", op, e.tokB, nil); ans.Status != 204 {
		t.Fatalf("clear: %d %s", ans.Status, ans.Body)
	}
	if doc := e.get(e.tokC, "v3/collections/acme/tools/"); !strings.Contains(string(doc.Body), `"deprecated":false`) {
		t.Errorf("the clear is not served: %s", doc.Body)
	}
	if l := e.st.Do(m, "GET", "/api/v1/packages?format=ansible&deprecated=true", e.tokC, nil); strings.Contains(string(l.Body), "acme.tools") {
		t.Errorf("the filter after the clear: %s", l.Body)
	}
}
