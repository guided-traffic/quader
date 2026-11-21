package nugetcms

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"testing"
	"time"

	"github.com/digitorus/pkcs7"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki/pkitest"
)

func run(t *testing.T, in scheme.Input) scheme.Output {
	t.Helper()
	out, err := Check(context.Background(), in)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return out
}

func wantState(t *testing.T, out scheme.Output, state signatures.State, reason string) {
	t.Helper()
	if out.Result.State != state || out.Result.Reason != reason {
		t.Fatalf("state %s (%s), want %s (%s); signer %s", out.Result.State, out.Result.Reason, state, reason, out.Result.Signer)
	}
}

// packageInput is the check of part of a signed package's signature, as the NuGet detector names it.
func packageInput(pkg []byte, part int) scheme.Input {
	blobs := pkitest.NewBlobs()
	d := blobs.Put(pkg)
	return scheme.Input{
		Scheme: signatures.SchemeNuGetCMS, Role: signatures.RolePublisher, Part: part, ItemBlob: d, Locator: signatureLocator, Subject: d,
		ContentBlob: d, ContentLocator: unsignedLocator, Blobs: blobs, Limits: scheme.DefaultLimits(), ReceivedAt: time.Now(),
	}
}

func TestR_SIG_006_TheUnsignedBytesOfASignedPackageAreThePackageBeforeSigning(t *testing.T) {
	unsigned := unsignedPackage(t)
	want := sha256.Sum256(unsigned)
	for _, tc := range []struct {
		name string
		at   int
	}{{"first", 0}, {"in the middle", 1}, {"last", 3}} {
		t.Run(tc.name, func(t *testing.T) {
			pkg := insertEntry(t, unsigned, tc.at, signatureEntry, []byte("a signature of some bytes"))
			blobs := pkitest.NewBlobs()
			in := scheme.Input{Blobs: blobs, Limits: scheme.DefaultLimits()}
			h := sha256.New()
			if err := hashUnsigned(context.Background(), in, blobs.Put(pkg), h); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(h.Sum(nil), want[:]) {
				t.Fatalf("the unsigned bytes of the package signed %s are not the package before signing", tc.name)
			}
		})
	}
}

func TestR_SIG_029_AnAuthorSignatureWithATimestampAndARepositoryCountersignature(t *testing.T) {
	unsigned := unsignedPackage(t)
	repo := newSigner(t, "Example Repository GmbH")
	p7s := sign(t, unsigned, signOptions{author: newSigner(t, "Example GmbH"), tsa: pkitest.NewTSA(t), counter: &repo})
	pkg := insertEntry(t, unsigned, 3, signatureEntry, p7s)
	author := run(t, packageInput(pkg, 0))
	wantState(t, author, signatures.StateValid, "")
	var s signatures.Signer
	if err := json.Unmarshal(author.Result.Signer, &s); err != nil {
		t.Fatal(err)
	}
	ev := author.Evidence
	if author.Result.Role != signatures.RolePublisher || s.CommitmentType != commitmentAuthor || author.Parts != 2 || len(ev.Chain) != 3 ||
		len(ev.Timestamps) != 1 || len(ev.Timestamps[0].Chain) != 2 || len(s.Timestamps) != 1 || author.Result.ClaimedAt.IsZero() {
		t.Fatalf("role %s, signer %+v, parts %d, evidence %+v", author.Result.Role, s, author.Parts, ev)
	}
	counter := run(t, packageInput(pkg, 1))
	wantState(t, counter, signatures.StateValid, "")
	s = signatures.Signer{}
	if err := json.Unmarshal(counter.Result.Signer, &s); err != nil {
		t.Fatal(err)
	}
	if counter.Result.Role != signatures.RoleRegistry || s.CommitmentType != commitmentRepository ||
		s.ServiceIndex != "https://api.nuget.example.com/v3/index.json" || len(s.Owners) != 1 || s.Subject != repo.leaf.Subject.String() {
		t.Fatalf("role %s, signer %+v", counter.Result.Role, s)
	}
}

func TestR_SIG_006_AFlippedByteOfTheContentOrTheSignatureIsInvalid(t *testing.T) {
	unsigned := unsignedPackage(t)
	author, repo := newSigner(t, "Example GmbH"), newSigner(t, "Example Repository GmbH")
	p7s := sign(t, unsigned, signOptions{author: author, counter: &repo})
	changed := insertEntry(t, insertEntry(t, unsigned, 0, "extra.txt", []byte("added after signing")), 4, signatureEntry, p7s)
	flipped := bytes.Clone(p7s)
	p, err := pkcs7.Parse(p7s)
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.Index(flipped, p.Signers[0].EncryptedDigest)
	flipped[i+len(p.Signers[0].EncryptedDigest)-1] ^= 1
	foreign := sign(t, unsigned, signOptions{author: author, counter: &repo, counterOver: []byte("another signature value")})
	for _, tc := range []struct {
		name   string
		in     scheme.Input
		reason string
	}{
		{"the package's bytes changed after signing", packageInput(changed, 0), signatures.ReasonDigestMismatch},
		{"a flipped byte of the signature", packageInput(insertEntry(t, unsigned, 3, signatureEntry, flipped), 0), signatures.ReasonSignatureMismatch},
		{"a countersignature over another signature value", packageInput(insertEntry(t, unsigned, 3, signatureEntry, foreign), 1), signatures.ReasonSignatureMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := run(t, tc.in)
			wantState(t, out, signatures.StateInvalid, tc.reason)
			if out.Evidence != nil {
				t.Fatalf("an invalid signature has evidence")
			}
		})
	}
}

func TestR_SIG_013_ACMSWithAnIndefinitelyNestedAttributeIsRefused(t *testing.T) {
	// A ContentInfo of signedData whose content nests indefinite-length SEQUENCEs forty deep.
	p7s := []byte{0x30, 0x80, 0x06, 0x09, 0x2a, 0x86, 0x48, 0x86, 0xf7, 0x0d, 0x01, 0x07, 0x02, 0xa0, 0x80}
	for range 40 {
		p7s = append(p7s, 0x30, 0x80)
	}
	for range 42 {
		p7s = append(p7s, 0x00, 0x00)
	}
	in := scheme.Input{Scheme: signatures.SchemeNuGetCMS, Raw: p7s, Limits: scheme.DefaultLimits()}
	wantState(t, run(t, in), signatures.StateMalformed, signatures.ReasonLimitsExceeded)
}
