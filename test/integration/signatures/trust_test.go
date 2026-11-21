//go:build integration

package signatures

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/openpgp"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// The second package of stage 1 against the stack: the job signature-verify reads and checks what the hooks recorded, the trust step
// judges it against the anchors of a scope, and the management API writes anchors, blocks and the switch (signatures.md R-SIG-011,
// R-SIG-012, R-SIG-018 to R-SIG-021, R-SIG-034). The signatures are made in the test with a release key of its own (the owner's third
// way, R-SIG-042); the schemes themselves are tested at the unit tier in internal/signatures/scheme/....

// trustEnv is one installation with a web replica and a worker that runs the jobs, and six callers:
//
//	rita   an administrator
//	olga   an auditor (settings:read, content:metadata-read)
//	owen   the Owner of web
//	mia    a member of web (developer)
//	dave   holds no grant
//
// web/files-internal and ops/files are generic local repositories of two projects.
type trustEnv struct {
	t  *testing.T
	st *testenv.Stack
	r  *testenv.Replica

	tokRita, tokOlga, tokOwen, tokMia, tokDave string
}

func newTrustEnv(t *testing.T) *trustEnv {
	t.Helper()
	st := testenv.NewStack(t, testenv.StackOptions{Name: "signatures"})
	rita, olga, owen, mia, dave := st.User("rita"), st.User("olga"), st.User("owen"), st.User("mia"), st.User("dave")
	st.Bind("admin", rita)
	st.Bind("auditor", olga)
	st.Repository("web/files-internal", "local", "generic", `{}`)
	st.Project("ops")
	st.RepositoryIn("ops", "files", "local", "generic", `{}`)
	st.Member("web", owen, "owner")
	st.Member("web", mia, "developer")
	e := &trustEnv{t: t, st: st}
	e.tokRita = st.Token(rita, scope.Scope{Inherit: true, Admin: true})
	e.tokOlga = st.Token(olga, scope.Scope{Inherit: true, System: []string{"settings:read", "content:metadata-read"}})
	e.tokOwen = st.Token(owen, scope.Scope{Inherit: true, Admin: true})
	e.tokMia = st.Token(mia, scope.Default())
	e.tokDave = st.Token(dave, scope.Default())
	env := []string{"QUADER_API_RATE_LIMITS_ENABLED=false", "QUADER_JOBS_POLL_INTERVAL=200ms", "QUADER_CLUSTER_EPOCH_POLL_INTERVAL=500ms"}
	e.r = st.Start(t, testenv.ReplicaOptions{Name: "web1", Env: env})
	st.Start(t, testenv.ReplicaOptions{Name: "worker1", Role: "worker", Env: env})
	return e
}

func (e *trustEnv) put(token, path string, content []byte) {
	e.t.Helper()
	a := e.st.Do(e.r, "PUT", "/files/"+path, token, content, "Content-Type", "application/octet-stream")
	if a.Status != http.StatusCreated && a.Status != http.StatusOK {
		e.t.Fatalf("deploy %s: %d %s", path, a.Status, a.Body)
	}
}

// call sends a JSON request and requires the status.
func (e *trustEnv) call(method, path, token string, body any, want int, hdr ...string) testenv.Answer {
	e.t.Helper()
	if body != nil {
		hdr = append(hdr, "Content-Type", contentType(method))
	}
	a := e.st.Do(e.r, method, path, token, body, hdr...)
	if a.Status != want {
		e.t.Fatalf("%s %s: %d, want %d: %s", method, path, a.Status, want, a.Body)
	}
	return a
}

func contentType(method string) string {
	if method == http.MethodPatch {
		return "application/merge-patch+json"
	}
	return "application/json"
}

