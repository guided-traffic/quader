package notation

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/notaryproject/notation-core-go/signature"
	"github.com/notaryproject/tspclient-go"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki/pkitest"
)

// The company CA of the tests (R-SIG-042): a root, an intermediate and a code-signing leaf made with crypto/x509; Notation envelopes
// signed with notation-core-go; a timestamp authority of the test's own.

func run(t *testing.T, in scheme.Input) scheme.Output {
	t.Helper()
	out, err := Check(context.Background(), in)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return out
}

func wantState(t *testing.T, out scheme.Output, state signatures.State, reason string) {
	t.Helper()
	if out.Result.State != state || out.Result.Reason != reason {
		t.Fatalf("state %s (%s), want %s (%s); signer %s", out.Result.State, out.Result.Reason, state, reason, out.Result.Signer)
	}
}

// image is a subject manifest of the tests.
var image = []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","name":"app"}`)

// timestamper is a tspclient.Timestamper of the test's authority that states at.
type timestamper struct {
	t   *testing.T
	tsa *pkitest.TSA
	at  time.Time
}

func (ts timestamper) Timestamp(_ context.Context, req *tspclient.Request) (*tspclient.Response, error) {
	der := ts.tsa.Response(ts.t, crypto.SHA256, req.MessageImprint.HashedMessage, ts.at, req.Nonce)
	resp := &tspclient.Response{}
	if err := resp.UnmarshalBinary(der); err != nil {
		return nil, err
	}
	return resp, nil
}

// envelopeOf signs a Notation payload naming digest and size with the chain and key, at signingTime, timestamped when tsa is set.
func envelopeOf(t *testing.T, mediaType string, chain []*x509.Certificate, key *ecdsa.PrivateKey, digest string, size int, signingTime time.Time, tsa *pkitest.TSA) []byte {
	t.Helper()
	signer, err := signature.NewLocalSigner(chain, key)
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Appendf(nil, `{"targetArtifact":{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":%q,"size":%d}}`, digest, size)
	req := &signature.SignRequest{
		Payload: signature.Payload{ContentType: payloadType, Content: payload}, Signer: signer, SigningTime: signingTime,
		SigningScheme: signature.SigningSchemeX509, SigningAgent: "notation-go/1.3.0",
	}
	if tsa != nil {
		req.Timestamper = timestamper{t: t, tsa: tsa, at: signingTime}
		req.TSARootCAs = x509.NewCertPool()
		req.TSARootCAs.AddCert(tsa.Root)
	}
	env, err := signature.NewEnvelope(mediaType)
	if err != nil {
		t.Fatal(err)
	}
	out, err := env.Sign(req)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func digestOf(b []byte) string {
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:])
}

// referrer stores the envelope as layer 0 of a Notation signature manifest and answers the input of its check.
func referrer(env []byte, mediaType, schemeName string) scheme.Input {
	blobs := pkitest.NewBlobs()
	layer := blobs.Put(env)
	m := fmt.Appendf(nil, `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json",`+
		`"artifactType":"application/vnd.cncf.notary.signature","config":{"mediaType":"application/vnd.oci.empty.v1+json",`+
		`"digest":"sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a","size":2},`+
		`"layers":[{"mediaType":%q,"digest":"sha256:%s","size":%d}],"subject":{"mediaType":"application/vnd.oci.image.manifest.v1+json",`+
		`"digest":%q,"size":%d}}`, mediaType, hex.EncodeToString(layer[:]), len(env), digestOf(image), len(image))
	return scheme.Input{
		Scheme: schemeName, Role: signatures.RolePublisher, ItemBlob: blobs.Put(m), Subject: sha256.Sum256(image), SubjectSize: int64(len(image)),
		Blobs: blobs, Limits: scheme.DefaultLimits(), ReceivedAt: time.Now(),
	}
}

func TestR_SIG_006_NotationEnvelopesOfTheCompanyCAAreValid(t *testing.T) {
	ca := pkitest.NewCA(t, "Example GmbH")
	leaf, key := ca.Issue(t, pkitest.Leaf{})
	tsa := pkitest.NewTSA(t)
	at := time.Now().Add(-time.Minute).Truncate(time.Second)
	for _, tc := range []struct {
		name, mediaType, scheme string
	}{
		{"JWS", mediaJWS, signatures.SchemeNotationJWS},
		{"COSE", mediaCOSE, signatures.SchemeNotationCOSE},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := envelopeOf(t, tc.mediaType, ca.Chain(leaf), key, digestOf(image), len(image), at, tsa)
			out := run(t, referrer(env, tc.mediaType, tc.scheme))
			wantState(t, out, signatures.StateValid, "")
			var s signatures.Signer
			if err := json.Unmarshal(out.Result.Signer, &s); err != nil {
				t.Fatal(err)
			}
			if s.SigningScheme != "notary.x509" || s.SigningAgent != "notation-go/1.3.0" || len(s.Chain) != 3 || !out.Result.ClaimedAt.Equal(at) {
				t.Fatalf("signer %+v, claimed %v", s, out.Result.ClaimedAt)
			}
			ev := out.Evidence
			if ev == nil || len(ev.Chain) != 3 || len(ev.Timestamps) != 1 || !ev.Timestamps[0].Time.Equal(at) || len(ev.Timestamps[0].Chain) != 2 {
				t.Fatalf("evidence %+v", ev)
			}
			if len(out.Result.KeyIDs) != 4 || out.Parts != 1 {
				t.Fatalf("key IDs %v, parts %d", out.Result.KeyIDs, out.Parts)
			}
		})
	}
}

