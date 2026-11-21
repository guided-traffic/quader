package untrusted

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // MatchesHashPrefix compares the prefix of a hash a signer chose; nothing is trusted on it
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash"
)

// The bounds of [OpenPGPSignatures]: a detector reads what a client or an upstream sent (signatures.md R-SIG-013).
const (
	// MaxOpenPGPInput is the most bytes the walk reads: a signature object of R-SIG-013's default bound.
	MaxOpenPGPInput = 256 << 10
	// MaxOpenPGPPackets is the most packets the walk steps over before it refuses.
	MaxOpenPGPPackets = 1024
)

const (
	armorBegin = "-----BEGIN PGP SIGNATURE-----"
	armorEnd   = "-----END PGP SIGNATURE-----"
	// tagSignature is the packet tag of a signature packet (RFC 9580, section 5.2).
	tagSignature = 2
)

// errOpenPGP() is the refusal of an input that is no OpenPGP packet sequence within the bounds.
func errOpenPGP() error { return Refused("The content is not an OpenPGP signature.") }

// OpenPGPArmored reports whether b holds an ASCII-armored OpenPGP signature block, on its own or after the text of a cleartext
// signature (Debian's InRelease).
func OpenPGPArmored(b []byte) bool { return bytes.Contains(b, []byte(armorBegin)) }

// OpenPGPSignatures counts the signature packets of an OpenPGP signature: an armored block (the first one in b, so the signature part
// of a cleartext-signed document), or binary packets. It reads only the packet headers (RFC 9580, section 4.2) and steps over every
// body, checks nothing cryptographic, and refuses an input longer than [MaxOpenPGPInput], a packet that runs past the end, more than
// [MaxOpenPGPPackets] packets, and an input that holds no signature packet.
func OpenPGPSignatures(b []byte) (int, error) {
	if len(b) > MaxOpenPGPInput {
		return 0, TooLarge(fmt.Sprintf("An OpenPGP signature is larger than %d bytes.", MaxOpenPGPInput))
	}
	if OpenPGPArmored(b) {
		var err error
		if b, err = dearmor(b); err != nil {
			return 0, err
		}
	}
	n, err := countSignaturePackets(b)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, errOpenPGP()
	}
	return n, nil
}

// OpenPGPBinary reports whether b starts like a binary OpenPGP signature packet: the high bit set and the tag of a signature.
func OpenPGPBinary(b []byte) bool {
	if len(b) < 2 || b[0]&0x80 == 0 {
		return false
	}
	if b[0]&0x40 != 0 {
		return b[0]&0x3f == tagSignature
	}
	return (b[0]>>2)&0x0f == tagSignature
}

// dearmor decodes the first armored signature block of b: the header lines up to the first empty line are skipped, the base64 lines
// are decoded up to the checksum line or the end line; the checksum itself is not checked here.
func dearmor(b []byte) ([]byte, error) {
	i := bytes.Index(b, []byte(armorBegin))
	if i < 0 {
		return nil, errOpenPGP()
	}
	lines := bytes.Split(b[i+len(armorBegin):], []byte("\n"))
	var body bytes.Buffer
	inBody := false
	for _, raw := range lines[1:] {
		line := bytes.TrimSpace(raw)
		switch {
		case bytes.HasPrefix(line, []byte(armorEnd)):
			return decodeArmor(body.Bytes())
		case !inBody:
			if len(line) == 0 {
				inBody = true
			} else if !bytes.Contains(line, []byte(":")) {
				// no header lines at all: the first line is already base64
				inBody = true
				body.Write(line)
			}
		case len(line) > 0 && line[0] == '=':
			return decodeArmor(body.Bytes())
		default:
			body.Write(line)
		}
	}
	return nil, errOpenPGP()
}

func decodeArmor(s []byte) ([]byte, error) {
	out := make([]byte, base64.StdEncoding.DecodedLen(len(s)))
	n, err := base64.StdEncoding.Decode(out, s)
	if err != nil {
		return nil, errOpenPGP()
	}
	return out[:n], nil
}

// countSignaturePackets walks the packet headers of b and counts the signature packets.
func countSignaturePackets(b []byte) (int, error) {
	count, packets := 0, 0
	for len(b) > 0 {
		packets++
		if packets > MaxOpenPGPPackets {
			return 0, TooLarge(fmt.Sprintf("An OpenPGP signature holds more than %d packets.", MaxOpenPGPPackets))
		}
		tag, rest, err := nextPacket(b)
		if err != nil {
			return 0, err
		}
		if tag == tagSignature {
			count++
		}
		b = rest
	}
	return count, nil
}

