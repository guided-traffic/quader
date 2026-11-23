package nugetcms

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/asn1"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki/pkitest"
)

// The signatures of the real tools (signatures.md R-SIG-029, R-SIG-042, Q-SIG-216; developer/signatures.md, "Measured with real
// clients", defect 1):
//
//   - testdata/dotnet-sign-by-key.nupkg (5.6 KiB) and testdata/dotnet-sign-by-issuer.nupkg (5.7 KiB): one toy package of `dotnet pack`
//     (Example.Toy, a class with one constant, at 1.0.0 and 1.1.0), each signed by `dotnet nuget sign` of the .NET SDK 10.0.403 (NuGet
//     7.9.0) on 2026-11-23 with a leaf of a throwaway RSA root made for the test (O=Example GmbH, valid until 2036 for the leaves and 2046
//     for the root; the keys were thrown away, the root is in the signature); no timestamp. NuGet names the signer by its subject key
//     identifier when the leaf carries the extension and by issuer and serial number when it does not (NuGet's SigningUtility), so
//     the first leaf carries it and the second does not: the two forms of RFC 5652's SignerIdentifier, written by the real tool.
//   - testdata/nugetorg-microsoft.extensions.primitives-8.0.0.p7s (24 KiB, kept since 2026-11-21): the .signature.p7s of
//     Microsoft.Extensions.Primitives 8.0.0 as nuget.org serves it: Microsoft's author signature and nuget.org's repository
//     countersignature, each with a timestamp of DigiCert, both named by key. The package itself (205 KiB) is not kept: the steps before
//     the package's hash are what the test needs.
//
// Before Q-SIG-216 nothing read the first form: every signature of the tests was made by digitorus/pkcs7, which writes and reads the
// second alone.

// dotnetPackages are the two packages dotnet signed, with the form of the signer each names and its subject.
var dotnetPackages = []struct {
	file, form, subject string
	byKey               bool
}{
	{"dotnet-sign-by-key.nupkg", "subject key identifier", "CN=Example NuGet author,O=Example GmbH,C=DE", true},
	{"dotnet-sign-by-issuer.nupkg", "issuer and serial number", "CN=Example NuGet author by issuer and serial,O=Example GmbH,C=DE", false},
}

// collector is a hash.Hash that keeps what is written: the unsigned package writeUnsigned streams.
type collector struct{ bytes.Buffer }

func (c *collector) Sum(b []byte) []byte { return append(b, c.Bytes()...) }
func (c *collector) Size() int           { return 0 }
func (c *collector) BlockSize() int      { return 1 }

// unsignedOf is a signed package without its signature entry, as the package's hash is computed over it (R-SIG-006).
func unsignedOf(t *testing.T, pkg []byte) []byte {
	t.Helper()
	z, err := layout(bytes.NewReader(pkg), int64(len(pkg)))
	if err != nil {
		t.Fatal(err)
	}
	var c collector
	if err := z.writeUnsigned(context.Background(), bytes.NewReader(pkg), &c); err != nil {
		t.Fatal(err)
	}
	return c.Bytes()
}

