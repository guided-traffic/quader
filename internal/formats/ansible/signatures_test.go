package ansible

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/openpgp"
)

// manifestSignature signs a MANIFEST.json detached with a release key of the test (Ed25519, milliseconds).
func manifestSignature(t testing.TB) string {
	t.Helper()
	secret, _, _, err := openpgp.Generate("Collection key", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	key, err := openpgp.ParseSecret(secret, nil, time.Now(), openpgp.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	sig, err := openpgp.DetachSign([]byte(`{"collection_info":{"namespace":"acme","name":"tools"}}`), []*openpgp.Key{key}, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return string(sig)
}

var archiveSum = sha256.Sum256([]byte("acme-tools-1.0.0.tar.gz"))

// versionDocument is an upstream's version document whose signatures[] holds a signing service's signature and an uploaded one.
func versionDocument(sig string) []byte {
	b, _ := json.Marshal(map[string]any{
		"namespace": map[string]any{"name": "acme"}, "name": "tools", "version": "1.0.0",
		"download_url": "https://galaxy.example.org/download/acme-tools-1.0.0.tar.gz",
		"artifact":     map[string]any{"filename": "acme-tools-1.0.0.tar.gz", "sha256": hex.EncodeToString(archiveSum[:]), "size": 23},
		"collection":   map[string]any{"id": "11111111-2222-3333-4444-555555555555", "name": "tools"},
		"signatures": []map[string]any{
			{"signature": sig, "pubkey_fingerprint": "ABCD", "signing_service": "ansible-default"},
			{"signature": sig, "pubkey_fingerprint": "ABCD", "signing_service": nil},
		},
		"metadata": map[string]any{},
	})
	return b
}

// signatureCases are check 18's samples of Ansible Galaxy: a remote's version document with both kinds.
func signatureCases(t testing.TB) []formattest.SignatureCase {
	sig := manifestSignature(t)
	const p = "collections/acme/tools/1.0.0.json"
	want := func(i int, role string) formats.SignatureCandidate {
		return formats.SignatureCandidate{
			Object: formats.SignatureLocator{Raw: []byte(sig)}, Subject: archiveSum, Role: role, SchemeHint: formats.SignatureSchemeOpenPGP,
			Source: formats.SignatureSourceDocument, Location: p + "#/signatures/" + string(rune('0'+i)),
			SignedContent: formats.SignatureLocator{Blob: archiveSum, Locator: formats.SignatureLocatorEntryPrefix + manifestEntry},
		}
	}
	doc := versionDocument(sig)
	return []formattest.SignatureCase{
		{Kind: "signing service signature", Path: p, Body: doc, MediaType: "application/json", Want: []formats.SignatureCandidate{want(0, formats.SignatureRoleRegistry)}},
		{Kind: "uploaded signature", Path: p, Body: doc, MediaType: "application/json", Want: []formats.SignatureCandidate{want(1, formats.SignatureRolePublisher)}},
	}
}

func detectAnsible(t *testing.T, class formats.Class, p string, body []byte) []formats.SignatureCandidate {
	t.Helper()
	sum := sha256.Sum256(body)
	got, err := New().Signatures().Detect(context.Background(), formats.SignatureEvent{
		Repo: formats.RepoRef{Class: class}, Path: formats.Path{Path: p}, Digests: formats.Digests{SHA256: sum[:]}, Content: formattest.ContentOf(body),
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestR_SIG_031_TheSignaturesOfARemotesVersionDocument: each entry of signatures[] is a signature of the archive artifact.sha256
// names, over its MANIFEST.json; one that names a signing service is the registry's, one without is the publisher's; a local
// repository's document, the collection document and the docs-blob document name nothing (Q-SIG-37). Unit, milliseconds.
func TestR_SIG_031_TheSignaturesOfARemotesVersionDocument(t *testing.T) {
	doc := versionDocument(manifestSignature(t))
	got := detectAnsible(t, formats.Remote, "collections/acme/tools/1.0.0.json", doc)
	if len(got) != 2 || got[0].Role != formats.SignatureRoleRegistry || got[1].Role != formats.SignatureRolePublisher ||
		got[0].Subject != archiveSum || got[1].Location != "collections/acme/tools/1.0.0.json#/signatures/1" {
		t.Fatalf("candidates = %+v", got)
	}
	for _, c := range []struct {
		class formats.Class
		p     string
	}{
		{formats.Local, "collections/acme/tools/1.0.0.json"},
		{formats.Remote, "collections/acme/tools/" + CollectionDocument},
		{formats.Remote, "collections/acme/tools/docs/1.0.0.json"},
	} {
		if got := detectAnsible(t, c.class, c.p, doc); len(got) != 0 {
			t.Errorf("%s %s: %+v", c.class, c.p, got)
		}
	}
}

// gateCases is check 18's classification of Ansible Galaxy (R-SIG-063): a collection archive and a role archive are the artifacts; the
// documents a remote stores are none. Milliseconds.
func gateCases() []formattest.GateCase {
	const archive = "collections/acme/tools/acme-tools-1.0.0.tar.gz"
	return []formattest.GateCase{
		{Name: "a collection archive is an artifact", Seed: putItem(archive), Path: archive, Want: formats.GateArtifact},
		{Name: "a role archive is an artifact", Seed: putItem("roles/123/v1.0.tar.gz"), Path: "roles/123/v1.0.tar.gz", Want: formats.GateArtifact},
		{Name: "a version document is none", Seed: putItem("collections/acme/tools/1.0.0.json"), Path: "collections/acme/tools/1.0.0.json", Want: formats.GateNone},
	}
}

// putItem stores bytes at a path of the kit's host, the item a gate case asks about.
func putItem(p string) func(h *formattest.Host) error {
	return func(h *formattest.Host) error {
		h.PutItem(p, "application/octet-stream", []byte("bytes of "+p))
		return nil
	}
}
