package npm

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// The bytes the samples carry: a registry signature entry and Sigstore bundles. The detector checks none of them.
var (
	registryEntry   = []byte(`{"keyid":"SHA256:jl3bwswu80PjjokCgh0o2w5c2U4LhQAE57gj9cz1kzA","sig":"MEUCIQDexample"}`)
	bundleSample    = []byte(`{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json","verificationMaterial":{}}`)
	publishSample   = []byte(`{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json","dsseEnvelope":{}}`)
	sampleIntegrity = "sha512-" + base64.StdEncoding.EncodeToString(func() []byte { s := sha512.Sum512([]byte("tarball")); return s[:] }())
)

// registryDocument is a remote's package document whose version carries a registry signature.
func registryDocumentBytes() []byte {
	doc := map[string]any{
		"_id": "lodash", "name": "lodash",
		"versions": map[string]any{"4.17.21": map[string]any{
			"name": "lodash", "version": "4.17.21",
			"dist": map[string]any{"integrity": sampleIntegrity, "signatures": []json.RawMessage{registryEntry}},
		}},
	}
	b, _ := json.Marshal(doc)
	return b
}

// attestationsDocument is npm's attestations answer of a version: SLSA provenance and the publish attestation.
func attestationsDocumentBytes() []byte {
	b, _ := json.Marshal(map[string]any{"attestations": []map[string]any{
		{"predicateType": "https://slsa.dev/provenance/v1", "bundle": json.RawMessage(bundleSample)},
		{"predicateType": "https://github.com/npm/attestation/tree/main/specs/publish/v0.1", "bundle": json.RawMessage(publishSample)},
	}})
	return b
}

