package sigstore

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	protorekor "github.com/sigstore/protobuf-specs/gen/pb-go/rekor/v1"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
)

// fixture is what most tests share: the company's Sigstore, the public stand-in, and their roots as a check hands them.
type fixture struct {
	co     *company
	pub    *ca.VirtualSigstore
	roots  []scheme.SigstoreRoot
	public []scheme.SigstoreRoot
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	co := newCompany(t)
	pub, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatal(err)
	}
	public := []scheme.SigstoreRoot{{TrustedRoot: publicRoot(t, pub)}}
	return &fixture{co: co, pub: pub, public: public, roots: append([]scheme.SigstoreRoot{{AnchorID: 7, TrustedRoot: co.trustRoot}}, public...)}
}

func check(t *testing.T, in scheme.Input) scheme.Output {
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

func signerOf(t *testing.T, out scheme.Output) signatures.Signer {
	t.Helper()
	var s signatures.Signer
	if err := json.Unmarshal(out.Result.Signer, &s); err != nil {
		t.Fatalf("signer: %v", err)
	}
	return s
}

// genericInput is a generic <file>.sigstore.json beside a file: the bundle is the object's blob, the subject the file's SHA-256.
func genericInput(t *testing.T, f *fixture, bundleDoc, file []byte) scheme.Input {
	t.Helper()
	blobs := newBlobs()
	return scheme.Input{
		Scheme: signatures.SchemeSigstoreBundle, Role: signatures.RolePublisher, ItemBlob: blobs.put(bundleDoc), Subject: sha256.Sum256(file),
		Blobs: blobs, Roots: f.roots, Limits: scheme.DefaultLimits(),
	}
}

func TestR_SIG_006_MessageSignatureBundleIsValidAtItsLogTime(t *testing.T) {
	f := newFixture(t)
	file := []byte("tool.tar.gz, a kilobyte of it")
	at := time.Now().Add(-3 * time.Minute)
	out := check(t, genericInput(t, f, bundleJSON(t, f.co.messageBundle(t, file, at, true)), file))
	wantState(t, out, signatures.StateValid, "")
	r := out.Result
	if r.TimeProof != "log" || !r.ProvenAt.Equal(at.Add(time.Minute).Truncate(time.Second)) {
		t.Fatalf("proven %v by %q, want the log's integrated time", r.ProvenAt, r.TimeProof)
	}
	if r.Role != signatures.RolePublisher || out.Evidence == nil || out.Evidence.Sigstore == nil || len(out.Evidence.Chain) != 1 {
		t.Fatalf("role %q, evidence %+v", r.Role, out.Evidence)
	}
	if got := out.Evidence.Sigstore; got.DigestAlg != "sha256" || hex.EncodeToString(got.Digest) != hex.EncodeToString(sha256Of(file)) {
		t.Fatalf("evidence digest %s %x", got.DigestAlg, got.Digest)
	}
	if len(out.Evidence.Timestamps) != 1 || len(r.Evidence) == 0 {
		t.Fatalf("timestamps %d, stored evidence %d bytes", len(out.Evidence.Timestamps), len(r.Evidence))
	}
}

func TestR_SIG_006_DSSEBundleOfAnOCIReferrerIsValid(t *testing.T) {
	f := newFixture(t)
	subject := image("app")
	stmt := statementOf(predicateCosignSign, "registry.example.com/platform/app", "sha256", subject[:])
	blobs := newBlobs()
	doc := bundleJSON(t, f.co.dsseBundle(t, stmt, time.Now().Add(-3*time.Minute)))
	m := ociManifest(t, blobs, mediaBundleV03, subject, [][]byte{doc}, []string{mediaBundleV03}, nil)
	out := check(t, scheme.Input{
		Scheme: signatures.SchemeSigstoreBundle, Role: signatures.RoleAttestation, ItemBlob: m, Part: 0, Subject: subject,
		Blobs: blobs, Roots: f.roots, Limits: scheme.DefaultLimits(),
	})
	wantState(t, out, signatures.StateValid, "")
	if out.Result.Role != signatures.RolePublisher || out.Result.PredicateType != predicateCosignSign || out.Parts != 1 {
		t.Fatalf("role %q, predicate %q, parts %d", out.Result.Role, out.Result.PredicateType, out.Parts)
	}
	if out.Result.TimeProof != "log" {
		t.Fatalf("time proof %q", out.Result.TimeProof)
	}
}

func TestR_SIG_006_AFlippedByteIsInvalidWithItsReason(t *testing.T) {
	f := newFixture(t)
	file := []byte("tool.tar.gz")
	subject := image("app")
	at := time.Now().Add(-3 * time.Minute)
	flippedSig := f.co.messageBundle(t, file, at, false)
	flippedSig.GetMessageSignature().Signature[10] ^= 1
	flippedSubject := f.co.dsseBundle(t, statementOf(predicateSLSA1, "app", "sha256", subject[:]), at)
	payload := flippedSubject.GetDsseEnvelope().Payload
	i := strings.Index(string(payload), hex.EncodeToString(subject[:]))
	payload[i] ^= 1 // one hex digit of the statement's subject digest
	for _, tc := range []struct {
		name    string
		in      scheme.Input
		reason  string
		subject [32]byte
	}{
		{"content", genericInput(t, f, bundleJSON(t, f.co.messageBundle(t, file, at, false)), []byte("tool.tar.gz, replaced")), signatures.ReasonDigestMismatch, [32]byte{}},
		{"signature", genericInput(t, f, bundleJSON(t, flippedSig), file), signatures.ReasonSignatureMismatch, [32]byte{}},
		{"statement subject", scheme.Input{Raw: bundleJSON(t, flippedSubject), Subject: subject, Roots: f.roots}, signatures.ReasonSubjectMismatch, subject},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := check(t, tc.in)
			wantState(t, out, signatures.StateInvalid, tc.reason)
			if out.Evidence != nil || len(out.Result.Evidence) != 0 {
				t.Fatal("an invalid signature keeps no evidence")
			}
		})
	}
}

