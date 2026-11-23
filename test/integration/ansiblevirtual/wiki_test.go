//go:build integration

package ansiblevirtual

import (
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/auth/scope"
)

// TestR_ANS_041_AMemberWhoseWikiTheCallerMayNotReadIsSkippedForTheDocumentation: the wiki check applies to the virtual repository and to the
// member it reads from (R-ANS-037, R-ANS-041, R-AUTHZ-034). A caller who reads the artifacts of every repository through their internal
// level, and not the wiki of the local member, gets the documentation of the remote member through the virtual repository, the local
// member's own route answers as a version without documentation, and its version document is served as before. A reader with a grant
// gets the local member's documentation.
func TestR_ANS_041_AMemberWhoseWikiTheCallerMayNotReadIsSkippedForTheDocumentation(t *testing.T) {
	e := newEnv(t, options{levels: map[string][2]string{
		rLocal: {"internal", "private"}, rGalaxy: {"internal", "internal"}, rVirtual: {"internal", "internal"},
	}})
	s := spec("acme", "tools", "1.0.0")
	s.Files["plugins/modules/local_only.py"] = "DOCUMENTATION = '''\nshort_description: only in the local member\n'''\n"
	if a, _ := e.publish(rLocal, e.writer, s); a.Status != http.StatusAccepted {
		t.Fatalf("publish: %d %s", a.Status, a.Body)
	}
	outsider := e.st.Token(e.st.User("outsider"), scope.Default())
	// The remote member's documentation is filled by a reader with a grant, so the outsider's read needs no fill of a cache.
	if a := e.get(rGalaxy, docsPathOf("acme", "tools", "1.0.0"), e.reader); a.Status != http.StatusOK {
		t.Fatalf("the remote member's documentation: %d %s", a.Status, a.Body)
	}

	if a := e.get(rVirtual, docsPathOf("acme", "tools", "1.0.0"), e.reader); a.Status != http.StatusOK || !strings.Contains(string(a.Body), `"content_name":"local_only"`) {
		t.Errorf("a reader with a grant through the virtual repository: %d %s", a.Status, a.Body)
	}
	a := e.get(rVirtual, docsPathOf("acme", "tools", "1.0.0"), outsider)
	if a.Status != http.StatusOK || strings.Contains(string(a.Body), "local_only") || !strings.Contains(string(a.Body), `"content_name":"ping_host"`) {
		t.Errorf("the outsider through the virtual repository gets the remote member's documentation: %d %s", a.Status, a.Body)
	}
	if a := e.get(rLocal, docsPathOf("acme", "tools", "1.0.0"), outsider); a.Status != http.StatusNotFound || strings.Contains(string(a.Body), "local_only") {
		t.Errorf("the outsider at the local member's route: %d %s", a.Status, a.Body)
	}
	if a := e.get(rLocal, "v3/collections/acme/tools/versions/1.0.0/", outsider); a.Status != http.StatusOK {
		t.Errorf("the outsider reads the local member's version document: %d %s", a.Status, a.Body)
	}
}
