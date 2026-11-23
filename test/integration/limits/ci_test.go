//go:build integration

package limits

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/ci"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/oidcstub"
)

// R-LIM-001: a CI token on the request path is charged to its CI project, ci:<issuer>:<project>: a GitLab job's ID token of the
// issuer double (the one the area ci runs) is exchanged for a CI token (R-CI-007), which downloads the file once; the request is
// recorded under the subject ci with the issuer's ID and the job's project_id, and nothing under an address. About 6 s, one file of
// 64 KiB.
func TestR_LIM_001_ACITokenIsChargedToItsCIProject(t *testing.T) {
	const audience, issuer = "http://" + testenv.DefaultHost, "gitlab-main"
	stub, err := oidcstub.New(oidcstub.Config{Audience: audience})
	if err != nil {
		t.Fatal(err)
	}
	run, ca, err := stub.StartSelfSigned()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = run.Close() })
	bundle := filepath.Join(t.TempDir(), "gitlab-ca.crt")
	if err := os.WriteFile(bundle, ca.CAPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	// The issuer double listens on the loopback address, which the egress policy denies by default.
	e := newEnvWith(t, envOptions{
		valkey: true, env: []string{`QUADER_PROXY_OUTBOUND_ADDRESS_POLICY_ALLOW_CIDRS=["127.0.0.0/8","::1/128"]`},
		seed: func(st *testenv.Stack) { st.Policy("ci-files", nil, []string{rFiles}) },
	})
	if a := e.st.Do(e.r, "POST", "/api/v1/ci/issuers", e.admin, map[string]any{
		"name": issuer, "kind": "gitlab", "issuer": stub.Issuer(), "publicIssuer": false, "caBundleRef": "file:" + bundle,
	}); a.Status != 201 {
		t.Fatalf("the issuer: %d %s", a.Status, a.Body)
	}
	if a := e.st.Do(e.r, "POST", "/api/v1/ci/issuers/"+issuer+"/mappings", e.admin, map[string]any{
		"name": "files", "priority": 10, "match": map[string]any{"claim": "namespace_id", "equals": "4711"},
		"grants": map[string]any{"policies": []string{"ci-files"}},
	}); a.Status != 201 {
		t.Fatalf("the mapping: %d %s", a.Status, a.Body)
	}
	// The exchange answers the mapping's grants from the replica's grant index (R-AUTHZ-028).
	want := e.st.Number(`SELECT epoch FROM cluster_epoch WHERE area = $1`, cluster.AreaAuthz)
	idx, _ := e.r.App.Authorizer().(*authz.Engine)
	if idx == nil {
		t.Fatal("the replica has no grant index")
	}
	testenv.WaitFor(t, "the replica's grant index at the mapping's epoch", 10*time.Second, func() bool { return idx.Epoch() >= want })
	subject, err := stub.Mint(oidcstub.MintOptions{}) // project_id 1234, namespace_id 4711
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"grant_type": {ci.GrantTypeTokenExchange}, "subject_token_type": {ci.TokenTypeIDToken}, "subject_token": {subject}}
	a := e.st.Do(e.r, "POST", "/api/v1/auth/oidc/token", "", []byte(form.Encode()), "Content-Type", "application/x-www-form-urlencoded")
	token, _ := a.JSON(t)["access_token"].(string)
	if a.Status != 200 || token == "" {
		t.Fatalf("the exchange: %d %s", a.Status, a.Body)
	}
	if a := e.st.Do(e.r, "GET", e.filePath, token, nil); a.Status != 200 {
		t.Fatalf("the CI token's download: %d %s", a.Status, a.Body)
	}
	ref := e.st.Text(`SELECT id::text FROM ci_issuers WHERE name = $1`, issuer) + ":1234"
	testenv.WaitFor(t, "the CI project's request in the usage store", 10*time.Second, func() bool { return e.usageOf("ci", ref) >= 1 })
	if n := e.usageOf("ci", ref); n != 1 {
		t.Fatalf("the CI project %s was charged %d requests, want the one download", ref, n)
	}
	if n := e.st.Number(`SELECT COALESCE(sum(requests), 0) FROM usage_bucket WHERE granularity = '5m' AND subject_kind = 'anonymous'`); n != 0 {
		t.Fatalf("%d requests were charged to an address", n)
	}
}
