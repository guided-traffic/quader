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
	t.Run("the signing tree, which builds its trusted root and its bundles with sigstore-go, passes", func(t *testing.T) {
		wantClean(t, CheckSignatureLibraries([]Package{
			pkg("internal/signing", "github.com/sigstore/sigstore-go/pkg/root"),
			pkg("internal/signing/bundle", "github.com/sigstore/sigstore-go/pkg/sign"),
		}))
	})
}

// TestR_SIG_049_OnlyTheSigningTreeOpensTheIdentitysKey: the management API, a format or the item store importing the package of the
// private key, or anything but the composition root importing the job that holds the opened keys, names the edge; the store (which
// seals), the job (which opens and reads the key file) and the composition root (which wires the job and the re-encryption) pass.
func TestR_SIG_049_OnlyTheSigningTreeOpensTheIdentitysKey(t *testing.T) {
	t.Run("an import from elsewhere names the edge", func(t *testing.T) {
		vs := CheckSigningKey([]Package{
			pkg("internal/api/v1", mod+"/internal/signing", mod+"/internal/signing/store", mod+"/internal/signing/key"),
			pkg("internal/formats/npm", mod+"/internal/signing/job"),
			pkg("internal/signing", mod+"/internal/signing/key"),
		})
		wantEdge(t, vs, "R-SIG-049", "internal/api/v1", mod+"/internal/signing/key")
		wantEdge(t, vs, "R-SIG-049", "internal/formats/npm", mod+"/internal/signing/job")
		wantEdge(t, vs, "R-SIG-049", "internal/signing", mod+"/internal/signing/key")
		if len(vs) != 3 {
			t.Fatalf("want exactly the three edges, got %v", Strings(vs))
		}
	})
	t.Run("the store, the job, the composition root and the tests pass", func(t *testing.T) {
		wantClean(t, CheckSigningKey([]Package{
			pkg("internal/signing/store", mod+"/internal/signing/key"),
			pkg("internal/signing/job", mod+"/internal/signing/key", mod+"/internal/signing/store"),
			pkg("internal/app", mod+"/internal/signing/key", mod+"/internal/signing/job"),
			pkg("test/integration/signing", mod+"/internal/signing/key"),
		}))
	})
	t.Run("the rule holds on the current tree", func(t *testing.T) {
		_, _, pkgs := realTree(t)
		wantClean(t, CheckSigningKey(pkgs))
	})
}
