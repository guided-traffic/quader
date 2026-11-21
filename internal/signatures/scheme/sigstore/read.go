package sigstore

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore/pkg/cryptoutils"

	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
)

// The read (R-SIG-005): the leaf's identity, OIDC issuer and Fulcio extensions, the chain's subjects, the log entries and timestamps,
// the statement's predicate type and subjects, cosign's docker reference, and the key IDs an anchor or a block matches (the SHA-256 of
// every certificate, the leaf's issuer and serial, a raw key's SHA-256). Every value is the signature's claim until the integrity check
// and the trust step confirm it.

// The predicate types that correct a role (R-SIG-023, R-SIG-028).
const (
	predicateSLSAPrefix = "https://slsa.dev/provenance/"
	predicateCosignSign = "https://sigstore.dev/cosign/sign/v1"
	predicateNPMPublish = "https://github.com/npm/attestation/tree/main/specs/publish/v0.1"
)

// maxStatementSubjects is the most subject names the read keeps of a statement: signer is at most 64 KiB (R-SIG-003).
const maxStatementSubjects = 32

// reading is what the read found and what the integrity check adds to it.
type reading struct {
	signer    signatures.Signer
	keyIDs    []string
	role      string
	predicate string
	parts     int
	// claimed is the earliest time a log entry or timestamp no trust root verified states; proven and proof the earliest one a root
	// verified (R-SIG-007).
	claimed time.Time
	proven  time.Time
	proof   string
	// noKey is a bundle signed with a key that no known key verifies (Q-SIG-23); key is the known key that verified it.
	noKey bool
	key   crypto.PublicKey
	// signedDigest and digestAlg are what a key signature commits to; stamps the timestamps that matched the signature.
	signedDigest []byte
	digestAlg    string
	stamps       []scheme.Timestamp
}

// result is the reading as the Result's fields.
func (rd *reading) result() signatures.Result {
	res := signatures.Result{
		Role: rd.role, PredicateType: rd.predicate, KeyIDs: rd.keyIDs, ClaimedAt: rd.claimed, ProvenAt: rd.proven, TimeProof: rd.proof,
		SignedDigest: rd.signedDigest,
	}
	if len(rd.signedDigest) > 0 {
		res.DigestAlg = rd.digestAlg
	}
	if b, err := json.Marshal(rd.signer); err == nil {
		res.Signer = b
	}
	return res
}

// read takes from the object what the signature claims.
func read(o *object, in scheme.Input) *reading {
	rd := &reading{role: roleOf(o, in.Role), parts: o.parts, digestAlg: "sha256"}
	if o.stmt != nil {
		rd.predicate = o.stmt.PredicateType
		rd.signer.PredicateType = o.stmt.PredicateType
		rd.signer.StatementSubjects, rd.signer.Cut = subjectNames(o.stmt)
	}
	if o.legacy != nil {
		rd.signer.DockerReference = o.legacy.Critical.Identity.DockerReference
	}
	if leaf := o.leaf(); leaf != nil {
		readCertificates(&rd.signer, o.certs)
		rd.keyIDs = certificateKeyIDs(o.certs)
		rd.signer.Algorithm = algorithmName(leaf.PublicKey)
	} else if o.hint != "" {
		rd.keyIDs = []string{hintKeyID(o.hint)}
		rd.signer.KeyID = rd.keyIDs[0]
	}
	rd.signer.HashAlgorithm = hashName(o)
	rd.signer.Log = logEntries(o)
	rd.signer.Timestamps = timestampInfos(o)
	if len(o.notes) > 0 {
		rd.signer.Detail = strings.Join(o.notes, "; ")
	}
	return rd
}

// roleOf corrects the detector's role by what the signature is: an in-toto statement by its predicate, a message signature and cosign's
// simple signing as the publisher's.
func roleOf(o *object, detected string) string {
	if o.stmt == nil {
		return signatures.RolePublisher
	}
	p := o.stmt.PredicateType
	switch {
	case strings.HasPrefix(p, predicateSLSAPrefix):
		return signatures.RoleProvenance
	case p == predicateCosignSign:
		return signatures.RolePublisher
	case p == predicateNPMPublish:
		return signatures.RoleRegistry
	case p == "" && detected != "":
		return detected
	}
	return signatures.RoleAttestation
}

