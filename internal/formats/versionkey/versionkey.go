// Package versionkey builds the stored sort key of a version (api-and-ui.md R-API-029, Q-API-429): a byte string whose bytewise order
// is the order of a format's versions ([formats.ViewAdapter.Compare]), so that PostgreSQL orders the versions of a package by an index
// and a page of the version list in the order of the versions, and the latest version of a package, is one keyset query.
//
// A key is a class byte, the format's encoding of the version's place in its order, and the version's own bytes: a string that is no
// version of the format has the class [NotVersion] and its bytes alone, so it sorts before every version and by its bytes, as Compare
// orders it; every version has the class [Version]. The bytes at the end make two different strings two different keys, the tie-break
// Compare ends with. The class is what a statement reads of a key: a key below 0x01 is a string the format does not rank, which the lists
// put after every version, newest first by its time (api-and-ui.md Q-API-431). A format's encoding must be self-delimiting (no key of one version is a proper prefix of the key of another), so
// that the bytes at the end never change the order of two versions. The helpers here are self-delimiting.
//
// The package is a leaf: it imports the standard library only.
package versionkey

import (
	"encoding/binary"
	"strings"
)

// The class bytes of a key.
const (
	// NotVersion is the class of a string that is no version of the format.
	NotVersion byte = 0x00
	// Version is the class of a version.
	Version byte = 0x01
)

// Of returns the key of raw: [NotVersion] and raw's bytes when ok is false, else [Version], the format's encoding and raw's bytes.
func Of(encoding []byte, ok bool, raw string) []byte {
	if !ok {
		return Text([]byte{NotVersion}, raw)
	}
	k := make([]byte, 0, 1+len(encoding)+len(raw)+2)
	k = append(k, Version)
	k = append(k, encoding...)
	return Text(k, raw)
}

// Text appends s in an order-preserving, self-delimiting form: a 0x00 byte is written as 0x00 0xFF and 0x00 0x01 ends the text, so a
// text that is a prefix of another sorts first whatever follows either.
func Text(k []byte, s string) []byte {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			k = append(k, 0x00, 0xFF)
			continue
		}
		k = append(k, s[i])
	}
	return append(k, 0x00, 0x01)
}

// Number appends a decimal number of any length in its numeric order: its digits without leading zeros, prefixed by their count as two
// bytes, big-endian. Zero and the empty string are the count 0. digits holds ASCII digits only; a version string is far shorter than
// 65,536 digits.
func Number(k []byte, digits string) []byte {
	digits = strings.TrimLeft(digits, "0")
	k = binary.BigEndian.AppendUint16(k, uint16(min(len(digits), 0xFFFF))) //nolint:gosec // bounded by the min
	return append(k, digits...)
}
