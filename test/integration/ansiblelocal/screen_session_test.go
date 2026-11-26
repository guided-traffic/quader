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

// TestR_ANS_061_TheScreensUploadWithASessionIsGuardedAsEveryManagementWrite: the browser's road on an installation with split roles
// (R-ANS-061, Q-ANS-140). The session asks the role api for the upload token: without its CSRF token, with a wrong one or from a
// foreign origin the authentication stage refuses it with the management API's own refusal, and no token and no version exist; with
// the token the session gets an upload token, created via the UI, and the publish with it on the role packages names the session's
// user. The session cookie alone does not reach the Galaxy publish route; the body's bound and the read-only maintenance mode hold on
// the browser's road as on the client's. Integration, archives under a kilobyte and one body of 1 MiB and one byte (the bound is what
// is tested), about 4 s with the stack's start.
func TestR_ANS_061_TheScreensUploadWithASessionIsGuardedAsEveryManagementWrite(t *testing.T) {
	e := newEnvOpts(t, envOpts{split: true, env: []string{"QUADER_ANSIBLE_MAX_ARCHIVE_BYTES=1MiB"}})
	cookie, csrf := e.browserSession(e.bob)
	browser := screenUpload{e: e, api: e.api, pkg: e.pkg, hdr: []string{"Cookie", cookie, "Origin", browserOrigin, "X-CSRF-Token", csrf}}
	archive := collectiontest.Build(spec("1.0.0"))
	uploadTokens := func() int64 {
		return e.st.Number(`SELECT count(*) FROM api_token WHERE description LIKE 'Upload of a package from the screen%'`)
	}

	for name, hdr := range map[string][]string{
		"no CSRF token":      {"Cookie", cookie, "Origin", browserOrigin},
		"a wrong CSRF token": {"Cookie", cookie, "Origin", browserOrigin, "X-CSRF-Token", strings.Repeat("x", len(csrf))},
		"a foreign origin":   {"Cookie", cookie, "Origin", "https://evil.example", "X-CSRF-Token", csrf},
	} {
		if a := (screenUpload{e: e, api: e.api, pkg: e.pkg, hdr: hdr}).mint(repo); !isProblem(a, 403, "csrf_failed") {
			t.Errorf("%s: %d %s %s", name, a.Status, a.Header.Get("Content-Type"), a.Body)
		}
	}
	if uploadTokens() != 0 {
		t.Fatal("a refused session write issued an upload token")
	}

	// The cookie alone is no credential of the protocol's write: the Galaxy publish answers as to a caller without one.
	body, ct := form(archive, true, "")
	galaxy := e.st.Do(e.pkg, "POST", base+"v3/artifacts/collections/", "", body, "Content-Type", ct, "Cookie", cookie, "Origin", browserOrigin, "X-CSRF-Token", csrf)
	if galaxy.Status != 401 || !strings.Contains(galaxy.Header.Get("WWW-Authenticate"), "Basic") || e.versionRows() != 0 {
		t.Errorf("the Galaxy publish with a session cookie: %d %v %s", galaxy.Status, galaxy.Header.Get("WWW-Authenticate"), galaxy.Body)
	}

	a, _ := browser.publish(repo, archive)
	if a.Status != 202 || browser.task(repo, a).JSON(t)["state"] != "completed" {
		t.Fatalf("with the session and its token: %d %s", a.Status, a.Body)
	}
	if got := e.st.Text(`SELECT a.actor_id::text || ' ' || k.created_via FROM audit_event a JOIN api_token k ON k.id = a.token_id
	                     WHERE a.action = 'package.published'`); got != e.bob.ID+" ui" {
		t.Errorf("the event of the session's publish: %q, want bob with a token created via the UI", got)
	}

	big := bytes.Repeat([]byte{0}, 1<<20+1)
	if a, _ := browser.publish(repo, big); a.Status != 413 {
		t.Errorf("a body over ansible.max_archive_bytes with the upload token: %d %s", a.Status, a.Body)
	}
	if a := e.st.Do(e.pkg, "POST", base+"v3/artifacts/collections/", e.tokB, big, "Content-Type", "application/gzip"); a.Status != 413 {
		t.Errorf("the same body with the client's token: %d %s", a.Status, a.Body)
	}

	m := browser.mint(repo)
	if m.Status != 201 {
		t.Fatalf("a second upload token: %d %s", m.Status, m.Body)
	}
	if a := e.st.Do(e.api, "PUT", "/api/v1/system/maintenance", e.tokAdmin, map[string]any{"mode": "read_only", "retryAfterSeconds": 120}); a.Status != 200 {
		t.Fatalf("maintenance on: %d %s", a.Status, a.Body)
	}
	tok, _ := m.JSON(t)["token"].(string)
	// Each replica reads the mode within its bound: the probe is an archive that is no collection, which stores nothing either way.
	waitFor(t, "the read-only mode on the role packages", 3*time.Second, func() bool {
		return e.st.Do(e.pkg, "POST", base+"v3/artifacts/collections/", tok, []byte("probe"), "Content-Type", "application/gzip").Status == 503
	})
	if a := e.st.Do(e.pkg, "POST", base+"v3/artifacts/collections/", tok, collectiontest.Build(spec("1.1.0")), "Content-Type", "application/gzip"); a.Status != 503 {
		t.Errorf("the screen's publish in maintenance: %d %s", a.Status, a.Body)
	}
	var minted testenv.Answer
	waitFor(t, "the read-only mode on the role api", 3*time.Second, func() bool { minted = browser.mint(repo); return minted.Status == 503 })
	if minted.Header.Get("Retry-After") != "120" {
		t.Errorf("an upload token in maintenance: %d %v %s", minted.Status, minted.Header, minted.Body)
	}
	if e.versionRows() != 1 {
		t.Errorf("rows after the refusals: %d", e.versionRows())
	}
}