// subjectNames are the names of a statement's subjects, at most maxStatementSubjects; cut says what was left out.
func subjectNames(st *statement) (names []string, cut string) {
	for i, s := range st.Subject {
		if i == maxStatementSubjects {
			return names, fmt.Sprintf("%d of %d statement subjects kept", maxStatementSubjects, len(st.Subject))
		}
		names = append(names, s.Name)
	}
	return names, ""
}

// readCertificates fills the signer from the leaf and the chain.
func readCertificates(s *signatures.Signer, certs []*x509.Certificate) {
	leaf := certs[0]
	s.Subject, s.Issuer = leaf.Subject.String(), leaf.Issuer.String()
	s.Serial = leaf.SerialNumber.String()
	s.CertificateNotBefore, s.CertificateNotAfter = leaf.NotBefore.UTC(), leaf.NotAfter.UTC()
	s.AlternativeNames = alternativeNames(leaf)
	if len(s.AlternativeNames) > 0 {
		s.Identity = s.AlternativeNames[0]
	}
	for _, c := range certs {
		s.Chain = append(s.Chain, c.Subject.String())
	}
	ext, err := certificate.ParseExtensions(leaf.Extensions)
	if err != nil {
		s.Detail = "a Fulcio extension of the certificate does not parse"
		return
	}
	s.OIDCIssuer = ext.Issuer
	s.CI = signerCI(ext)
}

// alternativeNames are every subject alternative name of a certificate: URIs first (a Sigstore identity), then e-mails, Fulcio's other
// name, DNS names and IP addresses.
func alternativeNames(c *x509.Certificate) []string {
	var out []string
	for _, u := range c.URIs {
		out = append(out, u.String())
	}
	out = append(out, c.EmailAddresses...)
	if on, err := cryptoutils.UnmarshalOtherNameSAN(c.Extensions); err == nil && on != "" {
		out = append(out, on)
	}
	out = append(out, c.DNSNames...)
	for _, ip := range c.IPAddresses {
		out = append(out, ip.String())
	}
	return out
}

// signerCI maps Fulcio's extensions (https://github.com/sigstore/fulcio/blob/main/docs/oid-info.md) into the signer's CI claims. The
// deprecated GitHub extensions 1.2, 1.3, 1.5 and 1.6 fill the field of their successor when the successor is absent; 1.4, the
// workflow's name, has no successor and is kept as it is.
func signerCI(e certificate.Extensions) *signatures.SignerCI {
	ci := &signatures.SignerCI{
		Issuer: e.Issuer, BuildSignerURI: e.BuildSignerURI, BuildSignerDigest: e.BuildSignerDigest, RunnerEnvironment: e.RunnerEnvironment,
		SourceRepositoryURI: e.SourceRepositoryURI, SourceRepositoryDigest: e.SourceRepositoryDigest, SourceRepositoryRef: e.SourceRepositoryRef,
		BuildConfigURI: e.BuildConfigURI, BuildTrigger: e.BuildTrigger, RunInvocationURI: e.RunInvocationURI,
		SourceRepositoryOwner: e.SourceRepositoryOwnerURI, SourceRepositoryIdentifier: e.SourceRepositoryIdentifier,
		SourceRepositoryOwnerIdentifier: e.SourceRepositoryOwnerIdentifier, BuildConfigDigest: e.BuildConfigDigest,
		WorkflowName: e.GithubWorkflowName, SourceRepositoryVisibility: e.SourceRepositoryVisibilityAtSigning,
	}
	fill := func(dst *string, old string) {
		if *dst == "" {
			*dst = old
		}
	}
	fill(&ci.BuildTrigger, e.GithubWorkflowTrigger)
	fill(&ci.SourceRepositoryDigest, e.GithubWorkflowSHA)
	fill(&ci.SourceRepositoryURI, e.GithubWorkflowRepository)
	fill(&ci.SourceRepositoryRef, e.GithubWorkflowRef)
	if *ci == (signatures.SignerCI{Issuer: ci.Issuer}) {
		return nil // a certificate with an issuer and no CI claims: the issuer is OIDCIssuer already
	}
	return ci
}

