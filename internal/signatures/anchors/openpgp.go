package anchors

import (
	"errors"
	"fmt"
	"strings"

	"github.com/guided-traffic/quader/internal/openpgp"
)

// checkOpenPGP checks an OpenPGP anchor's material (R-SIG-015) by the rules of deb.upstreamKeys (internal/openpgp, R-DEB-020): 1 to 32
// armored public certificates in at most 256 KiB, no secret key material, a signing key or subkey, RSA keys of at least 2048 bits, and
// at save neither revoked nor expired. Stored material is read again without the checks of its validity at save. This package adds the
// algorithms of R-SIG-015 and the key IDs of every key, which it reads from the certificate's public key packets.
func checkOpenPGP(m *OpenPGPMaterial, c checking) (*Parsed, error) {
	const ptr = "/openpgp/keys"
	if strings.TrimSpace(m.Keys) == "" {
		return nil, refuse(ptr, "at least one armored certificate is needed")
	}
	if len(m.Keys) > maxOpenPGPBytes {
		return nil, refuse(ptr, fmt.Sprintf("the keys are at most %d KiB", maxOpenPGPBytes>>10))
	}
	lim := openpgp.Limits{MaxCertificateBytes: maxOpenPGPBytes, MaxCertificates: maxOpenPGPCerts}
	var (
		certs []openpgp.Certificate
		err   error
	)
	if c.atSave {
		certs, err = openpgp.ParseCertificates([]byte(m.Keys), c.now, lim)
	} else {
		certs, err = openpgp.ParseStoredCertificates([]byte(m.Keys), lim)
	}
	if err != nil {
		return nil, openPGPRefusal(err)
	}
	out := &Parsed{}
	for _, cert := range certs {
		pc, err := openPGPCertificate(cert)
		if err != nil {
			return nil, err
		}
		out.OpenPGP = append(out.OpenPGP, pc)
		out.Describes = append(out.Describes, Fingerprint{Kind: "openpgp", Value: pc.Fingerprint, Subject: pc.UserID, NotAfter: pc.Expires})
	}
	return out, nil
}

// openPGPRefusal turns an error of internal/openpgp into a refusal; secret key material answers secret_material.
func openPGPRefusal(err error) *Invalid {
	reason := strings.TrimPrefix(err.Error(), openpgp.ErrInvalid.Error()+": ")
	if errors.Is(err, openpgp.ErrInvalid) && strings.Contains(reason, "secret key material") {
		return &Invalid{Pointer: "/openpgp/keys", Code: CodeSecretMaterial, Reason: "the keys hold secret key material; an anchor holds public material only"}
	}
	return refuse("/openpgp/keys", reason)
}

// The OpenPGP public-key algorithms of RFC 9580 an anchor's keys may use (R-SIG-015): a key that signs is RSA, ECDSA or EdDSA; a subkey
// that only encrypts may also be RSA encrypt-only, Elgamal, ECDH, X25519 or X448. DSA and unknown algorithms are refused.
func signAlgorithm(a uint8) bool {
	switch a {
	case 1, 3, 19, 22, 27, 28: // RSA, RSA sign-only, ECDSA, EdDSA legacy, Ed25519, Ed448
		return true
	}
	return false
}

func encryptAlgorithm(a uint8) bool {
	switch a {
	case 2, 16, 18, 25, 26: // RSA encrypt-only, Elgamal, ECDH, X25519, X448
		return true
	}
	return false
}

// openPGPCertificate is one certificate with its armor, its key IDs and the algorithms of its keys checked.
func openPGPCertificate(cert openpgp.Certificate) (OpenPGPCertificate, error) {
	const ptr = "/openpgp/keys"
	armored := openpgp.ArmorCertificates(cert)
	keys, err := publicKeyPackets(armored)
	if err != nil || len(keys) == 0 || keys[0].fingerprint != cert.Fingerprint {
		return OpenPGPCertificate{}, refuse(ptr, fmt.Sprintf("certificate %s cannot be read again", cert.Fingerprint))
	}
	out := OpenPGPCertificate{
		Armored: armored, Fingerprint: cert.Fingerprint, UserID: cert.UserID, Created: cert.Created,
		Expires: cert.Expires,
	}
	for i, k := range keys {
		allowed := signAlgorithm(k.algorithm) || (i > 0 && encryptAlgorithm(k.algorithm))
		if !allowed {
			return OpenPGPCertificate{}, refuse(ptr, fmt.Sprintf("certificate %s has a key of algorithm %d; RSA, ECDSA and EdDSA keys can sign",
				cert.Fingerprint, k.algorithm))
		}
		out.KeyIDs = append(out.KeyIDs, k.keyID)
	}
	return out, nil
}
