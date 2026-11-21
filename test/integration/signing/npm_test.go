//go:build integration

package signing

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha1" //nolint:gosec // dist.shasum is a SHA-1
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/signing"
	"github.com/guided-traffic/quader/internal/signing/signingtest"
	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/npmstub"
)

// The npm half of stage 2 against the stack (signatures.md R-SIG-048 to R-SIG-050, R-SIG-055): a local repository npm-local, a remote
// npm-remote of the npm test upstream, which announces a key of its own, and a virtual npm-all of both that publishes to npm-local.
const (
	npmLocal  = "npm-local"
	npmRemote = "npm-remote"
	npmAll    = "npm-all"
)

// stubKeys is the npm test upstream's keys document.
const stubKeys = `{"keys":[{"expires":null,"keyid":"SHA256:stub-key","keytype":"ecdsa-sha2-nistp256","scheme":"ecdsa-sha2-nistp256","key":"MFkw"}]}`

// newNPMEnv is the installation of the npm cases: newEnv's callers and npm-local, with the npm test upstream behind npm-remote and
// npm-all over both.
func newNPMEnv(t *testing.T) *env {
	t.Helper()
	stub, err := npmstub.New(npmstub.Config{})
	if err != nil {
		t.Fatal(err)
	}
	stub.SetKeys([]byte(stubKeys))
	ss, err := npmstub.NewSelfSigned("localhost", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	run, err := stub.Start("127.0.0.1:0", ss.ServerConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = run.Close() })
	caFile := filepath.Join(t.TempDir(), "stub-ca.pem")
	if err := os.WriteFile(caFile, ss.CAPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	st := testenv.NewStack(t, testenv.StackOptions{Name: "signingnpm", Env: []string{
		"QUADER_PROXY_OUTBOUND_TLS_CA_FILES=[" + strconv.Quote(caFile) + "]", `QUADER_PROXY_OUTBOUND_ADDRESS_POLICY_ALLOW_CIDRS=["127.0.0.1/32"]`,
		"QUADER_VULNERABILITIES_SOURCES_OSV_MODE=off",
	}})
	rita, sam, olga, pia := st.User("rita"), st.User("sam"), st.User("olga"), st.User("pia")
	st.Bind("admin", rita)
	e := &env{t: t, st: st}
	e.admin = st.Token(rita, scope.Scope{Inherit: true, Admin: true})
	e.secadm = st.Token(sam, scope.Scope{Inherit: true, Admin: true})
	e.auditor = st.Token(olga, scope.Scope{Inherit: true, System: []string{"settings:read"}})
	st.Repository(npmLocal, "local", "npm", `{"local":{"autoClaim":false,"packageOwnership":"enforce"}}`)
	st.Repository(npmRemote, "remote", "npm", remoteConfig(t, run.URL+"/"))
	st.Repository(npmAll, "virtual", "npm", `{}`)
	for i, m := range []string{npmLocal, npmRemote} {
		st.Exec(`INSERT INTO virtual_members (virtual_id, position, member_id)
			SELECT v.id, $2, m.id FROM repositories v, repositories m WHERE v.key = $1 AND m.key = $3`, npmAll, i, m)
	}
	st.Exec(`INSERT INTO virtual_deploy_targets (virtual_id, target_id) SELECT v.id, t.id FROM repositories v, repositories t WHERE v.key = $1 AND t.key = $2`,
		npmAll, npmLocal)
	st.Policy("npm-publishers", []string{"read", "deploy"}, []string{npmLocal}, pia)
	st.Policy("npm-readers", []string{"read"}, []string{npmRemote, npmAll}, pia)
	e.pub = st.Token(pia, scope.Default())
	e.web = st.Start(t, testenv.ReplicaOptions{Name: "web1", Env: baseEnv()})
	e.wkr = st.Start(t, testenv.ReplicaOptions{Name: "worker1", Role: "worker", Env: baseEnv()})
	return e
}

// remoteConfig is the canonical document of a remote repository of the test upstream, every member present.
func remoteConfig(t *testing.T, url string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"remote": map[string]any{
		"url": url, "extraHosts": []any{}, "authMode": "none", "username": "", "password": nil, "clientCertificate": nil, "headers": []any{},
		"forwardAuthOnRedirect": false, "outboundProxy": map[string]any{"mode": "direct"}, "connectTimeoutMs": 5000, "readTimeoutMs": 15000,
		"sourceNetwork": nil, "cacheFillAction": "read",
		"cache": map[string]any{
			"storeArtifacts": true, "metadataTtlSeconds": 3600, "metadataRefreshTimeoutSeconds": 10, "notFoundTtlSeconds": 60,
			"maxStaleSeconds": 0, "unusedRetentionHours": 0,
		},
		"integrity":    map[string]any{"checksumPolicy": "verify-if-available", "blockUnexpectedContentTypes": true, "allowedContentTypes": []any{}},
		"availability": map[string]any{"mode": "online", "failureThreshold": 3, "circuitOpenSeconds": 30},
		"limits":       map[string]any{"maxArtifactBytes": nil, "maxMetadataBytes": nil, "maxConcurrentFetches": 16},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// publish publishes name@version to npm-local, a tarball of a few hundred bytes, and answers when the publish answered.
func (e *env) publish(name, version string) time.Time {
	e.t.Helper()
	pj, _ := json.Marshal(map[string]any{"name": name, "version": version})
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for n, c := range map[string]string{"package/package.json": string(pj), "package/README.md": "# " + name + "\n"} {
		_ = tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(c)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(c))
	}
	_ = tw.Close()
	_ = zw.Close()
	tgz := buf.Bytes()
	s512, s1 := sha512.Sum512(tgz), sha1.Sum(tgz) //nolint:gosec // see the import
	base := name[strings.LastIndex(name, "/")+1:]
	doc := map[string]any{
		"_id": name, "name": name, "dist-tags": map[string]string{"latest": version},
		"versions": map[string]any{version: map[string]any{
			"name": name, "version": version, "_id": name + "@" + version,
			"dist": map[string]any{"integrity": "sha512-" + base64.StdEncoding.EncodeToString(s512[:]), "shasum": hex.EncodeToString(s1[:])},
		}},
		"_attachments": map[string]any{fmt.Sprintf("%s-%s.tgz", base, version): map[string]any{
			"content_type": "application/octet-stream", "data": base64.StdEncoding.EncodeToString(tgz), "length": len(tgz),
		}},
	}
	a := e.st.Do(e.web, "PUT", "/api/npm/it/"+npmLocal+"/"+strings.ReplaceAll(name, "/", "%2f"), e.pub, doc, "Content-Type", "application/json")
	if a.Status != http.StatusCreated {
		e.t.Fatalf("publish %s@%s: %d %s", name, version, a.Status, a.Body)
	}
	return time.Now()
}

// npmDoc is what the cases read of a package document.
type npmDoc struct {
	Time     map[string]time.Time `json:"time"`
	Versions map[string]struct {
		Dist struct {
			Integrity  string                 `json:"integrity"`
			Signatures []signing.NPMSignature `json:"signatures"`
		} `json:"dist"`
	} `json:"versions"`
}

func (e *env) doc(repo, name string, hdr ...string) npmDoc {
	e.t.Helper()
	a := e.st.Do(e.web, "GET", "/api/npm/it/"+repo+"/"+strings.ReplaceAll(name, "/", "%2f"), e.pub, nil, hdr...)
	var d npmDoc
	if a.Status != http.StatusOK || json.Unmarshal(a.Body, &d) != nil {
		e.t.Fatalf("the document of %s in %s: %d %s", name, repo, a.Status, a.Body)
	}
	return d
}

// keyIDsOf are the keyids of a version's signatures in a document.
func (d npmDoc) keyIDsOf(version string) []string {
	out := []string{}
	for _, s := range d.Versions[version].Dist.Signatures {
		out = append(out, s.KeyID)
	}
	return out
}

// npmKey is an entry of a keys document.
type npmKey struct {
	KeyID   string  `json:"keyid"`
	Key     string  `json:"key"`
	Expires *string `json:"expires"`
}

func (e *env) keys(repo string) []npmKey {
	e.t.Helper()
	a := e.st.Do(e.web, "GET", "/api/npm/it/"+repo+"/-/npm/v1/keys", e.pub, nil)
	var d struct {
		Keys []npmKey `json:"keys"`
	}
	if a.Status != http.StatusOK || json.Unmarshal(a.Body, &d) != nil {
		e.t.Fatalf("the keys of %s: %d %s", repo, a.Status, a.Body)
	}
	return d.Keys
}

func keyIDs(keys []npmKey) []string {
	out := []string{}
	for _, k := range keys {
		out = append(out, k.KeyID)
	}
	return out
}

// eventually polls cond every 100 ms for at most 20 s.
func (e *env) eventually(what string, cond func() bool) time.Duration {
	e.t.Helper()
	start := time.Now()
	for time.Since(start) < 20*time.Second {
		if cond() {
			return time.Since(start)
		}
		time.Sleep(100 * time.Millisecond)
	}
	jobs, _ := e.st.TryText(`SELECT string_agg(kind || ':' || state::text || ':' || COALESCE(error::text, ''), E'\n' ORDER BY created_at) FROM job WHERE kind = 'signing'`)
	work, _ := e.st.TryText(`SELECT string_agg(kind || ':' || item_id || ':' || reason || ':' || attempts || ':' || COALESCE(code, ''), E'\n') FROM signing_work`)
	e.t.Fatalf("%s: not within 20 s\njobs:\n%s\nwork:\n%s\nworker:\n%s", what, jobs, work, testenv.Tail(e.wkr.Logs.String(), 30))
	return 0
}

// verified checks every signature of a version of a document with pacote's check against the keys of the repository.
func (e *env) verified(repo, name, version string) {
	e.t.Helper()
	d := e.doc(repo, name)
	sigs := d.Versions[version].Dist.Signatures
	if len(sigs) == 0 {
		e.t.Fatalf("%s@%s in %s carries no signature", name, version, repo)
	}
	keys := map[string]string{}
	for _, k := range e.keys(repo) {
		keys[k.KeyID] = k.Key
	}
	for _, s := range sigs {
		if err := signing.VerifyNPM(keys[s.KeyID], signing.NPMMessage(name, version, d.Versions[version].Dist.Integrity), s.Sig); err != nil {
			e.t.Errorf("%s@%s in %s, key %s: %v", name, version, repo, s.KeyID, err)
		}
	}
}

// TestR_SIG_050_QuaderSignsTheVersionsOfLocalNpmRepositories: the npm half of stage 2 in one installation, step by step. Before an
// identity exists a local version carries no signature and the keys of npm-local are none; with the first identity, discovery signs
// what was published before (R-SIG-049) and a publish carries its signature shortly after its answer (R-SIG-049); the documents of
// npm-local and npm-all carry the current keyid and pacote's check passes against each one's keys, npm-all's keys hold the upstream's
// key and Quader's, npm-remote's the upstream's alone, and the signatures answer names the identity (R-SIG-050, R-SIG-055); after a
// rotation the old signature stays with its key's expiry after the version's time and a new publish carries the new keyid; a retirement
// takes the old key and its signatures off the wire at once and the job signs those versions again; a removal leaves no key and no
// signature (R-SIG-048). Integration, about 10 s, the npm test upstream, packages of a few hundred bytes.
func TestR_SIG_050_QuaderSignsTheVersionsOfLocalNpmRepositories(t *testing.T) {
	e := newNPMEnv(t)
	ca := signingtest.NewCA(t, "Example")

	e.publish("early", "1.0.0")
	if d := e.doc(npmLocal, "early"); d.Versions["1.0.0"].Dist.Signatures != nil {
		t.Fatalf("a version carries a signature before an identity exists: %+v", d)
	}
	if k := e.keys(npmLocal); len(k) != 0 {
		t.Fatalf("the keys of npm-local before an identity: %+v", k)
	}
	if n := e.st.Number(`SELECT count(*) FROM signing_work`); n != 0 {
		t.Errorf("a publish before an identity left %d rows of work", n)
	}

	first, _ := e.importIdentity(ca)
	if first.State != "current" {
		t.Fatalf("the first identity: %+v", first)
	}

	t.Run("R-SIG-049 discovery signs a version published before the identity existed", func(t *testing.T) {
		e.eventually("early@1.0.0 is signed", func() bool {
			return slices.Equal(e.doc(npmLocal, "early").keyIDsOf("1.0.0"), []string{first.NpmKeyID})
		})
		e.verified(npmLocal, "early", "1.0.0")
	})

	t.Run("R-SIG-049 a publish is signed right after its answer", func(t *testing.T) {
		answered := e.publish("@acme/ui", "1.0.0")
		e.eventually("@acme/ui@1.0.0 is signed", func() bool { return len(e.doc(npmLocal, "@acme/ui").keyIDsOf("1.0.0")) == 1 })
		took := time.Since(answered)
		t.Logf("the signature was in the document %s after the publish's answer", took.Round(10*time.Millisecond))
		if took > 5*time.Second {
			t.Errorf("signed %s after the answer", took)
		}
	})

	t.Run("R-SIG-050 the documents and the keys of every class", func(t *testing.T) {
		for _, repo := range []string{npmLocal, npmAll} {
			if got := e.doc(repo, "@acme/ui").keyIDsOf("1.0.0"); !slices.Equal(got, []string{first.NpmKeyID}) {
				t.Errorf("%s: keyids %v", repo, got)
			}
			e.verified(repo, "@acme/ui", "1.0.0")
		}
		if got := e.doc(npmLocal, "@acme/ui", "Accept", "application/vnd.npm.install-v1+json").keyIDsOf("1.0.0"); !slices.Equal(got, []string{first.NpmKeyID}) {
			t.Errorf("the abbreviated document: keyids %v", got)
		}
		if got := keyIDs(e.keys(npmAll)); !slices.Equal(got, []string{first.NpmKeyID, "SHA256:stub-key"}) {
			t.Errorf("the keys of npm-all: %v", got)
		}
		if got := keyIDs(e.keys(npmRemote)); !slices.Equal(got, []string{"SHA256:stub-key"}) {
			t.Errorf("the keys of npm-remote: %v", got)
		}
		a := e.st.Do(e.web, "GET", "/api/v1/projects/it/repositories/"+npmLocal+"/signatures?path=@acme/ui/-/ui-1.0.0.tgz", e.admin, nil)
		if a.Status != http.StatusOK || !strings.Contains(string(a.Body), `"source":"issued"`) || !strings.Contains(string(a.Body), `"identity":`+strconv.FormatInt(first.ID, 10)) {
			t.Errorf("the signatures answer of the tarball: %d %s", a.Status, a.Body)
		}
	})

	t.Run("R-SIG-062 Quader's own signature never passes a repository's gate", func(t *testing.T) {
		// The gate of npm-local on: the version Quader signed carries no publisher's signature and no provenance, so its tarball is
		// refused as missing, while its document still lists it with Quader's signature (Q-SIG-78, Q-SIG-89); then the gate off again.
		patch := func(body string) {
			t.Helper()
			path := "/api/v1/projects/it/repositories/" + npmLocal
			cur := e.st.Do(e.web, "GET", path, e.admin, nil)
			if a := e.st.Do(e.web, "PATCH", path, e.admin, body, "Content-Type", "application/merge-patch+json", "If-Match", cur.Header.Get("ETag")); a.Status != http.StatusOK {
				t.Fatalf("PATCH %s %s: %d %s", path, body, a.Status, a.Body)
			}
		}
		patch(`{"signatures":{"onlyVerified":true}}`)
		e.eventually("the gate refuses the tarball Quader signed", func() bool {
			return e.st.Do(e.web, "GET", "/api/npm/it/"+npmLocal+"/@acme/ui/-/ui-1.0.0.tgz", e.pub, nil).Status == http.StatusForbidden
		})
		if got := e.st.Text(`SELECT COALESCE(string_agg(f.reason, ','), '') FROM signature_refusals f JOIN repositories r ON r.id = f.repo_id WHERE r.key = $1`, npmLocal); got != "missing" {
			t.Errorf("the refused download listed %q, want missing", got)
		}
		if got := e.doc(npmLocal, "@acme/ui").keyIDsOf("1.0.0"); !slices.Equal(got, []string{first.NpmKeyID}) {
			t.Errorf("the gated document: keyids %v", got)
		}
		patch(`{"signatures":{"onlyVerified":false}}`)
	})

	t.Run("R-SIG-048 a rotation keeps the old signature and signs new versions with the new key", func(t *testing.T) {
		second, _ := e.importIdentity(ca)
		if second.State != "announced" {
			t.Fatalf("the second identity: %+v", second)
		}
		if got := keyIDs(e.keys(npmLocal)); !slices.Contains(got, second.NpmKeyID) || !slices.Contains(got, first.NpmKeyID) {
			t.Errorf("the keys during the announce period: %v", got)
		}
		e.waitFor("the second identity is switched", func(o overview) bool { return o.Current != nil && o.Current.ID == second.ID })
		e.eventually("the keys list the old key with its expiry", func() bool {
			for _, k := range e.keys(npmLocal) {
				if k.KeyID == first.NpmKeyID {
					return k.Expires != nil
				}
			}
			return false
		})
		e.publish("@acme/ui", "1.1.0")
		e.eventually("@acme/ui@1.1.0 carries the new keyid", func() bool {
			return slices.Equal(e.doc(npmLocal, "@acme/ui").keyIDsOf("1.1.0"), []string{second.NpmKeyID})
		})
		d := e.doc(npmLocal, "@acme/ui")
		if got := d.keyIDsOf("1.0.0"); !slices.Equal(got, []string{first.NpmKeyID}) {
			t.Errorf("1.0.0 after the rotation: keyids %v", got)
		}
		for _, k := range e.keys(npmLocal) {
			if k.KeyID != first.NpmKeyID {
				continue
			}
			exp, err := time.Parse(signing.NPMExpiresLayout, *k.Expires)
			if err != nil || !d.Time["1.0.0"].Before(exp) {
				t.Errorf("1.0.0 was published at %s, the old key expires %v (%v)", d.Time["1.0.0"], k.Expires, err)
			}
		}
		e.verified(npmLocal, "@acme/ui", "1.0.0")
		e.verified(npmLocal, "@acme/ui", "1.1.0")

		t.Run("R-SIG-048 a retirement takes the old key off the wire and the job signs its versions again", func(t *testing.T) {
			e.call("POST", "/api/v1/signing/identities/"+strconv.FormatInt(first.ID, 10)+"/retire", e.admin, nil, http.StatusOK)
			e.eventually("the old key leaves the keys", func() bool { return !slices.Contains(keyIDs(e.keys(npmLocal)), first.NpmKeyID) })
			e.eventually("the old versions carry the new keyid", func() bool {
				return slices.Equal(e.doc(npmLocal, "@acme/ui").keyIDsOf("1.0.0"), []string{second.NpmKeyID}) &&
					slices.Equal(e.doc(npmLocal, "early").keyIDsOf("1.0.0"), []string{second.NpmKeyID})
			})
			e.verified(npmAll, "@acme/ui", "1.0.0")
			// Its receipt attestations are made again at the same priority by the kind attestation, so they may follow the npm signatures.
			e.eventually("no attachment of the retired identity's signatures stays", func() bool {
				return e.st.Number(`SELECT count(*) FROM signature_attachments a JOIN signatures s ON s.id = a.signature_id WHERE s.identity_id = $1`, first.ID) == 0
			})
		})

		t.Run("R-SIG-048 a removal leaves no key and no signature on the wire", func(t *testing.T) {
			e.call("DELETE", "/api/v1/signing/identity", e.admin, nil, http.StatusNoContent)
			e.eventually("the keys are none", func() bool { return len(e.keys(npmLocal)) == 0 })
			e.eventually("the documents carry no signature", func() bool {
				d := e.doc(npmAll, "@acme/ui")
				return d.Versions["1.0.0"].Dist.Signatures == nil && d.Versions["1.1.0"].Dist.Signatures == nil
			})
			if got := keyIDs(e.keys(npmAll)); !slices.Equal(got, []string{"SHA256:stub-key"}) {
				t.Errorf("the keys of npm-all without an identity: %v", got)
			}
		})
	})
}
