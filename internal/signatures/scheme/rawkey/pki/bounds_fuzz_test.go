package pki_test

import (
	"archive/zip"
	"bytes"
	"crypto"
	"encoding/asn1"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki/pkitest"
)

// The seeds of the BER bounds' fuzz target (R-SIG-013, Q-SIG-216): the signatures of the real tools nugetcms keeps as fixtures, a
// timestamp token of the tests' authority, and short structures of what BER allows and DER does not.

// realP7s are the .signature.p7s of `dotnet nuget sign` (out of its package) and of nuget.org.
func realP7s(tb testing.TB) [][]byte {
	tb.Helper()
	pkg, err := os.ReadFile("../../nugetcms/testdata/dotnet-sign-author.nupkg")
	if err != nil {
		tb.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(pkg), int64(len(pkg)))
	if err != nil {
		tb.Fatal(err)
	}
	f, err := z.Open(".signature.p7s")
	if err != nil {
		tb.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	dotnet, err := io.ReadAll(f)
	if err != nil {
		tb.Fatal(err)
	}
	nugetorg, err := os.ReadFile("../../nugetcms/testdata/nugetorg-microsoft.extensions.primitives-8.0.0.p7s")
	if err != nil {
		tb.Fatal(err)
	}
	return [][]byte{dotnet, nugetorg}
}

// berSeeds are structures BER allows: indefinite lengths, a long-form length where a short one does, a constructed OCTET STRING, a high
// tag number; and two that are refused: a tag number below 31 in the long form, and a nesting deeper than the bound.
func berSeeds() [][]byte {
	deep := []byte{}
	for range 40 {
		deep = append(deep, 0x30, 0x80)
	}
	for range 40 {
		deep = append(deep, 0, 0)
	}
	return [][]byte{
		{0x30, 0x80, 0x02, 0x01, 0x05, 0x00, 0x00},
		{0x30, 0x81, 0x03, 0x02, 0x01, 0x05},
		{0x24, 0x80, 0x04, 0x02, 'a', 'b', 0x04, 0x01, 'c', 0x00, 0x00},
		{0x3f, 0x81, 0x00, 0x00},
		{0x1f, 0x05, 0x00},
		deep,
	}
}

// FuzzDefinite: whatever bytes arrive, Definite neither panics nor hangs, refuses with a closed refusal of the state malformed,
// or returns a value that is DER in its tags and lengths at every level, nests at most ASN1Depth deep, is at most 2*ASN1Depth bytes
// longer than the input, passes CheckBER, and comes back unchanged from a second Definite (R-SIG-013, Q-SIG-216).
func FuzzDefinite(f *testing.F) {
	for _, p7s := range realP7s(f) {
		f.Add(p7s)
	}
	f.Add(pkitest.NewTSA(f).Token(f, crypto.SHA256, []byte("a signature value"), time.Now()))
	for _, s := range berSeeds() {
		f.Add(s)
	}
	depth := scheme.DefaultLimits().ASN1Depth
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := pki.Definite(data, depth)
		if err != nil {
			var r *pki.Refusal
			if !errors.As(err, &r) || r.State != signatures.StateMalformed ||
				(r.Reason != signatures.ReasonContentUnreadable && r.Reason != signatures.ReasonLimitsExceeded) {
				t.Fatalf("not a closed refusal: %v", err)
			}
			return
		}
		if len(out) > len(data)+2*depth {
			t.Fatalf("%d bytes out of %d in, more than %d more", len(out), len(data), 2*depth)
		}
		derTree(t, out, 1, depth)
		if err := pki.CheckBER(out, depth); err != nil {
			t.Fatalf("the output fails the bounds: %v", err)
		}
		again, err := pki.Definite(out, depth)
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("the output is not a fixed point: %v", err)
		}
	})
}

// derTree walks a sequence of values as encoding/asn1 reads DER, into every constructed value, at most maxDepth levels deep: an empty
// constructed value at the deepest level holds no value one level deeper (the seed ca72bc74dfb10117, which the target's first version of
// this walk refused).
func derTree(t *testing.T, b []byte, depth, maxDepth int) {
	t.Helper()
	for len(b) > 0 {
		if depth > maxDepth {
			t.Fatalf("the output nests deeper than %d levels", maxDepth)
		}
		var v asn1.RawValue
		rest, err := asn1.Unmarshal(b, &v)
		if err != nil {
			t.Fatalf("the output is not DER: %v", err)
		}
		if v.IsCompound {
			derTree(t, v.Bytes, depth+1, maxDepth)
		}
		b = rest
	}
}
