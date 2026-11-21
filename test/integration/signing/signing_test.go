//go:build integration

// Package signing is the integration area of Quader's signing identity (signatures.md R-SIG-046, R-SIG-047, R-SIG-048, R-SIG-055,
// R-SIG-059) against the stack: the identity of the files of the Helm values, loaded, rotated and refused by the worker without a
// restart; the uploaded identity as a key request, the CA's certificate and an import; the switch after the announce period; removal and
// retirement; the permissions and the public material. The material is made by a company CA of the test in process
// (internal/signing/signingtest); a second CA of the test stands for a signing service's own CA. The replay of a withdrawal into a
// restored database is the area backup's (TestR_SIG_059_AWithdrawnIdentityDoesNotComeBackWithARestore).
package signing

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sigstore/sigstore-go/pkg/root"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/secrets"
	"github.com/guided-traffic/quader/internal/signing/key"
	"github.com/guided-traffic/quader/internal/signing/signingtest"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// env is one installation with a web replica and a worker, and three callers: an administrator, a security-admin and an auditor.
type env struct {
	t   *testing.T
	st  *testenv.Stack
	web *testenv.Replica
	wkr *testenv.Replica

	admin, secadm, auditor string
	// pub is the token of a publisher with read and deploy on npm-local.
	pub string
}

// baseEnv is what every replica of the area takes: a fast job poll, a fast change log, an announce period of a second.
func baseEnv() []string {
	return []string{
		"QUADER_API_RATE_LIMITS_ENABLED=false", "QUADER_JOBS_POLL_INTERVAL=200ms", "QUADER_CLUSTER_EPOCH_POLL_INTERVAL=500ms",
		"QUADER_SIGNING_IDENTITY_ANNOUNCE_PERIOD=1s", "QUADER_SIGNING_IDENTITY_RELOAD_INTERVAL=1s",
	}
}

func newEnv(t *testing.T, name string, webEnv, workerEnv []string) *env {
	t.Helper()
	st := testenv.NewStack(t, testenv.StackOptions{Name: name})
	rita, sam, olga := st.User("rita"), st.User("sam"), st.User("olga")
	st.Bind("admin", rita)
	st.Bind("security-admin", sam)
	st.Bind("auditor", olga)
	e := &env{t: t, st: st}
	e.admin = st.Token(rita, scope.Scope{Inherit: true, Admin: true})
	e.secadm = st.Token(sam, scope.Scope{Inherit: true, System: []string{"settings:read", "security-settings:write"}})
	e.auditor = st.Token(olga, scope.Scope{Inherit: true, System: []string{"settings:read"}})
	pia := st.User("pia")
	st.Repository(npmLocal, "local", "npm", `{"local":{"autoClaim":false,"packageOwnership":"enforce"}}`)
	st.Policy("npm-publishers", []string{"read", "deploy"}, []string{npmLocal}, pia)
	e.pub = st.Token(pia, scope.Default())
	e.web = st.Start(t, testenv.ReplicaOptions{Name: "web1", Env: append(baseEnv(), webEnv...)})
	if workerEnv != nil {
		e.wkr = st.Start(t, testenv.ReplicaOptions{Name: "worker1", Role: "worker", Env: append(baseEnv(), workerEnv...)})
	}
	return e
}

// call sends a JSON request and requires the status.
func (e *env) call(method, path, token string, body any, want int) testenv.Answer {
	e.t.Helper()
	var hdr []string
	if body != nil {
		hdr = []string{"Content-Type", "application/json"}
	}
	a := e.st.Do(e.web, method, path, token, body, hdr...)
	if a.Status != want {
		e.t.Fatalf("%s %s: %d, want %d: %s", method, path, a.Status, want, a.Body)
	}
	if strings.Contains(string(a.Body), "PRIVATE KEY") {
		e.t.Fatalf("%s %s answers key material", method, path)
	}
	return a
}

// problemCode is the code of a problem answer.
func problemCode(t *testing.T, a testenv.Answer) string {
	t.Helper()
	var p struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(a.Body, &p)
	return p.Code
}

