package trust

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/url"
	"testing"
	"time"

	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	protorekor "github.com/sigstore/protobuf-specs/gen/pb-go/rekor/v1"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"
	"github.com/sigstore/sigstore-go/pkg/tlog"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki/pkitest"
)

// The company's Sigstore of the trust tests (R-SIG-042, Q-SIG-46), the minimal part of internal/signatures/scheme/sigstore's helpers: a
// Fulcio of the test's own that issues a GitLab job's certificate, and the log of sigstore-go's VirtualSigstore.

const (
	gitlabIdentity = "https://gitlab.example.com/platform/app//.gitlab-ci.yml@refs/heads/main"
	gitlabIssuer   = "https://gitlab.example.com"
	mediaBundleV01 = "application/vnd.dev.sigstore.bundle+json;version=0.1"
)

// company is the company's Sigstore.
type company struct {
	vs        *ca.VirtualSigstore
	inter     *x509.Certificate
	interKey  *ecdsa.PrivateKey
	logID     []byte
	trustRoot []byte
}

func sigstoreCATemplate(cn string) *x509.Certificate {
	return &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn, Organization: []string{"Example GmbH"}},
		NotBefore: time.Now().AddDate(-10, 0, 0), NotAfter: time.Now().AddDate(10, 0, 0),
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true, IsCA: true,
	}
}

func newCompany(t *testing.T) *company {
	t.Helper()
	vs, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatal(err)
	}
	rootKey, interKey := pkitest.Key(t), pkitest.Key(t)
	c := &company{vs: vs, interKey: interKey}
	rootCert := pkitest.Cert(t, sigstoreCATemplate("Example Fulcio Root"), nil, rootKey.Public(), rootKey)
	c.inter = pkitest.Cert(t, sigstoreCATemplate("Example Fulcio Intermediate"), rootCert, interKey.Public(), rootKey)
	logIDHex, err := vs.RekorLogID()
	if err != nil {
		t.Fatal(err)
	}
	c.logID, _ = hex.DecodeString(logIDHex)
	start := time.Now().AddDate(-10, 0, 0)
	fca := &root.FulcioCertificateAuthority{
		Root: rootCert, Intermediates: []*x509.Certificate{c.inter}, ValidityPeriodStart: start, URI: "https://fulcio.gitlab.example.com",
	}
	logs := map[string]*root.TransparencyLog{}
	for id, tl := range vs.RekorLogs() {
		raw, _ := hex.DecodeString(id)
		logs[id] = &root.TransparencyLog{
			BaseURL: tl.BaseURL, ID: raw, ValidityPeriodStart: start, HashFunc: crypto.SHA256, PublicKey: tl.PublicKey, SignatureHashFunc: crypto.SHA256,
		}
	}
	tr, err := root.NewTrustedRoot(root.TrustedRootMediaType01, []root.CertificateAuthority{fca}, map[string]*root.TransparencyLog{},
		vs.TimestampingAuthorities(), logs)
	if err != nil {
		t.Fatal(err)
	}
	if c.trustRoot, err = tr.MarshalJSON(); err != nil {
		t.Fatal(err)
	}
	return c
}

// leaf issues a ten-minute certificate of the GitLab job from notBefore, with Fulcio's issuer extension.
func (c *company) leaf(t *testing.T, notBefore time.Time) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	k := pkitest.Key(t)
	u, _ := url.Parse(gitlabIdentity)
	issuer, err := asn1.MarshalWithParams(gitlabIssuer, "utf8")
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), NotBefore: notBefore, NotAfter: notBefore.Add(10 * time.Minute),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning}, URIs: []*url.URL{u},
		ExtraExtensions: []pkix.Extension{
			{Id: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 1}, Value: []byte(gitlabIssuer)},
			{Id: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 8}, Value: issuer},
		},
	}
	return pkitest.Cert(t, tmpl, c.inter, k.Public(), c.interKey), k
}

// messageBundle is a bundle of version 0.1 with a message signature over artifact by a GitLab job's certificate issued at, logged a minute
// later by the company's log.
func (c *company) messageBundle(t *testing.T, artifact []byte, at time.Time) []byte {
	t.Helper()
	leaf, k := c.leaf(t, at)
	digest := sha256.Sum256(artifact)
	sig, err := ecdsa.SignASN1(rand.Reader, k, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	pemLeaf := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw})
	body := fmt.Appendf(nil, `{"apiVersion":"0.0.1","kind":"hashedrekord","spec":{"data":{"hash":{"algorithm":"sha256","value":"%s"}},`+
		`"signature":{"content":"%s","publicKey":{"content":"%s"}}}}`, hex.EncodeToString(digest[:]), base64.StdEncoding.EncodeToString(sig),
		base64.StdEncoding.EncodeToString(pemLeaf))
	logged := at.Add(time.Minute)
	set, err := c.vs.RekorSignPayload(tlog.RekorPayload{
		Body: base64.StdEncoding.EncodeToString(body), IntegratedTime: logged.Unix(), LogIndex: 1000, LogID: hex.EncodeToString(c.logID),
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
	b := &protobundle.Bundle{
		MediaType: mediaBundleV01,
		VerificationMaterial: &protobundle.VerificationMaterial{
			Content: &protobundle.VerificationMaterial_X509CertificateChain{X509CertificateChain: &protocommon.X509CertificateChain{
				Certificates: []*protocommon.X509Certificate{{RawBytes: leaf.Raw}},
			}},
			TlogEntries: []*protorekor.TransparencyLogEntry{{
				LogIndex: 1000, LogId: &protocommon.LogId{KeyId: c.logID}, IntegratedTime: logged.Unix(),
				KindVersion: &protorekor.KindVersion{Kind: kv.Kind, Version: kv.APIVersion}, CanonicalizedBody: body,
				InclusionPromise: &protorekor.InclusionPromise{SignedEntryTimestamp: set},
			}},
		},
		Content: &protobundle.Bundle_MessageSignature{MessageSignature: &protocommon.MessageSignature{
			MessageDigest: &protocommon.HashOutput{Algorithm: protocommon.HashAlgorithm_SHA2_256, Digest: digest[:]}, Signature: sig,
		}},
	}
	out, err := protojson.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
