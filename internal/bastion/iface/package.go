package iface

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// The package names of the interface (R-BAS-081 to R-BAS-083): the package a response belongs to, in [HeaderPackage], and the
// packages of the check. A name is the package as the format's coordinates carry it (an npm scope and name, an image name, a
// Debian source package); the interface reads it as an opaque string that two sides compare byte for byte.

// MaxPackageBytes bounds a package name of the interface, decoded: the header and the check refuse a longer one (R-BAS-082,
// R-BAS-083).
const MaxPackageBytes = 512

// ValidPackage reports whether name is a package name the interface carries: 1 to [MaxPackageBytes] bytes of valid UTF-8
// without a control character (R-BAS-082).
func ValidPackage(name string) bool {
	if name == "" || len(name) > MaxPackageBytes || !utf8.ValidString(name) {
		return false
	}
	return strings.IndexFunc(name, unicode.IsControl) < 0
}

// packageVerbatim reports whether b stands for itself in [HeaderPackage]: A-Z, a-z, 0-9 and "-._~@/". Every other byte is
// written as %XX with upper-case hexadecimal digits.
func packageVerbatim(b byte) bool {
	switch {
	case 'A' <= b && b <= 'Z', 'a' <= b && b <= 'z', '0' <= b && b <= '9':
		return true
	}
	return strings.IndexByte("-._~@/", b) >= 0
}

const upperHex = "0123456789ABCDEF"

// encodePackage writes a package name in the form of [HeaderPackage]: "@acme/sdk" stays as it is, Debian's "g++" becomes
// "g%2B%2B".
func encodePackage(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if packageVerbatim(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(upperHex[c>>4])
		b.WriteByte(upperHex[c&0x0F])
	}
	return b.String()
}

// upperHexDigit is the value of an upper-case hexadecimal digit; a lower-case one is not a digit of the header.
func upperHexDigit(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// decodePackage reverses [encodePackage] strictly: every byte of v is a verbatim byte or starts %XX with two upper-case
// hexadecimal digits, and %XX never stands for a verbatim byte, so a name has exactly one spelling on the wire. It says nothing
// about the bounds of the decoded name ([ValidPackage]).
func decodePackage(v string) (string, bool) {
	out := make([]byte, 0, len(v))
	for i := 0; i < len(v); i++ {
		c := v[i]
		if packageVerbatim(c) {
			out = append(out, c)
			continue
		}
		if c != '%' || i+2 >= len(v) {
			return "", false
		}
		hi, okHi := upperHexDigit(v[i+1])
		lo, okLo := upperHexDigit(v[i+2])
		if !okHi || !okLo {
			return "", false
		}
		d := hi<<4 | lo
		if packageVerbatim(d) {
			return "", false
		}
		out = append(out, d)
		i += 2
	}
	return string(out), true
}