// publishWithBundle is the body of a publish of name@version with a generated tarball and a provenance bundle beside it.
func publishWithBundle(t testing.TB, name, version string, bundle []byte) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(publishDoc(t, name, version, "latest"), &doc); err != nil {
		t.Fatal(err)
	}
	doc["_attachments"].(map[string]any)[name+"-"+version+".sigstore"] = map[string]any{
		"content_type": "application/vnd.dev.sigstore.bundle.v0.3+json", "data": base64.StdEncoding.EncodeToString(bundle), "length": len(bundle),
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// signatureCases are check 18's samples of npm: a remote's document with a registry signature, a publish with its provenance bundle,
// and a remote's attestations document.
func signatureCases(t testing.TB, bind func(*formattest.Host)) []formattest.SignatureCase {
	msg := []byte("lodash@4.17.21:" + sampleIntegrity)
	attPath := AttestationsPrefix + "lodash@4.17.21"
	return []formattest.SignatureCase{
		{
			Kind: "registry signature", Path: "lodash", Body: registryDocumentBytes(), MediaType: "application/json",
			Want: []formats.SignatureCandidate{{
				Object: formats.SignatureLocator{Raw: registryEntry}, Subject: sha256.Sum256(msg), SubjectName: "lodash@4.17.21",
				Role: formats.SignatureRoleRegistry, SchemeHint: formats.SignatureSchemeNPMRegistry, Source: formats.SignatureSourceDocument,
				Location: "lodash#/versions/4.17.21/dist/signatures/0", SignedContent: formats.SignatureLocator{Raw: msg},
			}},
		},
		{
			Kind: "provenance attestation", Path: "lodash/-/lodash-4.17.21.sigstore",
			Commit: func(h *formattest.Host) error {
				bind(h)
				return serveOK(h, http.MethodPut, "lodash", publishWithBundle(t, "lodash", "4.17.21", bundleSample), http.StatusCreated)
			},
			Want: []formats.SignatureCandidate{{
				Object: formats.SignatureLocator{Blob: sha256.Sum256(bundleSample)}, SubjectPath: "lodash/-/lodash-4.17.21.tgz",
				Role: formats.SignatureRoleProvenance, SchemeHint: formats.SignatureSchemeSigstoreBundle, Source: formats.SignatureSourceUpload,
				Location: "lodash/-/lodash-4.17.21.sigstore",
			}},
		},
		{
			Kind: "publish attestation", Path: attPath, Body: attestationsDocumentBytes(), MediaType: "application/json",
			Want: []formats.SignatureCandidate{{
				Object: formats.SignatureLocator{Raw: publishSample}, SubjectPath: "lodash/-/lodash-4.17.21.tgz", Role: formats.SignatureRoleRegistry,
				SchemeHint: formats.SignatureSchemeSigstoreBundle, Source: formats.SignatureSourceDocument, Location: attPath + "#/attestations/1",
			}},
		},
	}
}

func detectNPM(t *testing.T, class formats.Class, item int64, p string, body []byte) []formats.SignatureCandidate {
	t.Helper()
	sum := sha256.Sum256(body)
	got, err := New().Signatures().Detect(context.Background(), formats.SignatureEvent{
		Repo: formats.RepoRef{Class: class}, Item: formats.ItemRef{ID: item, Path: p}, Path: formats.Path{Path: p},
		Digests: formats.Digests{SHA256: sum[:]}, Content: formattest.ContentOf(body),
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestR_SIG_028_DetectOverADocumentAndAPublish: a remote's stored document names the registry signature of each version with the
// message it signs as its subject; a publish names its bundle as provenance of its tarball; the attestations document names each
// bundle by its predicate; a local repository's document and a tarball name nothing. Unit, milliseconds, documents built by the test.
func TestR_SIG_028_DetectOverADocumentAndAPublish(t *testing.T) {
	t.Run("the registry signature of a remote's document", func(t *testing.T) {
		got := detectNPM(t, formats.Remote, 0, "lodash", registryDocumentBytes())
		msg := []byte("lodash@4.17.21:" + sampleIntegrity)
		if len(got) != 1 || got[0].Subject != sha256.Sum256(msg) || string(got[0].SignedContent.Raw) != string(msg) ||
			string(got[0].Object.Raw) != string(registryEntry) || got[0].SubjectName != "lodash@4.17.21" {
			t.Fatalf("candidates = %+v", got)
		}
	})
	t.Run("a scoped package's document and a version with a slash in its pointer", func(t *testing.T) {
		doc := strings.ReplaceAll(string(registryDocumentBytes()), `"lodash"`, `"@acme/ui"`)
		got := detectNPM(t, formats.Remote, 0, "@acme/ui", []byte(doc))
		if len(got) != 1 || got[0].SubjectName != "@acme/ui@4.17.21" || got[0].Location != "@acme/ui#/versions/4.17.21/dist/signatures/0" {
			t.Fatalf("candidates = %+v", got)
		}
	})
	t.Run("a local repository's document and a tarball name nothing", func(t *testing.T) {
		if got := detectNPM(t, formats.Local, 0, "lodash", registryDocumentBytes()); len(got) != 0 {
			t.Fatalf("local document: %+v", got)
		}
		if got := detectNPM(t, formats.Remote, 3, "lodash/-/lodash-4.17.21.tgz", []byte("tarball")); len(got) != 0 {
			t.Fatalf("tarball: %+v", got)
		}
	})
	t.Run("the provenance bundle of a publish", func(t *testing.T) {
		got := detectNPM(t, formats.Local, 4, "@acme/ui/-/ui-1.2.0.sigstore", bundleSample)
		if len(got) != 1 || got[0].SubjectPath != "@acme/ui/-/ui-1.2.0.tgz" || got[0].Role != formats.SignatureRoleProvenance {
			t.Fatalf("candidates = %+v", got)
		}
	})
	t.Run("the attestations of a remote's version, by their predicates", func(t *testing.T) {
		got := detectNPM(t, formats.Remote, 0, AttestationsPrefix+"lodash@4.17.21", attestationsDocumentBytes())
		if len(got) != 2 || got[0].Role != formats.SignatureRoleProvenance || got[1].Role != formats.SignatureRoleRegistry {
			t.Fatalf("candidates = %+v", got)
		}
	})
}
