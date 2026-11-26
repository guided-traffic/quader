package registrytoken

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/secrets"
)

// The form of a registry token (R-OCI-005, ADR-163, Q-BAS-339, Q-BAS-388):
//
//	payload = "2" ":" <opaque key id> ":" <readable claims as JSON> ":" <sealed claims>
//	signed  = "qdr_rgt_" + base64url(payload)                 // RFC 4648 §5, no padding
//	token   = signed + "." + base64url(HMAC-SHA-256(key, signed))
//
// The key ID stands before the claims, so the verifier finds the key and checks the MAC before it parses anything the
// client sent; the MAC covers the exact characters the client presents, so no canonical form of the claims is needed.
//
// Version 2 keeps readable only what a Bastion reads without a key (bastion.md R-BAS-052, R-BAS-115): a, exp, g and, on a token of
// a system subject, s. Every other claim (the endpoint, the principal, the Quader token behind it, the issue time, the catalog entry,
// a scan's ID and digest, a blob's repository) is sealed: AES-256-GCM under a key derived from the ring's key with the info
// [SealInfo], a random nonce of 12 bytes before the ciphertext, and the payload before the sealed part (its last ":" included) as
// the additional data. The opaque key ID is the first 8 bytes, in hexadecimal, of an HMAC of [opaqueKeyIDLabel] and the key ID under
// the MAC key, so it names no key of the ring to anyone without the ring. Version 1, "1:<key id>:<claims as JSON>", every claim
// readable, is issued no more; the verifier reads it until the tokens issued before have expired (they live at most [MaxLifetime]).
const (
	// Prefix starts every registry token. It is not one of the prefixes of R-AUTH-022, so the scanner pattern there does not
	// match a registry token and the syntax step of a quader token refuses it.
	Prefix = "qdr_rgt_" //nolint:gosec // G101: a prefix, not a credential
	// MaxLength bounds a registry token; a longer value is refused before anything is decoded.
	MaxLength = 4096
	// MaxEntries bounds the entries of a grant and the scope entries a token request may carry (R-OCI-004).
	MaxEntries = 16
	// MaxLifetime is the upper bound of oci.token_lifetime (R-OCI-005); a [Signer] takes the configured value.
	MaxLifetime = time.Hour
	// MaxClockSkew is how far a token's issue time may lie ahead of the verifying replica's clock: replicas' clocks differ
	// by less, and a token issued further in the future was not issued by a replica.
	MaxClockSkew = time.Minute
	// KeyInfo is the HKDF info of the MAC keys (R-ARCH-021).
	KeyInfo = "quader registry token v1"
	// SealInfo is the HKDF info of the keys that seal the claims of payload version 2 (R-ARCH-021, Q-BAS-388).
	SealInfo = "quader registry token seal v2"

	payloadV1 = "1"
	payloadV2 = "2"
	keySize   = sha256.Size
	macChars  = 43 // base64url of 32 bytes without padding
	// opaqueKeyIDLabel and the ring's key ID are what the opaque key ID of version 2 is the HMAC of.
	opaqueKeyIDLabel = "quader registry token key id"
	opaqueKeyIDBytes = 8
)

// The verification errors. None of them names the reason to the client: the stage answers each with 401 invalid_token
// (R-AUTH-002); the reason goes to the metric of the stage.
var (
	ErrMalformed  = errors.New("registrytoken: malformed")
	ErrUnknownKey = errors.New("registrytoken: signed with a key the ring does not hold")
	ErrSignature  = errors.New("registrytoken: the signature does not verify")
	ErrExpired    = errors.New("registrytoken: expired")
	// ErrLifetime marks claims issued in the future or spanning more than the configured lifetime: only a holder of the key
	// ring can sign them (H-579).
	ErrLifetime = errors.New("registrytoken: issued outside the configured lifetime")
	ErrEndpoint = errors.New("registrytoken: issued on another endpoint")
	// ErrTooLarge is the refusal of [Signer.Issue] for claims that do not fit in [MaxLength].
	ErrTooLarge = errors.New("registrytoken: the grant does not fit in one token")
)

