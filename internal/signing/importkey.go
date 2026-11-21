package signing

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"

	"github.com/youmark/pkcs8"
)

// maxKeyPEM bounds an imported key's PEM: an ECDSA P-256 key is a few hundred bytes, encrypted with its parameters below 1 KiB.
const maxKeyPEM = 16 << 10

// ErrKeyFormat is a private key that cannot be read. Its text never holds key material.
var ErrKeyFormat = errors.New("signing: the private key cannot be read")

// ParseKey reads an imported private key (R-SIG-047): one PEM block of PKCS#8 ("PRIVATE KEY"), SEC 1 ("EC PRIVATE KEY") or encrypted
// PKCS#8 ("ENCRYPTED PRIVATE KEY", PBES2 as openssl writes it), whose passphrase is used for this call alone. The key must be ECDSA on
// P-256 (R-SIG-045). PKCS#12, an RSA key and the legacy encrypted PEM of OpenSSL (Proc-Type headers) are refused; the refusal names the
// form, never the key.
func ParseKey(text, passphrase []byte) (*ecdsa.PrivateKey, error) {
	if len(text) > maxKeyPEM {
		return nil, refuse(CheckMalformed, "the private key is larger than 16 KiB; an ECDSA P-256 key in PEM is a few hundred bytes")
	}
	b, rest := pem.Decode(bytes.TrimSpace(text))
	if b == nil {
		return nil, refuse(CheckMalformed, "the private key is not PEM; convert a PKCS#12 file with openssl pkcs12 -in <file> -nodes")
	}
	if len(bytes.TrimSpace(rest)) > 0 {
		return nil, refuse(CheckMalformed, "the private key field holds more than one PEM block; give the key alone and the certificates in the chain")
	}
	if _, legacy := b.Headers["Proc-Type"]; legacy {
		return nil, refuse(CheckMalformed, "the key is encrypted in OpenSSL's legacy PEM form; convert it with openssl pkcs8 -topk8 to an encrypted PKCS#8 key")
	}
	var (
		k   any
		err error
	)
	switch b.Type {
	case "PRIVATE KEY":
		k, err = x509.ParsePKCS8PrivateKey(b.Bytes)
	case "EC PRIVATE KEY":
		k, err = x509.ParseECPrivateKey(b.Bytes)
	case "ENCRYPTED PRIVATE KEY":
		if len(passphrase) == 0 {
			return nil, refuse(CheckMalformed, "the key is encrypted: give its passphrase")
		}
		k, _, err = pkcs8.ParsePrivateKey(b.Bytes, passphrase)
		if err != nil {
			return nil, refuse(CheckMalformed, "the encrypted key does not open with the passphrase")
		}
	default:
		return nil, refuse(CheckMalformed, "the PEM block is %s; give a PRIVATE KEY, EC PRIVATE KEY or ENCRYPTED PRIVATE KEY block", b.Type)
	}
	if err != nil {
		return nil, refuse(CheckMalformed, "%s", ErrKeyFormat.Error())
	}
	ek, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		if s, ok := k.(crypto.Signer); ok {
			_, perr := P256(s.Public())
			return nil, perr
		}
		return nil, refuse(CheckKeyType, "the key is no ECDSA key; an identity's key is ECDSA on P-256")
	}
	if _, err := P256(&ek.PublicKey); err != nil {
		return nil, err
	}
	return ek, nil
}