// waitFor polls a query until it answers want, at most 20 s: the jobs run on the worker.
func (e *trustEnv) waitFor(what, sql, want string, args ...any) {
	e.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	got := ""
	for time.Now().Before(deadline) {
		if got, _ = e.st.TryText(sql, args...); got == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	jobs, _ := e.st.TryText(`SELECT string_agg(kind || ':' || state::text || ':' || COALESCE(result::text, '') || ':' || COALESCE(error::text, ''), E'\n' ORDER BY created_at) FROM job WHERE kind LIKE 'signature%'`)
	cur, _ := e.st.TryText(`SELECT discovery_after::text || ' ' || COALESCE(discovery_pass_at::text, '-') FROM signature_settings`)
	e.t.Fatalf("%s: %q, want %q\njobs:\n%s\ndiscovery: %s", what, got, want, jobs, cur)
}

// badge answers the state of a stored file's signatures as a caller reads them (R-SIG-034).
func (e *trustEnv) badge(token, project, repo, path string) string {
	e.t.Helper()
	a := e.call("GET", "/api/v1/projects/"+project+"/repositories/"+repo+"/signatures?path="+path, token, nil, http.StatusOK)
	var rep struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(a.Body, &rep); err != nil {
		e.t.Fatal(err)
	}
	return rep.State
}

// releaseKey is a release key made by the test (Ed25519, milliseconds): its armored public certificate, its fingerprint and a detached
// signer.
type releaseKey struct {
	public []byte
	fp     string
	key    *openpgp.Key
}

func newReleaseKey(t *testing.T) releaseKey {
	t.Helper()
	secret, public, fp, err := openpgp.Generate("Release key", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	key, err := openpgp.ParseSecret(secret, nil, time.Now(), openpgp.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return releaseKey{public: public, fp: fp, key: key}
}

func (k releaseKey) sign(t *testing.T, data []byte) []byte {
	t.Helper()
	sig, err := openpgp.DetachSign(data, []*openpgp.Key{k.key}, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

const stateOf = `SELECT s.state FROM signatures s JOIN items i ON i.blob_sha256 = s.subject_sha256 JOIN repositories r ON r.id = i.repo_id
	WHERE r.key = $1 AND i.path = $2`

const installationTrust = `SELECT t.state FROM signature_trust t JOIN signatures s ON s.id = t.signature_id
	JOIN items i ON i.blob_sha256 = s.subject_sha256 JOIN repositories r ON r.id = i.repo_id
	WHERE r.key = $1 AND i.path = $2 AND t.project_id IS NULL`

// TestR_SIG_011_TheJobChecksASignatureAndTrustFollowsTheAnchors: a signature the hooks recorded is checked by the job on the worker
// (no_key without an anchor); an OpenPGP anchor added through the API makes it valid and internal within one run of the trust job, a
// block of its fingerprint blocked and the badge "signer not trusted", the block lifted internal again (R-SIG-011, R-SIG-012, R-SIG-020);
// a row removed is found again by discovery; a file replaced by other bytes makes its signature invalid, which the security dashboard's
// list shows (R-SIG-038). Integration, about 4 s, files of a kilobyte.
func TestR_SIG_011_TheJobChecksASignatureAndTrustFollowsTheAnchors(t *testing.T) {
	e := newTrustEnv(t)
	k := newReleaseKey(t)
	file := make([]byte, 1024)
	e.put(e.tokRita, "web/files-internal/tools/tool.tar.gz", file)
	e.put(e.tokRita, "web/files-internal/tools/tool.tar.gz.asc", k.sign(t, file))
	e.waitFor("the check without an anchor", stateOf, "no_key", "files-internal", "tools/tool.tar.gz")
	if got := e.badge(e.tokRita, "web", "files-internal", "tools/tool.tar.gz"); got != "untrusted" {
		t.Fatalf("badge without an anchor = %s, want untrusted", got)
	}

	a := e.call("POST", "/api/v1/signatures/anchors", e.tokRita, map[string]any{
		"key": "release-key", "title": "Release key", "kind": "openpgp", "openpgp": map[string]string{"keys": string(k.public)},
	}, http.StatusCreated)
	var anchor struct {
		ID           int64 `json:"id"`
		Fingerprints []struct {
			Value string `json:"value"`
		} `json:"fingerprints"`
	}
	if err := json.Unmarshal(a.Body, &anchor); err != nil || len(anchor.Fingerprints) == 0 {
		t.Fatalf("anchor %s: %v", a.Body, err)
	}
	e.waitFor("the integrity by the anchor's key", stateOf, "valid", "files-internal", "tools/tool.tar.gz")
	e.waitFor("the trust under the anchor", installationTrust, "internal", "files-internal", "tools/tool.tar.gz")
	if got := e.badge(e.tokRita, "web", "files-internal", "tools/tool.tar.gz"); got != "verified_internal" {
		t.Fatalf("badge = %s, want verified_internal", got)
	}

	t.Run("R-SIG-020 a block of the fingerprint acts on the signature at once", func(t *testing.T) {
		b := e.call("POST", "/api/v1/signatures/blocks", e.tokRita, map[string]string{
			"kind": "fingerprint", "value": k.fp, "reason": "the key left the build machine",
		}, http.StatusCreated)
		e.waitFor("the block", installationTrust, "blocked", "files-internal", "tools/tool.tar.gz")
		if got := e.badge(e.tokRita, "web", "files-internal", "tools/tool.tar.gz"); got != "untrusted" {
			t.Fatalf("badge of a blocked signer = %s, want untrusted", got)
		}
		var block struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(b.Body, &block); err != nil {
			t.Fatal(err)
		}
		e.call("DELETE", "/api/v1/signatures/blocks/"+strconv.FormatInt(block.ID, 10), e.tokRita, nil, http.StatusNoContent)
		e.waitFor("the block lifted", installationTrust, "internal", "files-internal", "tools/tool.tar.gz")
		if n := e.st.Number(`SELECT count(*) FROM audit_event WHERE action IN ('signature.signer.blocked', 'signature.signer.unblocked')`); n != 2 {
			t.Fatalf("%d events of the block, want 2", n)
		}
	})

	t.Run("R-SIG-011 a row removed is found again by discovery", func(t *testing.T) {
		e.st.Exec(`DELETE FROM signatures`)
		e.st.Exec(`UPDATE signature_settings SET discovery_after = 0, discovery_pass_at = now()`)
		// The task signature-verify runs discovery first (every five minutes by default); here it is run at once.
		e.call("POST", "/api/v1/system/tasks/signature-verify/run", e.tokRita, nil, http.StatusAccepted)
		e.waitFor("the signature found again", stateOf, "valid", "files-internal", "tools/tool.tar.gz")
	})

	t.Run("R-SIG-038 a file replaced by other bytes makes its signature invalid", func(t *testing.T) {
		e.put(e.tokRita, "web/files-internal/tools/tool.tar.gz", []byte("other bytes"))
		e.waitFor("the replaced file's signature", stateOf, "invalid", "files-internal", "tools/tool.tar.gz")
		if got := e.badge(e.tokRita, "web", "files-internal", "tools/tool.tar.gz"); got != "invalid" {
			t.Fatalf("badge = %s, want invalid", got)
		}
		f := e.call("GET", "/api/v1/signatures/findings?kind=invalid", e.tokOlga, nil, http.StatusOK)
		if !strings.Contains(string(f.Body), "digest_mismatch") || !strings.Contains(string(f.Body), "web/files-internal") {
			t.Fatalf("findings %s", f.Body)
		}
		if n := e.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'signature.invalid'`); n != 1 {
			t.Fatalf("%d events signature.invalid, want 1", n)
		}
		if f := e.call("GET", "/api/v1/signatures/findings?kind=invalid", e.tokDave, nil, http.StatusOK); strings.Contains(string(f.Body), "digest_mismatch") {
			t.Fatalf("a caller who reads no repository sees a finding: %s", f.Body)
		}
	})

	t.Run("R-SIG-016 removing the anchor leaves the integrity and drops the trust", func(t *testing.T) {
		e.put(e.tokRita, "web/files-internal/tools/b.bin", []byte("b"))
		e.put(e.tokRita, "web/files-internal/tools/b.bin.asc", k.sign(t, []byte("b")))
		e.waitFor("the second signature", installationTrust, "internal", "files-internal", "tools/b.bin")
		e.call("DELETE", "/api/v1/signatures/anchors/"+strconv.FormatInt(anchor.ID, 10), e.tokRita, nil, http.StatusNoContent)
		e.waitFor("the trust without the anchor", installationTrust, "untrusted", "files-internal", "tools/b.bin")
		if got := e.st.Text(stateOf, "files-internal", "tools/b.bin"); got != "valid" {
			t.Fatalf("integrity after the anchor went = %s, want valid", got)
		}
	})
}

// TestR_SIG_019_ProjectAnchorsApplyInTheirProjectOnly: with the switch off the Owner of web is refused; an auditor cannot change the
// switch; an administrator switches it on; the Owner's OpenPGP anchor makes the release key's signature internal in web and leaves the
// same bytes and signature in another project untrusted; the administrator lists every project's anchors; switched off again, the
// project anchor stays listed, not applied, and the signature reads untrusted (R-SIG-008, R-SIG-019 to R-SIG-021). Integration, about
// 4 s, bytes.
func TestR_SIG_019_ProjectAnchorsApplyInTheirProjectOnly(t *testing.T) {
	e := newTrustEnv(t)
	k := newReleaseKey(t)
	file := []byte("tool")
	asc := k.sign(t, file)
	for _, at := range []string{"web/files-internal", "ops/files"} {
		e.put(e.tokRita, at+"/tool.bin", file)
		e.put(e.tokRita, at+"/tool.bin.asc", asc)
	}
	e.waitFor("the check without an anchor", stateOf, "no_key", "files-internal", "tool.bin")
	projectAnchor := map[string]any{"key": "release-key", "title": "Release key", "kind": "openpgp", "openpgp": map[string]string{"keys": string(k.public)}}

	e.call("POST", "/api/v1/projects/web/signatures/anchors", e.tokOwen, projectAnchor, http.StatusForbidden)
	settings := e.call("GET", "/api/v1/signatures/settings", e.tokOlga, nil, http.StatusOK)
	etag := settings.Header.Get("ETag")
	e.call("PATCH", "/api/v1/signatures/settings", e.tokOlga, map[string]bool{"projectAnchors": true}, http.StatusForbidden, "If-Match", etag)
	e.call("PATCH", "/api/v1/signatures/settings", e.tokRita, map[string]bool{"projectAnchors": true}, http.StatusOK, "If-Match", etag)

	created := e.call("POST", "/api/v1/projects/web/signatures/anchors", e.tokOwen, projectAnchor, http.StatusCreated)
	e.waitFor("the integrity by the project's key", stateOf, "valid", "files-internal", "tool.bin")
	e.waitFor("the trust in web", `SELECT t.state FROM signature_trust t JOIN project p ON p.id = t.project_id WHERE p.key = 'web'`, "internal")
	if got := e.badge(e.tokOwen, "web", "files-internal", "tool.bin"); got != "verified_internal" {
		t.Fatalf("badge in web = %s, want verified_internal", got)
	}
	if got := e.badge(e.tokRita, "ops", "files", "tool.bin"); got != "untrusted" {
		t.Fatalf("badge in ops = %s, want untrusted", got)
	}
	if l := e.call("GET", "/api/v1/signatures/anchors?scope=project", e.tokRita, nil, http.StatusOK); !strings.Contains(string(l.Body), `"release-key"`) {
		t.Fatalf("the administrator's list of project anchors: %s", l.Body)
	}
	e.call("GET", "/api/v1/projects/web/signatures/anchors", e.tokMia, nil, http.StatusOK)
	e.call("POST", "/api/v1/signatures/blocks", e.tokOwen, map[string]string{"kind": "fingerprint", "value": k.fp, "reason": "no"}, http.StatusForbidden)

	settings = e.call("GET", "/api/v1/signatures/settings", e.tokRita, nil, http.StatusOK)
	e.call("PATCH", "/api/v1/signatures/settings", e.tokRita, map[string]bool{"projectAnchors": false}, http.StatusOK, "If-Match", settings.Header.Get("ETag"))
	e.waitFor("the project's rows dropped", `SELECT count(*) FROM signature_trust WHERE project_id IS NOT NULL`, "0")
	if got := e.badge(e.tokOwen, "web", "files-internal", "tool.bin"); got != "untrusted" {
		t.Fatalf("badge in web with the switch off = %s, want untrusted", got)
	}
	l := e.call("GET", "/api/v1/projects/web/signatures/anchors", e.tokOwen, nil, http.StatusOK)
	if !strings.Contains(string(l.Body), `"applied":false`) {
		t.Fatalf("a stored project anchor with the switch off: %s", l.Body)
	}
	var a struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(created.Body, &a); err != nil {
		t.Fatal(err)
	}
	e.call("PATCH", "/api/v1/projects/web/signatures/anchors/"+strconv.FormatInt(a.ID, 10), e.tokOwen, map[string]string{"title": "Renamed"}, http.StatusForbidden, "If-Match", "*")
	e.call("DELETE", "/api/v1/signatures/anchors/"+strconv.FormatInt(a.ID, 10), e.tokRita, nil, http.StatusNoContent)
}

// TestR_SIG_018_TheConfigurationsAnchorsAreAppliedByTheMigration: quader migrate applies the shipped anchors and a CA anchor of the
// configuration; the API lists the latter read-only and refuses its removal with 409 anchor_managed; a shipped anchor can be switched
// off and nothing else; an anchor no longer declared is removed at the next migration; an invalid certificate stops the migration with
// the anchor's key named (R-SIG-016 to R-SIG-018). Integration, about 2 s, one certificate.
func TestR_SIG_018_TheConfigurationsAnchorsAreAppliedByTheMigration(t *testing.T) {
	e := newTrustEnv(t)
	ca := caPEM(t)
	declare := func(anchors map[string]config.SignatureAnchor) error {
		return e.st.DeclareWith(func(c *config.Config) { c.Signatures.Anchors = anchors })
	}
	if err := declare(map[string]config.SignatureAnchor{
		"company-ca": {Title: "Company CA", Kind: "ca", CA: &config.SignatureAnchorCA{Certificates: ca}},
	}); err != nil {
		t.Fatalf("declare: %v", err)
	}
	if n := e.st.Number(`SELECT count(*) FROM signature_anchors WHERE scope = 'shipped'`); n != 6 {
		t.Fatalf("%d shipped anchors, want 6", n)
	}
	l := e.call("GET", "/api/v1/signatures/anchors?scope=configuration", e.tokOlga, nil, http.StatusOK)
	var list struct {
		Items []struct {
			ID    int64  `json:"id"`
			Key   string `json:"key"`
			Scope string `json:"scope"`
		} `json:"items"`
	}
	if err := json.Unmarshal(l.Body, &list); err != nil || len(list.Items) != 1 || list.Items[0].Key != "company-ca" {
		t.Fatalf("configuration anchors %s: %v", l.Body, err)
	}
	e.call("DELETE", "/api/v1/signatures/anchors/"+strconv.FormatInt(list.Items[0].ID, 10), e.tokRita, nil, http.StatusConflict)

	shipped := e.st.Text(`SELECT id::text FROM signature_anchors WHERE scope = 'shipped' AND key = 'sigstore-public'`)
	one := e.call("GET", "/api/v1/signatures/anchors/"+shipped, e.tokRita, nil, http.StatusOK)
	e.call("PATCH", "/api/v1/signatures/anchors/"+shipped, e.tokRita, map[string]string{"title": "Mine"}, http.StatusConflict,
		"If-Match", one.Header.Get("ETag"))
	e.call("PATCH", "/api/v1/signatures/anchors/"+shipped, e.tokRita, map[string]bool{"enabled": false}, http.StatusOK,
		"If-Match", one.Header.Get("ETag"))

	if err := declare(nil); err != nil {
		t.Fatalf("declare none: %v", err)
	}
	if n := e.st.Number(`SELECT count(*) FROM signature_anchors WHERE scope = 'configuration'`); n != 0 {
		t.Fatalf("%d configuration anchors after the declaration went, want 0", n)
	}
	if got := e.st.Text(`SELECT enabled::text FROM signature_anchors WHERE key = 'sigstore-public'`); got != "false" {
		t.Fatalf("a migration switched the shipped anchor back on: %s", got)
	}
	err := declare(map[string]config.SignatureAnchor{
		"broken-ca": {Title: "Broken", Kind: "ca", CA: &config.SignatureAnchorCA{Certificates: "not a certificate"}},
	})
	if err == nil || !strings.Contains(err.Error(), "broken-ca") {
		t.Fatalf("an invalid declaration: %v, want an error naming broken-ca", err)
	}
}

// caPEM is a CA certificate made by the test (ECDSA P-256), valid for a day.
func caPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Company CA", Organization: []string{"Example GmbH"}},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
