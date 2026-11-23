package iface

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The handover per blob (R-BAS-070 to R-BAS-073): the Bastion offers to take one on a miss, and the main installation may
// answer the forward with a link of its object store instead of the bytes. This file holds the two values of
// [HeaderHandover] and every check of R-BAS-073; who offers, who hands over and who reads are the two sides' code.

// ErrHandover is the error of an offer or a handover value that the interface refuses. Its texts never hold the link or a
// data key: a refusal ends in the forward of R-BAS-075 and may be logged.
var ErrHandover = errors.New("bastion handover: refused")

// HandoverOffer is the Bastion's offer to take a handover (R-BAS-072): the value "accept; max-size=<n>" of [HeaderHandover]
// on a forward. MaxSize is the largest blob the Bastion would keep at that moment, at least 1.
type HandoverOffer struct {
	MaxSize int64
}

// handoverOfferPrefix is the offer's value up to the size: one spelling, no optional whitespace.
const handoverOfferPrefix = "accept; max-size="

// String formats the offer for [HeaderHandover]. An offer whose MaxSize is below 1 is none: [HandoverOffer.Set] writes
// nothing for it.
func (o HandoverOffer) String() string {
	return handoverOfferPrefix + strconv.FormatInt(o.MaxSize, 10)
}

// Set writes the offer onto a request's header, or nothing for a MaxSize below 1: a Bastion that can keep nothing
// offers nothing.
func (o HandoverOffer) Set(h http.Header) {
	if o.MaxSize >= 1 {
		h.Set(HeaderHandover, o.String())
	}
}

// ParseHandoverOffer reads an offer written by [HandoverOffer.String]: exactly "accept; max-size=" and a decimal number
// from 1 to 2^63−1 without a sign or a leading zero. The main installation treats any other value as no offer.
func ParseHandoverOffer(v string) (HandoverOffer, error) {
	digits, ok := strings.CutPrefix(v, handoverOfferPrefix)
	if !ok || digits == "" || len(digits) > 19 || digits[0] == '0' {
		return HandoverOffer{}, fmt.Errorf("%w: the offer %q is not %q and a size", ErrHandover, v, handoverOfferPrefix)
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return HandoverOffer{}, fmt.Errorf("%w: the offer %q is not %q and a size", ErrHandover, v, handoverOfferPrefix)
		}
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return HandoverOffer{}, fmt.Errorf("%w: the offer's size %q is out of range", ErrHandover, digits)
	}
	return HandoverOffer{MaxSize: n}, nil
}

// HandoverOfferFrom reads the offer of a request's header: ok is false when the request carries none; more than one value,
// or a value [ParseHandoverOffer] refuses, is an error.
func HandoverOfferFrom(h http.Header) (offer HandoverOffer, ok bool, err error) {
	vs := h.Values(HeaderHandover)
	switch len(vs) {
	case 0:
		return HandoverOffer{}, false, nil
	case 1:
		o, err := ParseHandoverOffer(vs[0])
		return o, err == nil, err
	}
	return HandoverOffer{}, false, fmt.Errorf("%w: %d values of %s", ErrHandover, len(vs), HeaderHandover)
}

// MaxHandoverBytes bounds the value of [HeaderHandover] on a handover answer, encoded (R-BAS-073): 8 KiB.
const MaxHandoverBytes = 8 << 10

// HandoverKeyBytes is the size of a handed data key (R-STOR-076): 32 bytes, 43 characters of base64url without padding.
const HandoverKeyBytes = 32

// handoverKeyChars is the length of a data key in base64url without padding.
const handoverKeyChars = 43

// HandoverKey is the data key of one encrypted copy, handed with its [Handover] (R-BAS-073, R-BAS-076). Its String,
// GoString, Format, LogValue and MarshalJSON render the redaction marker and nothing of the key; only [HandoverKey.Bytes]
// returns it, and only the reader of the direct read calls that. [EncodeHandover] and [DecodeHandover] carry it on the wire.
// The zero value is no key.
type HandoverKey struct {
	m *handoverKeyMaterial
}

type handoverKeyMaterial struct {
	b    [HandoverKeyBytes]byte
	live bool
}

// handoverKeyRedacted is what every rendering of a key writes.
const handoverKeyRedacted = "[REDACTED]"

