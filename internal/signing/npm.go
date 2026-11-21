package signing

import (
	"crypto/ecdsa"
	"encoding/base64"
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
