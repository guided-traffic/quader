package trust

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"testing"
	"time"

	notary "github.com/notaryproject/notation-core-go/signature"
	_ "github.com/notaryproject/notation-core-go/signature/jws" // registers the JWS envelope
	"github.com/notaryproject/tspclient-go"

	qopenpgp "github.com/guided-traffic/quader/internal/openpgp"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/scheme/notation"
	"github.com/guided-traffic/quader/internal/signatures/scheme/openpgp"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki"
	"github.com/guided-traffic/quader/internal/signatures/scheme/rawkey/pki/pkitest"
	"github.com/guided-traffic/quader/internal/signatures/scheme/sigstore"
)

// The trust tests judge what the verifiers stored for real signatures made in process (R-SIG-042): Notation envelopes of the company CA,
// OpenPGP signatures of the release key, npm registry signatures, and bundles of the company's Sigstore.

// signature turns a verifier's Output into the stored fields the trust step judges, received at receivedAt.
func signature(t *testing.T, out scheme.Output, receivedAt time.Time) Signature {
	t.Helper()
	if out.Result.State != signatures.StateValid && out.Result.State != signatures.StateNoKey {
		t.Fatalf("the verifier answered %s (%s): %s", out.Result.State, out.Result.Reason, out.Result.Signer)
	}
	return Signature{
		State: out.Result.State, KeyIDs: out.Result.KeyIDs, SignedDigest: out.Result.SignedDigest, DigestAlg: out.Result.DigestAlg,
		Evidence: out.Evidence, ReceivedAt: receivedAt,
	}
}

func caAnchor(id int64, scope, use string, subject map[string]string, certs ...*x509.Certificate) anchors.Anchor {
	return anchors.Anchor{ID: id, Scope: scope, Kind: anchors.KindCA, Enabled: true, Parsed: &anchors.Parsed{
		CA: &anchors.ParsedCA{Certificates: certs, Use: use, Subject: subject},
	}}
}

func openPGPAnchor(id int64, scope string, certs ...anchors.OpenPGPCertificate) anchors.Anchor {
	return anchors.Anchor{ID: id, Scope: scope, Kind: anchors.KindOpenPGP, Enabled: true, Parsed: &anchors.Parsed{OpenPGP: certs}}
}

func wantVerdict(t *testing.T, v Verdict, state string, ids ...int64) {
	t.Helper()
	if v.State != state || !slices.Equal(v.AnchorIDs, ids) {
		t.Fatalf("verdict %s by %v, want %s by %v (%+v)", v.State, v.AnchorIDs, state, ids, v)
	}
}

// image is a subject manifest of the tests.
var image = []byte(`{"schemaVersion":2,"name":"app"}`)

// timestamper is a tspclient.Timestamper of the test's authority that states at.
type timestamper struct {
	t   *testing.T
	tsa *pkitest.TSA
	at  time.Time
}

func (ts timestamper) Timestamp(_ context.Context, req *tspclient.Request) (*tspclient.Response, error) {
	resp := &tspclient.Response{}
	return resp, resp.UnmarshalBinary(ts.tsa.Response(ts.t, crypto.SHA256, req.MessageImprint.HashedMessage, ts.at, req.Nonce))
}

