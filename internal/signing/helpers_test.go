package signing_test

import (
	"crypto/ecdsa"
	"encoding/pem"
	"errors"
	"strings"
	"testing"

	"github.com/youmark/pkcs8"

	"github.com/guided-traffic/quader/internal/errs"
)

// encryptedPKCS8 encrypts a key as openssl pkcs8 -topk8 -v2 aes-256-cbc writes it.
func encryptedPKCS8(t *testing.T, k *ecdsa.PrivateKey, pass string) []byte {
	t.Helper()
	der, err := pkcs8.MarshalPrivateKey(k, []byte(pass), nil)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: der})
}

// fieldsOf renders the pointers of a validation error's fields.
func fieldsOf(err error) string {
	var e *errs.Error
	if !errors.As(err, &e) {
		return ""
	}
	var out []string
	for _, f := range e.Fields {
		out = append(out, f.Pointer)
	}
	return strings.Join(out, " ")
}
