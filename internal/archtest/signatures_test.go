package archtest

import "testing"

func TestR_SIG_014_OnlyTheSignaturesTreeImportsTheVerificationLibraries(t *testing.T) {
	t.Run("a format or another package importing a verification library names the edge", func(t *testing.T) {
		vs := CheckSignatureLibraries([]Package{
			pkg("internal/formats/oci", "github.com/sigstore/sigstore-go/pkg/bundle"),
			pkg("internal/formats/nuget", "github.com/digitorus/pkcs7"),
			pkg("internal/api/v1", "github.com/notaryproject/notation-core-go/signature/jws", "github.com/veraison/go-cose"),
			pkg("internal/proxy", "github.com/digitorus/timestamp", "github.com/notaryproject/tspclient-go"),
		})
		wantEdge(t, vs, "R-SIG-014", "internal/formats/oci", "github.com/sigstore/sigstore-go/pkg/bundle")
		wantEdge(t, vs, "R-SIG-014", "internal/formats/nuget", "github.com/digitorus/pkcs7")
		wantEdge(t, vs, "R-SIG-014", "internal/api/v1", "github.com/notaryproject/notation-core-go/signature/jws")
		wantEdge(t, vs, "R-SIG-014", "internal/api/v1", "github.com/veraison/go-cose")
		wantEdge(t, vs, "R-SIG-014", "internal/proxy", "github.com/digitorus/timestamp")
		wantEdge(t, vs, "R-SIG-014", "internal/proxy", "github.com/notaryproject/tspclient-go")
		if len(vs) != 6 {
			t.Fatalf("want exactly the six edges, got %v", Strings(vs))
		}
	})
	t.Run("the signatures tree, the test trees and go-crypto outside the tree pass", func(t *testing.T) {
		wantClean(t, CheckSignatureLibraries([]Package{
			pkg("internal/signatures/scheme/sigstore", "github.com/sigstore/sigstore-go/pkg/verify", "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"),
			pkg("internal/signatures/scheme/nugetcms", "github.com/digitorus/pkcs7", "github.com/digitorus/timestamp"),
			pkg("internal/signatures/scheme/notation", "github.com/notaryproject/notation-core-go/signature", "github.com/veraison/go-cose"),
			pkg("test/integration/signatures", "github.com/sigstore/sigstore-go/pkg/testing/ca"),
			pkg("internal/openpgp", "github.com/ProtonMail/go-crypto/openpgp"),
		}))
	})
	t.Run("the rule holds on the current tree", func(t *testing.T) {
		_, _, pkgs := realTree(t)
		wantClean(t, CheckSignatureLibraries(pkgs))
	})
}
