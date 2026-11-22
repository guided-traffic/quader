package blobcrypt

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// KeyedName is the object name of a blob on a connection with keyed names (R-STOR-086):
// <prefix>/keyed/<m0m1>/<m2m3>/<64 hex of m> with m = HMAC-SHA-256(name key, FMT ‖ " name" ‖ digest), in the two-level
// fan-out of R-STOR-001. With an empty prefix the name starts at keyed/.
func KeyedName(nameKey Key, prefix string, digest [32]byte) (string, error) {
	b, err := nameKey.bytes()
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, b)
	mac.Write([]byte(labelName))
	mac.Write(digest[:])
	h := hex.EncodeToString(mac.Sum(nil))
	return KeyedPrefix(prefix) + h[0:2] + "/" + h[2:4] + "/" + h, nil
}

// KeyedPrefix is the prefix below which the keyed names of a connection lie: <prefix>/keyed/, or keyed/ without a prefix.
func KeyedPrefix(prefix string) string {
	if prefix == "" {
		return "keyed/"
	}
	return prefix + "/keyed/"
}