func TestR_SIG_006_ALogEntryAKnownRootDoesNotVerifyIsInvalid(t *testing.T) {
	f := newFixture(t)
	file := []byte("tool.tar.gz")
	b := f.co.messageBundle(t, file, time.Now().Add(-3*time.Minute), false)
	b.GetVerificationMaterial().GetTlogEntries()[0].GetInclusionPromise().SignedEntryTimestamp[8] ^= 1
	wantState(t, check(t, genericInput(t, f, bundleJSON(t, b), file)), signatures.StateInvalid, signatures.ReasonLogEntryInvalid)
}

func TestR_SIG_006_ALogNoRootKnowsLeavesTheTimeUnproven(t *testing.T) {
	f := newFixture(t)
	file := []byte("tool.tar.gz")
	at := time.Now().Add(-3 * time.Minute)
	in := genericInput(t, f, bundleJSON(t, f.co.messageBundle(t, file, at, true)), file)
	in.Roots = f.public
	out := check(t, in)
	wantState(t, out, signatures.StateValid, "")
	if out.Result.TimeProof != "" || !out.Result.ProvenAt.IsZero() {
		t.Fatalf("proven %v by %q, want no proof", out.Result.ProvenAt, out.Result.TimeProof)
	}
	if !out.Result.ClaimedAt.Equal(at.Add(time.Minute).Truncate(time.Second)) {
		t.Fatalf("claimed %v, want the unverified integrated time", out.Result.ClaimedAt)
	}
}

