package nugetcms

import (
	"archive/zip"
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"sort"
	"testing"
	"time"

	"github.com/digitorus/pkcs7"

	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki/pkitest"
)

// The NuGet packages of the tests (R-SIG-042): an unsigned package built with archive/zip, signed by the test with digitorus/pkcs7 (an
// author signature of the company CA with a timestamp of the test's authority, and a repository countersignature of a second CA), the
// signature entry inserted first, in the middle or last.

// unsignedPackage builds a package of three entries.
func unsignedPackage(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, f := range []struct{ name, body string }{
		{"Example.Tools.nuspec", `<?xml version="1.0"?><package><metadata><id>Example.Tools</id><version>1.0.0</version></metadata></package>`},
		{"lib/net8.0/Example.Tools.dll", "MZ, a few bytes of a library"},
		{"[Content_Types].xml", `<?xml version="1.0"?><Types/>`},
	} {
		fw, err := w.Create(f.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// insertEntry inserts a stored entry before entry k of a package (k = the number of entries: after the last), moving the later entries'
// offsets, the central directory and the end record as a signing tool does.
func insertEntry(t *testing.T, pkg []byte, k int, name string, data []byte) []byte {
	t.Helper()
	le := binary.LittleEndian
	end := pkg[len(pkg)-endLen:]
	count, cdSize, cdOff := int(le.Uint16(end[10:])), int(le.Uint32(end[12:])), int(le.Uint32(end[16:]))
	records, err := centralRecords(pkg[cdOff:cdOff+cdSize], count)
	if err != nil {
		t.Fatal(err)
	}
	starts := make([]int, 0, count)
	for _, r := range records {
		starts = append(starts, int(r.local))
	}
	sort.Ints(starts)
	at := cdOff
	if k < count {
		at = starts[k]
	}
	crc := crc32.ChecksumIEEE(data)
	local := make([]byte, 30, 30+len(name)+len(data))
	le.PutUint32(local[0:], 0x04034b50)
	le.PutUint16(local[4:], 20)
	le.PutUint32(local[14:], crc)
	le.PutUint32(local[18:], uint32(len(data)))
	le.PutUint32(local[22:], uint32(len(data)))
	le.PutUint16(local[26:], uint16(len(name)))
	local = append(append(local, name...), data...)
	central := make([]byte, centralLen, centralLen+len(name))
	le.PutUint32(central[0:], sigCentral)
	le.PutUint16(central[4:], 20)
	le.PutUint16(central[6:], 20)
	le.PutUint32(central[16:], crc)
	le.PutUint32(central[20:], uint32(len(data)))
	le.PutUint32(central[24:], uint32(len(data)))
	le.PutUint16(central[28:], uint16(len(name)))
	le.PutUint32(central[42:], uint32(at))
	central = append(central, name...)
	var out bytes.Buffer
	out.Write(pkg[:at])
	out.Write(local)
	out.Write(pkg[at:cdOff])
	for i, r := range records {
		if i == k {
			out.Write(central)
		}
		rec := bytes.Clone(r.raw)
		if int(r.local) >= at {
			le.PutUint32(rec[42:], uint32(int(r.local)+len(local)))
		}
		out.Write(rec)
	}
	if k >= count {
		out.Write(central)
	}
	e := bytes.Clone(end)
	le.PutUint16(e[8:], uint16(count+1))
	le.PutUint16(e[10:], uint16(count+1))
	le.PutUint32(e[12:], uint32(cdSize+len(central)))
	le.PutUint32(e[16:], uint32(cdOff+len(local)))
	out.Write(e)
	return out.Bytes()
}

// content is NuGet's signature content for a package hash.
func content(sum []byte) []byte {
	return []byte("Version:1\r\n\r\n2.16.840.1.101.3.4.2.1-Hash:" + base64.StdEncoding.EncodeToString(sum) + "\r\n\r\n")
}

// signingCertV2 is the signing-certificate-v2 attribute's value for a certificate (SHA-256, the default algorithm).
func signingCertV2(t *testing.T, c *x509.Certificate) asn1.RawValue {
	t.Helper()
	sum := sha256.Sum256(c.Raw)
	b, err := asn1.Marshal(struct {
		Certs []struct{ CertHash []byte }
	}{Certs: []struct{ CertHash []byte }{{CertHash: sum[:]}}})
	if err != nil {
		t.Fatal(err)
	}
	return asn1.RawValue{FullBytes: b}
}

func commitmentOf(t *testing.T, oid asn1.ObjectIdentifier) asn1.RawValue {
	t.Helper()
	b, err := asn1.Marshal(struct{ ID asn1.ObjectIdentifier }{oid})
	if err != nil {
		t.Fatal(err)
	}
	return asn1.RawValue{FullBytes: b}
}

func ia5(t *testing.T, s string) asn1.RawValue {
	t.Helper()
	b, err := asn1.MarshalWithParams(s, "ia5")
	if err != nil {
		t.Fatal(err)
	}
	return asn1.RawValue{FullBytes: b}
}

// signer is who signs in the tests: a leaf and its chain to the root.
type signer struct {
	ca   *pkitest.CA
	leaf *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newSigner(t *testing.T, org string) signer {
	t.Helper()
	ca := pkitest.NewCA(t, org)
	leaf, key := ca.Issue(t, pkitest.Leaf{})
	return signer{ca: ca, leaf: leaf, key: key}
}

// signOptions say what the test's signature carries.
type signOptions struct {
	author      signer
	tsa         *pkitest.TSA
	counter     *signer
	counterOver []byte // the signature value the countersignature signs; nil: the primary's
}

// sign makes the .signature.p7s of an unsigned package: an author signature (proof of origin, signing-certificate-v2), with a timestamp
// over its signature value when tsa is set, and a repository countersignature when counter is set.
func sign(t *testing.T, unsigned []byte, o signOptions) []byte {
	t.Helper()
	sum := sha256.Sum256(unsigned)
	sd, err := pkcs7.NewSignedData(content(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	sd.SetDigestAlgorithm(pkcs7.OIDDigestAlgorithmSHA256)
	err = sd.AddSignerChain(o.author.leaf, o.author.key, []*x509.Certificate{o.author.ca.Inter, o.author.ca.Root}, pkcs7.SignerInfoConfig{
		ExtraSignedAttributes: []pkcs7.Attribute{
			{Type: oidCommitmentType(), Value: commitmentOf(t, oidProofOfOrigin())},
			{Type: oidSigningCertV2(), Value: signingCertV2(t, o.author.leaf)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	primary := &sd.GetSignedData().SignerInfos[0]
	var unsignedAttrs []pkcs7.Attribute
	if o.tsa != nil {
		tok := o.tsa.Token(t, crypto.SHA256, primary.EncryptedDigest, time.Now().Add(-time.Minute))
		unsignedAttrs = append(unsignedAttrs, pkcs7.Attribute{Type: oidTimestampToken(), Value: asn1.RawValue{FullBytes: tok}})
	}
	if o.counter != nil {
		over := o.counterOver
		if over == nil {
			over = primary.EncryptedDigest
		}
		unsignedAttrs = append(unsignedAttrs, pkcs7.Attribute{Type: oidCountersignature(), Value: countersign(t, *o.counter, over)})
		for _, c := range []*x509.Certificate{o.counter.leaf, o.counter.ca.Inter, o.counter.ca.Root} {
			sd.AddCertificate(c)
		}
	}
	if len(unsignedAttrs) > 0 {
		if err := primary.SetUnauthenticatedAttributes(unsignedAttrs); err != nil {
			t.Fatal(err)
		}
	}
	p7s, err := sd.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return p7s
}

// countersign makes a repository countersignature (proof of receipt, a service index, an owner) over a signature value.
func countersign(t *testing.T, s signer, over []byte) asn1.RawValue {
	t.Helper()
	sd, err := pkcs7.NewSignedData(over)
	if err != nil {
		t.Fatal(err)
	}
	sd.SetDigestAlgorithm(pkcs7.OIDDigestAlgorithmSHA256)
	err = sd.AddSignerChain(s.leaf, s.key, []*x509.Certificate{s.ca.Inter, s.ca.Root}, pkcs7.SignerInfoConfig{
		ExtraSignedAttributes: []pkcs7.Attribute{
			{Type: oidCommitmentType(), Value: commitmentOf(t, oidProofOfReceipt())},
			{Type: oidServiceIndex(), Value: ia5(t, "https://api.nuget.example.com/v3/index.json")},
			{Type: oidPackageOwners(), Value: []string{"example"}},
			{Type: oidSigningCertV2(), Value: signingCertV2(t, s.leaf)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	der, err := asn1.Marshal(sd.GetSignedData().SignerInfos[0])
	if err != nil {
		t.Fatal(err)
	}
	return asn1.RawValue{FullBytes: der}
}
