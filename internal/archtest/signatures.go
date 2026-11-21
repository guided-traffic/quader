package archtest

// signatureRoot is the tree of signatures on artifacts, the only importer of the verification libraries (signatures.md R-SIG-014).
const signatureRoot = "internal/signatures"

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

// CheckSignatureLibraries implements R-SIG-014's rule that only internal/signatures/... imports the verification libraries, so that no
// format and no other package reaches them. The trees that are not checked (tests, tools) are left out, as for every import rule.
func CheckSignatureLibraries(pkgs []Package) []Violation {
	var out []Violation
	for _, p := range pkgs {
		if underAny(p.Rel, unchecked()) || under(p.Rel, signatureRoot) {
			continue
		}
		for _, imp := range p.Imports {
			if pathForbids(imp, signatureLibraries()) {
				out = append(out, Violation{
					Rule: "R-SIG-014", From: p.Rel, To: imp,
					Detail: "only internal/signatures/... imports the libraries that verify signatures",
				})
			}
		}
	}
	sortViolations(out)
	return out
}
