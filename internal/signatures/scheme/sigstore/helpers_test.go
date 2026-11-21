package sigstore

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"testing"
	"time"

	"github.com/secure-systems-lab/go-securesystemslib/dsse"
	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	protodsse "github.com/sigstore/protobuf-specs/gen/pb-go/dsse"
	protorekor "github.com/sigstore/protobuf-specs/gen/pb-go/rekor/v1"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"
	"github.com/sigstore/sigstore-go/pkg/tlog"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/guided-traffic/quader/internal/signatures/anchors"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
)

// The three ways of the owner's CI (R-SIG-042, Q-SIG-46), made in process: the company's Sigstore (a Fulcio of the test's own that issues
// a GitLab job's certificate, and the log and the timestamp authority of sigstore-go's VirtualSigstore), a second VirtualSigstore standing
// for the public one, the company CA (a root, an intermediate and a code-signing leaf of crypto/x509) for cosign's older layout, and an
// ECDSA key for a bundle signed with a key.

const (
	gitlabIdentity = "https://gitlab.example.com/platform/app//.gitlab-ci.yml@refs/heads/main"
	gitlabIssuer   = "https://gitlab.example.com"
	mediaBundleV03 = "application/vnd.dev.sigstore.bundle.v0.3+json"
	predicateSLSA1 = "https://slsa.dev/provenance/v1"
)

// fulcio is the OID arc of Fulcio's extensions.
func fulcio(n int) asn1.ObjectIdentifier { return asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, n} }

// gitlabExtensions are the Fulcio extensions of a GitLab job's certificate (oid-info.md: 1.8 and later are DER UTF8Strings).
func gitlabExtensions(t *testing.T) []pkix.Extension {
	t.Helper()
	values := map[int]string{
		8: gitlabIssuer, 9: "https://gitlab.example.com/platform/app//.gitlab-ci.yml@refs/heads/main",
		10: "0123456789abcdef0123456789abcdef01234567", 11: "gitlab-hosted", 12: "https://gitlab.example.com/platform/app",
		13: "89abcdef0123456789abcdef0123456789abcdef", 14: "refs/heads/main", 15: "4711", 16: "https://gitlab.example.com/platform",
		17: "42", 18: "https://gitlab.example.com/platform/app//.gitlab-ci.yml@refs/heads/main", 19: "fedcba9876543210fedcba9876543210fedcba98",
		20: "push", 21: "https://gitlab.example.com/platform/app/-/jobs/1234", 22: "private",
	}
	exts := []pkix.Extension{{Id: fulcio(1), Value: []byte(gitlabIssuer)}}
	for n := 8; n <= 22; n++ {
		v, err := asn1.MarshalWithParams(values[n], "utf8")
		if err != nil {
			t.Fatal(err)
		}
		exts = append(exts, pkix.Extension{Id: fulcio(n), Value: v})
	}
	return exts
}

// makeCert issues a certificate; a nil parent makes it self-signed.
func makeCert(t *testing.T, tmpl *x509.Certificate, parent *x509.Certificate, pub crypto.PublicKey, signer crypto.Signer) *x509.Certificate {
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

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// caTemplate is a CA certificate valid for ten years either side of now.
func caTemplate(cn string, serial int64) *x509.Certificate {
	return &x509.Certificate{
		SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: cn, Organization: []string{"Example GmbH"}},
		NotBefore: time.Now().AddDate(-10, 0, 0), NotAfter: time.Now().AddDate(10, 0, 0),
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true, IsCA: true,
	}
}

// company is the company's Sigstore.
type company struct {
	vs        *ca.VirtualSigstore
	root      *x509.Certificate
	inter     *x509.Certificate
	interKey  *ecdsa.PrivateKey
	logID     []byte
	logIDHex  string
	trustRoot []byte
}