// Is reports whether raw has the prefix of a registry token. It decides nothing about validity.
func Is(raw string) bool { return strings.HasPrefix(raw, Prefix) }

// Binding names the quader token a registry token was issued for (R-OCI-005): its ID for the request log and the audit
// actor, its type for the endpoint's token types, and the SHA-256 of its secret, the key of the token cache of R-AUTH-023.
// The hash allows no guess of the secret (256 bits of entropy, R-AUTH-023); it lets the verifier ask the cache without a
// second index, and a holder of the key ring cannot mint a token for a live quader token without having read it (H-579).
type Binding struct {
	TokenID      string
	TokenType    string
	SecretSHA256 [32]byte
}

// Claims are the content of a registry token (R-OCI-005).
type Claims struct {
	// KeyID is the ID of the ring's key whose derivation signed the token; set by [Signer.Issue].
	KeyID string
	// Anonymous is a token issued to the anonymous principal: it has no principal ID and no binding.
	Anonymous bool
	// PrincipalKind and PrincipalID name the principal the token was issued to.
	PrincipalKind string
	PrincipalID   string
	Binding       Binding
	// Endpoint is the name of the endpoint the token request arrived on (R-EP-002); the token is refused on another.
	Endpoint  string
	IssuedAt  time.Time
	ExpiresAt time.Time
	Grant     Grant
	// System, ScanID and Digest are set on a scan token only (R-OCI-037, scan.go): the system subject that uses it, the scan's ID
	// and the digest of the one manifest it reaches. A token without System carries neither of the others.
	System string
	ScanID string
	Digest string
	// Repo is set on a blob token only (malware-scanning.md R-MAL-006, blob.go): the one repository that holds its blob, whose
	// item the scan listener's blob route requires. Digest is then the blob's.
	Repo qname.Repo
}

// wire is the JSON of the claims. The names are short because the token travels in every request.
type wire struct {
	Anonymous bool        `json:"a,omitempty"`
	Kind      string      `json:"pk,omitempty"`
	Principal string      `json:"p,omitempty"`
	TokenID   string      `json:"t,omitempty"`
	TokenType string      `json:"tt,omitempty"`
	TokenHash string      `json:"th,omitempty"`
	Endpoint  string      `json:"e"`
	IssuedAt  int64       `json:"iat"`
	ExpiresAt int64       `json:"exp"`
	Grant     []wireEntry `json:"g,omitempty"`
	Catalog   bool        `json:"c,omitempty"`
	// The members of a scan token (R-OCI-037).
	System string `json:"s,omitempty"`
	ScanID string `json:"sid,omitempty"`
	Digest string `json:"d,omitempty"`
	// The repository of a blob token (R-MAL-006).
	Repo string `json:"rp,omitempty"`
}

type wireEntry struct {
	Repo    string  `json:"r"`
	Name    string  `json:"n"`
	Actions Actions `json:"a"`
}

// readableV2 are the claims of payload version 2 that stay readable: the ones a Bastion reads without a key (iface.RegistryToken*,
// R-BAS-052). The member names are the wire's.
type readableV2 struct {
	Anonymous bool        `json:"a,omitempty"`
	ExpiresAt int64       `json:"exp"`
	Grant     []wireEntry `json:"g,omitempty"`
	System    string      `json:"s,omitempty"`
}

// sealedV2 are the claims of payload version 2 that are sealed: everything that names the installation, the principal or its token.
type sealedV2 struct {
	Kind      string `json:"pk,omitempty"`
	Principal string `json:"p,omitempty"`
	TokenID   string `json:"t,omitempty"`
	TokenType string `json:"tt,omitempty"`
	TokenHash string `json:"th,omitempty"`
	Endpoint  string `json:"e"`
	IssuedAt  int64  `json:"iat"`
	Catalog   bool   `json:"c,omitempty"`
	ScanID    string `json:"sid,omitempty"`
	Digest    string `json:"d,omitempty"`
	Repo      string `json:"rp,omitempty"`
}