func TestR_SIG_005_TheFulcioExtensionsOfAGitLabJobAreRead(t *testing.T) {
	f := newFixture(t)
	file := []byte("tool.tar.gz")
	out := check(t, genericInput(t, f, bundleJSON(t, f.co.messageBundle(t, file, time.Now().Add(-3*time.Minute), true)), file))
	s := signerOf(t, out)
	if s.Identity != gitlabIdentity || s.OIDCIssuer != gitlabIssuer || s.Issuer == "" || s.Serial == "" || len(s.Chain) != 1 {
		t.Fatalf("signer %+v", s)
	}
	want := signatures.SignerCI{
		Issuer: gitlabIssuer, BuildSignerURI: gitlabIdentity, BuildSignerDigest: "0123456789abcdef0123456789abcdef01234567",
		RunnerEnvironment: "gitlab-hosted", SourceRepositoryURI: "https://gitlab.example.com/platform/app",
		SourceRepositoryDigest: "89abcdef0123456789abcdef0123456789abcdef", SourceRepositoryRef: "refs/heads/main", BuildConfigURI: gitlabIdentity,
		BuildTrigger: "push", RunInvocationURI: "https://gitlab.example.com/platform/app/-/jobs/1234", SourceRepositoryOwner: "https://gitlab.example.com/platform",
		SourceRepositoryIdentifier: "4711", SourceRepositoryOwnerIdentifier: "42", BuildConfigDigest: "fedcba9876543210fedcba9876543210fedcba98",
		SourceRepositoryVisibility: "private",
	}
	if s.CI == nil || *s.CI != want {
		t.Fatalf("ci %+v", s.CI)
	}
	if len(s.Log) != 1 || s.Log[0].Proof != "promise" || s.Log[0].Index != 1000 || len(s.Timestamps) != 1 {
		t.Fatalf("log %+v, timestamps %+v", s.Log, s.Timestamps)
	}
	if len(out.Result.KeyIDs) != 2 || !strings.Contains(out.Result.KeyIDs[1], "|") || len(out.Result.KeyIDs[0]) != 64 {
		t.Fatalf("key ids %v", out.Result.KeyIDs)
	}
}

func TestR_SIG_023_CosignSignatureManifestOfTwoLayers(t *testing.T) {
	f := newFixture(t)
	pki := newCompanyCA(t)
	subject := image("app")
	blobs := newBlobs()
	var payloads [][]byte
	var ann []map[string]string
	for range 2 {
		p := simpleSigningPayload(subject)
		payloads = append(payloads, p)
		ann = append(ann, map[string]string{
			annSignature:   base64.StdEncoding.EncodeToString(sign(t, pki.leafKey, sha256Of(p))),
			annCertificate: string(certPEM(pki.leaf)), annChain: string(certPEM(pki.inter)) + string(certPEM(pki.root)),
		})
	}
	m := ociManifest(t, blobs, "", subject, payloads, []string{mediaSimpleSigning, mediaSimpleSigning}, ann)
	for part := range 2 {
		out := check(t, scheme.Input{
			Scheme: signatures.SchemeCosignLegacy, Role: signatures.RolePublisher, ItemBlob: m, Part: part, Subject: subject,
			Blobs: blobs, Roots: f.roots, Limits: scheme.DefaultLimits(),
		})
		wantState(t, out, signatures.StateValid, "")
		s := signerOf(t, out)
		if out.Parts != 2 || len(out.Evidence.Chain) != 3 || s.DockerReference != "registry.example.com/platform/app" || out.Result.TimeProof != "" {
			t.Fatalf("part %d: parts %d, chain %d, signer %+v", part, out.Parts, len(out.Evidence.Chain), s)
		}
		if !strings.Contains(s.Subject, "Release signing") || len(out.Result.KeyIDs) != 4 {
			t.Fatalf("part %d: subject %q, key ids %v", part, s.Subject, out.Result.KeyIDs)
		}
	}
	// The payload of part 1 names another image: that part alone is invalid.
	other := image("other")
	ann[1][annSignature] = base64.StdEncoding.EncodeToString(sign(t, pki.leafKey, sha256Of(simpleSigningPayload(other))))
	m = ociManifest(t, blobs, "", subject, [][]byte{payloads[0], simpleSigningPayload(other)}, []string{mediaSimpleSigning, mediaSimpleSigning}, ann)
	out := check(t, scheme.Input{ItemBlob: m, Part: 1, Subject: subject, Blobs: blobs, Roots: f.roots})
	wantState(t, out, signatures.StateInvalid, signatures.ReasonSubjectMismatch)
}

