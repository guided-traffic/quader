//go:build integration

package ansiblevirtual

import (
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/ansible/collection/collectiontest"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_ANS_061_TheScreensPublishThroughAVirtualRepositoryIsItsTargetsAndItsRefusalsAreTheClients: the screens' upload on a virtual
// repository stores the version in the deployment target and answers the target's task; a first publish of a name a remote member serves
// is refused without manage, as the client's is (R-REPO-048), in the management API's shape; a virtual repository without a target takes
// none (R-ANS-061, R-ANS-024). Integration, archives under a kilobyte, about two seconds with the stub.
func TestR_ANS_061_TheScreensPublishThroughAVirtualRepositoryIsItsTargetsAndItsRefusalsAreTheClients(t *testing.T) {
	e := newEnv(t, options{})
	post := func(repo, token string, s collectiontest.Spec) testenv.Answer {
		return e.st.Do(e.rep, http.MethodPost, "/api/v1/projects/"+testenv.DefaultProject+"/repositories/"+repo+"/ansible/collections", token,
			collectiontest.Build(s), "Content-Type", "application/gzip")
	}
	a := post(rVirtual, e.writer, spec("mine", "tools", "1.0.0"))
	if a.Status != http.StatusCreated || a.JSON(t)["state"] != "completed" {
		t.Fatalf("publish: %d %s", a.Status, a.Body)
	}
	if n := e.st.Number(`SELECT count(*) FROM ansible_import_tasks k JOIN repositories r ON r.id = k.repo_id WHERE r.key = $1 AND k.id::text = $2`,
		rLocal, a.JSON(t)["id"]); n != 1 {
		t.Errorf("the answered task is not the target's: %d", n)
	}
	if n := e.st.Number(`SELECT count(*) FROM ansible_collection_versions v JOIN repositories r ON r.id = v.repo_id WHERE r.key = $1 AND v.name = 'mine.tools'`, rLocal); n != 1 {
		t.Errorf("%d rows in %s", n, rLocal)
	}
	if b := post(rVirtual, e.plain, spec("other", "stuff", "77.0.0")); b.Status != http.StatusForbidden ||
		!strings.Contains(b.Header.Get("Content-Type"), "problem+json") || !strings.Contains(string(b.Body), `"code":"shadows_remote_package"`) {
		t.Errorf("a name the remote member serves, without manage: %d %s", b.Status, b.Body)
	}
	if c := post(rNoLocal, e.writer, spec("mine", "tools", "1.1.0")); c.Status != http.StatusForbidden {
		t.Errorf("a virtual repository without a target: %d %s", c.Status, c.Body)
	}
}