// TestR_SIG_029_TheSignaturesDotnetNugetSignWritesAreFoundByBothFormsOfTheSigner: each of dotnet's packages reads valid, the publisher's
// author signature of its leaf under the throwaway root, its signer found by the form the package names it by; the package with one
// byte of its signature value changed reads invalid (signature_mismatch), and with an entry added after the signing invalid
// (digest_mismatch), each with no evidence. Unit, milliseconds, the two fixtures (11.3 KiB).
func TestR_SIG_029_TheSignaturesDotnetNugetSignWritesAreFoundByBothFormsOfTheSigner(t *testing.T) {
	for _, d := range dotnetPackages {
		t.Run(d.form, func(t *testing.T) {
			pkg := fixture(t, d.file)
			out := run(t, packageInput(pkg, 0))
			wantState(t, out, signatures.StateValid, "")
			s := signerOfOutput(t, out)
			if out.Result.Role != signatures.RolePublisher || s.CommitmentType != commitmentAuthor || out.Parts != 1 || s.Subject != d.subject ||
				len(out.Evidence.Chain) != 2 || len(out.Evidence.Timestamps) != 0 {
				t.Fatalf("role %s, parts %d, signer %+v, evidence %d certificates and %d timestamps", out.Result.Role, out.Parts, s,
					len(out.Evidence.Chain), len(out.Evidence.Timestamps))
			}
			p7s, err := signatureBytes(context.Background(), packageInput(pkg, 0))
			if err != nil {
				t.Fatal(err)
			}
			p, err := parse(p7s, 32, 16, 16<<10)
			if err != nil {
				t.Fatal(err)
			}
			// The form of the signer: [0] subjectKeyIdentifier (a SignerInfo of version 3), or the SEQUENCE issuerAndSerialNumber.
			if byKey := p.primary.sid.Class == asn1.ClassContextSpecific; byKey != d.byKey {
				t.Fatalf("the fixture's signer is named by key: %v, want %v; it no longer shows what dotnet writes", byKey, d.byKey)
			}

			unsigned := unsignedOf(t, pkg)
			count := len(mustLayout(t, unsigned).records)
			if again := run(t, packageInput(insertEntry(t, unsigned, count, signatureEntry, p7s), 0)); again.Result.State != signatures.StateValid {
				t.Fatalf("the package put together again reads %s (%s)", again.Result.State, again.Result.Reason)
			}
			flipped := bytes.Clone(p7s)
			i := bytes.Index(flipped, p.primary.EncryptedDigest)
			flipped[i+len(p.primary.EncryptedDigest)-1] ^= 1
			added := insertEntry(t, insertEntry(t, unsigned, 0, "extra.txt", []byte("added after signing")), count+1, signatureEntry, p7s)
			for _, tc := range []struct {
				name   string
				pkg    []byte
				reason string
			}{
				{"one byte of the signature value changed", insertEntry(t, unsigned, count, signatureEntry, flipped), signatures.ReasonSignatureMismatch},
				{"an entry added after the signing", added, signatures.ReasonDigestMismatch},
			} {
				out := run(t, packageInput(tc.pkg, 0))
				wantState(t, out, signatures.StateInvalid, tc.reason)
				if out.Evidence != nil {
					t.Fatalf("%s: an invalid signature has evidence", tc.name)
				}
			}
		})
	}
}

func mustLayout(t *testing.T, pkg []byte) *zipLayout {
	t.Helper()
	z, err := layout(bytes.NewReader(pkg), int64(len(pkg)))
	if err != nil {
		t.Fatal(err)
	}
	return z
}

func fixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func signerOfOutput(t *testing.T, out scheme.Output) signatures.Signer {
	t.Helper()
	var s signatures.Signer
	if err := json.Unmarshal(out.Result.Signer, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestR_SIG_029_NugetOrgsAuthorSignatureAndRepositoryCountersignatureRead(t *testing.T) {
	p7s := fixture(t, "nugetorg-microsoft.extensions.primitives-8.0.0.p7s")
	l := pki.Limits(scheme.DefaultLimits())
	p, err := parse(p7s, l.ASN1Depth, 2*l.MaxChain, l.MaxCertificateSize)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p.counters) != 1 {
		t.Fatalf("%d countersignatures, want nuget.org's one", len(p.counters))
	}
	for _, tc := range []struct {
		name, subject, commitment string
		part                      int
		si                        *signerInfo
		message                   []byte
	}{
		{"Microsoft's author signature", "Microsoft Corporation", commitmentAuthor, 0, p.primary, p.content},
		{"nuget.org's repository countersignature", "NuGet.org Repository by Microsoft", commitmentRepository, 1, p.counters[0], p.primary.EncryptedDigest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.si.sid.Class != asn1.ClassContextSpecific {
				t.Fatalf("the signer is named by issuer and serial number; the fixture no longer shows what nuget.org writes")
			}
			chain, err := signerChain(tc.si, p.certs, l)
			if err != nil {
				t.Fatalf("chain: %v", err)
			}
			rd := &pki.Reading{}
			if err := readAttributes(tc.si, tc.part, rd); err != nil {
				t.Fatalf("attributes: %v", err)
			}
			if err := checkPrimary(p, tc.si, chain[0], tc.message, l); err != nil {
				t.Fatalf("signature: %v", err)
			}
			stamps, err := timestamps(tc.si, l, rd)
			if err != nil {
				t.Fatalf("timestamps: %v", err)
			}
			if chain[0].Subject.CommonName != tc.subject || rd.Signer.CommitmentType != tc.commitment || len(stamps) != 1 ||
				len(rd.Signer.Timestamps) != 1 || !strings.HasPrefix(rd.Signer.Timestamps[0].Authority, "CN=DigiCert") || stamps[0].Time.Year() != 2023 {
				t.Fatalf("signer %s, commitment %s, %d timestamps %+v", chain[0].Subject, rd.Signer.CommitmentType, len(stamps), rd.Signer.Timestamps)
			}
			if tc.part == 1 {
				root := chain[len(chain)-1]
				if rd.Signer.ServiceIndex != "https://api.nuget.org/v3/index.json" || root.SignatureAlgorithm != x509.SHA1WithRSA ||
					!bytes.Equal(root.RawIssuer, root.RawSubject) {
					t.Fatalf("service index %q, the chain ends at %s signed with %s", rd.Signer.ServiceIndex, root.Subject, root.SignatureAlgorithm)
				}
			}
		})
	}
	t.Run("the whole check reaches the package's hash", func(t *testing.T) {
		// The package is not kept: the check passes every step before it and stops at the hash of another package.
		blobs := pkitest.NewBlobs()
		other := blobs.Put(insertEntry(t, unsignedPackage(t), 3, signatureEntry, p7s))
		for part := range 2 {
			out := run(t, scheme.Input{
				Scheme: signatures.SchemeNuGetCMS, Part: part, Raw: p7s, Subject: other, ContentBlob: other, ContentLocator: unsignedLocator,
				Blobs: blobs, Limits: scheme.DefaultLimits(),
			})
			if out.Result.State != signatures.StateInvalid || out.Result.Reason != signatures.ReasonDigestMismatch {
				t.Fatalf("part %d: %s (%s), want invalid (digest_mismatch): %s", part, out.Result.State, out.Result.Reason, out.Result.Signer)
			}
		}
	})
}

func TestR_SIG_006_ASignerNamedByItsKeyIdentifierIsFound(t *testing.T) {
	unsigned := unsignedPackage(t)
	repo := newSigner(t, "Example Repository GmbH")
	author := newSigner(t, "Example GmbH")
	p7s := sign(t, unsigned, signOptions{author: author, tsa: pkitest.NewTSA(t), counter: &repo, keyIDs: true})
	for _, tc := range []struct {
		name string
		p7s  []byte
	}{{"DER", p7s}, {"BER with indefinite lengths", indefinite(t, p7s)}} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := insertEntry(t, unsigned, 3, signatureEntry, tc.p7s)
			for part := range 2 {
				out := run(t, packageInput(pkg, part))
				wantState(t, out, signatures.StateValid, "")
				if part == 0 && (len(out.Evidence.Timestamps) != 1 || len(signerOfOutput(t, out).Timestamps) != 1) {
					t.Fatalf("the timestamp named by its authority's key identifier is not evidence: %+v", out.Evidence)
				}
			}
		})
	}
	t.Run("a key identifier no certificate of the signature has", func(t *testing.T) {
		stranger := newSigner(t, "Stranger GmbH")
		foreign := signedDataByKeyID(t, sign(t, unsigned, signOptions{author: author}), stranger.leaf)
		out := run(t, packageInput(insertEntry(t, unsigned, 3, signatureEntry, foreign), 0))
		wantState(t, out, signatures.StateInvalid, signatures.ReasonChainBroken)
		if !strings.Contains(signerOfOutput(t, out).Detail, "the key ") {
			t.Fatalf("the detail does not name the key: %s", out.Result.Signer)
		}
	})
}
