package anchors

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
	"time"
)

// keyIDPrefix opens npm's keyid: "SHA256:" and the base64 of a SHA-256 (R-SIG-015).
const keyIDPrefix = "SHA256:"

// checkPublicKeys checks a public-key anchor's material (R-SIG-015): 1 to 16 keys, each one PEM PKIX public key of an allowed algorithm,
// with an optional keyid of npm's form. An expired key stays: it judges the signatures made while it was valid (npm keeps its expired
// keys in keys.json for that).
func checkPublicKeys(m *PublicKeyMaterial) (*Parsed, error) {
	if len(m.Keys) == 0 {
		return nil, refuse("/publicKey/keys", "at least one key is needed")
	}
	if len(m.Keys) > maxPublicKeys {
		return nil, refuse("/publicKey/keys", fmt.Sprintf("an anchor holds at most %d keys", maxPublicKeys))
	}
	out := &Parsed{}
	for i, e := range m.Keys {
		k, err := publicKey(i, e)
		if err != nil {
			return nil, err
		}
		out.Public = append(out.Public, k)
		out.Describes = append(out.Describes, Fingerprint{Kind: "sha256", Value: k.SHA256, Subject: k.KeyID, NotAfter: k.Expires})
	}
	return out, nil
}

// publicKey reads one key of the material; i is its index, for the pointer.
func publicKey(i int, e PublicKeyEntry) (ParsedKey, error) {
	ptr := fmt.Sprintf("/publicKey/keys/%d", i)
	if len(e.PEM) > maxPublicKeyPEM {
		return ParsedKey{}, refuse(ptr+"/pem", fmt.Sprintf("a key is at most %d KiB", maxPublicKeyPEM>>10))
	}
	block, rest := pem.Decode([]byte(e.PEM))
	if block == nil || len(bytes.TrimSpace(beforeBlock([]byte(e.PEM)))) != 0 || len(bytes.TrimSpace(rest)) != 0 {
		return ParsedKey{}, refuse(ptr+"/pem", "the key is one PEM block of type PUBLIC KEY")
	}
	if block.Type != "PUBLIC KEY" {
		return ParsedKey{}, refuse(ptr+"/pem", fmt.Sprintf("a block of type %q is no PKIX public key", block.Type))
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return ParsedKey{}, refuse(ptr+"/pem", "the key does not parse: "+err.Error())
	}
	if reason := keyReason(pub); reason != "" {
		return ParsedKey{}, refuse(ptr+"/pem", "the key "+strings.TrimPrefix(reason, "has a key "))
	}
	if e.KeyID != "" && !keyIDValid(e.KeyID) {
		return ParsedKey{}, refuse(ptr+"/keyid", "a keyid is SHA256: and the base64 of 32 bytes")
	}
	return ParsedKey{Key: pub, KeyID: e.KeyID, Expires: expiresOf(e.Expires), SHA256: sha256Hex(block.Bytes)}, nil
}

// keyIDValid reports whether id has npm's form: "SHA256:" and the base64 (standard alphabet, padded or not) of 32 bytes. Whether it is
// the hash of the key is not checked: npm's keyid is a name its signatures carry, and its documentation does not promise how it is made.
func keyIDValid(id string) bool {
	rest, ok := strings.CutPrefix(id, keyIDPrefix)
	if !ok {
		return false
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(rest, "="))
	return err == nil && len(raw) == 32
}

// expiresOf is the time of an optional expiry, the zero time without one.
func expiresOf(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}
