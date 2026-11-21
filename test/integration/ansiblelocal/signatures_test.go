//go:build integration

package ansiblelocal

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/formathost"
	"github.com/guided-traffic/quader/internal/formats/ansible/collection/collectiontest"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/openpgp"
	sigstore "github.com/guided-traffic/quader/internal/signatures/store"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// releaseKeySign signs data detached with a release key made by the test (Ed25519, milliseconds) and returns the armored signature
// and the key's fingerprint in lower-case hexadecimal.
func releaseKeySign(t *testing.T, data []byte) (string, string) {
	t.Helper()
	secret, _, fp, err := openpgp.Generate("Release key", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	key, err := openpgp.ParseSecret(secret, nil, time.Now(), openpgp.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	sig, err := openpgp.DetachSign(data, []*openpgp.Key{key}, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return string(sig), strings.ToLower(strings.ReplaceAll(fp, " ", ""))
}

// discover runs one pass of the job's discovery over every live file in this process, as the worker does (R-SIG-011), and then the
// job's removal of signatures without an attachment.
func discover(t *testing.T, e *env) {
	t.Helper()
	ctx := context.Background()
	loaded, err := config.Load(config.Options{Environ: e.st.Environ(), Command: config.CommandAdmin})
	if err != nil {
		t.Fatal(err)
	}
	db, err := postgres.Open(ctx, loaded.Config.Database, "ansiblelocal", meta.RoleWorker, testenv.Quiet())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	st := sigstore.New(sigstore.Options{Runner: db})
	x := &formathost.Discoverer{Deps: formathost.Deps{Signatures: st}, Items: st, DB: db}
	if _, err := x.Discover(ctx, 0, 1000); err != nil {
		t.Fatalf("discovery: %v", err)
	}
	if err := db.InTx(ctx, func(tx meta.Tx) error {
		_, _, err := st.RemoveOrphans(ctx, tx, 1000)
		return err
	}); err != nil {
		t.Fatalf("the removal of orphans: %v", err)
	}
}

// manifestOf is MANIFEST.json of the archive of a spec.
func manifestOf(t *testing.T, version string) []byte {
	t.Helper()
	for _, en := range collectiontest.Entries(spec(version)) {
		if en.Name == "MANIFEST.json" {
			return en.Body
		}
	}
	t.Fatal("the archive has no MANIFEST.json")
	return nil
}

// TestR_SIG_031_ASignaturePostedForALocalVersionIsServed: a CI job posts a detached signature of the version's MANIFEST.json through the
// management API; it is served in signatures[] of the version document with its key's fingerprint and recorded in the signature store
// with the source upload; a signature of other content answers 422 signature_invalid; a reader may not post; the signature is removed
// by its fingerprint. Integration, about 2 s, bytes.
func TestR_SIG_031_ASignaturePostedForALocalVersionIsServed(t *testing.T) {
	e := newEnv(t)
	e.mustPublish("1.0.0")
	op := "/api/v1/projects/" + project + "/repositories/" + repo + "/ansible/collections/acme.tools/versions/1.0.0/signatures"
	post := func(token, sig string) (int, map[string]any) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"signature": sig})
		a := e.st.Do(e.r, http.MethodPost, op, token, body, "Content-Type", "application/json")
		var doc map[string]any
		_ = json.Unmarshal(a.Body, &doc)
		return a.Status, doc
	}

	sig, fp := releaseKeySign(t, manifestOf(t, "1.0.0"))
	if status, doc := post(e.tokC, sig); status != http.StatusForbidden && status != http.StatusNotFound {
		t.Errorf("a reader posted: %d %v", status, doc)
	}
	other, _ := releaseKeySign(t, []byte(`{"collection_info":{"name":"evil"}}`))
	if status, doc := post(e.tokB, other); status != http.StatusUnprocessableEntity || doc["code"] != "signature_invalid" {
		t.Errorf("a signature of other content: %d %v", status, doc)
	}
	status, doc := post(e.tokB, sig)
	if status != http.StatusCreated || doc["fingerprint"] != fp {
		t.Fatalf("the post: %d %v, want 201 with the fingerprint %s", status, doc, fp)
	}

	a := e.get(e.tokC, "v3/collections/acme/tools/versions/1.0.0/")
	if a.Status != http.StatusOK {
		t.Fatalf("the version document: %d %s", a.Status, a.Body)
	}
	var vd struct {
		Signatures []struct {
			Signature      string  `json:"signature"`
			Fingerprint    string  `json:"pubkey_fingerprint"`
			SigningService *string `json:"signing_service"`
		} `json:"signatures"`
	}
	if err := json.Unmarshal(a.Body, &vd); err != nil {
		t.Fatal(err)
	}
	if len(vd.Signatures) != 1 || vd.Signatures[0].Signature != sig || vd.Signatures[0].Fingerprint != fp || vd.Signatures[0].SigningService != nil {
		t.Errorf("signatures[] = %+v", vd.Signatures)
	}
	if n := e.st.Number(`SELECT count(*) FROM signature_attachments a JOIN signatures s ON s.id = a.signature_id
	                     WHERE a.repo_id = $1 AND a.source = 'upload' AND s.role = 'publisher' AND s.content_locator = 'entry:MANIFEST.json'`, e.repoID()); n != 1 {
		t.Errorf("%d attachments of the posted signature, want 1", n)
	}
	// The post wakes the job signature-verify in its transaction, at the priority of an upload (R-SIG-011).
	if n := e.st.Number(`SELECT count(*) FROM job WHERE kind = 'signature-verify'`); n < 1 {
		t.Errorf("the post queued no job signature-verify")
	}
	if n := e.st.Number(`SELECT count(*) FROM signatures s JOIN signature_attachments a ON a.signature_id = s.id
	                     WHERE a.repo_id = $1 AND s.priority = 0 AND s.due`, e.repoID()); n != 1 {
		t.Errorf("%d posted signatures due at the priority of an upload, want 1", n)
	}
	t.Run("discovery passing the archive keeps the posted signature", func(t *testing.T) {
		// The worker's discovery records the detector's batch of the archive's item, which carries nothing, and the job then removes
		// what has no attachment (R-SIG-011); the attachment of the source upload is no detector's and stays (R-SIG-004, Q-SIG-217).
		discover(t, e)
		if n := e.st.Number(`SELECT count(*) FROM signature_attachments a JOIN signatures s ON s.id = a.signature_id
		                     JOIN signature_objects o ON o.sha256 = s.object_sha256 WHERE a.repo_id = $1 AND a.source = 'upload'`, e.repoID()); n != 1 {
			t.Errorf("%d posted signatures with their attachment, row and object after discovery, want 1", n)
		}
	})

	if a := e.st.Do(e.r, http.MethodDelete, op+"/"+fp, e.tokA, nil); a.Status != http.StatusNoContent {
		t.Fatalf("the removal: %d %s", a.Status, a.Body)
	}
	if n := e.st.Number(`SELECT count(*) FROM signature_attachments WHERE repo_id = $1`, e.repoID()); n != 0 {
		t.Errorf("%d attachments after the removal, want 0", n)
	}
	if a := e.get(e.tokC, "v3/collections/acme/tools/versions/1.0.0/"); !strings.Contains(string(a.Body), `"signatures":[]`) {
		t.Errorf("the version document after the removal: %s", a.Body)
	}
	// The post and the removal are audited with the key's fingerprint, never the signature (Q-SIG-107).
	for _, action := range []string{"ansible.signature.added", "ansible.signature.removed"} {
		if n := e.st.Number(`SELECT count(*) FROM audit_event WHERE action = $1 AND details->>'fingerprint' = $2 AND details->>'name' = 'acme.tools'
		                     AND NOT details ? 'signature'`, action, fp); n != 1 {
			t.Errorf("%d events %s with the fingerprint, want 1", n, action)
		}
	}
}