func (w wire) split() (readableV2, sealedV2) {
	return readableV2{Anonymous: w.Anonymous, ExpiresAt: w.ExpiresAt, Grant: w.Grant, System: w.System},
		sealedV2{
			Kind: w.Kind, Principal: w.Principal, TokenID: w.TokenID, TokenType: w.TokenType, TokenHash: w.TokenHash,
			Endpoint: w.Endpoint, IssuedAt: w.IssuedAt, Catalog: w.Catalog, ScanID: w.ScanID, Digest: w.Digest, Repo: w.Repo,
		}
}

func joined(r readableV2, s sealedV2) wire {
	return wire{
		Anonymous: r.Anonymous, Kind: s.Kind, Principal: s.Principal, TokenID: s.TokenID, TokenType: s.TokenType,
		TokenHash: s.TokenHash, Endpoint: s.Endpoint, IssuedAt: s.IssuedAt, ExpiresAt: r.ExpiresAt, Grant: r.Grant,
		Catalog: s.Catalog, System: r.System, ScanID: s.ScanID, Digest: s.Digest, Repo: s.Repo,
	}
}

// bindable reports whether a registry token may be bound to a quader token of the type: the types of R-AUTH-022 that
// authenticate a request on the client listener. A Bastion token never does (R-BAS-005).
func bindable(typ string) bool { return typ == "pat" || typ == "sat" || typ == "cit" }

// Signer issues and verifies registry tokens with the MAC keys derived from the key ring and the configured lifetime. It is
// immutable and safe for concurrent use.
type Signer struct {
	keys *secrets.DerivedKeys
	// seals are the keys that seal the claims of version 2, one per key of the ring ([SealInfo]).
	seals *secrets.DerivedKeys
	// opaque maps the opaque key ID of version 2 to the ring's key ID, and opaqueOf the other way.
	opaque, opaqueOf map[string]string
	lifetime         time.Duration
	// scanLifetime is vulnerabilities.images.pull_timeout, the bound of a scan token (Q-OCI-229); 0 issues and accepts none.
	scanLifetime time.Duration
	// blobLifetime is malware.read_timeout, the bound of a blob token (R-MAL-006); 0 issues and accepts none.
	blobLifetime time.Duration
}

// NewSigner derives the MAC keys from every key of the ring (R-ARCH-021, info [KeyInfo]): the active key's derivation
// signs, every key's derivation verifies, so a rotation of the ring needs no new secret and a token signed before it is
// accepted until it expires, as long as its key stays in the ring. lifetime is oci.token_lifetime: no token is issued or
// accepted whose claims span more, so even claims signed with the ring verify for at most that long (H-579).
func NewSigner(ring *secrets.KeyRing, lifetime time.Duration) (*Signer, error) {
	if ring == nil {
		return nil, errors.New("registrytoken: no key ring")
	}
	if lifetime < time.Second || lifetime > MaxLifetime {
		return nil, fmt.Errorf("registrytoken: the lifetime %s is not between 1 s and %s", lifetime, MaxLifetime)
	}
	keys, err := ring.Derive(KeyInfo, keySize)
	if err != nil {
		return nil, err
	}
	seals, err := ring.Derive(SealInfo, keySize)
	if err != nil {
		return nil, err
	}
	s := &Signer{keys: keys, seals: seals, opaque: map[string]string{}, opaqueOf: map[string]string{}, lifetime: lifetime}
	for _, id := range ring.KeyIDs() {
		key, _ := keys.Key(id)
		o := hex.EncodeToString(mac(key, opaqueKeyIDLabel+" "+id)[:opaqueKeyIDBytes])
		if _, taken := s.opaque[o]; taken {
			return nil, fmt.Errorf("registrytoken: two keys of the ring have the opaque key ID %s", o)
		}
		s.opaque[o], s.opaqueOf[id] = id, o
	}
	return s, nil
}