func TestR_SIG_006_AFlippedByteOfTheSubjectTheSignatureOrThePayloadsDigestIsInvalid(t *testing.T) {
	ca := pkitest.NewCA(t, "Example GmbH")
	leaf, key := ca.Issue(t, pkitest.Leaf{})
	at := time.Now().Add(-time.Minute)
	good := envelopeOf(t, mediaJWS, ca.Chain(leaf), key, digestOf(image), len(image), at, nil)
	otherDigest := []byte(digestOf(image))
	otherDigest[len(otherDigest)-1] ^= 1
	flippedPayload := envelopeOf(t, mediaJWS, ca.Chain(leaf), key, string(otherDigest), len(image), at, nil)
	flippedSig := mutateJWS(t, good, func(e map[string]any) {
		sig, _ := base64.RawURLEncoding.DecodeString(e["signature"].(string))
		sig[5] ^= 1
		e["signature"] = base64.RawURLEncoding.EncodeToString(sig)
	})
	other := pkitest.NewCA(t, "Other AG")
	brokenChain := mutateJWS(t, good, func(e map[string]any) {
		e["header"].(map[string]any)["x5c"] = []any{leaf.Raw, other.Inter.Raw, other.Root.Raw}
	})
	content := referrer(good, mediaJWS, signatures.SchemeNotationJWS)
	content.Subject[0] ^= 1
	for _, tc := range []struct {
		name   string
		in     scheme.Input
		reason string
	}{
		{"subject", content, signatures.ReasonSubjectMismatch},
		{"signature", referrer(flippedSig, mediaJWS, signatures.SchemeNotationJWS), signatures.ReasonSignatureMismatch},
		{"payload's digest", referrer(flippedPayload, mediaJWS, signatures.SchemeNotationJWS), signatures.ReasonSubjectMismatch},
		{"chain", referrer(brokenChain, mediaJWS, signatures.SchemeNotationJWS), signatures.ReasonChainBroken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantState(t, run(t, tc.in), signatures.StateInvalid, tc.reason)
		})
	}
}

// mutateJWS changes a JWS envelope's JSON.
func mutateJWS(t *testing.T, env []byte, change func(map[string]any)) []byte {
	t.Helper()
	var e map[string]any
	if err := json.Unmarshal(env, &e); err != nil {
		t.Fatal(err)
	}
	change(e)
	out, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestR_SIG_007_ATimestampOverAnotherSignatureValueIsInvalid(t *testing.T) {
	ca := pkitest.NewCA(t, "Example GmbH")
	leaf, key := ca.Issue(t, pkitest.Leaf{})
	tsa := pkitest.NewTSA(t)
	env := envelopeOf(t, mediaJWS, ca.Chain(leaf), key, digestOf(image), len(image), time.Now().Add(-time.Minute), tsa)
	foreign := mutateJWS(t, env, func(e map[string]any) {
		e["header"].(map[string]any)["io.cncf.notary.timestampSignature"] = tsa.Token(t, crypto.SHA256, []byte("another signature"), time.Now())
	})
	wantState(t, run(t, referrer(foreign, mediaJWS, signatures.SchemeNotationJWS)), signatures.StateInvalid, signatures.ReasonTimestampInvalid)
}

func TestR_SIG_013_HostileEnvelopesAreRefusedWithinTheBounds(t *testing.T) {
	ca := pkitest.NewCA(t, "Example GmbH")
	leaf, key := ca.Issue(t, pkitest.Leaf{})
	good := envelopeOf(t, mediaJWS, ca.Chain(leaf), key, digestOf(image), len(image), time.Now().Add(-time.Minute), nil)
	nine := mutateJWS(t, good, func(e map[string]any) {
		var chain []any
		for range 9 {
			chain = append(chain, ca.Inter.Raw)
		}
		e["header"].(map[string]any)["x5c"] = chain
	})
	// A COSE_Sign1 (tag 18) array whose first member declares a byte string of 2 GiB.
	huge := []byte{0xd2, 0x84, 0x5a, 0x80, 0x00, 0x00, 0x00, 0xa0, 0xf6, 0x40}
	deep := make([]byte, 0, 200)
	for range 100 {
		deep = append(deep, 0x81) // arrays of one, nested a hundred deep
	}
	deep = append(deep, 0x00)
	for _, tc := range []struct {
		name string
		in   scheme.Input
	}{
		{"a chain of nine certificates", referrer(nine, mediaJWS, signatures.SchemeNotationJWS)},
		{"a COSE envelope with a 2 GiB declared length", scheme.Input{Scheme: signatures.SchemeNotationCOSE, Raw: huge, Limits: scheme.DefaultLimits()}},
		{"a COSE envelope nested a hundred deep", scheme.Input{Scheme: signatures.SchemeNotationCOSE, Raw: deep, Limits: scheme.DefaultLimits()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantState(t, run(t, tc.in), signatures.StateMalformed, signatures.ReasonLimitsExceeded)
		})
	}
}