// nextPacket reads one packet header and steps over its body, partial body lengths included.
func nextPacket(b []byte) (tag int, rest []byte, err error) {
	if b[0]&0x80 == 0 {
		return 0, nil, errOpenPGP()
	}
	if b[0]&0x40 == 0 { // the legacy format
		tag = int(b[0]>>2) & 0x0f
		var n, hdr int
		switch b[0] & 0x03 {
		case 0:
			hdr = 2
		case 1:
			hdr = 3
		case 2:
			hdr = 5
		default: // indeterminate: the packet runs to the end
			return tag, nil, nil
		}
		if len(b) < hdr {
			return 0, nil, errOpenPGP()
		}
		for _, c := range b[1:hdr] {
			n = n<<8 | int(c)
		}
		if n < 0 || n > len(b)-hdr {
			return 0, nil, errOpenPGP()
		}
		return tag, b[hdr+n:], nil
	}
	tag = int(b[0] & 0x3f)
	b = b[1:]
	for {
		n, hdr, partial, ok := newLength(b)
		if !ok || n > len(b)-hdr {
			return 0, nil, errOpenPGP()
		}
		b = b[hdr+n:]
		if !partial {
			return tag, b, nil
		}
	}
}

// newLength reads a length of the current packet format: one, two or five octets, or a partial body length.
func newLength(b []byte) (n, hdr int, partial, ok bool) {
	if len(b) == 0 {
		return 0, 0, false, false
	}
	switch c := int(b[0]); {
	case c < 192:
		return c, 1, false, true
	case c < 224:
		if len(b) < 2 {
			return 0, 0, false, false
		}
		return (c-192)<<8 + int(b[1]) + 192, 2, false, true
	case c == 255:
		if len(b) < 5 {
			return 0, 0, false, false
		}
		v := int(b[1])<<24 | int(b[2])<<16 | int(b[3])<<8 | int(b[4])
		return v, 5, false, v >= 0
	default:
		return 1 << (c & 0x1f), 1, true, true
	}
}

// OpenPGPSig is what a signature packet says of itself, read without a key (RFC 9580, section 5.2): its version, its type, the hash
// algorithm, the fingerprint of the key it names, the left 16 bits of the hash it signs, and the trailer a version 4 signature appends
// to the signed data before it hashes it.
type OpenPGPSig struct {
	Version  int
	SigType  int
	HashAlgo int
	// Fingerprint is the issuer fingerprint subpacket's (version 4: 20 bytes, version 6: 32 bytes); nil when the packet names none.
	Fingerprint []byte
	Left16      [2]byte
	// Trailer is what a version 4 signature hashes after the signed data: the hashed part of the packet and the final trailer; nil for
	// another version.
	Trailer []byte
}

// subpacketIssuerFingerprint is the issuer fingerprint subpacket (RFC 9580, section 5.2.3.35).
const subpacketIssuerFingerprint = 33

// OpenPGPSignatureFacts reads every signature packet of an OpenPGP signature (an armored block or binary packets) without a key, under
// the bounds of [OpenPGPSignatures]: a detached signature a client posts (signatures.md R-SIG-031). A packet of a version other than 4
// and 6 is reported with its version alone.
func OpenPGPSignatureFacts(b []byte) ([]OpenPGPSig, error) {
	if _, err := OpenPGPSignatures(b); err != nil {
		return nil, err
	}
	if OpenPGPArmored(b) {
		var err error
		if b, err = dearmor(b); err != nil {
			return nil, err
		}
	}
	var out []OpenPGPSig
	for len(b) > 0 {
		tag, body, rest, err := packetBody(b)
		if err != nil {
			return nil, err
		}
		if tag == tagSignature {
			s, err := signatureFacts(body)
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		}
		b = rest
	}
	return out, nil
}

// packetBody reads one packet whose body has a definite length and returns it; a partial body length is refused here, because a
// signature packet never has one.
func packetBody(b []byte) (tag int, body, rest []byte, err error) {
	if len(b) == 0 || b[0]&0x80 == 0 {
		return 0, nil, nil, errOpenPGP()
	}
	if b[0]&0x40 == 0 {
		tag = int(b[0]>>2) & 0x0f
		var hdr int
		switch b[0] & 0x03 {
		case 0:
			hdr = 2
		case 1:
			hdr = 3
		case 2:
			hdr = 5
		default:
			return tag, b[1:], nil, nil
		}
		if len(b) < hdr {
			return 0, nil, nil, errOpenPGP()
		}
		n := readLen(b[1:hdr])
		if n < 0 || n > len(b)-hdr {
			return 0, nil, nil, errOpenPGP()
		}
		return tag, b[hdr : hdr+n], b[hdr+n:], nil
	}
	tag = int(b[0] & 0x3f)
	n, hdr, partial, ok := newLength(b[1:])
	if !ok || partial || n > len(b)-1-hdr {
		return 0, nil, nil, errOpenPGP()
	}
	start := 1 + hdr
	return tag, b[start : start+n], b[start+n:], nil
}

