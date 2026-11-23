package nugetcms

import (
	"context"
	"errors"
	"testing"

	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki/pkitest"
)

// FuzzSignatureP7s: whatever bytes a .signature.p7s holds, the reader of this package neither panics nor hangs, and every step a
// check runs on the parts it finds (the parse, each signer's chain, attributes and signature, its timestamps) refuses with a closed
// refusal or stays within the bounds the code enforces: an object at most signatures.max_object_size, nesting at most ASN1Depth levels,
// at most 2*max_chain certificates of at most max_certificate_size bytes in the signature and max_chain in a timestamp, one primary
// signer, at most maxCountersignatures countersignatures, at most maxTimestamps timestamps a signer, a chain of at most max_chain, and
// content no longer than the object (R-SIG-013, Q-SIG-216). Seeded with the real tools' three signatures (dotnet's with the signer named
// by its key and by issuer and serial number, nuget.org's) and the tests' own in both forms of a signer, in DER and in BER.
func FuzzSignatureP7s(f *testing.F) {
	for _, name := range []string{"dotnet-sign-by-key.nupkg", "dotnet-sign-by-issuer.nupkg"} {
		dotnet, err := signatureBytes(context.Background(), packageInput(fixture(f, name), 0))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(dotnet)
	}
	f.Add(fixture(f, "nugetorg-microsoft.extensions.primitives-8.0.0.p7s"))
	unsigned := unsignedPackage(f)
	author, repo := newSigner(f, "Example GmbH"), newSigner(f, "Example Repository GmbH")
	tsa := pkitest.NewTSA(f)
	for _, keyIDs := range []bool{false, true} {
		p7s := sign(f, unsigned, signOptions{author: author, tsa: tsa, counter: &repo, keyIDs: keyIDs})
		f.Add(p7s)
		f.Add(indefinite(f, p7s))
	}
	l := pki.Limits(scheme.DefaultLimits())
	f.Fuzz(func(t *testing.T, p7s []byte) {
		if int64(len(p7s)) > l.MaxObjectSize {
			return // the job refuses a larger object before any read (R-SIG-013)
		}
		p, err := parse(p7s, l.ASN1Depth, 2*l.MaxChain, l.MaxCertificateSize)
		if err != nil {
			closedRefusal(t, err)
			return
		}
		if len(p.certs) > 2*l.MaxChain || len(p.counters) > maxCountersignatures || len(p.content) > len(p7s) {
			t.Fatalf("%d certificates, %d countersignatures, %d bytes of content out of %d", len(p.certs), len(p.counters), len(p.content), len(p7s))
		}
		for _, c := range p.certs {
			if len(c.Raw) > l.MaxCertificateSize {
				t.Fatalf("a certificate of %d bytes", len(c.Raw))
			}
		}
		for part, si := range append([]*signerInfo{p.primary}, p.counters...) {
			message := p.content
			if part > 0 {
				message = p.primary.EncryptedDigest
			}
			rd := &pki.Reading{}
			if err := readAttributes(si, part, rd); err != nil {
				closedRefusal(t, err)
			}
			if chain, err := signerChain(si, p.certs, l); err != nil {
				closedRefusal(t, err)
			} else {
				if len(chain) > l.MaxChain {
					t.Fatalf("a chain of %d certificates", len(chain))
				}
				if err := checkPrimary(p, si, chain[0], message, l); err != nil {
					closedRefusal(t, err)
				}
			}
			stamps, err := timestamps(si, l, rd)
			if err != nil {
				closedRefusal(t, err)
				continue
			}
			if len(stamps) > maxTimestamps {
				t.Fatalf("%d timestamps of one signer", len(stamps))
			}
			for _, s := range stamps {
				if len(s.Chain) > l.MaxChain {
					t.Fatalf("a timestamp's chain of %d certificates", len(s.Chain))
				}
			}
		}
	})
}

// closedRefusal fails a refusal that is none of pki's: every step answers a state and a closed reason.
func closedRefusal(t *testing.T, err error) {
	t.Helper()
	var r *pki.Refusal
	if !errors.As(err, &r) || r.State == "" || r.Reason == "" {
		t.Fatalf("not a closed refusal: %v", err)
	}
}