// identity is a signing identity as the API answers it.
type identity struct {
	ID          int64      `json:"id"`
	Source      string     `json:"source"`
	State       string     `json:"state"`
	Fingerprint string     `json:"fingerprint"`
	NpmKeyID    string     `json:"npmKeyId"`
	Csr         *string    `json:"csr"`
	ExpiresAt   *time.Time `json:"expiresAt"`
	Names       []string   `json:"names"`
}

type overview struct {
	State     string     `json:"state"`
	Managed   bool       `json:"managed"`
	Current   *identity  `json:"current"`
	Announced *identity  `json:"announced"`
	Pending   *identity  `json:"pending"`
	Previous  []identity `json:"previous"`
	Refused   *struct {
		Reason string `json:"reason"`
	} `json:"refused"`
}

func (e *env) overview() overview {
	e.t.Helper()
	var o overview
	if err := json.Unmarshal(e.call("GET", "/api/v1/signing/identity", e.admin, nil, http.StatusOK).Body, &o); err != nil {
		e.t.Fatal(err)
	}
	return o
}

// waitFor polls the identity until cond holds, at most 20 s: the switch and the files are the worker's.
func (e *env) waitFor(what string, cond func(overview) bool) overview {
	e.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var o overview
	for time.Now().Before(deadline) {
		if o = e.overview(); cond(o) {
			return o
		}
		time.Sleep(100 * time.Millisecond)
	}
	jobs, _ := e.st.TryText(`SELECT string_agg(kind || ':' || state::text || ':' || COALESCE(error::text, ''), E'\n' ORDER BY created_at) FROM job WHERE kind = 'signing'`)
	b, _ := json.Marshal(o)
	log := ""
	if e.wkr != nil {
		log = testenv.Tail(e.wkr.Logs.String(), 20)
	}
	e.t.Fatalf("%s: %s\njobs:\n%s\nworker:\n%s", what, b, jobs, log)
	return o
}

// writePair writes the files of the Helm values (tls.crt with the leaf and its intermediate, tls.key, ca.crt with the root) into a new
// directory and swaps the link dir/current to it in one rename, as the kubelet swaps a Secret's volume.
func writePair(t *testing.T, dir string, chain, keyPEM, root []byte) {
	t.Helper()
	gen, err := os.MkdirTemp(dir, "gen-")
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string][]byte{"tls.crt": chain, "tls.key": keyPEM, "ca.crt": root} {
		if err := os.WriteFile(filepath.Join(gen, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tmp := filepath.Join(dir, "current.tmp")
	if err := os.Symlink(filepath.Base(gen), tmp); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, "current")); err != nil {
		t.Fatal(err)
	}
}

