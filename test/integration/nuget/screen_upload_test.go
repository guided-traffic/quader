//go:build integration

package nuget

import (
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/auth/session"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_NUGET_006_APackageUploadedOnTheScreenIsPushedOnTheRolesTheChartRuns: the upload of the screen on an installation with split
// roles (R-NUGET-006, Q-NUGET-162, Q-ANS-140): a browser's session asks a replica of the role api for the upload token of a NuGet
// repository, which names the package and the symbol publish resources; the package and then its symbol package go with it to a replica
// of the role packages, whose 201 names the version in Quader-Package and Quader-Version; the dialog revokes the token. A client's
// token gets no upload token, nor does a remote repository. Integration, two archives under a kilobyte, about 2 s with the stack's start.
func TestR_NUGET_006_APackageUploadedOnTheScreenIsPushedOnTheRolesTheChartRuns(t *testing.T) {
	e := newWriteEnv(t)
	e.st.Repository("nuget-remote", "remote", "nuget", `{"remote":{"upstreamUrl":"https://nuget.example.invalid/v3/index.json"}}`)
	api := e.st.Start(t, testenv.ReplicaOptions{Name: "api-a", Role: "api"})
	pkg := e.st.Start(t, testenv.ReplicaOptions{Name: "packages-a", Role: "packages"})

	id, secret, csrf := session.NewID(), session.NewSecret(), session.NewSecret()
	e.st.Exec(`INSERT INTO ui_session (id, secret_sha256, csrf_sha256, principal_id, auth_method, authenticated_at, idle_timeout,
	                                   absolute_expires_at, created_addr, user_agent)
	           VALUES ($1::uuid, $2, $3, $4::uuid, 'oidc', now(), interval '1 hour', now() + interval '12 hours', '192.0.2.7', 'it-browser')`,
		id, session.Hash(secret), session.Hash(csrf), e.devID)
	browser := []string{"Cookie", session.CookieSession + "=" + secret, "Origin", "http://" + testenv.DefaultHost, "X-CSRF-Token", csrf}
	tokens := func(repo string, hdr ...string) testenv.Answer {
		return e.st.Do(api, "POST", "/api/v1/projects/it/repositories/"+repo+"/upload-tokens", "", nil, hdr...)
	}

	m := tokens("nuget-local", browser...)
	doc := m.JSON(t)
	if m.Status != 201 || doc["publishPath"] != "/api/nuget/it/nuget-local/v3/package" || doc["symbolsPath"] != "/api/nuget/it/nuget-local/v3/symbolpackage" {
		t.Fatalf("the token: %d %s", m.Status, m.Body)
	}
	tok := doc["token"].(string)
	push := e.st.Do(pkg, "PUT", doc["publishPath"].(string), tok, nupkg(t, pkgSpec{id: "Acme.Screen", version: "1.2.0-Beta.1"}), "Content-Type", "application/octet-stream")
	if push.Status != 201 || push.Header.Get("Quader-Package") != "Acme.Screen" || push.Header.Get("Quader-Version") != "1.2.0-Beta.1" || strings.Contains(string(push.Body), tok) {
		t.Fatalf("the push with the upload token: %d %v %s", push.Status, push.Header, push.Body)
	}
	sym := e.st.Do(pkg, "PUT", doc["symbolsPath"].(string), tok, snupkg(t, "Acme.Screen", "1.2.0-Beta.1", "screen", nil), "Content-Type", "application/octet-stream")
	if sym.Status != 201 {
		t.Fatalf("the symbol push with the upload token: %d %s", sym.Status, sym.Body)
	}
	if n := e.st.Number(`SELECT count(*) FROM nuget_symbol_package s JOIN nuget_version v ON v.version_id = s.version_id WHERE v.lower_id = 'acme.screen'`); n != 1 {
		t.Errorf("symbol packages: %d", n)
	}
	if got := e.st.Text(`SELECT k.description FROM audit_event a JOIN api_token k ON k.id = a.token_id WHERE a.action = 'package.published' AND a.path = 'acme.screen/1.2.0-beta.1'`); got != "Upload of a package from the screen into it/nuget-local" {
		t.Errorf("the event names the upload token: %q", got)
	}
	// The dialog revokes the token once the pushes answered: it is refused from then on.
	if a := e.st.Do(api, "DELETE", "/api/v1/tokens/"+doc["id"].(string), "", nil, browser...); a.Status != 204 {
		t.Fatalf("the revocation: %d %s", a.Status, a.Body)
	}
	// That the replicas refuse a revoked token is the token's own rule, held by the collection upload's case (R-ANS-061).
	if got := e.st.Text(`SELECT (revoked_at IS NOT NULL)::text FROM api_token WHERE id = $1::uuid`, doc["id"]); got != "true" {
		t.Errorf("the upload token is not revoked: %s", got)
	}

	if a := tokens("nuget-local", "Authorization", "Bearer "+e.tokDev); a.Status != 403 {
		t.Errorf("a client's token asks for an upload token: %d %s", a.Status, a.Body)
	}
	// A remote repository takes no deploy: the decision point refuses it before the operation would answer 405 not_applicable.
	if a := tokens("nuget-remote", browser...); a.Status != 403 && a.Status != 405 {
		t.Errorf("a remote repository: %d %s", a.Status, a.Body)
	}
}
