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
	// cred is the caller's credential on the management API: a token, or nil with the browser's headers in hdr.
	cred string
	hdr  []string
}

func (u screenUpload) mint(key string) testenv.Answer {
	u.e.t.Helper()
	return u.e.st.Do(u.api, "POST", "/api/v1/projects/"+project+"/repositories/"+key+"/ansible/upload-tokens", u.cred, nil, u.hdr...)
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
	return u.e.st.Do(u.api, "GET", "/api/v1/projects/"+project+"/repositories/"+key+"/ansible/imports/"+id, u.cred, nil, u.hdr...)
}

func isProblem(a testenv.Answer, status int, code string) bool {
	return a.Status == status && strings.Contains(a.Header.Get("Content-Type"), "problem+json") && strings.Contains(string(a.Body), `"code":"`+code+`"`)
}

// TestR_ANS_061_TheScreensPublishRunsOnTheRolesTheChartRuns: the upload of the screen on an installation with split roles, the
// management API on a replica of the role api and the publish on one of the role packages (R-ARCH-001, Q-ANS-140). The token the role
// api issues is the caller's for five minutes with read and deploy on the repository only; the publish with it stores the version with
// the import's warnings (R-ANS-062), and the event names the caller and that token; the task is read on the role api; a reader, a
// generic repository and a repository the token does not name are refused. Integration, archives under a kilobyte, about 3 s with
// the stack's start.
func TestR_ANS_061_TheScreensPublishRunsOnTheRolesTheChartRuns(t *testing.T) {
	e := newEnvOpts(t, envOpts{split: true, seed: func(e *env) {
		e.st.Repository("files-local", "local", "generic", "")
		e.st.Repository("ansible-other", "local", "ansible", `{"local":{"packageOwnership":"off"}}`)
		e.st.Policy("ansible-other-deployers", []string{"read", "deploy"}, []string{"ansible-other"}, e.bob)
	}})
	u := screenUpload{e: e, api: e.api, pkg: e.pkg, cred: e.tokB}
	archive := collectiontest.Build(spec("1.0.0"))

	m := u.mint(repo)
	if m.Status != 201 || m.Header.Get("Cache-Control") != "no-store" || m.JSON(t)["publishPath"] != base+"v3/artifacts/collections/" {
		t.Fatalf("the token: %d %v %s", m.Status, m.Header, m.Body)
	}
	if got := e.st.Text(`SELECT type || ' ' || (expires_at - created_at <= interval '5 minutes')::text || ' ' || scope::text FROM api_token
	                     WHERE description LIKE 'Upload of a collection from the screen into it/ansible-local'`); !strings.HasPrefix(got, "pat true ") ||
		!strings.Contains(got, `"it/ansible-local"`) || strings.Contains(got, "inherit\": true") {
		t.Errorf("the upload token's row: %q", got)
	}

	a, tok := u.publish(repo, archive)
	if a.Status != 202 {
		t.Fatalf("the publish with the upload token: %d %s", a.Status, a.Body)
	}
	task := u.task(repo, a)
	if task.Status != 200 || task.JSON(t)["state"] != "completed" || !strings.Contains(string(task.Body), "No changelog found") {
		t.Fatalf("the task on the role api: %d %s", task.Status, task.Body)
	}
	if got := e.st.Text(`SELECT actor_id::text || ' ' || coalesce(auth_method, '-') FROM audit_event WHERE action = 'package.published'`); got != e.bob.ID+" bearer" {
		t.Errorf("the event of the screen's publish: %q", got)
	}
	if n := e.st.Number(`SELECT count(*) FROM audit_event a JOIN api_token k ON k.id = a.token_id WHERE a.action = 'package.published'
	                     AND k.description LIKE 'Upload of a collection from the screen%'`); n != 1 {
		t.Errorf("the event does not name the upload token: %d", n)
	}

	// The token names this repository only: another one the caller may publish into refuses it.
	other := e.st.Do(e.pkg, "POST", "/api/ansible/"+project+"/ansible-other/v3/artifacts/collections/", tok, collectiontest.Build(spec("2.0.0")), "Content-Type", "application/gzip")
	if other.Status != 403 && other.Status != 404 {
		t.Errorf("the upload token on another repository: %d %s", other.Status, other.Body)
	}
	if f, _ := u.publish(repo, []byte("not an archive")); f.Status != 202 || u.task(repo, f).JSON(t)["state"] != "failed" {
		t.Errorf("no collection: %d %s", f.Status, f.Body)
	}
	if a := (screenUpload{e: e, api: e.api, pkg: e.pkg, cred: e.tokC}).mint(repo); !isProblem(a, 403, "forbidden") {
		t.Errorf("a reader's token: %d %s", a.Status, a.Body)
	}
	if a := (screenUpload{e: e, api: e.api, pkg: e.pkg, cred: e.tokAdmin}).mint("files-local"); !isProblem(a, 405, "not_applicable") {
		t.Errorf("a generic repository: %d %s", a.Status, a.Body)
	}
	if a := e.st.Do(e.pkg, "POST", "/api/v1/projects/"+project+"/repositories/"+repo+"/ansible/upload-tokens", e.tokB, nil); a.Status != 404 {
		t.Errorf("the role packages serves no management operation: %d", a.Status)
	}
	if e.versionRows() != 1 {
		t.Errorf("rows: %d", e.versionRows())
	}
}
