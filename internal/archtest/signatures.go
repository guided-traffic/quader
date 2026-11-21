package archtest

import "strings"

// signatureRoots are the trees of signatures on artifacts (stage 1, internal/signatures) and of Quader's own signing (stage 2,
// internal/signing), the only importers of the verification libraries (signatures.md R-SIG-014; stage 2 builds its trusted root and its
// bundles with sigstore-go, R-SIG-052).
func signatureRoots() []string { return []string{"internal/signatures", "internal/signing"} }

// signatureLibraries are the verification libraries of R-SIG-014 and the modules they bring that verify: sigstore-go and the other
// modules of github.com/sigstore, notation-core-go, tspclient-go, go-cose, digitorus/pkcs7 and digitorus/timestamp. go-crypto is not
// among them: internal/openpgp imports it for the Debian format's own signing and verification (R-DEB-015, R-DEB-020, Q-ARCH-383) as well
// as internal/signatures/scheme/openpgp, so its rule is that of internal/openpgp, not this one.
func signatureLibraries() []string {
	return []string{
		"github.com/sigstore",
		"github.com/notaryproject/notation-core-go",
		"github.com/notaryproject/tspclient-go",
		"github.com/veraison/go-cose",
		"github.com/digitorus/pkcs7",
		"github.com/digitorus/timestamp",
	}
}

// CheckSignatureLibraries implements R-SIG-014's rule that only internal/signatures/... and internal/signing/... import the verification
// libraries, so that no format and no other package reaches them. The trees that are not checked (tests, tools) are left out, as for
// every import rule.
func CheckSignatureLibraries(pkgs []Package) []Violation {
	var out []Violation
	for _, p := range pkgs {
		if underAny(p.Rel, unchecked()) || underAny(p.Rel, signatureRoots()) {
			continue
		}
		for _, imp := range p.Imports {
			if pathForbids(imp, signatureLibraries()) {
				out = append(out, Violation{
					Rule: "R-SIG-014", From: p.Rel, To: imp,
					Detail: "only internal/signatures/... and internal/signing/... import the libraries that verify or make signatures",
				})
			}
		}
	}
	sortViolations(out)
	return out
}

// The packages of R-SIG-049's rule: internal/signing/key holds the sealed column signing_identities.sealed_key and reads
// signing.identity.key_file, internal/signing/job holds the opened keys and signs.
const (
	signingKeyPkg = "internal/signing/key"
	signingJobPkg = "internal/signing/job"
)

// signingKeyImporters are the packages that may import internal/signing/key: the store, which seals a key Quader made or imported, the
// job, which opens it and reads the key file, and the composition root, which wires the re-encryption of the column (R-ARCH-021).
func signingKeyImporters() []string {
	return []string{"internal/signing/store", signingJobPkg, "internal/app"}
}

// CheckSigningKey implements R-SIG-049's rule that only the signing tree opens the identity's private key: no package but those of
// signingKeyImporters imports internal/signing/key, and none but the composition root and the signing tree imports internal/signing/job,
// whose holder alone keeps an opened key. The management API (internal/api/v1) reads the identities through internal/signing and
// internal/signing/store and opens nothing; the composition root builds the job for the roles worker and all only, which
// TestR_SIG_049_OnlyTheRolesThatRunJobsBuildASigner of internal/app holds.
func CheckSigningKey(pkgs []Package) []Violation {
	var out []Violation
	for _, p := range pkgs {
		if underAny(p.Rel, unchecked()) || p.Rel == signingKeyPkg || p.Rel == signingJobPkg {
			continue
		}
		for _, imp := range p.Imports {
			switch {
			case importsPkg(imp, signingKeyPkg) && !contains(signingKeyImporters(), p.Rel):
				out = append(out, Violation{
					Rule: "R-SIG-049", From: p.Rel, To: imp,
					Detail: "only the signing store, the signing job and the composition root import the identity's private key",
				})
			case importsPkg(imp, signingJobPkg) && p.Rel != "internal/app":
				out = append(out, Violation{
					Rule: "R-SIG-049", From: p.Rel, To: imp,
					Detail: "only the composition root builds the signing job, the one holder of an opened key",
				})
			}
		}
	}
	sortViolations(out)
	return out
}

// importsPkg reports whether the import path imp is the module's package rel, whatever the module's path.
func importsPkg(imp, rel string) bool { return imp == rel || strings.HasSuffix(imp, "/"+rel) }

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