// Lifetime is the configured lifetime of a registry token (oci.token_lifetime).
func (s *Signer) Lifetime() time.Duration { return s.lifetime }

// Issue signs the claims with the active key and returns the token. KeyID is set from the ring. It fails with
// [ErrTooLarge] when the token would exceed [MaxLength], and with an error for claims that [Signer.Verify] would refuse.
func (s *Signer) Issue(c Claims) (string, error) {
	kid, key := s.keys.Active()
	c.KeyID = kid
	if err := c.check(); err != nil {
		return "", err
	}
	if c.ExpiresAt.Sub(c.IssuedAt) > s.boundOf(c) {
		return "", fmt.Errorf("%w: %s", ErrLifetime, c.ExpiresAt.Sub(c.IssuedAt))
	}
	w := wire{
		Anonymous: c.Anonymous, Kind: c.PrincipalKind, Principal: c.PrincipalID, Endpoint: c.Endpoint,
		IssuedAt: c.IssuedAt.Unix(), ExpiresAt: c.ExpiresAt.Unix(), Catalog: c.Grant.Catalog,
		System: c.System, ScanID: c.ScanID, Digest: c.Digest,
	}
	if !c.Repo.IsZero() {
		w.Repo = c.Repo.String()
	}
	if !c.Anonymous && c.System == "" {
		w.TokenID, w.TokenType = c.Binding.TokenID, c.Binding.TokenType
		w.TokenHash = base64.RawURLEncoding.EncodeToString(c.Binding.SecretSHA256[:])
	}
	for _, e := range c.Grant.Entries {
		w.Grant = append(w.Grant, wireEntry{Repo: e.Repo.String(), Name: e.Name, Actions: e.Actions})
	}
	readable, hidden := w.split()
	open, err := json.Marshal(readable)
	if err != nil {
		return "", fmt.Errorf("registrytoken: %w", err)
	}
	plain, err := json.Marshal(hidden)
	if err != nil {
		return "", fmt.Errorf("registrytoken: %w", err)
	}
	head := payloadV2 + ":" + s.opaqueOf[kid] + ":" + string(open) + ":"
	sealed, err := s.seal(kid, []byte(head), plain)
	if err != nil {
		return "", err
	}
	signed := Prefix + base64.RawURLEncoding.EncodeToString(append([]byte(head), sealed...))
	tok := signed + "." + base64.RawURLEncoding.EncodeToString(mac(key, signed))
	if len(tok) > MaxLength {
		return "", ErrTooLarge
	}
	return tok, nil
}

// seal encrypts the sealed claims of version 2 under the seal key of the ring's key kid: a random nonce, then the AES-256-GCM
// ciphertext, with head (the payload up to the sealed part) as the additional data.
func (s *Signer) seal(kid string, head, plain []byte) ([]byte, error) {
	aead, err := s.aead(kid)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize(), aead.NonceSize()+len(plain)+aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("registrytoken: nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, plain, head), nil
}

// open decrypts the sealed claims of version 2; any failure is [ErrMalformed], since the MAC verified before.
func (s *Signer) open(kid string, head, sealed []byte) ([]byte, error) {
	aead, err := s.aead(kid)
	if err != nil || len(sealed) < aead.NonceSize()+aead.Overhead() {
		return nil, ErrMalformed
	}
	plain, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], head)
	if err != nil {
		return nil, ErrMalformed
	}
	return plain, nil
}

func (s *Signer) aead(kid string) (cipher.AEAD, error) {
	key, ok := s.seals.Key(kid)
	if !ok {
		return nil, ErrUnknownKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("registrytoken: %w", err)
	}
	return cipher.NewGCM(block)
}