func TestR_SIG_023_CosignAttestationLayerWithItsLogEntry(t *testing.T) {
	f := newFixture(t)
	subject := image("app")
	at := time.Now().Add(-3 * time.Minute)
	leaf, k := f.co.leaf(t, at)
	stmt := statementOf(predicateSLSA1, "registry.example.com/platform/app", "sha256", subject[:])
	sig, tle := dsseSigned(t, f.co.vs, f.co.logID, leaf, k, stmt, at.Add(time.Minute))
	envelope, _ := json.Marshal(map[string]any{
		"payloadType": payloadInToto, "payload": base64.StdEncoding.EncodeToString(stmt),
		"signatures": []map[string]string{{"keyid": "", "sig": base64.StdEncoding.EncodeToString(sig)}},
	})
	rekor, _ := json.Marshal(map[string]any{
		"SignedEntryTimestamp": base64.StdEncoding.EncodeToString(tle.GetInclusionPromise().GetSignedEntryTimestamp()),
		"Payload": map[string]any{
			"body": base64.StdEncoding.EncodeToString(tle.GetCanonicalizedBody()), "integratedTime": tle.GetIntegratedTime(),
			"logIndex": tle.GetLogIndex(), "logID": f.co.logIDHex,
		},
	})
	blobs := newBlobs()
	m := ociManifest(t, blobs, "", subject, [][]byte{envelope}, []string{mediaDSSE}, []map[string]string{{
		annSignature: "", annCertificate: string(certPEM(leaf)), annChain: string(certPEM(f.co.inter)) + string(certPEM(f.co.root)),
		annBundle: string(rekor), "predicateType": predicateSLSA1,
	}})
	out := check(t, scheme.Input{
		Scheme: signatures.SchemeCosignLegacy, Role: signatures.RoleAttestation, ItemBlob: m, Subject: subject, Blobs: blobs,
		Roots: f.roots, Limits: scheme.DefaultLimits(),
	})
	wantState(t, out, signatures.StateValid, "")
	if out.Result.Role != signatures.RoleProvenance || out.Result.TimeProof != "log" || out.Parts != 1 {
		t.Fatalf("role %q, proof %q, parts %d", out.Result.Role, out.Result.TimeProof, out.Parts)
	}
	j, err := Judge(out.Evidence.Sigstore, mustRoot(t, f.co.trustRoot), Identity{Issuer: gitlabIssuer, Identity: gitlabIdentity})
	if err != nil || !j.Known {
		t.Fatalf("the converted bundle is not known: %+v %v", j, err)
	}
}

func TestR_SIG_006_ABundleSignedWithAKeyIsNoKeyUntilTheKeyIsKnown(t *testing.T) {
	f := newFixture(t)
	file := []byte("tool.tar.gz")
	k := newKey(t)
	digest := sha256Of(file)
	sig := sign(t, k, digest)
	der := keyID(k.Public())
	hintSum, _ := hex.DecodeString(der)
	b := &protobundle.Bundle{
		MediaType: mediaBundleV01,
		VerificationMaterial: &protobundle.VerificationMaterial{
			Content:     &protobundle.VerificationMaterial_PublicKey{PublicKey: &protocommon.PublicKeyIdentifier{Hint: base64.StdEncoding.EncodeToString(hintSum)}},
			TlogEntries: []*protorekor.TransparencyLogEntry{f.co.signedEntry(t, hashedRekord(digest, sig, keyPEM(t, k.Public())), time.Now().Add(-time.Minute), 3)},
		},
		Content: &protobundle.Bundle_MessageSignature{MessageSignature: &protocommon.MessageSignature{
			MessageDigest: &protocommon.HashOutput{Algorithm: protocommon.HashAlgorithm_SHA2_256, Digest: digest}, Signature: sig,
		}},
	}
	in := genericInput(t, f, bundleJSON(t, b), file)
	out := check(t, in)
	wantState(t, out, signatures.StateNoKey, "")
	if !slices.Contains(out.Result.KeyIDs, der) || out.Evidence.Key == nil || out.Evidence.Key.Kind != "ecdsa" {
		t.Fatalf("key ids %v, key evidence %+v", out.Result.KeyIDs, out.Evidence.Key)
	}
	if hex.EncodeToString(out.Result.SignedDigest) != hex.EncodeToString(digest) || out.Result.DigestAlg != "sha256" || out.Result.TimeProof != "log" {
		t.Fatalf("signed digest %x (%s), proof %q", out.Result.SignedDigest, out.Result.DigestAlg, out.Result.TimeProof)
	}
	in.Keys.Public = []anchors.ParsedKey{publicKeyOf(newKey(t).Public()), publicKeyOf(k.Public())}
	out = check(t, in)
	wantState(t, out, signatures.StateValid, "")
	if s := signerOf(t, out); s.KeyID != der || s.Algorithm != "ECDSA P-256" {
		t.Fatalf("signer %+v", s)
	}
}

