//go:build integration

package ansiblevirtual

import (
	"net/http"
	"path"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/auth/session"
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
	// The token of an upload from the screen, the operation of every format that publishes from the screen (R-NUGET-006).
	tokens := func(repo string) string {
		return "/api/v1/projects/" + testenv.DefaultProject + "/repositories/" + repo + "/upload-tokens"
	}
	// The browser of a person: a live session as a sign-in leaves it, its cookie, its CSRF token and the endpoint's origin.
	browser := func(u testenv.Person) []string {
		id, secret, csrf := session.NewID(), session.NewSecret(), session.NewSecret()
		e.st.Exec(`INSERT INTO ui_session (id, secret_sha256, csrf_sha256, principal_id, auth_method, authenticated_at, idle_timeout,
		                                   absolute_expires_at, created_addr, user_agent)
		           VALUES ($1::uuid, $2, $3, $4::uuid, 'oidc', now(), interval '1 hour', now() + interval '12 hours', '192.0.2.7', 'it-browser')`,
			id, session.Hash(secret), session.Hash(csrf), u.ID)
		return []string{"Cookie", session.CookieSession + "=" + secret, "X-CSRF-Token", csrf, "Origin", "http://" + testenv.DefaultHost}
	}
	writer, plain := browser(e.writerP), browser(e.plainP)
	upload := func(repo string, hdr []string, s collectiontest.Spec) testenv.Answer {
		m := e.st.Do(e.rep, http.MethodPost, tokens(repo), "", nil, hdr...)
		if m.Status != http.StatusCreated {
			t.Fatalf("the upload token of %s: %d %s", repo, m.Status, m.Body)
		}
		doc := m.JSON(t)
		tok, _ := doc["token"].(string)
		p, _ := doc["publishPath"].(string)
		return e.st.Do(e.rep, http.MethodPost, p, tok, collectiontest.Build(s), "Content-Type", "application/gzip")
	}
	a := upload(rVirtual, writer, spec("mine", "tools", "1.0.0"))
	if a.Status != http.StatusAccepted {
		t.Fatalf("publish: %d %s", a.Status, a.Body)
	}
	addr, _ := a.JSON(t)["task"].(string)
	task := e.st.Do(e.rep, http.MethodGet, api(rVirtual, "imports/"+path.Base(strings.TrimSuffix(addr, "/"))), "", nil, writer...)
	if task.Status != http.StatusOK || task.JSON(t)["state"] != "completed" {
		t.Fatalf("the task through the virtual repository: %d %s", task.Status, task.Body)
	}
	if n := e.st.Number(`SELECT count(*) FROM ansible_collection_versions v JOIN repositories r ON r.id = v.repo_id WHERE r.key = $1 AND v.name = 'mine.tools'`, rLocal); n != 1 {
		t.Errorf("%d rows in %s", n, rLocal)
	}
	if b := upload(rVirtual, plain, spec("other", "stuff", "77.0.0")); b.Status != http.StatusForbidden || errorCode(b) != "shadows_remote_package" {
		t.Errorf("a name the remote member serves, without manage: %d %s", b.Status, b.Body)
	}
	if c := e.st.Do(e.rep, http.MethodPost, tokens(rNoLocal), "", nil, writer...); c.Status != http.StatusForbidden && c.Status != http.StatusMethodNotAllowed {
		t.Errorf("a virtual repository without a target: %d %s", c.Status, c.Body)
	}
}
