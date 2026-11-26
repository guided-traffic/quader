//go:build integration

package ansiblevirtual

import (
	"net/http"
	"path"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/ansible/collection/collectiontest"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_ANS_061_TheScreensUploadThroughAVirtualRepositoryIsItsTargetsAndItsRefusalsAreTheClients: the upload token of a virtual
// repository names the virtual repository and its deployment target; the publish with it stores the version in the target, the task
// is read through the virtual repository; a first publish of a name a remote member serves is refused without manage, as the
// client's is (R-REPO-048); a virtual repository without a target issues no token (R-ANS-061, R-ANS-024). Integration, archives under
// a kilobyte, about two seconds with the stub.
func TestR_ANS_061_TheScreensUploadThroughAVirtualRepositoryIsItsTargetsAndItsRefusalsAreTheClients(t *testing.T) {
	e := newEnv(t, options{})
	api := func(repo, sub string) string {
		return "/api/v1/projects/" + testenv.DefaultProject + "/repositories/" + repo + "/ansible/" + sub
	}
	upload := func(repo, token string, s collectiontest.Spec) testenv.Answer {
		m := e.st.Do(e.rep, http.MethodPost, api(repo, "upload-tokens"), token, nil)
		if m.Status != http.StatusCreated {
			t.Fatalf("the upload token of %s: %d %s", repo, m.Status, m.Body)
		}
		doc := m.JSON(t)
		tok, _ := doc["token"].(string)
		p, _ := doc["publishPath"].(string)
		return e.st.Do(e.rep, http.MethodPost, p, tok, collectiontest.Build(s), "Content-Type", "application/gzip")
	}
	a := upload(rVirtual, e.writer, spec("mine", "tools", "1.0.0"))
	if a.Status != http.StatusAccepted {
		t.Fatalf("publish: %d %s", a.Status, a.Body)
	}
	addr, _ := a.JSON(t)["task"].(string)
	task := e.st.Do(e.rep, http.MethodGet, api(rVirtual, "imports/"+path.Base(strings.TrimSuffix(addr, "/"))), e.writer, nil)
	if task.Status != http.StatusOK || task.JSON(t)["state"] != "completed" {
		t.Fatalf("the task through the virtual repository: %d %s", task.Status, task.Body)
	}
	if n := e.st.Number(`SELECT count(*) FROM ansible_collection_versions v JOIN repositories r ON r.id = v.repo_id WHERE r.key = $1 AND v.name = 'mine.tools'`, rLocal); n != 1 {
		t.Errorf("%d rows in %s", n, rLocal)
	}
	if b := upload(rVirtual, e.plain, spec("other", "stuff", "77.0.0")); b.Status != http.StatusForbidden || errorCode(b) != "shadows_remote_package" {
		t.Errorf("a name the remote member serves, without manage: %d %s", b.Status, b.Body)
	}
	if c := e.st.Do(e.rep, http.MethodPost, api(rNoLocal, "upload-tokens"), e.writer, nil); c.Status != http.StatusForbidden && c.Status != http.StatusMethodNotAllowed {
		t.Errorf("a virtual repository without a target: %d %s", c.Status, c.Body)
	}
}
