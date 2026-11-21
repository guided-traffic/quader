package nuget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/asn1"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// The ASN.1 of a CMS SignedData as NuGet's signatures have it (RFC 5652): built by the test with the structure the detector reads
// (the commitment type of the primary signer and the countersignatures of its unsigned attributes). Nothing is signed: the read of
// R-SIG-005 checks the signature, the detector names it.
var (
	oidProofOfOrigin = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 6, 1}
	oidData          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidSHA256        = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidRSA           = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
)

func derOf(class, tag int, parts ...[]byte) []byte {
	b, err := asn1.Marshal(asn1.RawValue{Class: class, Tag: tag, IsCompound: true, Bytes: bytes.Join(parts, nil)})
	if err != nil {
		panic(err)
	}
	return b
}

func seqOf(parts ...[]byte) []byte { return derOf(asn1.ClassUniversal, asn1.TagSequence, parts...) }
func setOf(parts ...[]byte) []byte { return derOf(asn1.ClassUniversal, asn1.TagSet, parts...) }

func mustDER(v any) []byte {
	b, err := asn1.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// signerInfo is a SignerInfo with a commitment type and the countersignatures given.
func signerInfo(commitment asn1.ObjectIdentifier, counters ...[]byte) []byte {
	parts := [][]byte{
		mustDER(1), seqOf(), seqOf(mustDER(oidSHA256)),
		derOf(asn1.ClassContextSpecific, 0, seqOf(mustDER(oidCommitmentType()), setOf(seqOf(mustDER(commitment))))),
		seqOf(mustDER(oidRSA)), mustDER([]byte{1, 2, 3}),
	}
	if len(counters) > 0 {
		parts = append(parts, derOf(asn1.ClassContextSpecific, 1, seqOf(mustDER(oidCountersignatures()), setOf(counters...))))
	}
	return seqOf(parts...)
}

// signedCMS is the .signature.p7s of an author (proof of origin) or a repository (proof of receipt) with n repository countersignatures.
func signedCMS(receipt bool, counters int) []byte {
	commitment := oidProofOfOrigin
	if receipt {
		commitment = oidProofOfReceipt()
	}
	var cs [][]byte
	for range counters {
		cs = append(cs, signerInfo(oidProofOfReceipt()))
	}
	sd := seqOf(mustDER(1), setOf(seqOf(mustDER(oidSHA256))), seqOf(mustDER(oidData)), setOf(signerInfo(commitment, cs...)))
	return seqOf(mustDER(oidSignedData()), derOf(asn1.ClassContextSpecific, 0, sd))
}

// pushSigned pushes a package with a signature entry through the push route, as dotnet nuget push does.
func pushSigned(id, version string, p7s []byte) func(h *formattest.Host) error {
	return func(h *formattest.Host) error {
		pkg, err := makeNupkg(id, version, withEntry(signatureEntry, p7s))
		if err != nil {
			return err
		}
		body, ct, err := makeMultipart(pkg)
		if err != nil {
			return err
		}
		r, err := h.Serve(context.Background(), http.MethodPut, "v3/package", body, false, func(r *http.Request) { r.Header.Set("Content-Type", ct) })
		if err == nil && r.Code != http.StatusCreated {
			err = fmt.Errorf("push: status %d: %s", r.Code, r.Body.String())
		}
		return err
	}
}

// signatureCases are check 18's samples of NuGet: an author-signed package with a repository countersignature, and a
// repository-signed one, each pushed through the push route. The package's digest is the subject; the kit compares it from the event.
func signatureCases() []formattest.SignatureCase {
	const path = "acme.widgets/1.0.0/acme.widgets.1.0.0.nupkg"
	author, repo := signedCMS(false, 1), signedCMS(true, 0)
	pkgOf := func(p7s []byte) [32]byte {
		b, err := makeNupkg("Acme.Widgets", "1.0.0", withEntry(signatureEntry, p7s))
		if err != nil {
			panic(err)
		}
		return sha256.Sum256(b)
	}
	want := func(p7s []byte, part int, role string) formats.SignatureCandidate {
		pkg := pkgOf(p7s)
		return formats.SignatureCandidate{
			Object: formats.SignatureLocator{Blob: pkg, Locator: formats.SignatureLocatorEntryPrefix + signatureEntry, SHA256: sha256.Sum256(p7s)},
			Part:   part, Subject: pkg, Role: role, SchemeHint: formats.SignatureSchemeNuGetCMS, Source: formats.SignatureSourceEmbedded,
			Location: path, SignedContent: formats.SignatureLocator{Blob: pkg, Locator: formats.SignatureLocatorUnsigned},
		}
	}
	return []formattest.SignatureCase{
		{
			Kind: "author signature", Path: path, Commit: pushSigned("Acme.Widgets", "1.0.0", author),
			Want: []formats.SignatureCandidate{want(author, 0, formats.SignatureRolePublisher), want(author, 1, formats.SignatureRoleRegistry)},
		},
		{
			Kind: "repository signature", Path: path, Commit: pushSigned("Acme.Widgets", "1.0.0", repo),
			Want: []formats.SignatureCandidate{want(repo, 0, formats.SignatureRoleRegistry)},
		},
	}
}

func detectNuGet(t *testing.T, p string, body []byte) []formats.SignatureCandidate {
	t.Helper()
	sum := sha256.Sum256(body)
	got, err := New().Signatures().Detect(context.Background(), formats.SignatureEvent{
		Item: formats.ItemRef{ID: 2, Path: p}, Path: formats.Path{Path: p}, Digests: formats.Digests{SHA256: sum[:]},
		Content: formattest.ContentOf(body),
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestR_SIG_029_TheSignatureInsideThePackage: a package with .signature.p7s names its primary signature with the role its commitment
// type gives and each countersignature as a part of its own; an unsigned package and another file name nothing; a signature entry
// that is no CMS is named as the publisher's for the read to judge. Unit, milliseconds, packages built by the test.
func TestR_SIG_029_TheSignatureInsideThePackage(t *testing.T) {
	const p = "acme.widgets/1.0.0/acme.widgets.1.0.0.nupkg"
	for name, c := range map[string]struct {
		p7s   []byte
		roles []string
	}{
		"an author signature with two countersignatures": {signedCMS(false, 2), []string{formats.SignatureRolePublisher, formats.SignatureRoleRegistry, formats.SignatureRoleRegistry}},
		"a repository signature":                         {signedCMS(true, 0), []string{formats.SignatureRoleRegistry}},
		"an entry that is no CMS":                        {[]byte("not der"), []string{formats.SignatureRolePublisher}},
	} {
		t.Run(name, func(t *testing.T) {
			pkg := buildNupkg(t, "Acme.Widgets", "1.0.0", withEntry(signatureEntry, c.p7s))
			got := detectNuGet(t, p, pkg)
			if len(got) != len(c.roles) {
				t.Fatalf("%d candidates, want %d: %+v", len(got), len(c.roles), got)
			}
			for i, g := range got {
				if g.Role != c.roles[i] || g.Part != i || g.Object.SHA256 != sha256.Sum256(c.p7s) || g.Object.Size != int64(len(c.p7s)) ||
					g.SignedContent.Locator != formats.SignatureLocatorUnsigned || g.Source != formats.SignatureSourceEmbedded {
					t.Errorf("candidate %d = %+v", i, g)
				}
			}
		})
	}
	t.Run("an unsigned package and its .nuspec name nothing", func(t *testing.T) {
		if got := detectNuGet(t, p, buildNupkg(t, "Acme.Widgets", "1.0.0")); len(got) != 0 {
			t.Fatalf("unsigned: %+v", got)
		}
		if got := detectNuGet(t, "acme.widgets/1.0.0/acme.widgets.nuspec", []byte("<package/>")); len(got) != 0 {
			t.Fatalf("nuspec: %+v", got)
		}
	})
}

// TestR_SIG_029_TheServiceIndexListsNoRepositorySignaturesResource: Quader signs no NuGet package, so no service index announces the
// certificates a source signs with, whatever the class (Q-SIG-36). Unit, microseconds.
func TestR_SIG_029_TheServiceIndexListsNoRepositorySignaturesResource(t *testing.T) {
	for _, publish := range []bool{true, false} {
		for _, vulnerabilities := range []bool{true, false} {
			if idx := serviceIndexOf("https://quader.example.com/api/nuget/web/nuget-remote/v3/", publish, vulnerabilities); strings.Contains(string(idx), "RepositorySignatures") {
				t.Errorf("publish %t: the service index lists RepositorySignatures: %s", publish, idx)
			}
		}
	}
}