// TestR_SIG_046_TheIdentityOfTheFilesIsLoadedRotatedAndRefusedWithoutARestart: a pair in the files becomes current at the worker's start
// and the admin page names it from the Helm values; a key request answers 409 signing_identity_managed; a publish to npm-local is signed
// with the key of the files and verifies with pacote's check against npm-local's keys; a pair of a new key written over
// the files is announced and then current, the old one previous with its end of signing, without a restart; a pair with an RSA key is
// refused with signing.identity.refused and the admin page's refusal while the identity in use stays current; the chain anyone may fetch
// is the current one's. Integration, about 4 s, certificates of a kilobyte.
func TestR_SIG_046_TheIdentityOfTheFilesIsLoadedRotatedAndRefusedWithoutARestart(t *testing.T) {
	dir := t.TempDir()
	ca := signingtest.NewCA(t, "Example")
	leafA, keyA := ca.Issue(t, signingtest.Leaf{})
	writePair(t, dir, ca.ChainPEM(leafA), signingtest.KeyPEM(t, keyA), ca.RootPEM())
	files := []string{
		"QUADER_SIGNING_IDENTITY_CERT_FILE=" + filepath.Join(dir, "current", "tls.crt"), "QUADER_SIGNING_IDENTITY_KEY_FILE=" + filepath.Join(dir, "current", "tls.key"),
		"QUADER_SIGNING_IDENTITY_CA_FILE=" + filepath.Join(dir, "current", "ca.crt"),
	}
	e := newEnv(t, "signingfiles", files, files)

	o := e.waitFor("the pair of the files is current", func(o overview) bool { return o.Current != nil })
	if o.State != "configuration" || !o.Managed || o.Current.Fingerprint != fingerprint(leafA) {
		t.Fatalf("after the start: %+v", o)
	}
	if code := problemCode(t, e.call("POST", "/api/v1/signing/identity/key", e.admin, map[string]any{}, http.StatusConflict)); code != "signing_identity_managed" {
		t.Errorf("a key request answers %s", code)
	}
	// The key of the files signs: a publish to npm-local carries the identity's keyid and verifies against npm-local's keys (R-SIG-050).
	e.publish("files-signed", "1.0.0")
	e.eventually("files-signed@1.0.0 is signed with the key of the files", func() bool {
		return slices.Equal(e.doc(npmLocal, "files-signed").keyIDsOf("1.0.0"), []string{o.Current.NpmKeyID})
	})
	e.verified(npmLocal, "files-signed", "1.0.0")

	leafB, keyB := ca.Issue(t, signingtest.Leaf{})
	writePair(t, dir, ca.ChainPEM(leafB), signingtest.KeyPEM(t, keyB), ca.RootPEM())
	o = e.waitFor("the new pair is current", func(o overview) bool { return o.Current != nil && o.Current.Fingerprint == fingerprint(leafB) })
	if len(o.Previous) != 1 || o.Previous[0].Fingerprint != fingerprint(leafA) || o.Previous[0].ExpiresAt == nil {
		t.Errorf("the old pair: %+v", o.Previous)
	}
	if n := e.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'signing.identity.switched'`); n != 1 {
		t.Errorf("%d switches recorded", n)
	}

	rsaLeaf, rsaKey := rsaPair(t, ca)
	writePair(t, dir, ca.ChainPEM(rsaLeaf), rsaKey, ca.RootPEM())
	o = e.waitFor("the RSA pair is refused", func(o overview) bool { return o.Refused != nil })
	if !strings.Contains(o.Refused.Reason, "key_type") || o.Current == nil || o.Current.Fingerprint != fingerprint(leafB) {
		t.Errorf("after the refusal: %+v", o)
	}
	deadline := time.Now().Add(10 * time.Second)
	for e.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'signing.identity.refused'`) != 1 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if n := e.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'signing.identity.refused'`); n != 1 {
		t.Errorf("%d refusals recorded, want one per content", n)
	}
	chain := e.call("GET", "/api/v1/signing/chain.pem", "", nil, http.StatusOK)
	if b, _ := pem.Decode(chain.Body); b == nil || fingerprintDER(b.Bytes) != fingerprint(leafB) {
		t.Errorf("the chain does not start with the current leaf")
	}
	if strings.Contains(e.wkr.Logs.String(), "PRIVATE KEY") {
		t.Error("the worker logged key material")
	}
}

// TestR_SIG_047_AnUploadedIdentityIsAKeyRequestACertificateAndAnImport, with R-SIG-048's removal and retirement and R-SIG-055's
// permissions: a key request answers a request that parses, holds a P-256 key, the base URL as its URI and the code-signing use; the CA's
// certificate for it is accepted and the identity is current at once; its sealed key opens under its own row with the installation's key
// ring and under no other; a second key request is announced, a certificate for another key answers 422 signing_key_mismatch, the right
// one is announced and switched; an import of a key and a chain becomes current the same way; retiring a previous identity drops its
// sealed key; removing the current one leaves no current identity, a trusted root of the previous ones and no chain; an auditor reads and
// is refused every write, a security-admin writes, an anonymous caller gets the public material and 401 on the identity. No answer, audit
// event or log line holds a private key. Integration, about 6 s, certificates of a kilobyte.
func TestR_SIG_047_AnUploadedIdentityIsAKeyRequestACertificateAndAnImport(t *testing.T) {
	e := newEnv(t, "signingupload", nil, []string{})
	ca := signingtest.NewCA(t, "Example")
	service := signingtest.NewCA(t, "Signing service")

	e.call("GET", "/api/v1/signing/identity", "", nil, http.StatusUnauthorized)
	e.call("GET", "/api/v1/signing/trusted-root.json", "", nil, http.StatusNotFound)
	e.call("GET", "/api/v1/signing/identity", e.auditor, nil, http.StatusOK)
	e.call("POST", "/api/v1/signing/identity/key", e.auditor, map[string]any{}, http.StatusForbidden)

	first := e.createKey(e.admin)
	if code := problemCode(t, e.call("POST", "/api/v1/signing/identity/key", e.admin, map[string]any{}, http.StatusConflict)); code != "signing_identity_pending" {
		t.Errorf("a second key request answers %s", code)
	}
	e.call("PUT", "/api/v1/signing/identity/certificate", e.auditor, map[string]any{"chain": "x"}, http.StatusForbidden)
	leaf1 := ca.IssueRequest(t, *first.Csr)
	var installed identity
	unmarshal(t, e.call("PUT", "/api/v1/signing/identity/certificate", e.admin, map[string]any{"chain": string(ca.ChainPEM(leaf1)), "ca": string(ca.RootPEM())}, http.StatusOK), &installed)
	if installed.State != "current" || installed.Fingerprint != fingerprint(leaf1) {
		t.Fatalf("the first identity: %+v", installed)
	}

	ring := e.st.KeyRing(t)
	sealed := e.st.Text(`SELECT sealed_key FROM signing_identities WHERE id = $1`, first.ID)
	opened, err := key.Open(ring, first.ID, sealed)
	if err != nil || !opened.Public().Equal(leaf1.PublicKey) {
		t.Fatalf("the sealed key does not open under its row: %v", err)
	}
	if _, err := key.Open(ring, first.ID+1, sealed); err == nil {
		t.Error("the sealed key opens under another row")
	} else if !strings.Contains(err.Error(), secrets.ErrAuthentication.Error()) {
		t.Errorf("another row: %v", err)
	}

	second := e.createKey(e.secadm)
	stranger, _ := ca.Issue(t, signingtest.Leaf{})
	if code := problemCode(t, e.call("PUT", "/api/v1/signing/identity/certificate", e.admin, map[string]any{"chain": string(ca.ChainPEM(stranger)), "ca": string(ca.RootPEM())}, http.StatusUnprocessableEntity)); code != "signing_key_mismatch" {
		t.Errorf("a certificate for another key answers %s", code)
	}
	leaf2 := service.IssueRequest(t, *second.Csr)
	unmarshal(t, e.call("PUT", "/api/v1/signing/identity/certificate", e.admin, map[string]any{"chain": string(signingtest.PEM(leaf2, service.Intermediate, service.Root))}, http.StatusOK), &installed)
	if installed.State != "announced" {
		t.Fatalf("the second identity: %+v", installed)
	}
	o := e.waitFor("the second identity is switched", func(o overview) bool { return o.Current != nil && o.Current.ID == second.ID })
	if len(o.Previous) != 1 || o.Previous[0].ID != first.ID || o.Previous[0].ExpiresAt == nil {
		t.Errorf("after the switch: %+v", o.Previous)
	}

	imported, importedLeaf := e.importIdentity(ca)
	if imported.State != "announced" {
		t.Fatalf("the import: %+v", imported)
	}
	e.waitFor("the import is switched", func(o overview) bool { return o.Current != nil && o.Current.ID == imported.ID })

	e.call("POST", "/api/v1/signing/identities/"+strconv.FormatInt(first.ID, 10)+"/retire", e.auditor, nil, http.StatusForbidden)
	var retired identity
	unmarshal(t, e.call("POST", "/api/v1/signing/identities/"+strconv.FormatInt(first.ID, 10)+"/retire", e.admin, nil, http.StatusOK), &retired)
	if retired.State != "retired" || e.st.Text(`SELECT (sealed_key IS NULL)::text FROM signing_identities WHERE id = $1`, first.ID) != "true" {
		t.Errorf("the retired identity: %+v", retired)
	}
	if n := e.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'signing.identity.retired'`); n != 1 {
		t.Errorf("%d retirements recorded", n)
	}

	e.call("DELETE", "/api/v1/signing/identity", e.auditor, nil, http.StatusForbidden)
	e.call("DELETE", "/api/v1/signing/identity", e.admin, nil, http.StatusNoContent)
	o = e.overview()
	if o.State != "none" || o.Current != nil || len(o.Previous) != 2 {
		t.Errorf("after the removal: %+v", o)
	}
	e.call("GET", "/api/v1/signing/chain.pem", "", nil, http.StatusNotFound)
	tr := e.call("GET", "/api/v1/signing/trusted-root.json", "", nil, http.StatusOK)
	parsed, err := root.NewTrustedRootFromJSON(tr.Body)
	if err != nil || len(parsed.FulcioCertificateAuthorities()) != 2 {
		t.Errorf("the trusted root of the previous identities: %v", err)
	}
	_ = importedLeaf

	e.call("POST", "/api/v1/signing/identity/key", e.secadm, map[string]any{}, http.StatusCreated)
	e.call("DELETE", "/api/v1/signing/identity/pending", e.auditor, nil, http.StatusForbidden)
	e.call("DELETE", "/api/v1/signing/identity/pending", e.secadm, nil, http.StatusNoContent)

	if n := e.st.Number(`SELECT count(*) FROM audit_event WHERE action LIKE 'signing.%' AND details::text LIKE '%PRIVATE%'`); n != 0 {
		t.Errorf("%d audit events hold key material", n)
	}
	if n := e.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'signing.identity.installed'`); n != 3 {
		t.Errorf("%d installations recorded, want 3", n)
	}
	for _, r := range []*testenv.Replica{e.web, e.wkr} {
		if s := r.Logs.String(); strings.Contains(s, "PRIVATE KEY") || strings.Contains(s, "correct horse") {
			t.Errorf("%s logged key material", r.Name)
		}
	}
}

// createKey asks for a key with the defaults and checks the request: a P-256 key, the base URL as its URI, the code-signing use.
func (e *env) createKey(token string) identity {
	e.t.Helper()
	var id identity
	unmarshal(e.t, e.call("POST", "/api/v1/signing/identity/key", token, map[string]any{"subject": map[string]any{"O": "Example GmbH", "C": "DE"}}, http.StatusCreated), &id)
	if id.State != "pending" || id.Csr == nil {
		e.t.Fatalf("the pending key: %+v", id)
	}
	b, _ := pem.Decode([]byte(*id.Csr))
	if b == nil {
		e.t.Fatal("the request is no PEM")
	}
	csr, err := x509.ParseCertificateRequest(b.Bytes)
	if err != nil || csr.CheckSignature() != nil {
		e.t.Fatalf("the request: %v", err)
	}
	pub, ok := csr.PublicKey.(*ecdsa.PublicKey)
	codeSigning := false
	for _, x := range csr.Extensions {
		codeSigning = codeSigning || (x.Id.String() == "2.5.29.37" && strings.Contains(string(x.Value), "\x2b\x06\x01\x05\x05\x07\x03\x03"))
	}
	if !ok || pub.Curve != elliptic.P256() || len(csr.URIs) != 1 || csr.URIs[0].String() != "http://quader.test" || !codeSigning || csr.Subject.CommonName != "Quader signing" {
		e.t.Fatalf("the request asks for %v %v %v code signing %v", csr.PublicKey, csr.URIs, csr.Subject, codeSigning)
	}
	return id
}

// importIdentity imports an encrypted PKCS#8 key and its chain of the CA.
func (e *env) importIdentity(ca *signingtest.CA) (identity, *x509.Certificate) {
	e.t.Helper()
	leaf, k := ca.Issue(e.t, signingtest.Leaf{URIs: []string{"https://quader.example.com/import"}})
	var id identity
	unmarshal(e.t, e.call("POST", "/api/v1/signing/identity/import", e.admin, map[string]any{
		"key": string(encryptedKey(e.t, k, "correct horse")), "passphrase": "correct horse", "chain": string(ca.ChainPEM(leaf)), "ca": string(ca.RootPEM()),
	}, http.StatusCreated), &id)
	return id, leaf
}

func unmarshal(t *testing.T, a testenv.Answer, v any) {
	t.Helper()
	if err := json.Unmarshal(a.Body, v); err != nil {
		t.Fatalf("%d %s: %v", a.Status, a.Body, err)
	}
}
