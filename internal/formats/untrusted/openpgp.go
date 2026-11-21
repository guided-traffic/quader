package untrusted

import (
	"bytes"
	"encoding/base64"
	"fmt"
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