// NewHandoverKey copies a data key of [HandoverKeyBytes] bytes; the caller clears b. The error names the length only.
func NewHandoverKey(b []byte) (HandoverKey, error) {
	if len(b) != HandoverKeyBytes {
		return HandoverKey{}, fmt.Errorf("%w: a data key is %d bytes, not %d", ErrHandover, HandoverKeyBytes, len(b))
	}
	m := &handoverKeyMaterial{live: true}
	copy(m.b[:], b)
	return HandoverKey{m: m}, nil
}

// Present reports whether k holds a key that was not destroyed.
func (k HandoverKey) Present() bool { return k.m != nil && k.m.live }

// Bytes returns a copy of the key, or nil for no key or a destroyed one; the caller clears the copy once the format's key
// holds it (blobcrypt.KeyFromBytes).
func (k HandoverKey) Bytes() []byte {
	if !k.Present() {
		return nil
	}
	return append([]byte(nil), k.m.b[:]...)
}

// Destroy overwrites the key; every copy of k shares it. A destroyed key is no key.
func (k HandoverKey) Destroy() {
	if k.m != nil {
		clear(k.m.b[:])
		k.m.live = false
	}
}

func (HandoverKey) String() string       { return handoverKeyRedacted }
func (HandoverKey) GoString() string     { return handoverKeyRedacted }
func (HandoverKey) LogValue() slog.Value { return slog.StringValue(handoverKeyRedacted) }

// Format writes the redaction marker for every verb, so that no verb of fmt reaches the key's bytes.
func (HandoverKey) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(handoverKeyRedacted)) }

// MarshalJSON writes the redaction marker: a key reaches JSON only through [EncodeHandover].
func (HandoverKey) MarshalJSON() ([]byte, error) { return []byte(`"` + handoverKeyRedacted + `"`), nil }

// Handover is the value of [HeaderHandover] on an answer of the class [ClassHandover] (R-BAS-073): one link signed for a
// GET of one object, the blob's digest and sizes, the connection, and for a copy on an encrypted connection its data key.
// The JSON members are the field tags; [EncodeHandover] and [DecodeHandover] are its only wire form. String, Format and
// LogValue render the digest, the sizes, the connection and the expiry, never the link and never the key.
type Handover struct {
	// Link is an absolute https URL, signed for a GET of the object without overrides; a bearer for its life (H-12).
	Link string `json:"link"`
	// ExpiresAt is the end of the link's life.
	ExpiresAt time.Time `json:"expiresAt"`
	// Connection is the identifier of the connection the object lies on (R-STOR-050).
	Connection string `json:"connection"`
	// SHA256 is the blob's content address: 64 lower-case hexadecimal digits.
	SHA256 string `json:"sha256"`
	// Size is the blob's plaintext size.
	Size int64 `json:"size"`
	// StoredSize is the object's size at the store: Size for a plain copy, S(Size) of R-STOR-076 for an encrypted one.
	StoredSize int64 `json:"storedSize"`
	// Encrypted says the copy lies on an encrypted connection.
	Encrypted bool `json:"encrypted"`
	// DataKey is the copy's data key, present exactly when Encrypted is true.
	DataKey HandoverKey `json:"dataKey,omitzero"`
}

// handoverFields is a [Handover] without its methods, so that its members encode as they are and not through
// [Handover.MarshalJSON].
type handoverFields Handover

// handoverWire is the wire form: the handover's members, with the data key in base64url in place of the redacting
// [HandoverKey] (the outer member wins over the embedded one of the same name). A nil DataKey is an absent member.
type handoverWire struct {
	handoverFields
	DataKey *string `json:"dataKey,omitempty"`
}

// MarshalJSON writes the members with the link and a data key redacted, so that a handover inside another value that is
// written as JSON (a log record, a support bundle) shows neither. The wire form is [EncodeHandover]'s alone.
func (h Handover) MarshalJSON() ([]byte, error) {
	w := handoverWire{handoverFields: handoverFields(h)}
	w.Link = handoverKeyRedacted
	if h.DataKey.m != nil {
		redacted := handoverKeyRedacted
		w.DataKey = &redacted
	}
	return Marshal(w)
}

