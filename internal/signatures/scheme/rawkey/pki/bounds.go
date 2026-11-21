package pki

// The bounds of R-SIG-013 on ASN.1 (CMS, timestamps) and CBOR (COSE) that sit outside the libraries (Q-SIG-45): a structure is walked by
// its headers before any library decodes it, and refused when it nests deeper than the bound, when a length it declares is longer than the
// bytes that follow, or when its encoding cannot be read. The walk allocates nothing and visits every header once.

// maxLengthBytes is the most bytes an ASN.1 long-form length may take: four bytes declare up to 4 GiB, far beyond any object.
const maxLengthBytes = 4

// maxTagBytes is the most base-128 octets of a tag number in the high-tag-number form: four name a number below 2^28, as encoding/asn1
// reads it.
const maxTagBytes = 4

// CheckBER walks one BER or DER value (definite and indefinite lengths) and refuses it beyond maxDepth levels of nesting, with a declared
// length longer than the data, a long-form length of more than maxLengthBytes octets, or a tag number not in its shortest form (X.690,
// 8.1.2: the high-tag-number form only for 31 and more, no leading zero octet, at most maxTagBytes octets).
func CheckBER(data []byte, maxDepth int) error {
	if len(data) == 0 {
		return Malformed("the ASN.1 structure is empty")
	}
	_, err := berValue(data, 1, maxDepth)
	return err
}

// Definite re-encodes one BER value with definite lengths in the fewest octets, the form of DER's lengths: encoding/asn1 reads no
// indefinite length and no long form where a short one does (Q-SIG-216). A DER value comes back unchanged. It refuses what CheckBER
// refuses, and data after the value. What it returns is DER in its tags and lengths at every level (the content rules of DER, a primitive
// string or the order of a SET OF, are the schema's), at most 2*maxDepth bytes longer than data: only an indefinite length whose content
// needs a length of three or four octets grows, by at most two, and such values nest at most maxDepth deep.
func Definite(data []byte, maxDepth int) ([]byte, error) {
	if err := CheckBER(data, maxDepth); err != nil {
		return nil, err
	}
	out, rest, err := definiteValue(nil, data)
	if err != nil {
		return nil, err
	}
	if len(rest) > 0 {
		return nil, Malformed("the ASN.1 structure is followed by %d bytes", len(rest))
	}
	return out, nil
}

// definiteValue appends the value at the start of b, re-encoded, to out and returns what follows it. CheckBER has bounded the nesting
// and the lengths.
func definiteValue(out, b []byte) ([]byte, []byte, error) {
	constructed, rest, err := berTag(b)
	if err != nil {
		return nil, nil, err
	}
	out = append(out, b[:len(b)-len(rest)]...)
	length, indefinite, rest, err := berLength(rest)
	if err != nil {
		return nil, nil, err
	}
	if !constructed {
		return append(appendLength(out, length), rest[:length]...), rest[length:], nil
	}
	var children []byte
	if indefinite {
		for {
			if len(rest) < 2 {
				return nil, nil, Malformed("an indefinite ASN.1 value has no end")
			}
			if rest[0] == 0 && rest[1] == 0 {
				rest = rest[2:]
				break
			}
			if children, rest, err = definiteValue(children, rest); err != nil {
				return nil, nil, err
			}
		}
	} else {
		content := rest[:length]
		rest = rest[length:]
		for len(content) > 0 {
			if children, content, err = definiteValue(children, content); err != nil {
				return nil, nil, err
			}
		}
	}
	return append(appendLength(out, len(children)), children...), rest, nil
}

// appendLength appends a definite length in the fewest octets.
func appendLength(out []byte, n int) []byte {
	if n < 0x80 {
		return append(out, byte(n)) //nolint:gosec // below 0x80
	}
	var be [8]byte
	i := len(be)
	for ; n > 0; n >>= 8 {
		i--
		be[i] = byte(n)
	}
	return append(append(out, 0x80|byte(len(be)-i)), be[i:]...)
}

// berValue walks the value at the start of b and returns what follows it.
func berValue(b []byte, depth, maxDepth int) ([]byte, error) {
	if depth > maxDepth {
		return nil, TooLarge("the ASN.1 structure is nested deeper than %d levels", maxDepth)
	}
	constructed, rest, err := berTag(b)
	if err != nil {
		return nil, err
	}
	length, indefinite, rest, err := berLength(rest)
	if err != nil {
		return nil, err
	}
	if indefinite {
		if !constructed {
			return nil, Malformed("a primitive ASN.1 value has an indefinite length")
		}
		return berIndefinite(rest, depth, maxDepth)
	}
	if length > len(rest) {
		return nil, TooLarge("an ASN.1 value declares %d bytes where %d follow", length, len(rest))
	}
	content, after := rest[:length], rest[length:]
	if constructed {
		for len(content) > 0 {
			if content, err = berValue(content, depth+1, maxDepth); err != nil {
				return nil, err
			}
		}
	}
	return after, nil
}

// berIndefinite walks the children of an indefinite-length value up to its end-of-contents octets.
func berIndefinite(b []byte, depth, maxDepth int) ([]byte, error) {
	for {
		if len(b) < 2 {
			return nil, Malformed("an indefinite ASN.1 value has no end")
		}
		if b[0] == 0 && b[1] == 0 {
			return b[2:], nil
		}
		var err error
		if b, err = berValue(b, depth+1, maxDepth); err != nil {
			return nil, err
		}
	}
}

