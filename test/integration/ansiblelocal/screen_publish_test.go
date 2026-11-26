//go:build integration

package ansiblelocal

import (
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/ansible/collection/collectiontest"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// screenPublish sends an archive to the management operation of the screens' upload as the dialog sends it: the archive as the body.
func (e *env) screenPublish(token, key string, archive []byte, query string) testenv.Answer {
	e.t.Helper()
	return e.st.Do(e.r, "POST", "/api/v1/projects/"+project+"/repositories/"+key+"/ansible/collections"+query, token, archive,
		"Content-Type", "application/gzip")
}

// TestR_ANS_061_TheScreensPublishRunsTheClientsPublishAndAnswersTheTask: the management operation stores the version with the import task
// of the client's publish and answers that task, the import's warnings in it (R-ANS-062); an archive that is no collection is a failed
// task; every refusal of the client's publish comes in the management API's shape; a reader may not publish, and another format's
// repository is not applicable (R-ANS-061, Q-ANS-140, Q-ANS-141). Integration, archives under a kilobyte, about a second.
func TestR_ANS_061_TheScreensPublishRunsTheClientsPublishAndAnswersTheTask(t *testing.T) {
	e := newEnvOpts(t, envOpts{seed: func(e *env) { e.st.Repository("files-local", "local", "generic", "") }})
	archive := collectiontest.Build(spec("1.0.0"))

	a := e.screenPublish(e.tokB, repo, archive, "?sha256="+sum(archive))
	if a.Status != 201 {
		t.Fatalf("publish: %d %s", a.Status, a.Body)
	}
	task := a.JSON(t)
	if task["state"] != "completed" || task["namespace"] != "acme" || task["name"] != "tools" || task["version"] != "1.0.0" || task["createdBy"] != e.bob.ID {
		t.Fatalf("task %s", a.Body)
	}
	if !strings.Contains(string(a.Body), "No changelog found") {
		t.Errorf("the import's warning of R-ANS-062 is not in the task: %s", a.Body)
	}
	if e.versionRows() != 1 || e.st.Text(`SELECT actor_id::text FROM audit_event WHERE action = 'package.published' LIMIT 1`) != e.bob.ID {
		t.Errorf("rows %d, or the event does not name bob", e.versionRows())
	}
	if dl := e.get(e.tokC, "v3/artifacts/collections/acme-tools-1.0.0.tar.gz"); dl.Status != 200 || sum(dl.Body) != sum(archive) {
		t.Errorf("the client's download of what the screen published: %d", dl.Status)
	}

	if f := e.screenPublish(e.tokB, repo, []byte("not an archive"), ""); f.Status != 201 || f.JSON(t)["state"] != "failed" ||
		!strings.Contains(string(f.Body), `"code":"invalid_archive"`) {
		t.Errorf("no collection: %d %s", f.Status, f.Body)
	}

	other := spec("1.0.0")
	other.Files["README.md"] = "other bytes"
	for name, c := range map[string]struct {
		answer testenv.Answer
		status int
		code   string
	}{
		"other bytes of a stored version": {e.screenPublish(e.tokB, repo, collectiontest.Build(other), ""), 409, "ansible_version_exists"},
		"a wrong sha256":                  {e.screenPublish(e.tokB, repo, collectiontest.Build(spec("1.1.0")), "?sha256="+strings.Repeat("0", 64)), 400, "ansible_sha256_mismatch"},
		"a reader":                        {e.screenPublish(e.tokC, repo, collectiontest.Build(spec("1.2.0")), ""), 403, "forbidden"},
		"a generic repository":            {e.screenPublish(e.tokAdmin, "files-local", archive, ""), 405, "not_applicable"},
	} {
		ct := c.answer.Header.Get("Content-Type")
		if c.answer.Status != c.status || !strings.Contains(ct, "problem+json") || !strings.Contains(string(c.answer.Body), `"code":"`+c.code+`"`) {
			t.Errorf("%s: %d %s %s, want %d %s", name, c.answer.Status, ct, c.answer.Body, c.status, c.code)
		}
	}
	if e.versionRows() != 1 {
		t.Errorf("a refused publish left rows: %d", e.versionRows())
	}
}