// certificateKeyIDs are the key IDs of a chain (R-SIG-005): the lower-case hex SHA-256 of every certificate's DER, and the leaf's
// "<issuer DN>|<serial decimal>".
func certificateKeyIDs(certs []*x509.Certificate) []string {
	var out []string
	for _, c := range certs {
		s := sha256.Sum256(c.Raw)
		out = append(out, hex.EncodeToString(s[:]))
	}
	leaf := certs[0]
	return append(out, leaf.Issuer.String()+"|"+leaf.SerialNumber.String())
}

// hintKeyID is the key ID of a bundle's key hint: the lower-case hex of the SHA-256 the hint names when it is that SHA-256 in base64 (as
// cosign and sigstore-go write it), else the hint itself.
func hintKeyID(hint string) string {
	if b, ok := decodeBase64(hint); ok && len(b) == sha256.Size {
		return hex.EncodeToString(b)
	}
	if b, err := hex.DecodeString(hint); err == nil && len(b) == sha256.Size {
		return strings.ToLower(hint)
	}
	return hint
}

// keyID is the lower-case hex SHA-256 of a public key's PKIX DER: the key ID of a raw key (R-SIG-005).
func keyID(pub crypto.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return ""
	}
	s := sha256.Sum256(der)
	return hex.EncodeToString(s[:])
}

// hintMatches reports whether a bundle's key hint names a key: as the base64 or hex of its SHA-256, or as its npm keyid.
func hintMatches(hint string, k crypto.PublicKey, sum, npmKeyID string) bool {
	if hint == "" {
		return false
	}
	if sum == "" {
		sum = keyID(k)
	}
	if hintKeyID(hint) == sum || hint == npmKeyID {
		return true
	}
	if b, err := hex.DecodeString(sum); err == nil {
		return hint == base64.StdEncoding.EncodeToString(b)
	}
	return false
}

// algorithmName names a public key's algorithm as the signer shows it ("ECDSA P-256", "RSA 4096", "Ed25519").
func algorithmName(pub crypto.PublicKey) string {
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		return "ECDSA " + k.Curve.Params().Name
	case *rsa.PublicKey:
		return fmt.Sprintf("RSA %d", k.N.BitLen())
	case ed25519.PublicKey:
		return "Ed25519"
	}
	return ""
}

// hashName names the hash of the signature: a message signature's digest algorithm, a DSSE envelope's SHA-256.
func hashName(o *object) string {
	if len(o.digest) == 0 {
		return "SHA-256"
	}
	switch o.digestAlg {
	case protocommon.HashAlgorithm_SHA2_256:
		return "SHA-256"
	case protocommon.HashAlgorithm_SHA2_384:
		return "SHA-384"
	case protocommon.HashAlgorithm_SHA2_512:
		return "SHA-512"
	}
	return o.digestAlg.String()
}

// logEntries are the transparency-log entries the bundle carries, as shown.
func logEntries(o *object) []signatures.LogEntry {
	entries, err := o.b.TlogEntries()
	if err != nil {
		return nil
	}
	var out []signatures.LogEntry
	for _, e := range entries {
		le := signatures.LogEntry{LogID: hex.EncodeToString([]byte(e.LogKeyID())), Index: e.LogIndex(), Proof: "promise"}
		if t := e.IntegratedTime(); !t.IsZero() {
			le.IntegratedTime = t.UTC()
		}
		if e.HasInclusionProof() {
			le.Proof = "proof"
		}
		out = append(out, le)
	}
	return out
}

// timestampInfos are the RFC 3161 timestamps the bundle carries, as shown: the time each states and its authority.
func timestampInfos(o *object) []signatures.TimestampInfo {
	stamps, err := o.b.Timestamps()
	if err != nil {
		return nil
	}
	var out []signatures.TimestampInfo
	for _, raw := range stamps {
		t, err := parseStamp(raw)
		if err != nil {
			continue
		}
		out = append(out, signatures.TimestampInfo{Time: t.time.UTC(), Authority: t.authority()})
	}
	return out
}
