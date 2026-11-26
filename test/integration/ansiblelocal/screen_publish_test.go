//go:build integration

package ansiblelocal

import (
	"path"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/ansible/collection/collectiontest"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// screenUpload is what the dialog Upload collection does (R-ANS-061): it asks the management API for an upload token, sends the
// archive with it to the client's publish route, and reads the task the publish answered through the management API.
type screenUpload struct {
	e        *env
	api, pkg *testenv.Replica
	// hdr are the browser's headers on the management API: the session cookie, its CSRF token and the origin.
	hdr []string
}

func (u screenUpload) mint(key string) testenv.Answer {
	u.e.t.Helper()
	return u.e.st.Do(u.api, "POST", "/api/v1/projects/"+project+"/repositories/"+key+"/ansible/upload-tokens", "", nil, u.hdr...)
}

// publish mints the token of the repository and sends the archive with it; it returns the publish's answer and the token.
func (u screenUpload) publish(key string, archive []byte) (testenv.Answer, string) {
	u.e.t.Helper()
	m := u.mint(key)
	if m.Status != 201 {
		u.e.t.Fatalf("the upload token of %s: %d %s", key, m.Status, m.Body)
	}
	doc := m.JSON(u.e.t)
	tok, _ := doc["token"].(string)
	p, _ := doc["publishPath"].(string)
	return u.e.st.Do(u.pkg, "POST", p, tok, archive, "Content-Type", "application/gzip"), tok
}

// task reads the task a publish answered through the management API.
func (u screenUpload) task(key string, publish testenv.Answer) testenv.Answer {
	u.e.t.Helper()
	addr, _ := publish.JSON(u.e.t)["task"].(string)
	id := path.Base(strings.TrimSuffix(addr, "/"))
	return u.e.st.Do(u.api, "GET", "/api/v1/projects/"+project+"/repositories/"+key+"/ansible/imports/"+id, "", nil, u.hdr...)
}

func isProblem(a testenv.Answer, status int, code string) bool {
	return a.Status == status && strings.Contains(a.Header.Get("Content-Type"), "problem+json") && strings.Contains(string(a.Body), `"code":"`+code+`"`)
}

// browser is the dialog of a user's signed-in browser on the role api.
func (e *env) browser(u testenv.Person) screenUpload {
	cookie, csrf := e.browserSession(u)
	return screenUpload{e: e, api: e.api, pkg: e.pkg, hdr: []string{"Cookie", cookie, "Origin", browserOrigin, "X-CSRF-Token", csrf}}
}

// TestR_ANS_061_TheScreensPublishRunsOnTheRolesTheChartRuns: the upload of the screen on an installation with split roles, the
// management API on a replica of the role api and the publish on one of the role packages (R-ARCH-001, Q-ANS-140). The token the role
// api issues to the session is the caller's for five minutes with read and deploy on the repository only; the publish with it stores the
// version with the import's warnings (R-ANS-062), and the event names the caller and that token; the task is read on the role api. The
// token reads and publishes nothing else, mints no token, and is gone once revoked; a client's token gets no upload token; a reader and
// a generic repository get none either. Integration, archives under a kilobyte, about 3 s with the stack's start.
func TestR_ANS_061_TheScreensPublishRunsOnTheRolesTheChartRuns(t *testing.T) {
	e := newEnvOpts(t, envOpts{split: true, seed: func(e *env) {
		e.st.Repository("files-local", "local", "generic", "")
		e.st.Repository("ansible-other", "local", "ansible", `{"local":{"packageOwnership":"off"}}`)
		e.st.Policy("other-deployers", []string{"read", "deploy"}, []string{"ansible-other", "files-local"}, e.bob)
	}})
	u := e.browser(e.bob)
	archive := collectiontest.Build(spec("1.0.0"))

	m := u.mint(repo)
	if m.Status != 201 || m.Header.Get("Cache-Control") != "no-store" || m.JSON(t)["publishPath"] != base+"v3/artifacts/collections/" {
		t.Fatalf("the token: %d %v %s", m.Status, m.Header, m.Body)
	}
	if got := e.st.Text(`SELECT type || ' ' || created_via || ' ' || (expires_at - created_at <= interval '5 minutes')::text || ' ' || scope::text
	                     FROM api_token WHERE id = $1::uuid`, m.JSON(t)["id"]); !strings.HasPrefix(got, "pat ui true ") ||
		!strings.Contains(got, `"it/ansible-local"`) || strings.Contains(got, "ansible-other") || strings.Contains(got, `"inherit": true`) {
		t.Errorf("the upload token's row: %q", got)
	}

	a, tok := u.publish(repo, archive)
	if a.Status != 202 || strings.Contains(string(a.Body), tok) {
		t.Fatalf("the publish with the upload token: %d %s", a.Status, a.Body)
	}
	task := u.task(repo, a)
	if task.Status != 200 || task.JSON(t)["state"] != "completed" || !strings.Contains(string(task.Body), "No changelog found") {
		t.Fatalf("the task on the role api: %d %s", task.Status, task.Body)
	}
	if got := e.st.Text(`SELECT a.actor_id::text || ' ' || coalesce(a.auth_method, '-') || ' ' || k.description FROM audit_event a
	                     JOIN api_token k ON k.id = a.token_id WHERE a.action = 'package.published'`); got != e.bob.ID+" bearer Upload of a collection from the screen into it/ansible-local" {
		t.Errorf("the event of the screen's publish: %q", got)
	}

	// The token reads and publishes nothing but this repository, and mints no token, neither a personal one nor the next upload token.
	other := e.st.Do(e.pkg, "POST", "/api/ansible/"+project+"/ansible-other/v3/artifacts/collections/", tok, collectiontest.Build(spec("2.0.0")), "Content-Type", "application/gzip")
	read := e.st.Do(e.pkg, "GET", "/api/ansible/"+project+"/ansible-other/", tok, nil)
	minted := e.st.Do(e.api, "POST", "/api/v1/tokens", tok, map[string]any{"description": "next", "ttlSeconds": 3600})
	again := e.st.Do(e.api, "POST", "/api/v1/projects/"+project+"/repositories/"+repo+"/ansible/upload-tokens", tok, nil)
	for name, a := range map[string]testenv.Answer{"a publish elsewhere": other, "a read elsewhere": read, "a personal token": minted, "the next upload token": again} {
		t.Logf("the upload token, %s: %d", name, a.Status)
		if a.Status != 403 && a.Status != 404 {
			t.Errorf("the upload token, %s: %d %s", name, a.Status, a.Body)
		}
	}
	if a := e.st.Do(e.api, "POST", "/api/v1/projects/"+project+"/repositories/"+repo+"/ansible/upload-tokens", e.tokB, nil); !isProblem(a, 403, "forbidden") {
		t.Errorf("a client's token asks for an upload token: %d %s", a.Status, a.Body)
	}
	// The dialog revokes the token once the publish answered: it is refused from then on.
	if a := e.st.Do(e.api, "DELETE", "/api/v1/tokens/"+m.JSON(t)["id"].(string), "", nil, u.hdr...); a.Status != 204 {
		t.Fatalf("the revocation: %d %s", a.Status, a.Body)
	}
	if a := e.st.Do(e.pkg, "GET", base, m.JSON(t)["token"].(string), nil); a.Status != 401 {
		t.Errorf("a revoked upload token: %d", a.Status)
	}

	if f, _ := u.publish(repo, []byte("not an archive")); f.Status != 202 || u.task(repo, f).JSON(t)["state"] != "failed" {
		t.Errorf("no collection: %d %s", f.Status, f.Body)
	}
	if a := e.browser(e.carol).mint(repo); !isProblem(a, 403, "forbidden") {
		t.Errorf("a reader's token: %d %s", a.Status, a.Body)
	}
	if a := u.mint("files-local"); !isProblem(a, 405, "not_applicable") {
		t.Errorf("a generic repository: %d %s", a.Status, a.Body)
	}
	if a := e.st.Do(e.pkg, "POST", "/api/v1/projects/"+project+"/repositories/"+repo+"/ansible/upload-tokens", "", nil, u.hdr...); a.Status != 404 {
		t.Errorf("the role packages serves no management operation: %d", a.Status)
	}
	if e.versionRows() != 1 {
		t.Errorf("rows: %d", e.versionRows())
	}
}