func newCompany(t *testing.T) *company {
	t.Helper()
	vs, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatal(err)
	}
	rootKey, interKey := newKey(t), newKey(t)
	c := &company{vs: vs, interKey: interKey}
	c.root = makeCert(t, caTemplate("Example Fulcio Root", 1), nil, rootKey.Public(), rootKey)
	c.inter = makeCert(t, caTemplate("Example Fulcio Intermediate", 2), c.root, interKey.Public(), rootKey)
	c.logIDHex, err = vs.RekorLogID()
	if err != nil {
		t.Fatal(err)
	}
	c.logID, _ = hex.DecodeString(c.logIDHex)
	fca := &root.FulcioCertificateAuthority{
		Root: c.root, Intermediates: []*x509.Certificate{c.inter}, ValidityPeriodStart: time.Now().AddDate(-10, 0, 0),
		URI: "https://fulcio.gitlab.example.com",
	}
	c.trustRoot = trustRoot(t, []root.CertificateAuthority{fca}, vs, time.Now().AddDate(-10, 0, 0))
	return c
}

// trustRoot is a trusted_root.json of certificate authorities and the log and the timestamp authority of vs, the log valid from start.
func trustRoot(t *testing.T, cas []root.CertificateAuthority, vs *ca.VirtualSigstore, start time.Time) []byte {
	t.Helper()
	logs := map[string]*root.TransparencyLog{}
	for id, tl := range vs.RekorLogs() {
		raw, err := hex.DecodeString(id)
		if err != nil {
			t.Fatal(err)
		}
		logs[id] = &root.TransparencyLog{
			BaseURL: tl.BaseURL, ID: raw, ValidityPeriodStart: start, HashFunc: crypto.SHA256, PublicKey: tl.PublicKey,
			SignatureHashFunc: crypto.SHA256,
		}
	}
	tr, err := root.NewTrustedRoot(root.TrustedRootMediaType01, cas, map[string]*root.TransparencyLog{}, vs.TimestampingAuthorities(), logs)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := tr.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// publicRoot is the trusted root of a VirtualSigstore standing for the public Sigstore.
func publicRoot(t *testing.T, vs *ca.VirtualSigstore) []byte {
	t.Helper()
	return trustRoot(t, vs.FulcioCertificateAuthorities(), vs, time.Now().AddDate(-10, 0, 0))
}

// leaf issues a ten-minute certificate of the GitLab job from notBefore.
func (c *company) leaf(t *testing.T, notBefore time.Time) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	k := newKey(t)
	u, _ := url.Parse(gitlabIdentity)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), NotBefore: notBefore, NotAfter: notBefore.Add(10 * time.Minute),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		URIs: []*url.URL{u}, ExtraExtensions: gitlabExtensions(t),
	}
	return makeCert(t, tmpl, c.inter, k.Public(), c.interKey), k
}

// signedEntry makes a log entry of the company's log with its signed entry timestamp.
func (c *company) signedEntry(t *testing.T, body []byte, at time.Time, index int64) *protorekor.TransparencyLogEntry {
	t.Helper()
	return logEntry(t, c.vs, c.logID, body, at, index)
}

func logEntry(t *testing.T, vs *ca.VirtualSigstore, logID, body []byte, at time.Time, index int64) *protorekor.TransparencyLogEntry {
	t.Helper()
	set, err := vs.RekorSignPayload(tlog.RekorPayload{
		Body: base64.StdEncoding.EncodeToString(body), IntegratedTime: at.Unix(), LogIndex: index, LogID: hex.EncodeToString(logID),
	})
	if err != nil {
		t.Fatal(err)
	}
	var kv struct {
		Kind       string `json:"kind"`
		APIVersion string `json:"apiVersion"`
	}
	if err := json.Unmarshal(body, &kv); err != nil {
		t.Fatal(err)
	}
	return &protorekor.TransparencyLogEntry{
		LogIndex: index, LogId: &protocommon.LogId{KeyId: logID}, IntegratedTime: at.Unix(),
		KindVersion: &protorekor.KindVersion{Kind: kv.Kind, Version: kv.APIVersion}, CanonicalizedBody: body,
		InclusionPromise: &protorekor.InclusionPromise{SignedEntryTimestamp: set},
	}
}

