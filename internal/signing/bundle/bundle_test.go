package bundle_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"
	"time"

	sgbundle "github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"

	"github.com/guided-traffic/quader/internal/signing"
	"github.com/guided-traffic/quader/internal/signing/bundle"
	"github.com/guided-traffic/quader/internal/signing/signingtest"
	"github.com/guided-traffic/quader/internal/signing/statement"
)

// sampleStatement is a statement of an artifact of a few bytes.
func sampleStatement(t *testing.T, artifact []byte) []byte {
	t.Helper()
	now := time.Date(2026, 11, 21, 10, 2, 3, 0, time.UTC)
	b, err := statement.Build(statement.Facts{
		InstallationID: "6c3e", BaseURL: "https://quader.example.com", Host: "quader.example.com", Project: "web", Repository: "files",
		Format: "generic", Class: "local", Path: "tools/tool.tar.gz", Name: "tools/tool.tar.gz", Size: int64(len(artifact)),
		SHA256: sha256.Sum256(artifact), ReceivedAt: now, IssuedAt: now, Origin: statement.Origin{Kind: statement.OriginUpload, PrincipalKind: "user", PrincipalName: "ada"},
		Malware: statement.Malware{State: "clean", Database: "main-62", DatabaseBuiltAt: now, ScannedAt: now},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// trustedRoot is the trusted root Quader serves for the identity of a CA of the test (signing.TrustedRoot).
func trustedRoot(t *testing.T, ca *signingtest.CA, leafNotBefore, leafNotAfter time.Time) *root.TrustedRoot {
	t.Helper()
	doc, ok, err := signing.TrustedRoot([]signing.Identity{{
		ID: 1, State: signing.StateCurrent, Chain: [][]byte{ca.Intermediate.Raw, ca.Root.Raw}, NotBefore: leafNotBefore, NotAfter: leafNotAfter,
	}})
	if err != nil || !ok {
		t.Fatalf("trusted root: %v %v", ok, err)
	}
	tr, err := root.NewTrustedRootFromJSON(doc)
	if err != nil {
		t.Fatalf("sigstore-go does not take the trusted root Quader builds: %v", err)
	}
	return tr
}

// verifyBundle is what cosign verify-attestation does offline with --trusted-root, --certificate-identity, an issuer regexp of '.*' and
// without a log or a timestamp (R-SIG-052): sigstore-go's verifier at the current time.
func verifyBundle(t *testing.T, tr root.TrustedMaterial, raw, artifact []byte, san string) error {
	t.Helper()
	var b sgbundle.Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return err
	}
	v, err := verify.NewVerifier(tr, verify.WithCurrentTime())
	if err != nil {
		t.Fatalf("a verifier without a log or a timestamp: %v", err)
	}
	id, err := verify.NewShortCertificateIdentity("", ".*", san, "")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(artifact)
	_, err = v.Verify(&b, verify.NewPolicy(verify.WithArtifactDigest("sha256", sum[:]), verify.WithCertificateIdentity(id)))
	return err
}

// TestR_SIG_052_ABundleOfTheSignerVerifiesAgainstTheTrustedRootQuaderBuilds: a bundle Sign made with a leaf of the company's CA verifies
// with sigstore-go's verifier against the trusted root signing.TrustedRoot builds (no log, no timestamp, the current time, the identity
// by its URI), and does not with a flipped byte of the statement, under a trusted root of another CA, for another identity, or with a
// leaf whose validity has ended. The bundle carries the leaf alone and the statement's payload type. Unit, about 50 ms, a CA of the test.
func TestR_SIG_052_ABundleOfTheSignerVerifiesAgainstTheTrustedRootQuaderBuilds(t *testing.T) {
	ca := signingtest.NewCA(t, "Example Corp")
	leaf, key := ca.Issue(t, signingtest.Leaf{})
	artifact := []byte("the bytes of a file")
	raw, err := bundle.Sign(sampleStatement(t, artifact), key, leaf.Raw)
	if err != nil {
		t.Fatal(err)
	}
	tr := trustedRoot(t, ca, leaf.NotBefore, leaf.NotAfter)
	if err := verifyBundle(t, tr, raw, artifact, "https://quader.example.com"); err != nil {
		t.Fatalf("the bundle does not verify against Quader's trusted root: %v", err)
	}
	var shape struct {
		MediaType            string `json:"mediaType"`
		VerificationMaterial struct {
			Certificate *struct {
				RawBytes string `json:"rawBytes"`
			} `json:"certificate"`
			TlogEntries []json.RawMessage `json:"tlogEntries"`
		} `json:"verificationMaterial"`
		DSSEEnvelope struct {
			PayloadType string            `json:"payloadType"`
			Signatures  []json.RawMessage `json:"signatures"`
		} `json:"dsseEnvelope"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		t.Fatal(err)
	}
	switch {
	case shape.MediaType != bundle.MediaType:
		t.Errorf("mediaType = %q", shape.MediaType)
	case shape.VerificationMaterial.Certificate == nil || len(shape.VerificationMaterial.TlogEntries) != 0:
		t.Errorf("the verification material is not the leaf alone without a log entry: %s", raw)
	case shape.DSSEEnvelope.PayloadType != statement.PayloadType || len(shape.DSSEEnvelope.Signatures) != 1:
		t.Errorf("the envelope = %+v", shape.DSSEEnvelope)
	}
	if len(raw) > 4096 {
		t.Errorf("a bundle takes %d bytes, more than the 2.5 KiB R-SIG-052 estimates by far", len(raw))
	}

	t.Run("a flipped byte of the statement", func(t *testing.T) {
		var doc map[string]any
		_ = json.Unmarshal(raw, &doc)
		env := doc["dsseEnvelope"].(map[string]any)
		p := []byte(env["payload"].(string))
		p[10] ^= 1
		env["payload"] = string(p)
		flipped, _ := json.Marshal(doc)
		if err := verifyBundle(t, tr, flipped, artifact, "https://quader.example.com"); err == nil {
			t.Fatal("a bundle with a changed statement verifies")
		}
	})
	t.Run("another artifact", func(t *testing.T) {
		if err := verifyBundle(t, tr, raw, []byte("other bytes"), "https://quader.example.com"); err == nil {
			t.Fatal("the bundle verifies for an artifact it does not name")
		}
	})
	t.Run("a trusted root of another CA", func(t *testing.T) {
		other := signingtest.NewCA(t, "Other Corp")
		if err := verifyBundle(t, trustedRoot(t, other, leaf.NotBefore, leaf.NotAfter), raw, artifact, "https://quader.example.com"); err == nil {
			t.Fatal("the bundle verifies under another CA")
		}
	})
	t.Run("another identity", func(t *testing.T) {
		err := verifyBundle(t, tr, raw, artifact, "https://other.example.com")
		if err == nil || !strings.Contains(err.Error(), "other.example.com") && !strings.Contains(strings.ToLower(err.Error()), "san") {
			t.Fatalf("another identity: %v", err)
		}
	})
	t.Run("a leaf whose validity has ended", func(t *testing.T) {
		old, oldKey := ca.Issue(t, signingtest.Leaf{NotBefore: time.Now().Add(-48 * time.Hour), NotAfter: time.Now().Add(-time.Hour)})
		stale, err := bundle.Sign(sampleStatement(t, artifact), oldKey, old.Raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyBundle(t, trustedRoot(t, ca, old.NotBefore, old.NotAfter), stale, artifact, "https://quader.example.com"); err == nil {
			t.Fatal("a bundle verifies at a time outside its leaf's validity")
		}
	})
	t.Run("a key that is no P-256 key", func(t *testing.T) {
		if _, err := bundle.Sign([]byte("{}"), nil, leaf.Raw); err == nil {
			t.Fatal("Sign without a key")
		}
		if bytes.Contains(raw, []byte("PRIVATE KEY")) {
			t.Fatal("the bundle carries key material")
		}
	})
}
