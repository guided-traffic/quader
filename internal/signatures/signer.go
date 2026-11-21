package signatures

import "time"

// Signer is what the read takes from a signature (R-SIG-005), stored as signatures.signer (jsonb, at most 64 KiB) and shown by the API
// (R-SIG-034). Every value is the signature's claim until the integrity check and the trust step confirm it. A scheme fills what it
// carries; the rest stays empty. Text is kept as text: the API and the screens never render it as markup.
type Signer struct {
	// Subject is a certificate's subject distinguished name, or an OpenPGP key's user ID once a key is known.
	Subject string `json:"subject,omitempty"`
	// Issuer is the issuing certificate's subject distinguished name, or the authority that issued a Sigstore certificate.
	Issuer string `json:"issuer,omitempty"`
	// Identity is a Sigstore certificate's subject alternative name (a URI or an e-mail), or a certificate's first alternative name.
	Identity string `json:"identity,omitempty"`
	// OIDCIssuer is the OIDC issuer a Sigstore certificate names (Fulcio's extension 1.3.6.1.4.1.57264.1.8 or .1).
	OIDCIssuer string `json:"oidcIssuer,omitempty"`
	// AlternativeNames are every subject alternative name of the leaf.
	AlternativeNames []string `json:"alternativeNames,omitempty"`
	// Serial is the leaf's serial number, decimal.
	Serial string `json:"serial,omitempty"`
	// CertificateNotBefore and CertificateNotAfter are the leaf's validity window.
	CertificateNotBefore time.Time `json:"certificateNotBefore,omitzero"`
	CertificateNotAfter  time.Time `json:"certificateNotAfter,omitzero"`
	// Chain are the subjects of the chain the signature carries, leaf first.
	Chain []string `json:"chain,omitempty"`
	// CI is a CI job's claims a Sigstore certificate carries in Fulcio's extensions
	// (https://github.com/sigstore/fulcio/blob/main/docs/oid-info.md).
	CI *SignerCI `json:"ci,omitempty"`
	// KeyID is an OpenPGP issuer key ID or fingerprint, npm's keyid, or the SHA-256 of a raw public key; Fingerprint is an OpenPGP key's
	// full fingerprint once known.
	KeyID       string `json:"keyId,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	// Algorithm is the signature's public-key algorithm ("ECDSA P-256", "RSA 4096", "Ed25519"); HashAlgorithm its hash ("SHA-256").
	Algorithm     string `json:"algorithm,omitempty"`
	HashAlgorithm string `json:"hashAlgorithm,omitempty"`
	// Log are the transparency-log entries a Sigstore bundle carries.
	Log []LogEntry `json:"log,omitempty"`
	// Timestamps are the RFC 3161 timestamps the signature carries.
	Timestamps []TimestampInfo `json:"timestamps,omitempty"`
	// PredicateType and StatementSubjects are an attestation's: the in-toto statement's predicate type and the names of its subjects.
	PredicateType     string   `json:"predicateType,omitempty"`
	StatementSubjects []string `json:"statementSubjects,omitempty"`
	// SigningScheme is Notation's (notary.x509, notary.x509.signingAuthority); SigningAgent its signed attribute; Expiry its expiry.
	SigningScheme string    `json:"signingScheme,omitempty"`
	SigningAgent  string    `json:"signingAgent,omitempty"`
	Expiry        time.Time `json:"expiry,omitzero"`
	// CommitmentType is NuGet's: "author" (proof of origin) or "repository" (proof of receipt); ServiceIndex and Owners a repository
	// signature's.
	CommitmentType string   `json:"commitmentType,omitempty"`
	ServiceIndex   string   `json:"serviceIndex,omitempty"`
	Owners         []string `json:"owners,omitempty"`
	// DockerReference is the image reference an older cosign payload names (critical.identity.docker-reference): shown, judged by nothing.
	DockerReference string `json:"dockerReference,omitempty"`
	// SchemeName names a scheme Quader does not read where it can tell it (Docker Content Trust, minisign).
	SchemeName string `json:"schemeName,omitempty"`
	// Detail is a sentence about why a state was reached, in plain words ("timestamp by an authority no trust anchor knows").
	Detail string `json:"detail,omitempty"`
	// Cut says what the read cut at the bounds of R-SIG-013.
	Cut string `json:"cut,omitempty"`
}

// SignerCI is a CI job's claims from a Sigstore certificate.
type SignerCI struct {
	Issuer                 string `json:"issuer,omitempty"`
	BuildSignerURI         string `json:"buildSignerUri,omitempty"`
	BuildSignerDigest      string `json:"buildSignerDigest,omitempty"`
	RunnerEnvironment      string `json:"runnerEnvironment,omitempty"`
	SourceRepositoryURI    string `json:"sourceRepositoryUri,omitempty"`
	SourceRepositoryDigest string `json:"sourceRepositoryDigest,omitempty"`
	SourceRepositoryRef    string `json:"sourceRepositoryRef,omitempty"`
	BuildConfigURI         string `json:"buildConfigUri,omitempty"`
	BuildTrigger           string `json:"buildTrigger,omitempty"`
	RunInvocationURI       string `json:"runInvocationUri,omitempty"`
	SourceRepositoryOwner  string `json:"sourceRepositoryOwner,omitempty"`
	// SourceRepositoryIdentifier and SourceRepositoryOwnerIdentifier are the numeric identifiers of the repository and its owner,
	// stable across renames (1.15, 1.17); BuildConfigDigest the digest of the build configuration (1.19); WorkflowName the deprecated
	// workflow name (1.4); SourceRepositoryVisibility the repository's visibility when it signed (1.22).
	SourceRepositoryIdentifier      string `json:"sourceRepositoryIdentifier,omitempty"`
	SourceRepositoryOwnerIdentifier string `json:"sourceRepositoryOwnerIdentifier,omitempty"`
	BuildConfigDigest               string `json:"buildConfigDigest,omitempty"`
	WorkflowName                    string `json:"workflowName,omitempty"`
	SourceRepositoryVisibility      string `json:"sourceRepositoryVisibility,omitempty"`
}

// LogEntry is one transparency-log entry.
type LogEntry struct {
	LogID          string    `json:"logId,omitempty"`
	Index          int64     `json:"index"`
	IntegratedTime time.Time `json:"integratedTime,omitzero"`
	// Proof is "promise" (a signed entry timestamp) or "proof" (an inclusion proof).
	Proof string `json:"proof,omitempty"`
}

// TimestampInfo is one RFC 3161 timestamp as shown.
type TimestampInfo struct {
	Time      time.Time `json:"time"`
	Authority string    `json:"authority,omitempty"`
}