// Verify checks a presented registry token without any lookup (R-OCI-005): the length, the form, the named key, the MAC,
// then the claims, the expiry at now, the issue time (at most [MaxClockSkew] ahead of now), the span against the configured
// lifetime and the endpoint the request arrived on. What it returns is authentic; the state of
// the bound quader token is the stage's to check.
func (s *Signer) Verify(raw, endpoint string, now time.Time) (Claims, error) {
	if len(raw) > MaxLength || !Is(raw) {
		return Claims{}, ErrMalformed
	}
	encoded, sig, ok := strings.Cut(raw[len(Prefix):], ".")
	if !ok || len(sig) != macChars || strings.Contains(sig, ".") {
		return Claims{}, ErrMalformed
	}
	got, err := base64.RawURLEncoding.Strict().DecodeString(sig)
	if err != nil {
		return Claims{}, ErrMalformed
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return Claims{}, ErrMalformed
	}
	ver, rest, ok1 := bytes.Cut(payload, []byte(":"))
	named, body, ok2 := bytes.Cut(rest, []byte(":"))
	if !ok1 || !ok2 || len(named) == 0 {
		return Claims{}, ErrMalformed
	}
	var kid string
	switch string(ver) {
	case payloadV1:
		kid = string(named)
	case payloadV2:
		id, known := s.opaque[string(named)]
		if !known {
			return Claims{}, ErrUnknownKey
		}
		kid = id
	default:
		return Claims{}, ErrMalformed
	}
	key, known := s.keys.Key(kid)
	if !known {
		return Claims{}, ErrUnknownKey
	}
	if !hmac.Equal(got, mac(key, raw[:len(Prefix)+len(encoded)])) {
		return Claims{}, ErrSignature
	}
	var w wire
	if string(ver) == payloadV1 {
		w, err = decodeV1(body)
	} else {
		w, err = s.decodeV2(kid, payload, body)
	}
	if err != nil {
		return Claims{}, err
	}
	c, err := claimsOf(kid, w)
	if err != nil {
		return Claims{}, err
	}
	if !now.Before(c.ExpiresAt) {
		return Claims{}, ErrExpired
	}
	if c.IssuedAt.After(now.Add(MaxClockSkew)) || c.ExpiresAt.Sub(c.IssuedAt) > s.boundOf(c) {
		return Claims{}, ErrLifetime
	}
	if c.Endpoint != endpoint {
		return Claims{}, ErrEndpoint
	}
	return c, nil
}

func mac(key []byte, signed string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(signed))
	return h.Sum(nil)
}

// decodeV1 parses the authenticated claims of version 1 strictly: an unknown member or trailing data is malformed. A token that
// passed the MAC was issued by a ring holder, so a failure here and in [claimsOf] is a defect or a forgery with the ring (H-579),
// never a client mistake.
func decodeV1(body []byte) (wire, error) {
	var w wire
	if !strict(body, &w) {
		return wire{}, ErrMalformed
	}
	return w, nil
}

// decodeV2 parses the authenticated claims of version 2: the readable object, ":" and the sealed part, which it opens with the
// seal key of kid. Both are read strictly.
func (s *Signer) decodeV2(kid string, payload, body []byte) (wire, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var r readableV2
	if err := dec.Decode(&r); err != nil {
		return wire{}, ErrMalformed
	}
	rest := body[dec.InputOffset():]
	if len(rest) < 2 || rest[0] != ':' {
		return wire{}, ErrMalformed
	}
	head := payload[:len(payload)-len(rest)+1]
	plain, err := s.open(kid, head, rest[1:])
	if err != nil {
		return wire{}, err
	}
	var h sealedV2
	if !strict(plain, &h) {
		return wire{}, ErrMalformed
	}
	return joined(r, h), nil
}

