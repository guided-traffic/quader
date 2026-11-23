package offline

import (
	"encoding/hex"
	"strings"

	"github.com/guided-traffic/quader/internal/blob/blobcrypt"
	"github.com/guided-traffic/quader/internal/blob/keys"
)

// family is a key family below a connection's prefix that holds blobs: keyed/ (R-STOR-086), sha256/ (R-STOR-001) and a backup's
// pool/sha256/ (backup-and-restore.md R-BAK-006, R-BAK-032).
type family int

const (
	familyKeyed family = iota + 1
	familySHA256
	familyPool
)

// families are the roots the tool walks, in this order.
func families() []struct {
	root string
	fam  family
} {
	return []struct {
		root string
		fam  family
	}{{"keyed/", familyKeyed}, {"sha256/", familySHA256}, {"pool/sha256/", familyPool}}
}

// base is the start of every key below prefix: "<prefix>/", or nothing for the empty prefix.
func base(prefix string) string {
	if prefix == "" {
		return ""
	}
	return prefix + "/"
}

// objectName is what a name below a family says: its family, the 64 hex characters it ends with (the MAC of a keyed name, the digest
// of a plain one) and, for an encrypted connection's copy in a backup's pool, the identifier of the storage key it names
// (pool/sha256/<h0h1>/<h2h3>/<hex>.<key id>, R-BAK-032).
type objectName struct {
	fam   family
	hex   string
	keyID string
}

// parseName reads a name below prefix: <family>/<h0h1>/<h2h3>/<64 hex>, with ".<key id>" after it in the pool. ok is false for any
// other name, which the tool reports as a name that is not of the layout and never writes to a directory.
func parseName(prefix, name string) (objectName, bool) {
	for _, f := range families() {
		rest, ok := strings.CutPrefix(name, base(prefix)+f.root)
		if !ok {
			continue
		}
		if len(rest) < 70 || rest[2] != '/' || rest[5] != '/' {
			return objectName{}, false
		}
		h, suffix := rest[6:70], rest[70:]
		if !lowerHex(h) || rest[0:2] != h[0:2] || rest[3:5] != h[2:4] {
			return objectName{}, false
		}
		n := objectName{fam: f.fam, hex: h}
		if suffix != "" {
			if f.fam != familyPool || suffix[0] != '.' || !blobcrypt.ValidKeyID(suffix[1:]) {
				return objectName{}, false
			}
			n.keyID = suffix[1:]
		}
		return n, true
	}
	return objectName{}, false
}

func lowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// expectsRecord says whether an object of this name lies on an encrypted connection's layout whatever its metadata says: a keyed
// name, a pool name with a key, and a plain name on a connection the element (or the pull) says is encrypted.
func (n objectName) expectsRecord(encrypted bool) bool {
	return n.fam == familyKeyed || n.keyID != "" || (n.fam == familySHA256 && encrypted)
}

// checkName compares the name of an opened object with the digest and the storage key its wrap names: a keyed name must be the
// keyed name of the digest under the name key of the storage key's connection (R-STOR-086), a plain name must carry the digest, and
// a pool name the digest and the storage key. status is empty when it is; otherwise name_mismatch, or unknown_key or destroyed_key
// for a keyed name whose name key the export does not hold.
func (ks *Keys) checkName(prefix, name string, n objectName, dk blobcrypt.DataKey) (status, detail string) {
	digest := hex.EncodeToString(dk.Digest[:])
	switch n.fam {
	case familyKeyed:
		id, _ := keys.ParseID(dk.KeyID) // the lookup opened it, so the export lists it under an identifier of storage_keys
		nk, st, d := ks.nameKey(id.Connection)
		if st != "" {
			return st, d
		}
		want, err := blobcrypt.KeyedName(nk, prefix, dk.Digest)
		if err != nil || want != name {
			return StatusNameMismatch, "the name is not the keyed name of the digest its wrap binds, under the name key of connection " + id.Connection
		}
	case familySHA256:
		if n.hex != digest {
			return StatusNameMismatch, "the name carries another digest than its wrap binds"
		}
	case familyPool:
		if n.hex != digest || n.keyID != dk.KeyID {
			return StatusNameMismatch, "the pool name carries another digest or storage key than its wrap binds"
		}
	}
	return "", ""
}
