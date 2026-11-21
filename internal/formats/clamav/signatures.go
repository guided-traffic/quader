package clamav

import "github.com/guided-traffic/quader/internal/formats"

// noSignatureReason is what the details of a version say instead of "This format has no signatures" alone (signatures.md R-SIG-002,
// Q-SIG-96): a container's signature is part of ClamAV's own file format, which the malware scanning's import checks before it stores
// the container and which the version shows as its verification (formats/clamav.md R-CLAMAV-003, malware-scanning.md R-MAL-033).
const noSignatureReason = "ClamAV's signature of a container is part of its file format: the import checks it before the container is " +
	"stored, and the version shows it as its verification."

// Signatures declares none (Q-SIG-96): no client of the format reads a signature through Quader's store, and what ClamAV signs is
// checked where it arrives, by the import, and again by every clamd and freshclam that loads the container.
func (Format) Signatures() formats.SignatureAdapter {
	return formats.NoSignatures(noSignatureReason, noAttestationReason)
}

// noAttestationReason is why the definitions get no receipt attestation of Quader's (signatures.md R-SIG-044): they are what the product
// fetches for its own tool in a system repository, and clamd checks the publisher's signature of each container itself.
const noAttestationReason = "The definitions are the product's own tool data in a system repository: clamd checks ClamAV's signature of " +
	"each container itself, and a receipt of Quader's would be read by no one."