// String renders the handover without its link and its key.
func (h Handover) String() string {
	return fmt.Sprintf("handover of sha256:%s (%d bytes, stored %d, connection %s, encrypted %t, expires %s)",
		h.SHA256, h.Size, h.StoredSize, h.Connection, h.Encrypted, h.ExpiresAt.UTC().Format(time.RFC3339))
}

// GoString is String.
func (h Handover) GoString() string { return h.String() }

// Format writes String for every verb, so that no verb of fmt reaches the link or the key.
func (h Handover) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(h.String())) }

// LogValue renders the digest, the sizes, the connection, the expiry and whether a key came with it.
func (h Handover) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("sha256", h.SHA256), slog.Int64("size", h.Size), slog.Int64("stored_size", h.StoredSize),
		slog.String("connection", h.Connection), slog.Bool("encrypted", h.Encrypted), slog.Time("expires_at", h.ExpiresAt),
	)
}

// EncodeHandover writes h as the value of [HeaderHandover]: its JSON in base64url without padding (RFC 4648 §5). It refuses
// a value [DecodeHandover] would refuse, and one longer than [MaxHandoverBytes], so the main installation never sends a
// handover the Bastion cannot take.
func EncodeHandover(h Handover) (string, error) {
	if err := h.validate(); err != nil {
		return "", err
	}
	w := handoverWire{handoverFields: handoverFields(h)}
	var key string
	if h.Encrypted {
		raw := h.DataKey.Bytes()
		key = base64.RawURLEncoding.EncodeToString(raw)
		clear(raw)
		w.DataKey = &key
	}
	body, err := Marshal(w)
	if err != nil {
		return "", fmt.Errorf("%w: encoding the handover: %w", ErrHandover, err)
	}
	v := base64.RawURLEncoding.EncodeToString(body)
	clear(body)
	if len(v) > MaxHandoverBytes {
		return "", fmt.Errorf("%w: the value is %d bytes, more than %d", ErrHandover, len(v), MaxHandoverBytes)
	}
	return v, nil
}

// Set writes h onto an answer's header as [HeaderHandover] (EncodeHandover).
func (h Handover) Set(hdr http.Header) error {
	v, err := EncodeHandover(h)
	if err != nil {
		return err
	}
	hdr.Set(HeaderHandover, v)
	return nil
}

// DecodeHandover reads a value written by [EncodeHandover] and checks what needs nothing but the value (R-BAS-073): at
// most [MaxHandoverBytes]; strict base64url without padding; one JSON object without an unknown member or trailing data; a
// link that is an absolute https URL with a host and without user information or a fragment; an expiry; a connection
// identifier of R-STOR-050; a digest of 64 lower-case hexadecimal digits; sizes that are not negative; storedSize equal to
// size on a plain copy and to S(size) on an encrypted one; a data key of 32 bytes present exactly on an encrypted copy.
// What depends on the moment and the request is [Handover.Check]. A refused value's key is destroyed before it returns.
func DecodeHandover(v string) (Handover, error) {
	switch {
	case v == "":
		return Handover{}, fmt.Errorf("%w: the value is empty", ErrHandover)
	case len(v) > MaxHandoverBytes:
		return Handover{}, fmt.Errorf("%w: the value is %d bytes, more than %d", ErrHandover, len(v), MaxHandoverBytes)
	}
	body, err := base64.RawURLEncoding.Strict().DecodeString(v)
	if err != nil {
		return Handover{}, fmt.Errorf("%w: the value is not base64url without padding", ErrHandover)
	}
	defer clear(body)
	var w handoverWire
	if err := DecodeStrict(bytes.NewReader(body), &w); err != nil {
		return Handover{}, fmt.Errorf("%w: %s", ErrHandover, jsonProblem(err))
	}
	h := Handover(w.handoverFields)
	if w.DataKey != nil {
		k, err := decodeHandoverKey(*w.DataKey)
		if err != nil {
			return Handover{}, err
		}
		h.DataKey = k
	}
	if err := h.validate(); err != nil {
		h.DataKey.Destroy()
		return Handover{}, err
	}
	return h, nil
}

