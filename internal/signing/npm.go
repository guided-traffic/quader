package signing

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"sort"
	"time"

	"golang.org/x/crypto/ssh"
)

// NPMKeyType is the keytype and the scheme of every key Quader lists (R-SIG-050): the one npm's documentation names.
const NPMKeyType = "ecdsa-sha2-nistp256"

// NPMExpiresLayout is how npm's keys document writes expires: ISO 8601 in UTC with milliseconds.
const NPMExpiresLayout = "2006-01-02T15:04:05.000Z"

// NPMKeyID is npm's keyid of an ECDSA P-256 key (R-SIG-050): "SHA256:" and the standard base64 without padding of the SHA-256 of the
// key's SSH wire form, as ssh-keygen -l prints it and npmjs publishes it.
func NPMKeyID(pub *ecdsa.PublicKey) (string, error) {
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("npm keyid: %w", err)
	}
	return ssh.FingerprintSHA256(k), nil
}

// NPMKey is an entry of npm's keys document (R-SIG-050).
type NPMKey struct {
	KeyID   string
	KeyType string
	Scheme  string
	// Key is the standard base64 of the DER SubjectPublicKeyInfo, on one line.
	Key string
	// Expires is nil for a key that signs or is about to; else the moment its identity stopped signing.
	Expires *time.Time
}

// ExpiresText renders Expires as npm's document writes it, "" for none.
func (k NPMKey) ExpiresText() string {
	if k.Expires == nil {
		return ""
	}
	return k.Expires.UTC().Format(NPMExpiresLayout)
}

// NPMKeys is the keys document of a local npm repository for the identities (R-SIG-045's table, R-SIG-048): nothing while no identity is
// current, so that an installation that stops signing reads as one without keys; else the announced and the current identity without an
// expiry and every previous one with the moment it stopped signing, each keyid once with the state of its newest row. Pending and
// retired identities are never listed.
func NPMKeys(ids []Identity) []NPMKey {
	current := false
	for _, i := range ids {
		if i.State == StateCurrent {
			current = true
		}
	}
	if !current {
		return []NPMKey{}
	}
	newest := map[string]Identity{}
	for _, i := range ids {
		if !i.Listed() {
			continue
		}
		if n, ok := newest[i.NPMKeyID]; !ok || i.ID > n.ID {
			newest[i.NPMKeyID] = i
		}
	}
	out := make([]NPMKey, 0, len(newest))
	for _, i := range newest {
		k := NPMKey{KeyID: i.NPMKeyID, KeyType: NPMKeyType, Scheme: NPMKeyType, Key: base64.StdEncoding.EncodeToString(i.PublicKey)}
		if i.State == StatePrevious {
			t := i.ExpiresAt.UTC()
			k.Expires = &t
		}
		out = append(out, k)
	}
	sort.Slice(out, func(a, b int) bool {
		x, y := out[a], out[b]
		switch {
		case (x.Expires == nil) != (y.Expires == nil):
			return x.Expires == nil
		case x.Expires != nil && !x.Expires.Equal(*y.Expires):
			return x.Expires.After(*y.Expires)
		}
		return x.KeyID < y.KeyID
	})
	return out
}

// NPMMessage is the message of npm's registry signature of a version (R-SIG-050): "<name>@<version>:<integrity>", the _id and the
// dist.integrity of the version's document, as pacote's lib/registry.js builds it from mani._id and mani._integrity.
func NPMMessage(name, version, integrity string) []byte {
	return []byte(name + "@" + version + ":" + integrity)
}

// NPMSignature is an entry of a version's dist.signatures (R-SIG-050): the keyid of the key that made it and the standard base64 of the
// DER ECDSA signature over the SHA-256 of the message.
type NPMSignature struct {
	KeyID string `json:"keyid"`
	Sig   string `json:"sig"`
}

// Entry is the entry as the document carries it and the store keeps it: {"keyid": …, "sig": …}.
func (s NPMSignature) Entry() []byte {
	b, _ := json.Marshal(s) // two strings cannot fail
	return b
}

// SignNPM makes npm's registry signature of a message with the identity's key (R-SIG-050): ECDSA over SHA-256, DER, as
// crypto.createVerify('SHA256') checks it. signer is the key of the job's session, keyID the identity's npm keyid.
func SignNPM(signer crypto.Signer, keyID string, message []byte) (NPMSignature, error) {
	if signer == nil || keyID == "" {
		return NPMSignature{}, errors.New("npm signature: no key")
	}
	digest := sha256.Sum256(message)
	der, err := signer.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		return NPMSignature{}, fmt.Errorf("npm signature: %w", err)
	}
	return NPMSignature{KeyID: keyID, Sig: base64.StdEncoding.EncodeToString(der)}, nil
}

// VerifyNPM is pacote's check of a registry signature written in Go (pacote 22.0.1 lib/registry.js, R-SIG-050): the key's base64 DER
// wrapped in a PUBLIC KEY block, crypto.createVerify('SHA256') over the message, the base64 DER signature. It answers nil when the
// signature verifies.
func VerifyNPM(key string, message []byte, sig string) error {
	block, _ := pem.Decode([]byte("-----BEGIN PUBLIC KEY-----\n" + key + "\n-----END PUBLIC KEY-----\n"))
	if block == nil {
		return errors.New("npm signature: the key is no base64 DER public key")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("npm signature: the key: %w", err)
	}
	ec, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return errors.New("npm signature: the key is no ECDSA key")
	}
	der, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return fmt.Errorf("npm signature: the signature: %w", err)
	}
	digest := sha256.Sum256(message)
	if !ecdsa.VerifyASN1(ec, digest[:], der) {
		return errors.New("npm signature: the signature does not verify")
	}
	return nil
}

// NPMServed is the ids of the identities whose npm signatures the documents serve (R-SIG-045, R-SIG-048): every current and previous
// identity while one is current, none otherwise, ascending. What a document renders follows it; a change of it raises the revision of
// every package whose document changes.
func NPMServed(ids []Identity) []int64 {
	out := []int64{}
	current := false
	for _, i := range ids {
		current = current || i.State == StateCurrent
	}
	if !current {
		return out
	}
	for _, i := range ids {
		if i.State == StateCurrent || i.State == StatePrevious {
			out = append(out, i.ID)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a] < out[b] })
	return out
}
