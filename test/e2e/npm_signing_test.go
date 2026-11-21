//go:build e2e

package e2e

import (
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/signatures/shipped"
	"github.com/guided-traffic/quader/internal/signing/signingtest"
)

// The keys of signatures.md R-SIG-050's end-to-end case: a remote of the counting test upstream that announces the registry key of
// registry.npmjs.org which signed left-pad's recorded versions, and a virtual repository over npm-local and that remote. Both are the
// case's own, so that no keys document another test stored of npm-remote stands in its way.
const (
	npmSignedRemoteKey = "npm-signed-remote"
	npmSignedKey       = "npm-signed"
	// npmjsLeftPadKeyID is the key of registry.npmjs.org that signed left-pad's recorded versions, from the release's snapshot of npm's keys.
	npmjsLeftPadKeyID = "SHA256:jl3bwswu80PjjokCgh0o2w5c2U4LhQAE57gj9cz1kzA"
)

// TestNPMSignatures_R_SIG_050_NpmAuditSignaturesVerifiesALocalAndAnUpstreamPackage: with a signing identity of the installation, a
// package npm publishes to npm-local through the virtual repository carries Quader's registry signature, and `npm audit signatures` in
// a project that installed it and left-pad of the test upstream through that virtual repository exits 0 with both verified: Quader's
// key and the upstream's in the union of the virtual repository's keys (R-SIG-050, H-1176). The identity is imported by the test and
// removed at its end, so the other cases see an installation without one. npm reads a registry's keys from the public Sigstore TUF
// repository first (H-1184): the client pod reaches it. End to end, about 6 s beside the publish and the install, a package of a few
// hundred bytes and left-pad's recorded tarball.
func TestNPMSignatures_R_SIG_050_NpmAuditSignaturesVerifiesALocalAndAnUpstreamPackage(t *testing.T) {
	fx := npmFx(t)
	adm := fx.adm
	sc := stub(t)
	start := time.Now() // the fixture and the scatter profile are the set's; what follows is the case's own cost
	defer func() {
		t.Logf("the case took %s beside the set's fixture", time.Since(start).Round(100*time.Millisecond))
	}()

	keys := npmjsKeysDocument(t)
	sc.send(t, http.MethodPost, "/-/stub/keys", keys)
	t.Cleanup(func() { sc.send(t, http.MethodPost, "/-/stub/keys", nil) })

	for _, body := range []map[string]any{
		{"key": npmSignedRemoteKey, "class": "remote", "format": "npm", "remote": map[string]any{"url": "https://" + fx.stubHost + "/"}},
		{
			"key": npmSignedKey, "class": "virtual", "format": "npm",
			"virtual": map[string]any{"members": []string{npmLocalKey, npmSignedRemoteKey}, "deployTarget": npmLocalKey},
		},
	} {
		r, err := adm.send(http.MethodPost, reposRoute, body)
		if err != nil {
			t.Fatal(err)
		}
		r.want(t, "create "+body["key"].(string), 201, 409)
	}
	ensurePolicy(t, adm, npmPublisher+"-signed", []string{npmSignedRemoteKey, npmSignedKey}, [2]any{"sa:" + npmPublisher, []string{"read"}})

	ca := signingtest.NewCA(t, "Quader end-to-end")
	leaf, key := ca.Issue(t, signingtest.Leaf{})
	var installed struct {
		ID       int64  `json:"id"`
		State    string `json:"state"`
		NpmKeyID string `json:"npmKeyId"`
	}
	adm.do(http.MethodPost, "/api/v1/signing/identity/import", map[string]any{
		"key": string(signingtest.KeyPEM(t, key)), "chain": string(ca.ChainPEM(leaf)), "ca": string(ca.RootPEM()),
	}).want(t, "import the signing identity", 201).JSON(t, &installed)
	t.Cleanup(func() {
		adm.do(http.MethodDelete, "/api/v1/signing/identity", nil).want(t, "remove the signing identity", 204, 404)
	})
	if installed.State != "current" {
		t.Fatalf("the imported identity is %s; another identity is current on this installation", installed.State)
	}

	c := newNPMClient(t, fx, "npm12", 0)
	c.registryKey = npmSignedKey
	name := c.pkg("signed", true)
	c.publish(name, "1.0.0")
	deadline := time.Now().Add(30 * time.Second)
	for {
		var d struct {
			Versions map[string]struct {
				Dist struct {
					Signatures []struct {
						KeyID string `json:"keyid"`
					} `json:"signatures"`
				} `json:"dist"`
			} `json:"versions"`
		}
		r := fx.publisherGet(t, npmAddr(npmSignedKey)+"/"+url.PathEscape(name))
		if r.Status == http.StatusOK && json.Unmarshal(r.Body, &d) == nil {
			if s := d.Versions["1.0.0"].Dist.Signatures; len(s) == 1 && s[0].KeyID == installed.NpmKeyID {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s@1.0.0 carries no signature of the identity %s after 30 s: %s", name, installed.NpmKeyID, r)
		}
		time.Sleep(250 * time.Millisecond)
	}

	dir := c.project("audit-signatures", map[string]any{"dependencies": map[string]string{name: "1.0.0", "left-pad": "1.3.0"}})
	c.configure(dir)
	c.must(dir, "install", "--no-audit")
	r := c.run(dir, "audit", "signatures")
	t.Logf("npm audit signatures exited %d in %s:\n%s", r.Code, r.Dur.Round(10*time.Millisecond), tailLines(r.Out, 20))
	if r.Code != 0 || !strings.Contains(r.Out, "2 packages have verified registry signatures") {
		t.Fatalf("npm audit signatures exited %d:\n%s", r.Code, tailLines(r.Out, 40))
	}
}

// npmjsKeysDocument is the keys document of registry.npmjs.org's key that signed left-pad's recorded versions, as a registry answers
// -/npm/v1/keys, from the release's snapshot of npm's keys; announced without its expiry, as R-SIG-028's integration case does.
func npmjsKeysDocument(t testing.TB) map[string]any {
	t.Helper()
	keys, err := shipped.NPMKeys(shipped.Release().NPMKeys)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keys.Keys {
		if k.KeyID != npmjsLeftPadKeyID {
			continue
		}
		b, _ := pem.Decode([]byte(k.PEM))
		if b == nil {
			t.Fatal("the shipped key does not decode")
		}
		return map[string]any{"keys": []map[string]any{{
			"expires": nil, "keyid": k.KeyID, "keytype": "ecdsa-sha2-nistp256", "scheme": "ecdsa-sha2-nistp256",
			"key": base64.StdEncoding.EncodeToString(b.Bytes),
		}}}
	}
	t.Fatalf("the release's npm keys hold no %s", npmjsLeftPadKeyID)
	return nil
}
