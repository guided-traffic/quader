package openpgp

import (
	"bytes"
	"crypto"
	"encoding/hex"
	"errors"
	"hash"
	"strconv"
	"strings"
	"time"

	pgp "github.com/ProtonMail/go-crypto/openpgp"
	pgperrors "github.com/ProtonMail/go-crypto/openpgp/errors"
	"github.com/ProtonMail/go-crypto/openpgp/packet"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki"
)

// maxCertificate bounds one armored certificate the verification parses (the bound of an OpenPGP anchor's material, R-SIG-015).
const maxCertificate = 256 << 10

// KeyMatch is what one certificate says about a signature packet and a stored digest (R-SIG-006, R-SIG-008): whether one of its keys is
// the one the packet's issuer names, whether that key verifies the packet against the digest, and whether it was valid at the time asked.
type KeyMatch struct {
	// Named is set when a key of the certificate (the primary or a subkey) is the one the issuer fingerprint or key ID names.
	Named bool
	// Verified is set when that key verifies the signature against the digest.
	Verified bool
	// ValidAt is set when the key that verified was created before the time asked, had not expired at it and was not revoked then, and
	// may sign; it is false when no time was asked.
	ValidAt bool
	// Fingerprint is the certificate's primary fingerprint (upper-case hex), KeyFingerprint the fingerprint of the key that verified, and
	// UserID the certificate's primary user ID.
	Fingerprint    string
	KeyFingerprint string
	UserID         string
	// Bits is the size of an RSA key that verified, 0 for any other algorithm.
	Bits int
}

// VerifyDigest verifies a signature packet (its bytes as the object carries them) against the digest it commits to (Q-SIG-23: the hash of
// the content followed by the packet's trailer, as [Check] keeps it in signatures.signed_digest) with the keys of one certificate, and
// judges the key that verified at the time at (the zero time judges nothing). It reads no content: the check verifies through it after
// hashing the content, and the trust step verifies through it with a key added later. An error is a packet or a certificate that does
// not parse.
func VerifyDigest(sigPacket, digest []byte, cert anchors.OpenPGPCertificate, at time.Time) (KeyMatch, error) {
	sig, err := parseSignature(sigPacket)
	if err != nil {
		return KeyMatch{}, err
	}
	entities, err := readCertificate(cert.Armored)
	if err != nil {
		return KeyMatch{}, err
	}
	var m KeyMatch
	for _, e := range entities {
		for _, k := range keysOf(e) {
			if !sig.CheckKeyIdOrFingerprint(k.pub) {
				continue
			}
			m.Named = true
			if !verifyStored(k.pub, sig, digest) {
				continue
			}
			m.Verified, m.Fingerprint, m.KeyFingerprint, m.UserID = true, fingerprint(e.PrimaryKey.Fingerprint), fingerprint(k.pub.Fingerprint), userID(e)
			m.Bits = rsaBits(k.pub)
			m.ValidAt = !at.IsZero() && validAt(e, k, at)
			return m, nil
		}
	}
	return m, nil
}

// verifyStored verifies sig with pk against a digest computed before: go-crypto's verification writes the trailer into the hash it is
// handed and takes its sum, so a hash whose sum is the stored digest makes it verify the digest without the content. The hash tag (the
// digest's first two octets, which a version 6 verification compares) was compared with the stored digest when it was computed.
func verifyStored(pk *packet.PublicKey, sig *packet.Signature, digest []byte) bool {
	if len(digest) != sig.Hash.Size() {
		return false
	}
	return pk.VerifySignature(storedHash{sum: digest}, sig) == nil
}

// storedHash is a hash.Hash whose sum is a stored digest: what is written to it is dropped.
type storedHash struct{ sum []byte }

func (h storedHash) Write(p []byte) (int, error) { return len(p), nil }
func (h storedHash) Sum(b []byte) []byte         { return append(b, h.sum...) }
func (h storedHash) Reset()                      {}
func (h storedHash) Size() int                   { return len(h.sum) }
func (h storedHash) BlockSize() int              { return 64 }

var _ hash.Hash = storedHash{}

// parseSignature parses one signature packet: a version 4 or 6 signature over a document (binary or text), of a hash go-crypto reads.
func parseSignature(raw []byte) (*packet.Signature, error) {
	p, err := packet.Read(bytes.NewReader(raw))
	if err != nil {
		var unsupported pgperrors.UnsupportedError
		if errors.As(err, &unsupported) {
			if strings.Contains(string(unsupported), "hash function") {
				return nil, pki.Unsupported(signatures.ReasonWeakAlgorithm, "the signature uses a hash Quader does not accept")
			}
			return nil, pki.Unsupported(signatures.ReasonVersionUnsupported, "the signature packet is of a version or algorithm Quader does not read")
		}
		return nil, pki.Malformed("the signature packet does not parse")
	}
	sig, ok := p.(*packet.Signature)
	if !ok {
		return nil, pki.Malformed("the packet is not a signature")
	}
	if sig.Version != 4 && sig.Version != 6 {
		return nil, pki.Unsupported(signatures.ReasonVersionUnsupported, "a version %d signature packet is not one Quader reads", sig.Version)
	}
	if sig.SigType != packet.SigTypeBinary && sig.SigType != packet.SigTypeText {
		return nil, pki.Unsupported(signatures.ReasonVersionUnsupported, "a signature of type %#x signs no document", uint8(sig.SigType))
	}
	return sig, nil
}

