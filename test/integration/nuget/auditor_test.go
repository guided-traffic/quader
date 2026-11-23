//go:build integration

package nuget

import (
	"testing"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// auditorScope is the scope of an auditor's token that carries the metadata read
// (R-AUTHZ-013: a token exercises a role's capability only through its system items).
func auditorScope() scope.Scope {
	return scope.Scope{Inherit: true, System: []string{"content:metadata-read", "repositories:read-config", "projects:read"}}
}

// auditorEnv is the write installation with a user bound to auditor directly, a
// member of a group bound to auditor through authz.role_bindings, a generic repository
// with a raw file and one pushed NuGet package.
type auditorEnv struct {
	*wenv
	r                             *testenv.Replica
	tokAud, tokMem, tokMemDefault string
}

func newAuditorEnv(t *testing.T) *auditorEnv {
	t.Helper()
	e := &auditorEnv{wenv: newWriteEnv(t)}
	aud, mem := e.st.User("aud"), e.st.User("mem", "gh-auditors")
	e.st.Bind("auditor", aud)
	if err := e.st.Declare(config.AuthzRoleBinding{Role: "auditor", Group: "gh-auditors"}); err != nil {
		t.Fatal(err)
	}
	e.st.Repository("gen-local", "local", "generic", "{}")
	e.st.Policy("gen-writers", []string{"read", "deploy"}, []string{"gen-local"}, e.st.User("gen-dev"))
	e.tokAud, e.tokMem = e.st.Token(aud, auditorScope()), e.st.Token(mem, auditorScope())
	e.tokMemDefault = e.st.Token(mem, scope.Default())
	e.r = e.replica("web1")
	e.mustPush(e.r, "nuget-local", e.tokDev, nupkg(t, pkgSpec{id: "Acme.Widgets", version: "1.2.3", tags: "widgets"}))
	if a := e.st.Do(e.r, "PUT", "/files/it/gen-local/a/b.bin", e.tokAdmin, []byte("raw")); a.Status != 201 {
		t.Fatalf("raw upload: %d %s", a.Status, a.Body)
	}
	return e
}
