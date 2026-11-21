//go:build integration

package npm

import (
	"encoding/json"
	"testing"

	"github.com/guided-traffic/quader/test/npmstub"
)

// TestR_SIG_028_TheKeysAndTheAttestationsOfARemoteAreServed: through npm-remote the keys endpoint answers the stub's keys and the
// attestations endpoint the stub's document of a version, each fetched once for two requests and kept for the metadata period; an
// upstream without attestations of a version answers 404. Through npm-all the keys are the union of the members' keys, and the local
// member adds none; a local repository answers no keys. Integration, about 3 s, the npm stub, bytes.
func TestR_SIG_028_TheKeysAndTheAttestationsOfARemoteAreServed(t *testing.T) {
	r := newRemoteEnv(t, remoteOptions{})
	keys := `{"keys":[{"expires":null,"keyid":"SHA256:stub-key","keytype":"ecdsa-sha2-nistp256","scheme":"ecdsa-sha2-nistp256","key":"MFkw"}]}`
	r.stub.SetKeys([]byte(keys))
	p := upstreamPackage(t, 1)
	spec := p.Name + "@" + p.Versions[0].Version
	att := `{"attestations":[{"predicateType":"https://slsa.dev/provenance/v1","bundle":{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json"}}]}`
	r.stub.SetAttestations(spec, []byte(att))
	rep := r.start(1)[0]

	for range 2 {
		a := r.get(rep, rRemote, "-/npm/v1/keys", r.tokA)
		if a.Status != 200 || string(a.Body) != keys {
			t.Fatalf("the keys of npm-remote: %d %s", a.Status, a.Body)
		}
		b := r.get(rep, rRemote, "-/npm/v1/attestations/"+spec, r.tokA)
		if b.Status != 200 || string(b.Body) != att {
			t.Fatalf("the attestations of %s: %d %s", spec, b.Status, b.Body)
		}
	}
	c := r.stub.Counts()
	if n := c.Signatures(npmstub.KindKeys, ""); n != 1 {
		t.Errorf("the upstream's keys were asked %d times, want once", n)
	}
	if n := c.Signatures(npmstub.KindAttestations, spec); n != 1 {
		t.Errorf("the upstream's attestations were asked %d times, want once", n)
	}
	if a := r.get(rep, rRemote, "-/npm/v1/attestations/"+p.Name+"@99.0.0", r.tokA); a.Status != 404 {
		t.Errorf("attestations the upstream does not have: %d %s", a.Status, a.Body)
	}

	var union struct {
		Keys []struct {
			KeyID string `json:"keyid"`
		} `json:"keys"`
	}
	a := r.get(rep, rAll, "-/npm/v1/keys", r.tokA)
	if err := json.Unmarshal(a.Body, &union); err != nil || a.Status != 200 || len(union.Keys) != 1 || union.Keys[0].KeyID != "SHA256:stub-key" {
		t.Errorf("the keys of npm-all: %d %s", a.Status, a.Body)
	}
	if a := r.get(rep, rLocal, "-/npm/v1/keys", r.tokA); a.Status != 200 || string(a.Body) != `{"keys":[]}` {
		t.Errorf("the keys of a local repository: %d %s", a.Status, a.Body)
	}
}