// hashedRekord is a hashedrekord 0.0.1 body: the artifact's SHA-256, the signature and the PEM of the certificate or key.
func hashedRekord(digest, sig, pemBytes []byte) []byte {
	return fmt.Appendf(nil, `{"apiVersion":"0.0.1","kind":"hashedrekord","spec":{"data":{"hash":{"algorithm":"sha256","value":"%s"}},`+
		`"signature":{"content":"%s","publicKey":{"content":"%s"}}}}`, hex.EncodeToString(digest), base64.StdEncoding.EncodeToString(sig),
		base64.StdEncoding.EncodeToString(pemBytes))
}

func certPEM(c *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
}

func keyPEM(t *testing.T, pub crypto.PublicKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

func sign(t *testing.T, k *ecdsa.PrivateKey, digest []byte) []byte {
	t.Helper()
	sig, err := ecdsa.SignASN1(rand.Reader, k, digest)
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

// messageBundle is a bundle of version 0.1 (an inclusion promise without a proof) with a message signature over artifact by a GitLab job's
// certificate issued at, logged a minute later, with a timestamp of the company's authority when stamp is set.
func (c *company) messageBundle(t *testing.T, artifact []byte, at time.Time, stamp bool) *protobundle.Bundle {
	t.Helper()
	leaf, k := c.leaf(t, at)
	digest := sha256Of(artifact)
	sig := sign(t, k, digest)
	vm := &protobundle.VerificationMaterial{
		Content: &protobundle.VerificationMaterial_X509CertificateChain{X509CertificateChain: &protocommon.X509CertificateChain{
			Certificates: []*protocommon.X509Certificate{{RawBytes: leaf.Raw}},
		}},
		TlogEntries: []*protorekor.TransparencyLogEntry{c.signedEntry(t, hashedRekord(digest, sig, certPEM(leaf)), at.Add(time.Minute), 1000)},
	}
	if stamp {
		tsr, err := c.vs.TimestampResponse(sig)
		if err != nil {
			t.Fatal(err)
		}
		vm.TimestampVerificationData = &protobundle.TimestampVerificationData{Rfc3161Timestamps: []*protocommon.RFC3161SignedTimestamp{{SignedTimestamp: tsr}}}
	}
	return &protobundle.Bundle{MediaType: mediaBundleV01, VerificationMaterial: vm, Content: &protobundle.Bundle_MessageSignature{
		MessageSignature: &protocommon.MessageSignature{
			MessageDigest: &protocommon.HashOutput{Algorithm: protocommon.HashAlgorithm_SHA2_256, Digest: digest}, Signature: sig,
		},
	}}
}

// statementOf is an in-toto statement of one subject.
func statementOf(predicateType, name, alg string, digest []byte) []byte {
	return fmt.Appendf(nil, `{"_type":"https://in-toto.io/Statement/v1","subject":[{"name":%q,"digest":{%q:%q}}],"predicateType":%q,`+
		`"predicate":{"buildDefinition":{"buildType":"https://gitlab.example.com/build/v1"}}}`, name, alg, hex.EncodeToString(digest), predicateType)
}

// dsseBundle is a bundle of version 0.3 with a DSSE envelope of stmt signed by a GitLab job's certificate, logged with an inclusion
// promise and proof by the company's log.
func (c *company) dsseBundle(t *testing.T, stmt []byte, at time.Time) *protobundle.Bundle {
	t.Helper()
	leaf, k := c.leaf(t, at)
	sig, tle := dsseSigned(t, c.vs, c.logID, leaf, k, stmt, at.Add(time.Minute))
	return &protobundle.Bundle{
		MediaType: mediaBundleV03,
		VerificationMaterial: &protobundle.VerificationMaterial{
			Content:     &protobundle.VerificationMaterial_Certificate{Certificate: &protocommon.X509Certificate{RawBytes: leaf.Raw}},
			TlogEntries: []*protorekor.TransparencyLogEntry{tle},
		},
		Content: &protobundle.Bundle_DsseEnvelope{DsseEnvelope: &protodsse.Envelope{
			Payload: stmt, PayloadType: payloadInToto, Signatures: []*protodsse.Signature{{Sig: sig}},
		}},
	}
}

// dsseSigned signs stmt as a DSSE envelope with k and logs it in vs's log (a dsse 0.0.1 entry with promise and proof).
func dsseSigned(t *testing.T, vs *ca.VirtualSigstore, logID []byte, leaf *x509.Certificate, k *ecdsa.PrivateKey, stmt []byte, at time.Time) ([]byte, *protorekor.TransparencyLogEntry) {
	t.Helper()
	pae := dsse.PAE(payloadInToto, stmt)
	sig := sign(t, k, sha256Of(pae))
	env := &dsse.Envelope{PayloadType: payloadInToto, Payload: base64.StdEncoding.EncodeToString(stmt), Signatures: []dsse.Signature{
		{Sig: base64.StdEncoding.EncodeToString(sig)},
	}}
	entry, err := vs.GenerateTlogEntry(leaf, env, sig, at.Unix(), true)
	if err != nil {
		t.Fatal(err)
	}
	tle := entry.TransparencyLogEntry()
	promise := logEntry(t, vs, logID, tle.CanonicalizedBody, at, tle.LogIndex)
	tle.KindVersion, tle.InclusionPromise = promise.KindVersion, promise.InclusionPromise
	return sig, tle
}

// bundleJSON encodes a bundle.
func bundleJSON(t *testing.T, b *protobundle.Bundle) []byte {
	t.Helper()
	out, err := protojson.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// companyCA is the company's own PKI: a root, an intermediate and a code-signing leaf (cosign with its own PKI).
type companyCA struct {
	root, inter, leaf *x509.Certificate
	leafKey           *ecdsa.PrivateKey
}

func newCompanyCA(t *testing.T) *companyCA {
	t.Helper()
	rootKey, interKey, leafKey := newKey(t), newKey(t), newKey(t)
	c := &companyCA{leafKey: leafKey}
	c.root = makeCert(t, caTemplate("Example Root CA", 10), nil, rootKey.Public(), rootKey)
	c.inter = makeCert(t, caTemplate("Example Signing CA", 11), c.root, interKey.Public(), rootKey)
	c.leaf = makeCert(t, &x509.Certificate{
		SerialNumber: big.NewInt(12), Subject: pkix.Name{CommonName: "Release signing", Organization: []string{"Example GmbH"}},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
	}, c.inter, leafKey.Public(), interKey)
	return c
}

// rsaLeaf is a self-signed code-signing certificate of an RSA key of bits.
func rsaLeaf(t *testing.T, bits int) *x509.Certificate {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	return makeCert(t, &x509.Certificate{
		SerialNumber: big.NewInt(13), Subject: pkix.Name{CommonName: "Weak signing"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
	}, nil, k.Public(), k)
}

// memBlobs is the object store of a test.
type memBlobs struct {
	m   map[[32]byte][]byte
	err error
}

func newBlobs() *memBlobs { return &memBlobs{m: map[[32]byte][]byte{}} }

func (b *memBlobs) put(data []byte) [32]byte {
	d := sha256.Sum256(data)
	b.m[d] = data
	return d
}

func (b *memBlobs) Content(_ context.Context, d [32]byte) (scheme.Content, error) {
	if b.err != nil {
		return nil, b.err
	}
	data, ok := b.m[d]
	if !ok {
		return nil, scheme.ErrBlobMissing
	}
	return memContent(data), nil
}

type memContent []byte

func (c memContent) Size() int64 { return int64(len(c)) }

func (c memContent) Bytes(limit int64) ([]byte, error) {
	if int64(len(c)) > limit {
		return nil, fmt.Errorf("the blob is larger than %d bytes", limit)
	}
	return []byte(c), nil
}

func (c memContent) Reader() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(c)), nil }

func (c memContent) ReaderAt() (io.ReaderAt, error) { return bytes.NewReader(c), nil }

// layer is an OCI layer descriptor as the manifests of the tests write it.
type layer struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int               `json:"size"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ociManifest stores the layers and a manifest that names them, and answers the manifest's digest.
func ociManifest(t *testing.T, blobs *memBlobs, artifactType string, subject [32]byte, layers [][]byte, kinds []string, ann []map[string]string) [32]byte {
	t.Helper()
	m := map[string]any{
		"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{"mediaType": "application/vnd.oci.empty.v1+json", "digest": "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a", "size": 2},
	}
	if artifactType != "" {
		m["artifactType"] = artifactType
		m["subject"] = map[string]any{"mediaType": "application/vnd.oci.image.manifest.v1+json", "digest": "sha256:" + hex.EncodeToString(subject[:]), "size": 1}
	}
	var ls []layer
	for i, data := range layers {
		d := blobs.put(data)
		l := layer{MediaType: kinds[i], Digest: "sha256:" + hex.EncodeToString(d[:]), Size: len(data)}
		if ann != nil {
			l.Annotations = ann[i]
		}
		ls = append(ls, l)
	}
	m["layers"] = ls
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return blobs.put(raw)
}

// simpleSigningPayload is cosign's simple-signing payload for an image digest.
func simpleSigningPayload(subject [32]byte) []byte {
	return fmt.Appendf(nil, `{"critical":{"identity":{"docker-reference":"registry.example.com/platform/app"},`+
		`"image":{"docker-manifest-digest":"sha256:%s"},"type":"cosign container image signature"},"optional":{"creator":"pipeline"}}`,
		hex.EncodeToString(subject[:]))
}

// publicKeyOf is a raw key as the anchors hand it.
func publicKeyOf(pub crypto.PublicKey) anchors.ParsedKey {
	return anchors.ParsedKey{Key: pub, SHA256: keyID(pub)}
}

// image is a subject manifest's digest of the tests.
func image(name string) [32]byte {
	return sha256.Sum256([]byte(`{"schemaVersion":2,"name":"` + name + `"}`))
}

// publicBundle is the bundle of an attestation of the public stand-in: its certificate, envelope, log entry (with the signed entry
// timestamp the VirtualSigstore keeps to itself made again) and timestamp, as version 0.3.
func publicBundle(t *testing.T, f *fixture, e *ca.TestEntity) []byte {
	t.Helper()
	vc, _ := e.VerificationContent()
	sc, _ := e.SignatureContent()
	env := sc.EnvelopeContent().RawEnvelope()
	payload, _ := base64.StdEncoding.DecodeString(env.Payload)
	sig, _ := base64.StdEncoding.DecodeString(env.Signatures[0].Sig)
	entries, _ := e.TlogEntries()
	tle := entries[0].TransparencyLogEntry()
	logIDHex, _ := f.pub.RekorLogID()
	logID, _ := hex.DecodeString(logIDHex)
	promise := logEntry(t, f.pub, logID, tle.CanonicalizedBody, time.Unix(tle.IntegratedTime, 0), tle.LogIndex)
	tle.KindVersion, tle.InclusionPromise = promise.KindVersion, promise.InclusionPromise
	stamps, _ := e.Timestamps()
	vm := &protobundle.VerificationMaterial{
		Content:     &protobundle.VerificationMaterial_Certificate{Certificate: &protocommon.X509Certificate{RawBytes: vc.Certificate().Raw}},
		TlogEntries: []*protorekor.TransparencyLogEntry{tle},
	}
	for _, ts := range stamps {
		if vm.TimestampVerificationData == nil {
			vm.TimestampVerificationData = &protobundle.TimestampVerificationData{}
		}
		vm.TimestampVerificationData.Rfc3161Timestamps = append(vm.TimestampVerificationData.Rfc3161Timestamps, &protocommon.RFC3161SignedTimestamp{SignedTimestamp: ts})
	}
	return bundleJSON(t, &protobundle.Bundle{MediaType: mediaBundleV03, VerificationMaterial: vm, Content: &protobundle.Bundle_DsseEnvelope{
		DsseEnvelope: &protodsse.Envelope{Payload: payload, PayloadType: env.PayloadType, Signatures: []*protodsse.Signature{{Sig: sig}}},
	}})
}