func TestR_SIG_028_NpmProvenanceNamesTheTarballBySHA512(t *testing.T) {
	f := newFixture(t)
	tarball := []byte("package/package.json and a little more")
	sum := sha512.Sum512(tarball)
	stmt := statementOf(predicateSLSA1, "pkg:npm/%40example/app@1.0.0", "sha512", sum[:])
	e, err := f.pub.AttestAtTime("npm-publisher@example.com", "https://token.actions.githubusercontent.com", stmt, time.Now().Add(time.Minute), true)
	if err != nil {
		t.Fatal(err)
	}
	b := publicBundle(t, f, e)
	out := check(t, scheme.Input{
		Scheme: signatures.SchemeSigstoreBundle, Role: signatures.RoleProvenance, Raw: b, Subject: sha256.Sum256(tarball),
		SubjectSHA512: sum[:], Roots: f.public, Limits: scheme.DefaultLimits(),
	})
	wantState(t, out, signatures.StateValid, "")
	if out.Result.Role != signatures.RoleProvenance || out.Result.PredicateType != predicateSLSA1 || out.Evidence.Sigstore.DigestAlg != "sha512" {
		t.Fatalf("role %q, predicate %q, digest %s", out.Result.Role, out.Result.PredicateType, out.Evidence.Sigstore.DigestAlg)
	}
	if out.Result.TimeProof == "" {
		t.Fatal("the public log proves no time")
	}
}

func TestR_SIG_008_JudgeKnowsTheIdentityOfItsOwnRootOnly(t *testing.T) {
	f := newFixture(t)
	subject := image("app")
	stmt := statementOf(predicateCosignSign, "app", "sha256", subject[:])
	out := check(t, scheme.Input{Raw: bundleJSON(t, f.co.dsseBundle(t, stmt, time.Now().Add(-3*time.Minute))), Subject: subject, Roots: f.roots})
	wantState(t, out, signatures.StateValid, "")
	own, public := mustRoot(t, f.co.trustRoot), mustRoot(t, f.public[0].TrustedRoot)
	for _, tc := range []struct {
		name  string
		r     *Root
		id    Identity
		known bool
	}{
		{"identity and issuer", own, Identity{Issuer: gitlabIssuer, Identity: gitlabIdentity}, true},
		{"pattern", own, Identity{Issuer: gitlabIssuer, Pattern: regexp.MustCompile(`^https://gitlab\.example\.com/platform/.*$`)}, true},
		{"any issuer", own, Identity{Identity: gitlabIdentity}, true},
		{"another pattern", own, Identity{Issuer: gitlabIssuer, Pattern: regexp.MustCompile(`^https://gitlab\.example\.com/other/.*$`)}, false},
		{"an unanchored pattern matches whole identities only", own, Identity{Pattern: regexp.MustCompile(`platform/app`)}, false},
		{"another issuer", own, Identity{Issuer: "https://token.actions.githubusercontent.com", Identity: gitlabIdentity}, false},
		{"the public root", public, Identity{Identity: gitlabIdentity}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j, err := Judge(out.Evidence.Sigstore, tc.r, tc.id)
			if err != nil {
				t.Fatal(err)
			}
			if j.Known != tc.known || (j.Known && j.Proof != "log") {
				t.Fatalf("judgement %+v, want known %v", j, tc.known)
			}
		})
	}
}

func TestR_SIG_007_ATenMinuteCertificateExpiredLongAgoIsKnownAtItsLogTime(t *testing.T) {
	f := newFixture(t)
	file := []byte("tool.tar.gz")
	at := time.Now().AddDate(-2, 0, 0)
	out := check(t, genericInput(t, f, bundleJSON(t, f.co.messageBundle(t, file, at, false)), file))
	wantState(t, out, signatures.StateValid, "")
	logged := at.Add(time.Minute).Truncate(time.Second)
	if !out.Result.ProvenAt.Equal(logged) {
		t.Fatalf("proven %v, want %v", out.Result.ProvenAt, logged)
	}
	j, err := Judge(out.Evidence.Sigstore, mustRoot(t, f.co.trustRoot), Identity{Issuer: gitlabIssuer, Identity: gitlabIdentity})
	if err != nil || !j.Known || !j.ProvenAt.Equal(logged) || j.Proof != "log" {
		t.Fatalf("judgement %+v %v", j, err)
	}
}

