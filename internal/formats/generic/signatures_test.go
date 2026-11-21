package generic

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/openpgp"
)

// releaseKeySign signs data detached with n release keys made by the test (the owner's third way, R-SIG-042): Ed25519, milliseconds.
func releaseKeySign(t testing.TB, data []byte, n int) []byte {
	t.Helper()
	created := time.Now().Add(-time.Hour)
	keys := make([]*openpgp.Key, n)
	for i := range keys {
		secret, _, _, err := openpgp.Generate("Release key", created)
		if err != nil {
			t.Fatal(err)
		}
		if keys[i], err = openpgp.ParseSecret(secret, nil, time.Now(), openpgp.Limits{}); err != nil {
			t.Fatal(err)
		}
	}
	sig, err := openpgp.DetachSign(data, keys, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

// signatureCases is check 18's sample of generic: a release key's signature beside a file of a kilobyte.
func signatureCases(t testing.TB) []formattest.SignatureCase {
	file := make([]byte, 1024)
	asc := releaseKeySign(t, file, 1)
	return []formattest.SignatureCase{{
		Kind: "signature file", Path: "tools/tool.tar.gz.asc",
		Commit: func(h *formattest.Host) error {
			h.PutItem("tools/tool.tar.gz", "application/gzip", file)
			h.PutItem("tools/tool.tar.gz.asc", "application/pgp-signature", asc)
			return nil
		},
		Want: []formats.SignatureCandidate{{
			Object: formats.SignatureLocator{Blob: sha256.Sum256(asc)}, SubjectPath: "tools/tool.tar.gz", Role: formats.SignatureRolePublisher,
			SchemeHint: formats.SignatureSchemeOpenPGP, Source: formats.SignatureSourceSibling, Location: "tools/tool.tar.gz.asc",
		}},
	}}
}

func detect(t *testing.T, p string, body []byte) []formats.SignatureCandidate {
	t.Helper()
	sum := sha256.Sum256(body)
	got, err := Format{}.Signatures().Detect(context.Background(), formats.SignatureEvent{
		Origin: formats.OriginUpload, Item: formats.ItemRef{ID: 7, Path: p}, Path: formats.Path{Path: p},
		Digests: formats.Digests{SHA256: sum[:]}, Content: formattest.ContentOf(body),
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestR_SIG_032_ASiblingIsASignatureByItsNameAndItsSchemeByItsContent: the longest suffix finds the signed file, the content tells the
// scheme (Q-SIG-38), an armored signature of two keys names two parts, and a file names each signature file that may stand beside it.
// Unit, milliseconds, bytes made by the test.
func TestR_SIG_032_ASiblingIsASignatureByItsNameAndItsSchemeByItsContent(t *testing.T) {
	file := []byte("the file")
	two := releaseKeySign(t, file, 2)
	raw := base64.StdEncoding.EncodeToString(make([]byte, 71))
	for _, c := range []struct {
		path, scheme string
		body         []byte
		parts        int
		signed       string
	}{
		{"a/tool.tar.gz.asc", formats.SignatureSchemeOpenPGP, two, 2, "a/tool.tar.gz"},
		{"a/tool.tar.gz.sig", formats.SignatureSchemePublicKey, []byte(raw + "\n"), 1, "a/tool.tar.gz"},
		{"a/tool.tar.gz.sig", formats.SignatureSchemeUnknown, []byte("untrusted comment: minisign public key\nRWQ...\n"), 1, "a/tool.tar.gz"},
		{"a/tool.sigstore.json", formats.SignatureSchemeSigstoreBundle, []byte(`{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json"}`), 1, "a/tool"},
		{"a/x.jws.sig", formats.SignatureSchemeNotationJWS, []byte(`{"payload":"e30","protected":"e30","signature":"AA"}`), 1, "a/x"},
		{"a/x.cose.sig", formats.SignatureSchemeNotationCOSE, []byte{0xd2, 0x84}, 1, "a/x"},
	} {
		got := detect(t, c.path, c.body)
		var mine []formats.SignatureCandidate
		for _, g := range got {
			if g.Location == c.path {
				mine = append(mine, g)
			}
		}
		if len(mine) != c.parts {
			t.Fatalf("%s: %d candidates at its own path, want %d: %+v", c.path, len(mine), c.parts, mine)
		}
		for i, g := range mine {
			if g.SchemeHint != c.scheme || g.SubjectPath != c.signed || g.Part != i || g.Source != formats.SignatureSourceSibling {
				t.Errorf("%s: candidate %d = %+v, want scheme %s of %s", c.path, i, g, c.scheme, c.signed)
			}
		}
	}
	t.Run("a file names the five signature files that may stand beside it, by path", func(t *testing.T) {
		got := detect(t, "a/tool.tar.gz", file)
		want := map[string]bool{
			"a/tool.tar.gz.asc": true, "a/tool.tar.gz.sig": true, "a/tool.tar.gz.sigstore.json": true,
			"a/tool.tar.gz.jws.sig": true, "a/tool.tar.gz.cose.sig": true,
		}
		sum := sha256.Sum256(file)
		for _, g := range got {
			if !want[g.Object.Path] || g.Location != g.Object.Path || g.Subject != sum {
				t.Errorf("unexpected candidate %+v", g)
			}
			delete(want, g.Object.Path)
		}
		if len(want) > 0 {
			t.Errorf("not named: %v", want)
		}
	})
	t.Run("a.jws.sig is Notation's signature of a, not a .sig of a.jws", func(t *testing.T) {
		if f, s, ok := SignedFile("d/a.jws.sig"); !ok || f != "d/a" || s != ".jws.sig" {
			t.Fatalf("SignedFile = %q %q %v", f, s, ok)
		}
		if _, _, ok := SignedFile("d/.asc"); ok {
			t.Fatal("a bare suffix names a file")
		}
	})
}