// strict decodes one JSON value into v with no unknown member and nothing after it.
func strict(b []byte, v any) bool {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(v) == nil && !dec.More()
}

// claimsOf turns the wire's claims into [Claims] and checks them; a value outside the rules is malformed.
func claimsOf(kid string, w wire) (Claims, error) {
	c := Claims{
		KeyID: kid, Anonymous: w.Anonymous, PrincipalKind: w.Kind, PrincipalID: w.Principal, Endpoint: w.Endpoint,
		IssuedAt: time.Unix(w.IssuedAt, 0), ExpiresAt: time.Unix(w.ExpiresAt, 0),
		Binding: Binding{TokenID: w.TokenID, TokenType: w.TokenType}, Grant: Grant{Catalog: w.Catalog},
		System: w.System, ScanID: w.ScanID, Digest: w.Digest,
	}
	if w.Repo != "" {
		repo, err := qname.ParseRepo(w.Repo)
		if err != nil {
			return Claims{}, ErrMalformed
		}
		c.Repo = repo
	}
	if w.TokenHash != "" {
		h, err := base64.RawURLEncoding.Strict().DecodeString(w.TokenHash)
		if err != nil || len(h) != len(c.Binding.SecretSHA256) {
			return Claims{}, ErrMalformed
		}
		copy(c.Binding.SecretSHA256[:], h)
	}
	for _, e := range w.Grant {
		repo, err := qname.ParseRepo(e.Repo)
		if err != nil {
			return Claims{}, ErrMalformed
		}
		c.Grant.Entries = append(c.Grant.Entries, Entry{Repo: repo, Name: e.Name, Actions: e.Actions})
	}
	if err := c.check(); err != nil {
		return Claims{}, ErrMalformed
	}
	return c, nil
}

// check holds the rules both sides apply to the claims.
func (c Claims) check() error {
	switch {
	case c.KeyID == "":
		return fmt.Errorf("%w: no key ID", ErrMalformed)
	case c.Endpoint == "":
		return fmt.Errorf("%w: no endpoint", ErrMalformed)
	case !c.ExpiresAt.After(c.IssuedAt), c.ExpiresAt.Sub(c.IssuedAt) > MaxLifetime:
		return fmt.Errorf("%w: the lifetime is not between 1 s and %s", ErrMalformed, MaxLifetime)
	case len(c.Grant.Entries) > MaxEntries:
		return fmt.Errorf("%w: more than %d grant entries", ErrMalformed, MaxEntries)
	}
	switch c.System {
	case "":
	case ScanSubject:
		if !c.Repo.IsZero() {
			return fmt.Errorf("%w: a scan token of an image names its repository in its grant entry, not as a blob token's", ErrMalformed)
		}
		return c.checkScan()
	case BlobSubject:
		return c.checkBlob()
	default:
		return fmt.Errorf("%w: the system subject %q is no scan subject", ErrMalformed, c.System)
	}
	if c.ScanID != "" || c.Digest != "" || !c.Repo.IsZero() {
		return fmt.Errorf("%w: a token without a system subject carries no scan ID, no digest and no repository", ErrMalformed)
	}
	if c.Anonymous {
		if c.PrincipalID != "" || c.Binding != (Binding{}) {
			return fmt.Errorf("%w: an anonymous token names a principal or a token", ErrMalformed)
		}
	} else if c.PrincipalID == "" || c.PrincipalKind == "" || c.Binding.TokenID == "" || !bindable(c.Binding.TokenType) ||
		c.Binding.SecretSHA256 == [32]byte{} {
		return fmt.Errorf("%w: a token of a principal needs the principal and its Quader token", ErrMalformed)
	}
	for _, e := range c.Grant.Entries {
		if !e.Repo.Valid() || e.Name == "" || e.Actions == 0 || e.Actions&^All != 0 {
			return fmt.Errorf("%w: grant entry %s %q", ErrMalformed, e.Repo, e.Name)
		}
	}
	return nil
}