// decodeHandoverKey reads a data key of 43 base64url characters without padding. Its errors name lengths only.
func decodeHandoverKey(s string) (HandoverKey, error) {
	if len(s) != handoverKeyChars {
		return HandoverKey{}, fmt.Errorf("%w: the data key is %d characters, not %d", ErrHandover, len(s), handoverKeyChars)
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(s)
	if err != nil {
		return HandoverKey{}, fmt.Errorf("%w: the data key is not base64url without padding", ErrHandover)
	}
	defer clear(raw)
	return NewHandoverKey(raw)
}

// jsonProblem describes a decoding error without a byte of the input: the JSON of a handover holds a data key, and a
// syntax error's text quotes the character it met.
func jsonProblem(err error) string {
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syn):
		return "the JSON breaks off or is malformed at byte " + strconv.FormatInt(syn.Offset, 10)
	case errors.As(err, &typ):
		return "the member " + strconv.Quote(typ.Field) + " has the wrong type"
	}
	return "the JSON is not one handover object (an unknown member, trailing data or a malformed value)"
}

// HandoverFrom reads the handover of an answer's header: ok is false when the answer carries none; more than one value, or
// a value [DecodeHandover] refuses, is an error.
func HandoverFrom(hdr http.Header) (h Handover, ok bool, err error) {
	vs := hdr.Values(HeaderHandover)
	switch len(vs) {
	case 0:
		return Handover{}, false, nil
	case 1:
		h, err := DecodeHandover(vs[0])
		return h, err == nil, err
	}
	return Handover{}, false, fmt.Errorf("%w: %d values of %s", ErrHandover, len(vs), HeaderHandover)
}

// HandoverCheck is what the Bastion checks a handover against before it reads (R-BAS-073).
type HandoverCheck struct {
	// Now is the Bastion's clock; a zero time refuses every handover.
	Now time.Time
	// Offer is the offer the forward carried; size must be at most its MaxSize.
	Offer HandoverOffer
	// ETag is the answer's ETag; sha256 must equal its content address ([ETagContentAddress]).
	ETag string
	// RefusedHosts are the host names of server.base_url, bastion.main.url and bastion.main.packages_url (as
	// url.URL.Hostname gives them): a link on one of them is refused, whatever its port (R-BAS-074).
	RefusedHosts []string
}

// Check holds a handover to the checks of R-BAS-073 that depend on the moment and the request, after those of
// [DecodeHandover]: the link's expiry lies after Now; size is at most the offer's MaxSize; sha256 equals the content
// address of the answer's ETag; the link's host is none of RefusedHosts. A failure is a bad answer, which ends in the
// forward of R-BAS-075; the error names neither the link nor the key.
func (h Handover) Check(c HandoverCheck) error {
	if err := h.validate(); err != nil {
		return err
	}
	switch {
	case c.Now.IsZero() || !c.Now.Before(h.ExpiresAt):
		return fmt.Errorf("%w: the link expired at %s", ErrHandover, h.ExpiresAt.UTC().Format(time.RFC3339))
	case c.Offer.MaxSize < 1:
		return fmt.Errorf("%w: the forward offered nothing", ErrHandover)
	case h.Size > c.Offer.MaxSize:
		return fmt.Errorf("%w: the blob's %d bytes exceed the offered %d", ErrHandover, h.Size, c.Offer.MaxSize)
	}
	if addr, ok := ETagContentAddress(c.ETag); !ok || addr != h.SHA256 {
		return fmt.Errorf("%w: sha256 %s is not the content address of the answer's ETag", ErrHandover, h.SHA256)
	}
	u, err := url.Parse(h.Link)
	if err != nil {
		return fmt.Errorf("%w: the link is not a URL", ErrHandover)
	}
	host := hostKey(u.Hostname())
	for _, refused := range c.RefusedHosts {
		if r := hostKey(refused); r != "" && r == host {
			return fmt.Errorf("%w: the link leads to a host of the Bastion or of the main installation", ErrHandover)
		}
	}
	return nil
}

// hostKey is a host name as two hosts are compared: lower case, without a trailing dot and without brackets.
func hostKey(h string) string {
	return strings.TrimSuffix(strings.ToLower(strings.Trim(h, "[]")), ".")
}

