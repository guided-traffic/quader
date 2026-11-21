package shipped

import (
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/guided-traffic/quader/internal/signatures/anchors"
)

// maxNPMKeysBytes bounds a keys document: npm's names four entries in about 2 KiB.
const maxNPMKeysBytes = 256 << 10

// npmKeyEntry is one entry of a keys document in either of npm's two shapes: the TUF target registry.npmjs.org/keys.json (keyId,
// keyUsage, publicKey with rawBytes and validFor) and a registry's -/npm/v1/keys (keyid, key, expires).
type npmKeyEntry struct {
	KeyID     string `json:"keyId"`
	KeyUsage  string `json:"keyUsage"`
	PublicKey *struct {
		RawBytes   string `json:"rawBytes"`
		KeyDetails string `json:"keyDetails"`
		ValidFor   struct {
			Start *time.Time `json:"start"`
			End   *time.Time `json:"end"`
		} `json:"validFor"`
	} `json:"publicKey"`
	RegistryKeyID string     `json:"keyid"`
	Key           string     `json:"key"`
	Expires       *time.Time `json:"expires"`
}

// keyOf is the entry's keyid, the base64 of its PKIX DER and its expiry, in whichever shape it came.
func (e npmKeyEntry) keyOf() (keyID, raw string, expires *time.Time) {
	if e.PublicKey != nil {
		return e.KeyID, e.PublicKey.RawBytes, e.PublicKey.ValidFor.End
	}
	return e.RegistryKeyID, e.Key, e.Expires
}

func npmKeys(doc []byte) (*anchors.PublicKeyMaterial, error) {
	if len(doc) == 0 {
		return nil, errors.New("shipped: the npm keys document is empty; run make shipped-anchors")
	}
	if len(doc) > maxNPMKeysBytes {
		return nil, fmt.Errorf("shipped: the npm keys document is above %d KiB", maxNPMKeysBytes>>10)
	}
	var d struct {
		Keys []npmKeyEntry `json:"keys"`
	}
	if err := json.Unmarshal(doc, &d); err != nil {
		return nil, fmt.Errorf("shipped: read the npm keys document: %w", err)
	}
	out := &anchors.PublicKeyMaterial{}
	at := map[string]int{} // the index in out of a key by its DER
	for i, e := range d.Keys {
		keyID, raw, expires := e.keyOf()
		der, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(der) == 0 {
			return nil, fmt.Errorf("shipped: npm key %d is no base64 DER", i)
		}
		if j, ok := at[string(der)]; ok {
			out.Keys[j].Expires = laterExpiry(out.Keys[j].Expires, expires)
			continue
		}
		at[string(der)] = len(out.Keys)
		out.Keys = append(out.Keys, anchors.PublicKeyEntry{
			PEM:     string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})),
			KeyID:   keyID,
			Expires: utc(expires),
		})
	}
	if len(out.Keys) == 0 {
		return nil, errors.New("shipped: the npm keys document names no key")
	}
	return out, nil
}

// laterExpiry is the later of two expiries of one key named for two uses (npm names each key for npm:signatures and for
// npm:attestations); no expiry is the latest.
func laterExpiry(a, b *time.Time) *time.Time {
	if a == nil || b == nil {
		return nil
	}
	if b.After(*a) {
		return utc(b)
	}
	return a
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