// berTag reads an identifier octet and the octets of a high tag number, which must be the number's shortest form (X.690, 8.1.2.4).
func berTag(b []byte) (constructed bool, rest []byte, err error) {
	if len(b) == 0 {
		return false, nil, Malformed("the ASN.1 structure ends early")
	}
	constructed = b[0]&0x20 != 0
	if b[0]&0x1f != 0x1f {
		return constructed, b[1:], nil
	}
	if len(b) > 1 && b[1] == 0x80 {
		return false, nil, Malformed("an ASN.1 tag number starts with a zero octet")
	}
	n := 0
	for i := 1; i < len(b) && i <= maxTagBytes; i++ {
		n = n<<7 | int(b[i]&0x7f)
		if b[i]&0x80 == 0 {
			if n < 0x1f {
				return false, nil, Malformed("an ASN.1 tag number below 31 is written in the high-tag-number form")
			}
			return constructed, b[i+1:], nil
		}
	}
	return false, nil, Malformed("an ASN.1 tag number is too long")
}

// berLength reads a length octet: short form, long form of at most four octets, or indefinite.
func berLength(b []byte) (length int, indefinite bool, rest []byte, err error) {
	if len(b) == 0 {
		return 0, false, nil, Malformed("the ASN.1 structure ends early")
	}
	first := b[0]
	switch {
	case first < 0x80:
		return int(first), false, b[1:], nil
	case first == 0x80:
		return 0, true, b[1:], nil
	}
	n := int(first & 0x7f)
	if n > maxLengthBytes {
		return 0, false, nil, TooLarge("an ASN.1 length takes %d octets", n)
	}
	if len(b) < 1+n {
		return 0, false, nil, Malformed("the ASN.1 structure ends early")
	}
	var l uint64
	for _, c := range b[1 : 1+n] {
		l = l<<8 | uint64(c)
	}
	if l > uint64(len(b)) {
		return 0, false, nil, TooLarge("an ASN.1 value declares %d bytes where %d follow", l, len(b)-1-n)
	}
	return int(l), false, b[1+n:], nil
}

// CheckCBOR walks one CBOR data item (RFC 8949) and refuses it beyond maxDepth levels of nesting (arrays, maps, tags) or with a declared
// length or count longer than the data.
func CheckCBOR(data []byte, maxDepth int) error {
	if len(data) == 0 {
		return Malformed("the CBOR structure is empty")
	}
	_, err := cborItem(data, 1, maxDepth)
	return err
}

// The major types of CBOR.
const (
	cborBytes = 2
	cborText  = 3
	cborArray = 4
	cborMap   = 5
	cborTag   = 6
	cborBreak = 0xff
)

// cborItem walks the data item at the start of b and returns what follows it.
func cborItem(b []byte, depth, maxDepth int) ([]byte, error) {
	if depth > maxDepth {
		return nil, TooLarge("the CBOR structure is nested deeper than %d levels", maxDepth)
	}
	if len(b) == 0 {
		return nil, Malformed("the CBOR structure ends early")
	}
	major, info := b[0]>>5, b[0]&0x1f
	if info == 31 {
		return cborIndefinite(major, b[1:], depth, maxDepth)
	}
	arg, rest, err := cborArgument(info, b[1:])
	if err != nil {
		return nil, err
	}
	switch major {
	case cborBytes, cborText:
		if arg > uint64(len(rest)) {
			return nil, TooLarge("a CBOR string declares %d bytes where %d follow", arg, len(rest))
		}
		return rest[arg:], nil
	case cborArray, cborMap:
		items := arg
		if major == cborMap {
			items = arg * 2
		}
		if arg > uint64(len(rest)) || items > uint64(len(rest)) {
			return nil, TooLarge("a CBOR array or map declares %d items where %d bytes follow", arg, len(rest))
		}
		for range items {
			if rest, err = cborItem(rest, depth+1, maxDepth); err != nil {
				return nil, err
			}
		}
		return rest, nil
	case cborTag:
		return cborItem(rest, depth+1, maxDepth)
	}
	return rest, nil
}

// cborIndefinite walks an indefinite-length string, array or map up to its break.
func cborIndefinite(major byte, b []byte, depth, maxDepth int) ([]byte, error) {
	switch major {
	case cborBytes, cborText, cborArray, cborMap:
	default:
		return nil, Malformed("a CBOR item of major type %d has an indefinite length", major)
	}
	if depth+1 > maxDepth {
		return nil, TooLarge("the CBOR structure is nested deeper than %d levels", maxDepth)
	}
	for {
		if len(b) == 0 {
			return nil, Malformed("an indefinite CBOR item has no break")
		}
		if b[0] == cborBreak {
			return b[1:], nil
		}
		if (major == cborBytes || major == cborText) && (b[0]>>5 != major || b[0]&0x1f == 31) {
			return nil, Malformed("a chunk of an indefinite CBOR string is of another type")
		}
		var err error
		if b, err = cborItem(b, depth+1, maxDepth); err != nil {
			return nil, err
		}
	}
}

// cborArgument reads the argument of an initial byte's additional information.
func cborArgument(info byte, b []byte) (uint64, []byte, error) {
	if info < 24 {
		return uint64(info), b, nil
	}
	n := 0
	switch info {
	case 24:
		n = 1
	case 25:
		n = 2
	case 26:
		n = 4
	case 27:
		n = 8
	default:
		return 0, nil, Malformed("a CBOR item has a reserved additional information %d", info)
	}
	if len(b) < n {
		return 0, nil, Malformed("the CBOR structure ends early")
	}
	var v uint64
	for _, c := range b[:n] {
		v = v<<8 | uint64(c)
	}
	return v, b[n:], nil
}