// weak refuses a hash below SHA-256 and a DSA signature (R-SIG-013, weak_algorithm).
func weak(sig *packet.Signature) error {
	switch sig.Hash {
	case crypto.SHA256, crypto.SHA384, crypto.SHA512, crypto.SHA3_256, crypto.SHA3_512:
	default:
		return pki.Unsupported(signatures.ReasonWeakAlgorithm, "the signature uses %s", pki.HashName(sig.Hash))
	}
	if sig.PubKeyAlgo == packet.PubKeyAlgoDSA {
		return pki.Unsupported(signatures.ReasonWeakAlgorithm, "the signature is a DSA signature")
	}
	return nil
}

// readCertificate parses one armored (or binary) public certificate, bounded.
func readCertificate(data []byte) (pgp.EntityList, error) {
	if len(data) > maxCertificate {
		return nil, errors.New("openpgp: the certificate is larger than an anchor holds")
	}
	var (
		el  pgp.EntityList
		err error
	)
	if bytes.Contains(data, []byte("-----BEGIN PGP")) {
		el, err = pgp.ReadArmoredKeyRing(bytes.NewReader(data))
	} else {
		el, err = pgp.ReadKeyRing(bytes.NewReader(data))
	}
	if err != nil {
		return nil, errors.New("openpgp: the certificate does not parse")
	}
	for _, e := range el {
		if e.PrivateKey != nil {
			return nil, errors.New("openpgp: the certificate holds secret key material")
		}
	}
	return el, nil
}

// certKey is a key of a certificate: the primary or a subkey, with the self-signature that binds it.
type certKey struct {
	pub     *packet.PublicKey
	binding *packet.Signature
	sub     *pgp.Subkey
}

func keysOf(e *pgp.Entity) []certKey {
	self, _ := e.PrimarySelfSignature()
	out := []certKey{{pub: e.PrimaryKey, binding: self}}
	for i := range e.Subkeys {
		s := &e.Subkeys[i]
		out = append(out, certKey{pub: s.PublicKey, binding: s.Sig, sub: s})
	}
	return out
}

// validAt reports whether a key could sign at the time of a signature (R-SIG-007): created before it, not expired and not revoked at it,
// its certificate's primary key neither, and allowed to sign by its flags where its binding states flags.
func validAt(e *pgp.Entity, k certKey, at time.Time) bool {
	if !k.pub.CreationTime.Before(at) || e.Revoked(at) {
		return false
	}
	self, _ := e.PrimarySelfSignature()
	if self == nil || e.PrimaryKey.KeyExpired(self, at) {
		return false
	}
	if k.binding == nil || (k.binding.FlagsValid && !k.binding.FlagSign) {
		return false
	}
	if k.sub != nil && (k.sub.Revoked(at) || k.pub.KeyExpired(k.binding, at)) {
		return false
	}
	return true
}

func userID(e *pgp.Entity) string {
	if id := e.PrimaryIdentity(); id != nil {
		return id.Name
	}
	return ""
}

func rsaBits(pk *packet.PublicKey) int {
	if pk.PubKeyAlgo != packet.PubKeyAlgoRSA && pk.PubKeyAlgo != packet.PubKeyAlgoRSASignOnly {
		return 0
	}
	bits, err := pk.BitLength()
	if err != nil {
		return 0
	}
	return int(bits)
}

// fingerprint spells a fingerprint as internal/openpgp does: upper-case hex.
func fingerprint(b []byte) string { return strings.ToUpper(hex.EncodeToString(b)) }

// issuer is the identifier a packet names its key by: the issuer fingerprint when it carries one, and the key ID (16 hex digits) the
// fingerprint or the issuer subpacket gives.
func issuer(sig *packet.Signature) (fp, keyID string) {
	if len(sig.IssuerFingerprint) > 0 {
		fp = fingerprint(sig.IssuerFingerprint)
	}
	switch {
	case sig.IssuerKeyId != nil:
		keyID = strings.ToUpper(hex.EncodeToString(bigEndian(*sig.IssuerKeyId)))
	case len(sig.IssuerFingerprint) >= 20 && sig.IssuerKeyVersion == 6:
		keyID = fp[:16]
	case len(sig.IssuerFingerprint) == 20:
		keyID = fp[len(fp)-16:]
	}
	return fp, keyID
}

func bigEndian(v uint64) []byte {
	b := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		b[i] = byte(v)
		v >>= 8
	}
	return b
}

// algorithmName names a signature's public-key algorithm as the signer shows it.
func algorithmName(a packet.PublicKeyAlgorithm, bits int) string {
	switch a {
	case packet.PubKeyAlgoRSA, packet.PubKeyAlgoRSASignOnly:
		if bits > 0 {
			return "RSA " + strconv.Itoa(bits)
		}
		return "RSA"
	case packet.PubKeyAlgoECDSA:
		return "ECDSA"
	case packet.PubKeyAlgoEdDSA, packet.PubKeyAlgoEd25519:
		return "Ed25519"
	case packet.PubKeyAlgoEd448:
		return "Ed448"
	case packet.PubKeyAlgoDSA:
		return "DSA"
	}
	return "algorithm " + strconv.Itoa(int(a))
}
