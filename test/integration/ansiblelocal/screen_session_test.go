//go:build integration

package ansiblelocal

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/session"
	"github.com/guided-traffic/quader/internal/formats/ansible/collection/collectiontest"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// browserOrigin is the origin of the default endpoint's base URL, which a session's write must send (R-AUTH-012).
const browserOrigin = "http://" + testenv.DefaultHost

// browserSession inserts a live session of the user as a sign-in leaves it and returns the cookie and the CSRF token of that browser.
func (e *env) browserSession(u testenv.Person) (cookie, csrf string) {
	e.t.Helper()
	id, secret, token := session.NewID(), session.NewSecret(), session.NewSecret()
	e.st.Exec(`INSERT INTO ui_session (id, secret_sha256, csrf_sha256, principal_id, auth_method, authenticated_at, idle_timeout,
	                                   absolute_expires_at, created_addr, user_agent)
	           VALUES ($1::uuid, $2, $3, $4::uuid, 'oidc', now(), interval '1 hour', now() + interval '12 hours', '192.0.2.7', 'it-browser')`,
		id, session.Hash(secret), session.Hash(token), u.ID)
	return session.CookieSession + "=" + secret, token
}

// TestR_ANS_061_TheScreensPublishWithASessionIsGuardedAsEveryManagementWrite: the browser's road to the client's publish (R-ANS-061,
// Q-ANS-140). A session's POST without its CSRF token, with a wrong one or from a foreign origin is refused by the authentication stage
// with the management API's own refusal and stores nothing; with the token the upload is published and the event names the session's
// user and no token; the session cookie alone does not reach the Galaxy publish route; the body's bound and the read-only maintenance
// mode refuse the session's upload as they refuse the client's. Integration, archives under a kilobyte and one body of 1 MiB and one byte
// (the bound is what is tested), about 4 s with the stack's start.
func TestR_ANS_061_TheScreensPublishWithASessionIsGuardedAsEveryManagementWrite(t *testing.T) {
	e := newEnvOpts(t, envOpts{env: []string{"QUADER_ANSIBLE_MAX_ARCHIVE_BYTES=1MiB"}})
	cookie, csrf := e.browserSession(e.bob)
	op := "/api/v1/projects/" + project + "/repositories/" + repo + "/ansible/collections"
	post := func(body []byte, hdr ...string) testenv.Answer {
		return e.st.Do(e.r, "POST", op, "", body, append([]string{"Content-Type", "application/gzip"}, hdr...)...)
	}
	problem := func(a testenv.Answer, status int, code string) bool {
		return a.Status == status && strings.Contains(a.Header.Get("Content-Type"), "problem+json") && strings.Contains(string(a.Body), `"code":"`+code+`"`)
	}
	archive := collectiontest.Build(spec("1.0.0"))

	for name, hdr := range map[string][]string{
		"no CSRF token":      {"Cookie", cookie, "Origin", browserOrigin},
		"a wrong CSRF token": {"Cookie", cookie, "Origin", browserOrigin, "X-CSRF-Token", strings.Repeat("x", len(csrf))},
		"a foreign origin":   {"Cookie", cookie, "Origin", "https://evil.example", "X-CSRF-Token", csrf},
	} {
		if a := post(archive, hdr...); !problem(a, 403, "csrf_failed") {
			t.Errorf("%s: %d %s %s", name, a.Status, a.Header.Get("Content-Type"), a.Body)
		}
	}
	if e.versionRows() != 0 || e.st.Number(`SELECT count(*) FROM ansible_import_tasks`) != 0 {
		t.Fatal("a refused session write stored a version or a task")
	}

	// The cookie alone is no credential of the protocol's write: the Galaxy publish answers as to a caller without one.
	body, ct := form(archive, true, "")
	galaxy := e.st.Do(e.r, "POST", base+"v3/artifacts/collections/", "", body, "Content-Type", ct, "Cookie", cookie, "Origin", browserOrigin, "X-CSRF-Token", csrf)
	if galaxy.Status != 401 || !strings.Contains(galaxy.Header.Get("WWW-Authenticate"), "Basic") || e.versionRows() != 0 {
		t.Errorf("the Galaxy publish with a session cookie: %d %v %s", galaxy.Status, galaxy.Header.Get("WWW-Authenticate"), galaxy.Body)
	}

	browser := []string{"Cookie", cookie, "Origin", browserOrigin, "X-CSRF-Token", csrf}
	if a := post(archive, browser...); a.Status != 201 || a.JSON(t)["state"] != "completed" {
		t.Fatalf("with the session and its token: %d %s", a.Status, a.Body)
	}
	if got := e.st.Text(`SELECT actor_id::text || ' ' || coalesce(token_id::text, '-') || ' ' || coalesce(auth_method, '-')
	                     FROM audit_event WHERE action = 'package.published'`); got != e.bob.ID+" - session" {
		t.Errorf("the event of the session's publish: %q, want bob, no token, session", got)
	}

	big := bytes.Repeat([]byte{0}, 1<<20+1)
	if a := post(big, browser...); !problem(a, 413, "payload_too_large") {
		t.Errorf("a body over ansible.max_archive_bytes with the session: %d %s", a.Status, a.Body)
	}
	if a := e.st.Do(e.r, "POST", base+"v3/artifacts/collections/", e.tokB, big, "Content-Type", "application/gzip"); a.Status != 413 {
		t.Errorf("the same body through the client's route: %d %s", a.Status, a.Body)
	}

	if a := e.st.Do(e.r, "PUT", "/api/v1/system/maintenance", e.tokAdmin, map[string]any{"mode": "read_only", "retryAfterSeconds": 120}); a.Status != 200 {
		t.Fatalf("maintenance on: %d %s", a.Status, a.Body)
	}
	v2 := collectiontest.Build(spec("1.1.0"))
	var during testenv.Answer
	waitFor(t, "the read-only mode", 3*time.Second, func() bool { during = post(v2, browser...); return during.Status == 503 })
	if during.Header.Get("Retry-After") != "120" {
		t.Errorf("the session's upload in maintenance: %d %v %s", during.Status, during.Header, during.Body)
	}
	if a := e.publish(e.tokB, v2, true); a.Status != 503 {
		t.Errorf("the client's publish in maintenance: %d %s", a.Status, a.Body)
	}
	if e.versionRows() != 1 {
		t.Errorf("rows after the refusals: %d", e.versionRows())
	}
}
