// Package pkitest makes what the tests of the verifiers and of the trust step sign with, in process (signatures.md R-SIG-042, Q-SIG-46):
// the company CA (a root, an intermediate and leaves made with crypto/x509), a timestamp authority of the test's own (digitorus/timestamp),
// the release key (an OpenPGP key of go-crypto) and an object store in memory. Only tests import it.
package pkitest

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"io"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitorus/timestamp"

	"github.com/guided-traffic/quader/internal/signatures/scheme"
)

// Blobs is the object store of a test: blobs by their SHA-256; Err, when set, is the answer of every read.
type Blobs struct {
	M   map[[32]byte][]byte
	Err error
}

// NewBlobs is an empty object store.
func NewBlobs() *Blobs { return &Blobs{M: map[[32]byte][]byte{}} }

// Put stores data and answers its SHA-256.
func (b *Blobs) Put(data []byte) [32]byte {
	d := sha256.Sum256(data)
	b.M[d] = data
	return d
}

// Content implements scheme.Blobs.
func (b *Blobs) Content(_ context.Context, d [32]byte) (scheme.Content, error) {
	if b.Err != nil {
		return nil, b.Err
	}
	data, ok := b.M[d]
	if !ok {
		return nil, scheme.ErrBlobMissing
	}
	return content(data), nil
}

type content []byte

func (c content) Size() int64 { return int64(len(c)) }

func (c content) Bytes(limit int64) ([]byte, error) {
	if int64(len(c)) > limit {
		return nil, fmt.Errorf("the blob is larger than %d bytes", limit)
	}
	return []byte(c), nil
}

func (c content) Reader() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(c)), nil }

func (c content) ReaderAt() (io.ReaderAt, error) { return bytes.NewReader(c), nil }

// Key makes an ECDSA P-256 key.
func Key(t testing.TB) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// Cert issues a certificate; a nil parent makes it self-signed.
func Cert(t testing.TB, tmpl, parent *x509.Certificate, pub crypto.PublicKey, signer crypto.Signer) *x509.Certificate {
	t.Helper()
	if parent == nil {
		parent = tmpl
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, signer)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// serial numbers the certificates the tests issue.
var serial atomic.Int64

func nextSerial() *big.Int { return big.NewInt(100 + serial.Add(1)) }

// caTemplate is a CA certificate valid for ten years either side of now.
func caTemplate(cn, org string) *x509.Certificate {
	return &x509.Certificate{
		SerialNumber: nextSerial(), Subject: pkix.Name{CommonName: cn, Organization: []string{org}},
		NotBefore: time.Now().AddDate(-10, 0, 0), NotAfter: time.Now().AddDate(10, 0, 0),
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true, IsCA: true,
	}
}

// CA is the company CA: a root and an intermediate of an organisation.
type CA struct {
	Root, Inter       *x509.Certificate
	RootKey, InterKey *ecdsa.PrivateKey
}

// NewCA makes a root and an intermediate of org.
func NewCA(t testing.TB, org string) *CA {
	t.Helper()
	ca := &CA{RootKey: Key(t), InterKey: Key(t)}
	ca.Root = Cert(t, caTemplate(org+" Root CA", org), nil, ca.RootKey.Public(), ca.RootKey)
	ca.Inter = Cert(t, caTemplate(org+" Signing CA", org), ca.Root, ca.InterKey.Public(), ca.RootKey)
	return ca
}

// Leaf describes a leaf the CA issues; zero fields take a code-signing leaf of the CA's organisation valid from an hour ago for a year.
type Leaf struct {
	CN, Org             string
	NotBefore, NotAfter time.Time
	// NoEKU issues a leaf without an extended key use; EKU replaces codeSigning.
	NoEKU bool
	EKU   []x509.ExtKeyUsage
}

// Issue issues a leaf by the intermediate and answers it with its key.
func (ca *CA) Issue(t testing.TB, l Leaf) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	if l.CN == "" {
		l.CN = "Release signing"
	}
	if l.Org == "" {
		l.Org = ca.Root.Subject.Organization[0]
	}
	if l.NotBefore.IsZero() {
		l.NotBefore = time.Now().Add(-time.Hour)
	}
	if l.NotAfter.IsZero() {
		l.NotAfter = time.Now().AddDate(1, 0, 0)
	}
	eku := []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning}
	switch {
	case l.NoEKU:
		eku = nil
	case l.EKU != nil:
		eku = l.EKU
	}
	k := Key(t)
	c := Cert(t, &x509.Certificate{
		SerialNumber: nextSerial(), Subject: pkix.Name{CommonName: l.CN, Organization: []string{l.Org}},
		NotBefore: l.NotBefore, NotAfter: l.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: eku,
	}, ca.Inter, k.Public(), ca.InterKey)
	return c, k
}

// Chain is a leaf's chain, leaf first, to the root.
func (ca *CA) Chain(leaf *x509.Certificate) []*x509.Certificate {
	return []*x509.Certificate{leaf, ca.Inter, ca.Root}
}

// TSA is a timestamp authority of the test: a root and a signing certificate with the critical extended key use timeStamping only, as
// RFC 3161 asks.
type TSA struct {
	Root, Leaf *x509.Certificate
	Key        *ecdsa.PrivateKey
}

// NewTSA makes a timestamp authority.
func NewTSA(t testing.TB) *TSA {
	t.Helper()
	rootKey, key := Key(t), Key(t)
	ts := &TSA{Key: key}
	ts.Root = Cert(t, caTemplate("Example Timestamp Root", "Example Time GmbH"), nil, rootKey.Public(), rootKey)
	eku, err := asn1.Marshal([]asn1.ObjectIdentifier{{1, 3, 6, 1, 5, 5, 7, 3, 8}})
	if err != nil {
		t.Fatal(err)
	}
	ts.Leaf = Cert(t, &x509.Certificate{
		SerialNumber: nextSerial(), Subject: pkix.Name{CommonName: "Example Timestamp Authority", Organization: []string{"Example Time GmbH"}},
		NotBefore: time.Now().AddDate(-10, 0, 0), NotAfter: time.Now().AddDate(10, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage:     []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping},
		ExtraExtensions: []pkix.Extension{{Id: asn1.ObjectIdentifier{2, 5, 29, 37}, Critical: true, Value: eku}},
	}, ts.Root, key.Public(), rootKey)
	return ts
}

// Response is a granted RFC 3161 response (DER) over a hashed message, stating at.
func (ts *TSA) Response(t testing.TB, h crypto.Hash, hashed []byte, at time.Time, nonce *big.Int) []byte {
	t.Helper()
	stamp := &timestamp.Timestamp{
		HashAlgorithm: h, HashedMessage: hashed, Time: at, Policy: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 99999, 1},
		Certificates: []*x509.Certificate{ts.Root}, AddTSACertificate: true, Nonce: nonce,
	}
	resp, err := stamp.CreateResponseWithOpts(ts.Leaf, ts.Key, crypto.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// Token is the TimeStampToken of a response over the hash of message.
func (ts *TSA) Token(t testing.TB, h crypto.Hash, message []byte, at time.Time) []byte {
	t.Helper()
	sum := h.New()
	sum.Write(message)
	parsed, err := timestamp.ParseResponse(ts.Response(t, h, sum.Sum(nil), at, nil))
	if err != nil {
		t.Fatal(err)
	}
	return parsed.RawToken
}