// validate holds the checks of [DecodeHandover] that need nothing but the value. Its errors name neither the link nor the
// key.
func (h Handover) validate() error {
	if err := validHandoverLink(h.Link); err != nil {
		return err
	}
	switch {
	case h.ExpiresAt.IsZero():
		return fmt.Errorf("%w: the link has no expiry", ErrHandover)
	case !validConnection(h.Connection):
		return fmt.Errorf("%w: %q is no connection identifier", ErrHandover, h.Connection)
	case !lowerHex64(h.SHA256):
		return fmt.Errorf("%w: sha256 is not 64 lower-case hexadecimal digits", ErrHandover)
	case h.Size < 0 || h.StoredSize < 0:
		return fmt.Errorf("%w: a size is negative", ErrHandover)
	}
	if !h.Encrypted {
		switch {
		case h.StoredSize != h.Size:
			return fmt.Errorf("%w: a plain copy's stored size %d is not its size %d", ErrHandover, h.StoredSize, h.Size)
		case h.DataKey.m != nil:
			return fmt.Errorf("%w: a plain copy carries a data key", ErrHandover)
		}
		return nil
	}
	if want, ok := storedSizeOf(h.Size); !ok || h.StoredSize != want {
		return fmt.Errorf("%w: an encrypted copy's stored size %d is not S(%d)", ErrHandover, h.StoredSize, h.Size)
	}
	if !h.DataKey.Present() {
		return fmt.Errorf("%w: an encrypted copy carries no data key", ErrHandover)
	}
	return nil
}

// validHandoverLink accepts an absolute https URL with a host, without user information (which a client would send as a
// credential) and without a fragment.
func validHandoverLink(link string) error {
	u, err := url.Parse(link)
	switch {
	case err != nil:
		return fmt.Errorf("%w: the link is not a URL", ErrHandover)
	case u.Scheme != "https" || u.Opaque != "":
		return fmt.Errorf("%w: the link is not an absolute https URL", ErrHandover)
	case u.Hostname() == "":
		return fmt.Errorf("%w: the link has no host", ErrHandover)
	case u.User != nil:
		return fmt.Errorf("%w: the link carries user information", ErrHandover)
	case u.Fragment != "" || strings.Contains(link, "#"):
		return fmt.Errorf("%w: the link carries a fragment", ErrHandover)
	}
	return nil
}

// ETagContentAddress reads the SHA-256 of a strong ETag that is a content address (R-STOR-012: "<sha256 hex>"; the
// registry's "sha256:<hex>", R-BAS-053). ok is false for a weak validator or any other value.
func ETagContentAddress(etag string) (string, bool) {
	if strings.HasPrefix(etag, "W/") {
		return "", false
	}
	addr := strings.TrimPrefix(strings.Trim(etag, `"`), "sha256:")
	return addr, lowerHex64(addr)
}

// lowerHex64 reports 64 lower-case hexadecimal digits.
func lowerHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// validConnection is the pattern of a connection identifier, ^[a-z][a-z0-9-]{0,30}[a-z0-9]$ (R-STOR-050). The package
// imports nothing of the module, so the rule is written here; a test holds it to config.ValidConnectionID.
func validConnection(id string) bool {
	if len(id) < 2 || len(id) > 32 || id[0] < 'a' || id[0] > 'z' {
		return false
	}
	for i := 1; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-' && i < len(id)-1:
		default:
			return false
		}
	}
	return true
}

// The sizes of the stored format quader-blob-v1 that S needs (R-STOR-076). The package imports nothing of the module, so
// they are written here; a test holds storedSizeOf to blobcrypt.StoredSize.
const (
	storedSegmentPlain    = 64 << 10 // the plaintext of a full data segment
	storedSegmentOverhead = 12 + 16  // a segment's nonce and tag
	storedFinalSegment    = 8 + 32 + storedSegmentOverhead
	storedMaxPlain        = int64(1) << 42 // 4 TiB
)

// storedSizeOf is S(L) = L + 28·⌈L / 65,536⌉ + 68, the stored size of an encrypted object of L plaintext bytes; ok is false
// for a size outside 0 to 4 TiB, which no writer seals.
func storedSizeOf(plain int64) (int64, bool) {
	if plain < 0 || plain > storedMaxPlain {
		return 0, false
	}
	segments := (plain + storedSegmentPlain - 1) / storedSegmentPlain
	return plain + storedSegmentOverhead*segments + storedFinalSegment, true
}