// notationEvidence signs image with a Notation JWS envelope at signingTime (timestamped when tsa is set) and answers what the verifier
// stored for it.
func notationEvidence(t *testing.T, chain []*x509.Certificate, key *ecdsa.PrivateKey, signingTime time.Time, tsa *pkitest.TSA) scheme.Output {
	t.Helper()
	signer, err := notary.NewLocalSigner(chain, key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(image)
	payload := fmt.Appendf(nil, `{"targetArtifact":{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:%s","size":%d}}`,
		hex.EncodeToString(sum[:]), len(image))
	req := &notary.SignRequest{
		Payload: notary.Payload{ContentType: "application/vnd.cncf.notary.payload.v1+json", Content: payload}, Signer: signer,
		SigningTime: signingTime, SigningScheme: notary.SigningSchemeX509,
	}
	if tsa != nil {
		req.Timestamper, req.TSARootCAs = timestamper{t: t, tsa: tsa, at: signingTime}, x509.NewCertPool()
		req.TSARootCAs.AddCert(tsa.Root)
	}
	env, err := notary.NewEnvelope("application/jose+json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := env.Sign(req)
	if err != nil {
		t.Fatal(err)
	}
	out, err := notation.Check(context.Background(), scheme.Input{
		Scheme: signatures.SchemeNotationJWS, Role: signatures.RolePublisher, Raw: raw, Subject: sum, SubjectSize: int64(len(image)),
		Limits: scheme.DefaultLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestR_SIG_016_TheScopesOfTheAnchorsThatKnowASigner(t *testing.T) {
	ca := pkitest.NewCA(t, "Example GmbH")
	leaf, key := ca.Issue(t, pkitest.Leaf{})
	sig := signature(t, notationEvidence(t, ca.Chain(leaf), key, time.Now().Add(-time.Minute), nil), time.Now())
	other := pkitest.NewCA(t, "Other AG")
	anchor := func(id int64, scope string) anchors.Anchor {
		return caAnchor(id, scope, anchors.UseCodeSigning, nil, ca.Root)
	}
	for _, tc := range []struct {
		name    string
		anchors []anchors.Anchor
		state   string
		ids     []int64
	}{
		{"no anchor", nil, StateUntrusted, nil},
		{"an anchor of another CA", []anchors.Anchor{caAnchor(1, anchors.ScopeInstallation, anchors.UseCodeSigning, nil, other.Root)}, StateUntrusted, nil},
		{"shipped", []anchors.Anchor{anchor(1, anchors.ScopeShipped)}, StatePublic, []int64{1}},
		{"configuration", []anchors.Anchor{anchor(2, anchors.ScopeConfiguration)}, StateInternal, []int64{2}},
		{"installation", []anchors.Anchor{anchor(3, anchors.ScopeInstallation)}, StateInternal, []int64{3}},
		{"project", []anchors.Anchor{anchor(4, anchors.ScopeProject)}, StateInternal, []int64{4}},
		{"shipped and project: internal wins, both listed in ID order", []anchors.Anchor{anchor(9, anchors.ScopeProject), anchor(5, anchors.ScopeShipped)}, StateInternal, []int64{5, 9}},
		{"an anchor whose material no longer parses is skipped", []anchors.Anchor{{ID: 6, Scope: anchors.ScopeInstallation, Kind: anchors.KindCA}}, StateUntrusted, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantVerdict(t, Judge(sig, &Scope{Anchors: tc.anchors}, nil), tc.state, tc.ids...)
		})
	}
}

func TestR_SIG_008_ACAAnchorKnowsCodeSigningLeavesOfItsChainAndItsSubject(t *testing.T) {
	ca := pkitest.NewCA(t, "Example GmbH")
	restricted := caAnchor(1, anchors.ScopeConfiguration, anchors.UseCodeSigning, map[string]string{"O": "Example GmbH"}, ca.Root)
	sc := &Scope{Anchors: []anchors.Anchor{restricted}}
	judge := func(l pkitest.Leaf) Verdict {
		leaf, key := ca.Issue(t, l)
		return Judge(signature(t, notationEvidence(t, ca.Chain(leaf), key, time.Now().Add(-time.Minute), nil), time.Now()), sc, nil)
	}
	wantVerdict(t, judge(pkitest.Leaf{}), StateInternal, 1)
	wantVerdict(t, judge(pkitest.Leaf{Org: "Other AG"}), StateUntrusted)
	wantVerdict(t, judge(pkitest.Leaf{NoEKU: true}), StateUntrusted)
	timestamping := &Scope{Anchors: []anchors.Anchor{caAnchor(2, anchors.ScopeInstallation, anchors.UseTimestamping, nil, ca.Root)}}
	leaf, key := ca.Issue(t, pkitest.Leaf{})
	sig := signature(t, notationEvidence(t, ca.Chain(leaf), key, time.Now().Add(-time.Minute), nil), time.Now())
	wantVerdict(t, Judge(sig, timestamping, nil), StateUntrusted)
	pinned := &Scope{Anchors: []anchors.Anchor{caAnchor(3, anchors.ScopeShipped, anchors.UseCodeSigning, nil, leaf)}}
	wantVerdict(t, Judge(sig, pinned, nil), StatePublic, 3)
}

func TestR_SIG_007_ATimestampUnderATimestampingAnchorProvesTheTimeAfterTheCertificateExpired(t *testing.T) {
	ca := pkitest.NewCA(t, "Example GmbH")
	leaf, key := ca.Issue(t, pkitest.Leaf{NotBefore: time.Now().AddDate(-2, 0, 0), NotAfter: time.Now().AddDate(-1, 0, 0)})
	tsa := pkitest.NewTSA(t)
	at := time.Now().AddDate(0, -18, 0).Truncate(time.Second)
	sig := signature(t, notationEvidence(t, ca.Chain(leaf), key, at, tsa), time.Now())
	code := caAnchor(1, anchors.ScopeInstallation, anchors.UseCodeSigning, nil, ca.Root)
	stamps := caAnchor(2, anchors.ScopeInstallation, anchors.UseTimestamping, nil, tsa.Root)
	v := Judge(sig, &Scope{Anchors: []anchors.Anchor{code, stamps}}, nil)
	wantVerdict(t, v, StateInternal, 1)
	if !v.ProvenAt.Equal(at) || v.TimeProof != ProofTimestamp {
		t.Fatalf("proven %v by %q, want the stamped time %v", v.ProvenAt, v.TimeProof, at)
	}
	v = Judge(sig, &Scope{Anchors: []anchors.Anchor{code}}, nil)
	wantVerdict(t, v, StateUntrusted)
	if !v.ProvenAt.IsZero() {
		t.Fatalf("a timestamp of an authority no anchor knows proved %v", v.ProvenAt)
	}
}

func TestR_SIG_008_AnOpenPGPSignatureOfTheReleaseKeyIsInternalUnderItsAnchor(t *testing.T) {
	k, cert := pkitest.ReleaseKey(t, "Example Release", time.Now().Add(-time.Hour))
	_, other := pkitest.ReleaseKey(t, "Other Release", time.Now().Add(-time.Hour))
	release := []byte("Origin: Example\nSuite: stable\n")
	sig := detachedEvidence(t, k, release)
	if sig.State != signatures.StateNoKey {
		t.Fatalf("state %s", sig.State)
	}
	v := Judge(sig, &Scope{Anchors: []anchors.Anchor{openPGPAnchor(4, anchors.ScopeInstallation, cert)}}, nil)
	wantVerdict(t, v, StateInternal, 4)
	if v.Integrity != signatures.StateValid {
		t.Fatalf("integrity %q, want valid by the anchor's key", v.Integrity)
	}
	v = Judge(sig, &Scope{Anchors: []anchors.Anchor{openPGPAnchor(5, anchors.ScopeInstallation, other)}}, nil)
	wantVerdict(t, v, StateUntrusted)
	if v.Integrity != "" {
		t.Fatalf("a key the issuer does not name decided the integrity: %q", v.Integrity)
	}
	tampered := sig
	tampered.SignedDigest = append([]byte(nil), sig.SignedDigest...)
	tampered.SignedDigest[3] ^= 1
	v = Judge(tampered, &Scope{Anchors: []anchors.Anchor{openPGPAnchor(4, anchors.ScopeInstallation, cert)}}, nil)
	if v.Integrity != signatures.StateInvalid || v.Reason != signatures.ReasonSignatureMismatch || len(v.AnchorIDs) != 0 {
		t.Fatalf("a named key that does not verify: %+v", v)
	}
}

// detachedEvidence signs data detached with the release key and answers what the verifier stored without any key.
func detachedEvidence(t *testing.T, k *qopenpgp.Key, data []byte) Signature {
	t.Helper()
	sig, err := qopenpgp.DetachSign(data, []*qopenpgp.Key{k}, time.Now().Add(-30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return openPGPEvidence(t, sig, data, time.Now())
}

func openPGPEvidence(t *testing.T, sig, data []byte, receivedAt time.Time) Signature {
	t.Helper()
	blobs := pkitest.NewBlobs()
	out, err := openpgp.Check(context.Background(), scheme.Input{
		Scheme: signatures.SchemeOpenPGP, Role: signatures.RolePublisher, ItemBlob: blobs.Put(sig), Subject: blobs.Put(data), Blobs: blobs,
		Limits: scheme.DefaultLimits(), ReceivedAt: receivedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	return signature(t, out, receivedAt)
}

func TestR_SIG_007_AnOpenPGPSignatureReceivedAfterItsKeyExpiredIsUntrusted(t *testing.T) {
	created := time.Now().Add(-48 * time.Hour)
	e, cert := pkitest.ExpiringKey(t, "Expiring Release", created, 24*time.Hour)
	data := []byte("tool.tar.gz")
	armored := pkitest.DetachSign(t, e, data, created.Add(12*time.Hour))
	sc := &Scope{Anchors: []anchors.Anchor{openPGPAnchor(1, anchors.ScopeInstallation, cert)}}
	v := Judge(openPGPEvidence(t, armored, data, created.Add(13*time.Hour)), sc, nil)
	wantVerdict(t, v, StateInternal, 1)
	v = Judge(openPGPEvidence(t, armored, data, time.Now()), sc, nil)
	wantVerdict(t, v, StateUntrusted)
	if v.Integrity != signatures.StateValid {
		t.Fatalf("integrity %q: the key still verifies the bytes after it expired", v.Integrity)
	}
}

func TestR_SIG_008_AnNPMRegistrySignatureIsKnownByAPublicKeyAnchorUntilTheKeyExpires(t *testing.T) {
	k := pkitest.Key(t)
	const keyID = "SHA256:jl3bwswu80PjjokCgh0o2w5c2U4LhQAE57gj9cz1kzA"
	message := sha256.Sum256([]byte("left-pad@1.3.0:sha512-1hX9M2fsPT4gLk3l1qU0bQ=="))
	sig, err := ecdsa.SignASN1(rand.Reader, k, message[:])
	if err != nil {
		t.Fatal(err)
	}
	out, err := rawkey.Check(context.Background(), scheme.Input{
		Scheme: signatures.SchemeNPMRegistry, Role: signatures.RoleRegistry, SignedDigest: message[:], Limits: scheme.DefaultLimits(),
		Raw: fmt.Appendf(nil, `{"keyid":%q,"sig":%q}`, keyID, base64.StdEncoding.EncodeToString(sig)),
	})
	if err != nil {
		t.Fatal(err)
	}
	stored := signature(t, out, time.Now())
	key := anchors.ParsedKey{Key: k.Public(), KeyID: keyID, SHA256: pki.KeyID(k.Public())}
	anchor := func(k anchors.ParsedKey) *Scope {
		return &Scope{Anchors: []anchors.Anchor{{ID: 7, Scope: anchors.ScopeShipped, Kind: anchors.KindPublicKey, Parsed: &anchors.Parsed{Public: []anchors.ParsedKey{k}}}}}
	}
	v := Judge(stored, anchor(key), nil)
	wantVerdict(t, v, StatePublic, 7)
	if v.Integrity != signatures.StateValid {
		t.Fatalf("integrity %q", v.Integrity)
	}
	expired := key
	expired.Expires = time.Now().Add(-time.Hour)
	wantVerdict(t, Judge(stored, anchor(expired), nil), StateUntrusted)
}

func TestR_SIG_008_ABundleOfTheCompanysSigstoreIsKnownByItsIdentity(t *testing.T) {
	co := newCompany(t)
	file := []byte("tool.tar.gz, a kilobyte of it")
	sum := sha256.Sum256(file)
	sig := Signature{
		State: signatures.StateValid, ReceivedAt: time.Now(),
		Evidence: &scheme.Evidence{Sigstore: &scheme.SigstoreEvidence{
			Bundle: co.messageBundle(t, file, time.Now().Add(-3*time.Minute)), DigestAlg: "sha256", Digest: sum[:],
		}},
	}
	own, err := sigstore.LoadRoot(co.trustRoot)
	if err != nil {
		t.Fatal(err)
	}
	stand, err := sigstore.LoadRoot(newCompany(t).trustRoot)
	if err != nil {
		t.Fatal(err)
	}
	identity := func(id int64, scope, issuer, pattern string, trustedRoot []byte) anchors.Anchor {
		return anchors.Anchor{ID: id, Scope: scope, Kind: anchors.KindSigstore, Parsed: &anchors.Parsed{Sigstore: &anchors.ParsedSigstore{
			Issuer: issuer, Pattern: regexp.MustCompile(`^(?:` + pattern + `)$`), TrustedRoot: trustedRoot,
		}}}
	}
	company := `https://gitlab\.example\.com/platform/.+//\.gitlab-ci\.yml@refs/heads/main`
	for _, tc := range []struct {
		name   string
		anchor anchors.Anchor
		public *sigstore.Root
		state  string
	}{
		{"its issuer and pattern under its own root", identity(1, anchors.ScopeConfiguration, gitlabIssuer, company, co.trustRoot), nil, StateInternal},
		{"another pattern", identity(1, anchors.ScopeConfiguration, gitlabIssuer, `https://gitlab\.example\.com/other/.+`, co.trustRoot), nil, StateUntrusted},
		{"the public trust root", identity(1, anchors.ScopeConfiguration, gitlabIssuer, company, nil), stand, StateUntrusted},
		{"the shipped anchor of the public root that stands for it", identity(1, anchors.ScopeShipped, "", `.*`, nil), own, StatePublic},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := Judge(sig, &Scope{Anchors: []anchors.Anchor{tc.anchor}, PublicRoot: tc.public, Roots: map[int64]*sigstore.Root{1: own}}, nil)
			if v.State != tc.state {
				t.Fatalf("verdict %+v, want %s", v, tc.state)
			}
			if tc.state != StateUntrusted && v.TimeProof != ProofLog {
				t.Fatalf("time proof %q, want the log's", v.TimeProof)
			}
		})
	}
}

func TestR_SIG_020_ABlockOfEachKindMakesASignatureBlockedWhateverAnchorsKnowIt(t *testing.T) {
	ca := pkitest.NewCA(t, "Example GmbH")
	leaf, key := ca.Issue(t, pkitest.Leaf{})
	sig := signature(t, notationEvidence(t, ca.Chain(leaf), key, time.Now().Add(-time.Minute), nil), time.Now())
	sig.Signer = &signatures.Signer{OIDCIssuer: gitlabIssuer, Identity: gitlabIdentity}
	sc := &Scope{Anchors: []anchors.Anchor{caAnchor(1, anchors.ScopeInstallation, anchors.UseCodeSigning, nil, ca.Root)}}
	leafSum := sha256.Sum256(leaf.Raw)
	for _, tc := range []struct {
		name    string
		block   Block
		blocked bool
	}{
		{"fingerprint of the leaf", Block{ID: 11, Kind: BlockFingerprint, Value: hex.EncodeToString(leafSum[:])}, true},
		{"fingerprint in upper case", Block{ID: 12, Kind: BlockFingerprint, Value: fmt.Sprintf("%X", leafSum[:])}, true},
		{"issuer and serial", Block{ID: 13, Kind: BlockSerial, Value: leaf.Issuer.String() + "|" + leaf.SerialNumber.String()}, true},
		{"identity", Block{ID: 14, Kind: BlockIdentity, Issuer: gitlabIssuer, Value: `https://gitlab\.example\.com/platform/.+`}, true},
		{"identity of another issuer", Block{ID: 15, Kind: BlockIdentity, Issuer: "https://token.actions.githubusercontent.com", Value: `.*`}, false},
		{"another fingerprint", Block{ID: 16, Kind: BlockFingerprint, Value: hex.EncodeToString(make([]byte, 32))}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := Judge(sig, sc, []Block{tc.block})
			if !tc.blocked {
				wantVerdict(t, v, StateInternal, 1)
				return
			}
			wantVerdict(t, v, StateBlocked, 1)
			if v.BlockID != tc.block.ID {
				t.Fatalf("block %d, want %d", v.BlockID, tc.block.ID)
			}
		})
	}
	k, cert := pkitest.ReleaseKey(t, "Example Release", time.Now().Add(-time.Hour))
	pgp := detachedEvidence(t, k, []byte("tool.tar.gz"))
	v := Judge(pgp, &Scope{Anchors: []anchors.Anchor{openPGPAnchor(2, anchors.ScopeProject, cert)}}, []Block{{ID: 21, Kind: BlockFingerprint, Value: cert.Fingerprint}})
	wantVerdict(t, v, StateBlocked, 2)
	npm := Signature{State: signatures.StateNoKey, KeyIDs: []string{"SHA256:abc"}, ReceivedAt: time.Now()}
	if v := Judge(npm, &Scope{}, []Block{{ID: 31, Kind: BlockKeyID, Value: "SHA256:abc"}}); v.State != StateBlocked || v.BlockID != 31 {
		t.Fatalf("a keyid block: %+v", v)
	}
	if v := Judge(npm, &Scope{}, []Block{{ID: 32, Kind: BlockKeyID, Value: "sha256:ABC"}}); v.State != StateUntrusted {
		t.Fatalf("an npm keyid matched in another case: %+v", v)
	}
}
