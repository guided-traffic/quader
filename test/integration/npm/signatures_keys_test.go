//go:build integration

package npm

import (
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/signatures/shipped"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// The real registry key of registry.npmjs.org that signed the recorded documents of the npm stub (left-pad's versions), from the
// release's snapshot of npm's keys.
const leftPadKeyID = "SHA256:jl3bwswu80PjjokCgh0o2w5c2U4LhQAE57gj9cz1kzA"

// TestR_SIG_028_ARemotesRegistrySignatureIsMatchedByTheKeysItsUpstreamAnnounces: the keys document npm-remote stored at -/npm/v1/keys is
// what the job checks a registry signature of the remote with (R-SIG-006): left-pad's recorded registry signatures, made with npm's real
// key, read valid by the key the upstream announces, and untrusted, because no anchor of the installation knows it (Q-SIG-24). The
// stub announces the key without its expiry, so the receipt of today counts (H-1169). Integration, about 4 s, the npm stub, a recorded
// document.
func TestR_SIG_028_ARemotesRegistrySignatureIsMatchedByTheKeysItsUpstreamAnnounces(t *testing.T) {
	keys, err := shipped.NPMKeys(shipped.Release().NPMKeys)
	if err != nil {
		t.Fatal(err)
	}
	der := ""
	for _, k := range keys.Keys {
		if k.KeyID == leftPadKeyID {
			b, _ := pem.Decode([]byte(k.PEM))
			if b == nil {
				t.Fatal("the shipped key does not decode")
			}
			der = base64.StdEncoding.EncodeToString(b.Bytes)
		}
	}
	if der == "" {
		t.Fatalf("the release's npm keys hold no %s", leftPadKeyID)
	}
	doc, _ := json.Marshal(map[string]any{"keys": []map[string]any{{
		"expires": nil, "keyid": leftPadKeyID, "keytype": "ecdsa-sha2-nistp256", "scheme": "ecdsa-sha2-nistp256", "key": der,
	}}})
	r := newRemoteEnv(t, remoteOptions{Env: []string{"QUADER_JOBS_POLL_INTERVAL=200ms"}})
	r.stub.SetKeys(doc)
	rep := r.start(1)[0]
	if a := r.get(rep, rRemote, "-/npm/v1/keys", r.tokA); a.Status != 200 {
		t.Fatalf("the keys of npm-remote: %d %s", a.Status, a.Body)
	}
	if a := r.get(rep, rRemote, "left-pad", r.tokA); a.Status != 200 {
		t.Fatalf("left-pad through npm-remote: %d %s", a.Status, a.Body)
	}
	r.st.Start(t, testenv.ReplicaOptions{Name: "worker-sig", Role: "worker", Env: []string{"QUADER_JOBS_POLL_INTERVAL=200ms"}})
	const state = `SELECT COALESCE(string_agg(s.state || ':' || COALESCE(t.state, '-'), ',' ORDER BY s.subject_name), '')
		FROM signatures s LEFT JOIN signature_trust t ON t.signature_id = s.id AND t.project_id IS NULL
		WHERE s.subject_name IN ('left-pad@1.2.0', 'left-pad@1.3.0')`
	deadline := time.Now().Add(20 * time.Second)
	got := ""
	for time.Now().Before(deadline) {
		if got = r.st.Text(state); got == "valid:untrusted,valid:untrusted" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("left-pad's registry signatures read %q, want both valid and untrusted", got)
}
