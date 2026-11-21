package deb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
	"github.com/guided-traffic/quader/internal/openpgp"
)

// archiveKeys makes n archive keys of the test (Ed25519, milliseconds), as a Debian archive signs its releases with two.
func archiveKeys(t testing.TB, n int) []*openpgp.Key {
	t.Helper()
	keys := make([]*openpgp.Key, n)
	for i := range keys {
		secret, _, _, err := openpgp.Generate("Archive key", time.Now().Add(-time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if keys[i], err = openpgp.ParseSecret(secret, nil, time.Now(), openpgp.Limits{}); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

const signedRelease = "Origin: Debian\nSuite: stable\nCodename: bookworm\nSHA256:\n e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 0 main/binary-amd64/Packages\n"

// signatureCases is check 18's sample of Debian: a remote's InRelease signed by two archive keys, stored by the fill as a document.
func signatureCases(t testing.TB) []formattest.SignatureCase {
	in, err := openpgp.ClearSign([]byte(signedRelease), archiveKeys(t, 2), time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	const p = "dists/bookworm/InRelease"
	sum := sha256.Sum256(in)
	want := func(part int) formats.SignatureCandidate {
		return formats.SignatureCandidate{
			Object: formats.SignatureLocator{Blob: sum}, Part: part, Subject: sum, Role: formats.SignatureRoleRegistry,
			SchemeHint: formats.SignatureSchemeOpenPGP, Source: formats.SignatureSourceRelease, Location: p,
			SignedContent: formats.SignatureLocator{Blob: sum, Locator: formats.SignatureLocatorCleartext},
		}
	}
	return []formattest.SignatureCase{{Kind: "release signature", Path: p, Body: in, MediaType: "text/plain", Want: []formats.SignatureCandidate{want(0), want(1)}}}
}

func detectDeb(t *testing.T, class formats.Class, p string, body []byte) []formats.SignatureCandidate {
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

// TestR_SIG_030_TheReleaseDocumentIsSignedAndThePackageIsNot: a remote's InRelease names one signature per packet with its cleartext
// as the signed content; Release.gpg names Release beside it as its subject; Release names Release.gpg beside it; a package, a local
// repository's release and an index name nothing; the signature block of an InRelease larger than a signature object is read from its
// end. Unit, milliseconds, releases signed by the test.
func TestR_SIG_030_TheReleaseDocumentIsSignedAndThePackageIsNot(t *testing.T) {
	keys := archiveKeys(t, 2)
	at := time.Now().Add(-time.Minute)
	in, err := openpgp.ClearSign([]byte(signedRelease), keys, at)
	if err != nil {
		t.Fatal(err)
	}
	gpg, err := openpgp.DetachSign([]byte(signedRelease), keys, at)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("InRelease", func(t *testing.T) {
		got := detectDeb(t, formats.Remote, "dists/bookworm/InRelease", in)
		if len(got) != 2 || got[1].Part != 1 || got[0].SignedContent.Locator != formats.SignatureLocatorCleartext {
			t.Fatalf("candidates = %+v", got)
		}
	})
	t.Run("Release.gpg and Release", func(t *testing.T) {
		got := detectDeb(t, formats.Remote, "dists/bookworm/Release.gpg", gpg)
		if len(got) != 2 || got[0].SubjectPath != "dists/bookworm/Release" || got[0].Location != "dists/bookworm/Release.gpg" {
			t.Fatalf("Release.gpg: %+v", got)
		}
		got = detectDeb(t, formats.Remote, "dists/bookworm/Release", []byte(signedRelease))
		if len(got) != 1 || got[0].Object.Path != "dists/bookworm/Release.gpg" || got[0].Subject != sha256.Sum256([]byte(signedRelease)) {
			t.Fatalf("Release: %+v", got)
		}
	})
	t.Run("a package, a local release and an index name nothing", func(t *testing.T) {
		for _, c := range []struct {
			class formats.Class
			p     string
		}{{formats.Remote, "pool/main/h/hello/hello_1.0-1_amd64.deb"}, {formats.Local, "dists/bookworm/InRelease"}, {formats.Remote, "dists/bookworm/main/binary-amd64/Packages"}} {
			if got := detectDeb(t, c.class, c.p, in); len(got) != 0 {
				t.Errorf("%s %s: %+v", c.class, c.p, got)
			}
		}
	})
	t.Run("a large InRelease is read from its end", func(t *testing.T) {
		big := bytes.Repeat([]byte(" e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 0 main/x\n"), 4000)
		large, err := openpgp.ClearSign(append([]byte(signedRelease), big...), keys, at)
		if err != nil {
			t.Fatal(err)
		}
		if got := detectDeb(t, formats.Remote, "dists/bookworm/InRelease", large); len(got) != 2 {
			t.Fatalf("candidates = %+v", got)
		}
	})
}