// signatureFacts reads the fixed part and the subpackets of a signature packet's body.
func signatureFacts(body []byte) (OpenPGPSig, error) {
	if len(body) < 1 {
		return OpenPGPSig{}, errOpenPGP()
	}
	s := OpenPGPSig{Version: int(body[0])}
	if s.Version != 4 && s.Version != 6 {
		return s, nil
	}
	lenBytes := 2
	if s.Version == 6 {
		lenBytes = 4
	}
	if len(body) < 4+lenBytes {
		return OpenPGPSig{}, errOpenPGP()
	}
	s.SigType, s.HashAlgo = int(body[1]), int(body[3])
	hashed, at, err := subpacketArea(body, 4, lenBytes)
	if err != nil {
		return OpenPGPSig{}, err
	}
	hashedEnd := at
	unhashed, at, err := subpacketArea(body, at, lenBytes)
	if err != nil {
		return OpenPGPSig{}, err
	}
	if len(body) < at+2 {
		return OpenPGPSig{}, errOpenPGP()
	}
	copy(s.Left16[:], body[at:at+2])
	for _, area := range [][]byte{hashed, unhashed} {
		fp, err := issuerFingerprint(area)
		if err != nil {
			return OpenPGPSig{}, err
		}
		if fp != nil && s.Fingerprint == nil {
			s.Fingerprint = fp
		}
	}
	if s.Version == 4 {
		// RFC 9580, section 5.2.4: the hashed part of the packet, then 0x04 0xFF and its length as four octets.
		head := body[:hashedEnd]
		n := len(head)
		s.Trailer = append(append(make([]byte, 0, n+6), head...), 0x04, 0xff, 0, 0, 0, 0)
		binary.BigEndian.PutUint32(s.Trailer[n+2:], uint32(n)) //nolint:gosec // n is the length of a packet within MaxOpenPGPInput
	}
	return s, nil
}

// MatchesHashPrefix compares the left 16 bits of the hash a version 4 signature of a binary document (type 0) or of a text document
// (type 1, its line endings made CRLF) signs with the hash of content and the trailer (RFC 9580, section 5.2.4). It needs no key:
// it tells a signature of other content from one of this content, and proves nothing about who signed. checked is false for a
// signature it cannot judge so (another version or type, a hash algorithm outside SHA-1 and SHA-2).
func (s OpenPGPSig) MatchesHashPrefix(content []byte) (checked, ok bool) {
	if s.Version != 4 || s.Trailer == nil || (s.SigType != 0 && s.SigType != 1) {
		return false, false
	}
	var h hash.Hash
	switch s.HashAlgo {
	case 2:
		h = sha1.New() //nolint:gosec // the prefix of a hash the signer chose is compared, nothing is trusted on it
	case 8:
		h = sha256.New()
	case 9:
		h = sha512.New384()
	case 10:
		h = sha512.New()
	case 11:
		h = sha256.New224()
	default:
		return false, false
	}
	if s.SigType == 1 {
		content = canonicalText(content)
	}
	h.Write(content)
	h.Write(s.Trailer)
	sum := h.Sum(nil)
	return true, sum[0] == s.Left16[0] && sum[1] == s.Left16[1]
}

// canonicalText is a text document as a signature of type 1 hashes it: every line ending CRLF.
func canonicalText(b []byte) []byte {
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(b, []byte("\n"), []byte("\r\n"))
}

// subpacketArea reads a subpacket area that starts at the length field at: its bytes and where the next field starts.
func subpacketArea(body []byte, at, lenBytes int) (area []byte, next int, err error) {
	if len(body) < at+lenBytes {
		return nil, 0, errOpenPGP()
	}
	n := readLen(body[at : at+lenBytes])
	at += lenBytes
	if n < 0 || n > len(body)-at {
		return nil, 0, errOpenPGP()
	}
	return body[at : at+n], at + n, nil
}

func readLen(b []byte) int {
	n := 0
	for _, c := range b {
		n = n<<8 | int(c)
	}
	return n
}

// issuerFingerprint finds the issuer fingerprint subpacket of a subpacket area.
func issuerFingerprint(area []byte) ([]byte, error) {
	for len(area) > 0 {
		n, hdr, ok := subpacketLength(area)
		if !ok || n < 1 || n > len(area)-hdr {
			return nil, errOpenPGP()
		}
		sp := area[hdr : hdr+n]
		if int(sp[0]&0x7f) == subpacketIssuerFingerprint && len(sp) >= 2 {
			fp := sp[2:]
			if (sp[1] == 4 && len(fp) == 20) || (sp[1] == 6 && len(fp) == 32) {
				return append([]byte(nil), fp...), nil
			}
		}
		area = area[hdr+n:]
	}
	return nil, nil
}

// subpacketLength reads the length of a subpacket (RFC 9580, section 5.2.3.7): one, two or five octets.
func subpacketLength(b []byte) (n, hdr int, ok bool) {
	if len(b) == 0 {
		return 0, 0, false
	}
	switch c := int(b[0]); {
	case c < 192:
		return c, 1, true
	case c < 255:
		if len(b) < 2 {
			return 0, 0, false
		}
		return (c-192)<<8 + int(b[1]) + 192, 2, true
	default:
		if len(b) < 5 {
			return 0, 0, false
		}
		v := int(b[1])<<24 | int(b[2])<<16 | int(b[3])<<8 | int(b[4])
		return v, 5, v >= 0
	}
}
