package openpgp

import (
	"bytes"
	"io"

	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"

	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki"
)

// The armor lines of an OpenPGP signature and of a cleartext message (RFC 9580, sections 6 and 7).
const (
	armorSignature = "-----BEGIN PGP SIGNATURE-----"
	armorEnd       = "-----END PGP SIGNATURE-----"
	armorClear     = "-----BEGIN PGP SIGNED MESSAGE-----"
	typeSignature  = "PGP SIGNATURE"
)

// maxPackets bounds the signature packets of one object (R-SIG-013): more than a release document, a generic signature file or an
// Ansible signature ever carries (internal/openpgp's bound of a release document is 32 too).
const maxPackets = 32

// tagSignature is the packet tag of a signature packet.
const tagSignature = 2

// object is a signature object split into its signature packets, and the cleartext message when the object is one (an InRelease).
type object struct {
	packets [][]byte
	clear   *clearsign.Block
}

// split finds the signature packets of an object: a cleartext message's signature block, armored signature blocks, or binary packets.
// Every packet must be a signature packet of a definite length.
func split(data []byte, max int64) (*object, error) {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	switch {
	case bytes.HasPrefix(trimmed, []byte(armorClear)):
		return splitClear(trimmed, max)
	case bytes.Contains(data, []byte(armorSignature)):
		body, err := dearmor(data, max)
		if err != nil {
			return nil, err
		}
		return splitPackets(body)
	}
	return splitPackets(data)
}

// splitClear reads the signature block of a cleartext message and keeps the message.
func splitClear(data []byte, max int64) (*object, error) {
	block, rest := clearsign.Decode(data)
	if block == nil || block.ArmoredSignature == nil || block.ArmoredSignature.Type != typeSignature {
		return nil, pki.Malformed("the object is not a cleartext signed message")
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		return nil, pki.Malformed("the cleartext message has text after its signature")
	}
	body, err := readBounded(block.ArmoredSignature.Body, max)
	if err != nil {
		return nil, err
	}
	o, err := splitPackets(body)
	if err != nil {
		return nil, err
	}
	o.clear = block
	return o, nil
}

// dearmor decodes every armored signature block of data and joins their packets.
func dearmor(data []byte, max int64) ([]byte, error) {
	var out []byte
	for rest := data; ; {
		i := bytes.Index(rest, []byte(armorSignature))
		if i < 0 {
			break
		}
		j := bytes.Index(rest[i:], []byte(armorEnd))
		if j < 0 {
			return nil, pki.Malformed("an armored signature has no end line")
		}
		end := i + j + len(armorEnd)
		block, err := armor.Decode(bytes.NewReader(rest[i:end]))
		if err != nil || block.Type != typeSignature {
			return nil, pki.Malformed("an armored signature does not decode")
		}
		body, err := readBounded(block.Body, max)
		if err != nil {
			return nil, err
		}
		out, rest = append(out, body...), rest[end:]
	}
	if len(out) == 0 {
		return nil, pki.Malformed("the armored signature is empty")
	}
	return out, nil
}

func readBounded(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, pki.Malformed("the armored signature does not decode")
	}
	if int64(len(b)) > max {
		return nil, pki.TooLarge("the signature is larger than signatures.max_object_size")
	}
	return b, nil
}

// splitPackets cuts binary data into its packets, each a signature packet.
func splitPackets(data []byte) (*object, error) {
	o := &object{}
	for rest := data; len(rest) > 0; {
		tag, pkt, after, err := nextPacket(rest)
		if err != nil {
			return nil, err
		}
		if tag != tagSignature {
			return nil, pki.Malformed("the object holds a packet of tag %d, not a signature", tag)
		}
		if len(o.packets) == maxPackets {
			return nil, pki.TooLarge("the object holds more than %d signatures", maxPackets)
		}
		o.packets, rest = append(o.packets, pkt), after
	}
	if len(o.packets) == 0 {
		return nil, pki.Malformed("the object holds no signature packet")
	}
	return o, nil
}

// nextPacket reads one packet header (RFC 9580, section 4.2) and returns the packet's tag, its bytes with the header, and what follows.
// A partial or an indeterminate length is refused: a signature packet has a definite one.
func nextPacket(b []byte) (tag int, pkt, rest []byte, err error) {
	if len(b) < 2 || b[0]&0x80 == 0 {
		return 0, nil, nil, pki.Malformed("the data is not an OpenPGP packet")
	}
	var length, hdr int
	if b[0]&0x40 != 0 {
		tag = int(b[0] & 0x3f)
		length, hdr, err = newLength(b[1:])
		hdr++
	} else {
		tag = int(b[0]>>2) & 0x0f
		length, hdr, err = oldLength(b[0]&3, b[1:])
		hdr++
	}
	if err != nil {
		return 0, nil, nil, err
	}
	if length < 0 || length > len(b)-hdr {
		return 0, nil, nil, pki.TooLarge("an OpenPGP packet declares %d bytes where %d follow", length, len(b)-hdr)
	}
	return tag, b[:hdr+length], b[hdr+length:], nil
}

func newLength(b []byte) (length, hdr int, err error) {
	switch o := int(b[0]); {
	case o < 192:
		return o, 1, nil
	case o < 224:
		if len(b) < 2 {
			return 0, 0, pki.Malformed("an OpenPGP packet header ends early")
		}
		return (o-192)<<8 + int(b[1]) + 192, 2, nil
	case o == 255:
		if len(b) < 5 {
			return 0, 0, pki.Malformed("an OpenPGP packet header ends early")
		}
		l := int(b[1])<<24 | int(b[2])<<16 | int(b[3])<<8 | int(b[4])
		return l, 5, nil
	}
	return 0, 0, pki.Malformed("an OpenPGP packet has a partial length")
}

func oldLength(kind byte, b []byte) (length, hdr int, err error) {
	n := map[byte]int{0: 1, 1: 2, 2: 4}[kind]
	if n == 0 {
		return 0, 0, pki.Malformed("an OpenPGP packet has an indeterminate length")
	}
	if len(b) < n {
		return 0, 0, pki.Malformed("an OpenPGP packet header ends early")
	}
	for _, c := range b[:n] {
		length = length<<8 | int(c)
	}
	return length, n, nil
}
