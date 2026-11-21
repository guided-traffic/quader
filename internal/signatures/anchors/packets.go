package anchors

import (
	"crypto/sha1" // #nosec G505 -- the version 4 fingerprint of RFC 9580 section 5.5.4 is SHA-1 by definition; it names a key, it secures nothing here
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
)

// keyPacket is what an anchor needs of one public key packet: the fingerprint and key ID as internal/openpgp spells them (upper-case hex)
// and the public-key algorithm.
type keyPacket struct {
	fingerprint string
	keyID       string
	algorithm   uint8
}

// The packet tags of a public key and a public subkey (RFC 9580 section 5).
const (
	tagPublicKey    = 6
	tagPublicSubkey = 14
)

var errPacket = errors.New("anchors: malformed OpenPGP packet")

// publicKeyPackets reads the public key and subkey packets of one armored certificate that internal/openpgp wrote
// (openpgp.ArmorCertificates) and has read before: the primary key first, then its subkeys. internal/openpgp is the only importer of the
// OpenPGP module, and it does not name the key IDs of subkeys, which a signature's issuer is matched by; reading the packet framing of
// RFC 9580 section 4.2 is all that needs.
func publicKeyPackets(armored []byte) ([]keyPacket, error) {
	body, err := dearmor(armored)
	if err != nil {
		return nil, err
	}
	var out []keyPacket
	for len(body) > 0 {
		tag, content, rest, err := nextPacket(body)
		if err != nil {
			return nil, err
		}
		if tag == tagPublicKey || tag == tagPublicSubkey {
			k, err := readKeyPacket(content)
			if err != nil {
				return nil, err
			}
			out = append(out, k)
		}
		body = rest
	}
	return out, nil
}

// dearmor decodes the base64 body of one ASCII-armored block, without its headers and checksum line.
func dearmor(armored []byte) ([]byte, error) {
	lines := strings.Split(strings.ReplaceAll(string(armored), "\r\n", "\n"), "\n")
	var b64 strings.Builder
	state := 0 // 0 before BEGIN, 1 in the headers, 2 in the body
	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case state == 0 && strings.HasPrefix(line, "-----BEGIN "):
			state = 1
		case state == 1 && line == "":
			state = 2
		case state == 1 && !strings.Contains(line, ": "):
			state = 2 // no headers and no blank line: the body starts here
			b64.WriteString(line)
		case state == 2 && (strings.HasPrefix(line, "=") || strings.HasPrefix(line, "-----END ")):
			return base64.StdEncoding.DecodeString(b64.String())
		case state == 2:
			b64.WriteString(line)
		}
	}
	return nil, errPacket
}

// nextPacket splits the first packet off body: its tag, its content and what follows. Partial lengths and the indeterminate length of
// the old format do not occur in a certificate and are refused.
func nextPacket(body []byte) (tag byte, content, rest []byte, err error) {
	if len(body) < 2 || body[0]&0x80 == 0 {
		return 0, nil, nil, errPacket
	}
	var n, hdr int
	if body[0]&0x40 != 0 { // the current format
		tag = body[0] & 0x3f
		switch l := int(body[1]); {
		case l < 192:
			n, hdr = l, 2
		case l < 224:
			if len(body) < 3 {
				return 0, nil, nil, errPacket
			}
			n, hdr = (l-192)<<8+int(body[2])+192, 3
		case l == 255:
			if len(body) < 6 {
				return 0, nil, nil, errPacket
			}
			n, hdr = int(binary.BigEndian.Uint32(body[2:6])), 6
		default:
			return 0, nil, nil, errPacket
		}
	} else { // the legacy format
		tag = (body[0] >> 2) & 0x0f
		switch body[0] & 3 {
		case 0:
			n, hdr = int(body[1]), 2
		case 1:
			if len(body) < 3 {
				return 0, nil, nil, errPacket
			}
			n, hdr = int(binary.BigEndian.Uint16(body[1:3])), 3
		case 2:
			if len(body) < 5 {
				return 0, nil, nil, errPacket
			}
			n, hdr = int(binary.BigEndian.Uint32(body[1:5])), 5
		default:
			return 0, nil, nil, errPacket
		}
	}
	if n < 0 || hdr+n > len(body) {
		return 0, nil, nil, errPacket
	}
	return tag, body[hdr : hdr+n], body[hdr+n:], nil
}

// readKeyPacket computes the fingerprint and key ID of a public key packet's content (RFC 9580 section 5.5.4): version 4 is the SHA-1 of
// 0x99, a two-octet length and the content, its key ID the low 64 bits; versions 5 and 6 are the SHA-256 of 0x9A or 0x9B, a four-octet
// length and the content, their key ID the high 64 bits. The algorithm is the sixth octet of each version.
func readKeyPacket(content []byte) (keyPacket, error) {
	if len(content) < 6 {
		return keyPacket{}, errPacket
	}
	var fp, id []byte
	switch content[0] {
	case 4:
		if len(content) > 0xffff {
			return keyPacket{}, errPacket // a version 4 key's fingerprint frames it with two octets of length
		}
		var l [2]byte
		binary.BigEndian.PutUint16(l[:], uint16(len(content))) // #nosec G115 -- bounded just above
		h := sha1.New()                                        // #nosec G401 -- see the import
		h.Write(append([]byte{0x99}, l[:]...))
		h.Write(content)
		fp = h.Sum(nil)
		id = fp[len(fp)-8:]
	case 5, 6:
		h := sha256.New()
		prefix := byte(0x9a)
		if content[0] == 6 {
			prefix = 0x9b
		}
		var l [4]byte
		binary.BigEndian.PutUint32(l[:], uint32(len(content))) // #nosec G115 -- a packet is far below 4 GiB, bounded by the 256 KiB of the keys
		h.Write(append([]byte{prefix}, l[:]...))
		h.Write(content)
		fp = h.Sum(nil)
		id = fp[:8]
	default:
		return keyPacket{}, errPacket
	}
	return keyPacket{
		fingerprint: strings.ToUpper(hex.EncodeToString(fp)),
		keyID:       strings.ToUpper(hex.EncodeToString(id)),
		algorithm:   content[5],
	}, nil
}