func TestR_SIG_013_HostileShapesAreRefusedWithinTheBounds(t *testing.T) {
	f := newFixture(t)
	file := []byte("tool.tar.gz")
	nine := f.co.messageBundle(t, file, time.Now().Add(-3*time.Minute), false)
	chain := nine.GetVerificationMaterial().GetX509CertificateChain()
	for range 8 {
		chain.Certificates = append(chain.Certificates, &protocommon.X509Certificate{RawBytes: f.co.inter.Raw})
	}
	deep := `{"mediaType":"` + mediaBundleV03 + `","x":` + strings.Repeat("[", 100_000)
	weak := f.co.messageBundle(t, file, time.Now(), false)
	weak.GetVerificationMaterial().GetX509CertificateChain().Certificates[0].RawBytes = rsaLeaf(t, 1024).Raw
	newer := strings.Replace(string(bundleJSON(t, f.co.messageBundle(t, file, time.Now(), false))), mediaBundleV01, "application/vnd.dev.sigstore.bundle.v0.9+json", 1)
	for _, tc := range []struct {
		name   string
		raw    []byte
		state  signatures.State
		reason string
	}{
		{"JSON nested 100,000 levels", []byte(deep), signatures.StateMalformed, signatures.ReasonLimitsExceeded},
		{"a chain of nine certificates", bundleJSON(t, nine), signatures.StateMalformed, signatures.ReasonLimitsExceeded},
		{"a bundle version Quader does not read", []byte(newer), signatures.StateUnsupported, signatures.ReasonVersionUnsupported},
		{"an RSA key below 2048 bits", bundleJSON(t, weak), signatures.StateUnsupported, signatures.ReasonWeakAlgorithm},
		{"no JSON", []byte("-----BEGIN PGP SIGNATURE-----"), signatures.StateMalformed, signatures.ReasonContentUnreadable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := check(t, scheme.Input{Raw: tc.raw, Subject: sha256.Sum256(file), Roots: f.roots})
			wantState(t, out, tc.state, tc.reason)
		})
	}
}

func TestR_SIG_006_AStoredLayerThatIsGoneOrUnreadable(t *testing.T) {
	f := newFixture(t)
	subject := image("app")
	blobs := newBlobs()
	doc := bundleJSON(t, f.co.dsseBundle(t, statementOf(predicateCosignSign, "app", "sha256", subject[:]), time.Now()))
	m := ociManifest(t, blobs, mediaBundleV03, subject, [][]byte{doc}, []string{mediaBundleV03}, nil)
	delete(blobs.m, sha256.Sum256(doc))
	in := scheme.Input{ItemBlob: m, Subject: subject, Blobs: blobs, Roots: f.roots}
	wantState(t, check(t, in), signatures.StateMalformed, signatures.ReasonContentUnreadable)
	blobs.err = errors.New("the object store is down")
	if _, err := Check(context.Background(), in); !errors.Is(err, scheme.ErrTransient) {
		t.Fatalf("error %v, want a transient one", err)
	}
}

func TestR_SIG_007_ATimestampOfAKnownAuthorityProvesTheTimeAndAForeignOneIsInvalid(t *testing.T) {
	f := newFixture(t)
	file := []byte("tool.tar.gz")
	stamped := f.co.messageBundle(t, file, time.Now().Add(-3*time.Minute), true)
	stamped.GetVerificationMaterial().TlogEntries = nil
	out := check(t, genericInput(t, f, bundleJSON(t, stamped), file))
	wantState(t, out, signatures.StateValid, "")
	if out.Result.TimeProof != "timestamp" || out.Result.ProvenAt.IsZero() {
		t.Fatalf("proven %v by %q, want the timestamp", out.Result.ProvenAt, out.Result.TimeProof)
	}
	other, err := f.co.vs.TimestampResponse([]byte("another signature"))
	if err != nil {
		t.Fatal(err)
	}
	stamped.GetVerificationMaterial().GetTimestampVerificationData().GetRfc3161Timestamps()[0].SignedTimestamp = other
	wantState(t, check(t, genericInput(t, f, bundleJSON(t, stamped), file)), signatures.StateInvalid, signatures.ReasonTimestampInvalid)
}

func mustRoot(t *testing.T, doc []byte) *Root {
	t.Helper()
	r, err := LoadRoot(doc)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
