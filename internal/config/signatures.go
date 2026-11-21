package config

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Signatures is the signatures section (signatures.md R-SIG-040): the switch, the job's keys (R-SIG-011), the bounds on hostile
// signature containers (R-SIG-013, Q-SIG-45), the background fetch of remote repositories (R-SIG-026), the refresh of the public Sigstore
// root (R-SIG-017) and the anchors the configuration declares (R-SIG-018). The queue signatures has its slots in jobs.workers.signatures.
type Signatures struct {
	Enabled        bool          `yaml:"enabled" default:"true" doc:"false: no signature is detected, checked or shown with a badge; the stored rows stay as they are and are used again when the switch is turned back on (R-SIG-040)."`
	Concurrency    int           `yaml:"concurrency" default:"4" validate:"min=1,max=64" doc:"Checks the one job of the installation has in flight at a time (R-SIG-011). At least 1: with none no signature would ever be checked."`
	RunSlice       time.Duration `yaml:"run_slice" default:"10m" validate:"min=10s,max=24h" doc:"How long one job keeps its slot on the queue signatures before the rest of the work goes to the next job; a check in flight is finished first (R-SIG-011)."`
	MaxAttempts    int           `yaml:"max_attempts" default:"5" validate:"min=1,max=100" doc:"Attempts at a check that could not run (the object store, a timeout), with a doubling back-off from one minute; after the last the signature is failed, and the discovery of the task signature-verify makes it due once a day (R-SIG-011)."`
	CheckTimeout   time.Duration `yaml:"check_timeout" default:"5s" validate:"min=100ms,max=5m" doc:"The longest one check may take; a check past it spends an attempt, and after the last attempt the signature is failed (R-SIG-013)."`
	MaxObjectSize  ByteSize      `yaml:"max_object_size" default:"256KiB" validate:"min=1KiB,max=1MiB" doc:"Largest signature object that is read; a larger one is not read and is malformed with limits_exceeded (R-SIG-013). At most 1MiB: every real signature seen is below 20 KiB, and the worker that reads them holds the installation's secrets (H-1166)."`
	MaxPerSubject  int           `yaml:"max_per_subject" default:"32" validate:"min=1,max=1000" doc:"Signatures read per subject and repository; the rest is not read, and the details say how many of how many were read (R-SIG-013)."`
	MaxChain       int           `yaml:"max_chain" default:"8" validate:"min=1,max=32" doc:"Certificates in one chain, each of at most 16 KiB; a longer chain is malformed with limits_exceeded (R-SIG-013)."`
	MaxContentSize ByteSize      `yaml:"max_content_size" default:"8GiB" validate:"min=1MiB" doc:"Largest content a keyless scheme or a NuGet signature reads to compare its digest, streamed from the blob store and never written to disk; larger content leaves the signature unsupported with limits_exceeded (R-SIG-013)."`

	Remote    SignaturesRemote    `yaml:"remote"`
	TrustRoot SignaturesTrustRoot `yaml:"trust_root"`

	// Anchors is keyed by the anchor's key, which is unique in the scope configuration (R-SIG-015), the way auth.oidc.providers is keyed
	// by the provider ID (Q-AUTH-30): a map of structs may nest, a list element may not (R-ARCH-020).
	Anchors map[string]SignatureAnchor `yaml:"anchors" default:"{}" doc:"Trust anchors declared here, keyed by the anchor's key (R-SIG-018): quader migrate applies them with the scope configuration in one transaction, creates what is declared, removes what is no longer declared, and stops with the key and the reason when the material fails the checks of R-SIG-015. The API and the UI show them read-only. Each entry names title, kind and exactly the block of its kind (ca, openpgp, public_key, sigstore); material is given inline or as a file (certificates_file, keys_file, pem_file, trusted_root_file), never both. The material is public: no private key is accepted. In the chart, signatures.anchors renders this map. As an environment variable the map is JSON, for example QUADER_SIGNATURES_ANCHORS='{\"company-ca\":{\"title\":\"Company CA\",\"kind\":\"ca\",\"ca\":{\"certificates_file\":\"/etc/quader/signature-anchors/company-ca/certificates.pem\"}}}'."`
}

// SignaturesRemote is the background fetch of signatures through remote repositories (R-SIG-026, Q-SIG-33).
type SignaturesRemote struct {
	MaxManifestsPerSubject int           `yaml:"max_manifests_per_subject" default:"4" validate:"min=0,max=64" doc:"OCI signature and attestation manifests fetched per image in the background, signature types first (R-SIG-026); each GET of one counts against the upstream's pull limit where the upstream has one. 0 fetches none."`
	RefreshAfter           time.Duration `yaml:"refresh_after" default:"24h" validate:"min=1m,max=8760h" doc:"An image's signatures are asked for again when a client's revalidation finds its tag unchanged and the last fetch is older than this, so a signature added upstream after the image was cached arrives within this span of use (R-SIG-026)."`
	RequestsPerHour        int           `yaml:"requests_per_hour" default:"600" validate:"min=1,max=1000000" doc:"Background requests for signatures one replica sends to one remote per hour; what is above it waits for the next hour (R-SIG-026, H-1173). A client's own requests do not count."`
}

// SignaturesTrustRoot is the refresh of the public Sigstore root and npm's registry keys (R-SIG-017).
type SignaturesTrustRoot struct {
	Refresh bool   `yaml:"refresh" default:"true" doc:"true: the task signature-trust-root fetches the public Sigstore trusted root and npm's registry keys from url once a day, verified from the TUF root embedded in the release. false: the release's snapshot holds and ages with the release (H-1170); QuaderSignatureTrustRootStale fires 14 days after the release was built."`
	URL     string `yaml:"url" default:"https://tuf-repo-cdn.sigstore.dev" doc:"The TUF repository the task reads, through the outbound client and its egress rules: the public one, or a TUF mirror of the company for an installation without internet. https only: the chain is verified from the embedded TUF root either way, and TLS keeps the request and its answer private."`
}

// SignatureAnchor is one entry of signatures.anchors (R-SIG-015, R-SIG-018). An element of a map is decoded as it is written, so a key
// left out is the zero value and a block left out is nil. The shape is checked here; the material is checked by quader migrate with the
// rules of R-SIG-015 (internal/signatures/anchors), which reads the files.
type SignatureAnchor struct {
	Title     string                    `yaml:"title" json:"title"`
	Kind      string                    `yaml:"kind" json:"kind"`
	CA        *SignatureAnchorCA        `yaml:"ca" json:"ca,omitempty"`
	OpenPGP   *SignatureAnchorOpenPGP   `yaml:"openpgp" json:"openpgp,omitempty"`
	PublicKey *SignatureAnchorPublicKey `yaml:"public_key" json:"public_key,omitempty"`
	Sigstore  *SignatureAnchorSigstore  `yaml:"sigstore" json:"sigstore,omitempty"`
}

// SignatureAnchorCA is the block of a CA anchor: 1 to 16 PEM certificates, inline or in a file, the use, and the optional restriction of
// the leaf's subject (Q-SIG-30).
type SignatureAnchorCA struct {
	Certificates     string            `yaml:"certificates" json:"certificates,omitempty"`
	CertificatesFile string            `yaml:"certificates_file" json:"certificates_file,omitempty"`
	Use              string            `yaml:"use" json:"use,omitempty"`
	Subject          map[string]string `yaml:"subject" json:"subject,omitempty"`
	SubjectAltNames  []string          `yaml:"subject_alt_names" json:"subject_alt_names,omitempty"`
}

// SignatureAnchorOpenPGP is the block of an OpenPGP anchor: 1 to 32 armored public certificates, inline or in a file.
type SignatureAnchorOpenPGP struct {
	Keys     string `yaml:"keys" json:"keys,omitempty"`
	KeysFile string `yaml:"keys_file" json:"keys_file,omitempty"`
}

// SignatureAnchorPublicKey is the block of a public-key anchor: 1 to 16 keys.
type SignatureAnchorPublicKey struct {
	Keys []SignatureAnchorKey `yaml:"keys" json:"keys"`
}

// SignatureAnchorKey is one public key: the PEM inline or in a file, npm's keyid where it has one, and when the key stops signing
// (RFC 3339).
type SignatureAnchorKey struct {
	PEM     string `yaml:"pem" json:"pem,omitempty"`
	PEMFile string `yaml:"pem_file" json:"pem_file,omitempty"`
	KeyID   string `yaml:"keyid" json:"keyid,omitempty"`
	Expires string `yaml:"expires" json:"expires,omitempty"`
}

// SignatureAnchorSigstore is the block of a Sigstore identity anchor: the OIDC issuer, the identity as an exact value or a pattern, and
// the trust root (Q-SIG-27): public, the shipped public root, or own, a trusted_root.json of the company's Sigstore inline or in a file.
type SignatureAnchorSigstore struct {
	Issuer          string `yaml:"issuer" json:"issuer"`
	Identity        string `yaml:"identity" json:"identity,omitempty"`
	IdentityPattern string `yaml:"identity_pattern" json:"identity_pattern,omitempty"`
	TrustRoot       string `yaml:"trust_root" json:"trust_root,omitempty"`
	TrustedRoot     string `yaml:"trusted_root" json:"trusted_root,omitempty"`
	TrustedRootFile string `yaml:"trusted_root_file" json:"trusted_root_file,omitempty"`
}

// The kinds of a declared anchor (R-SIG-015) and the trust roots of a Sigstore anchor (Q-SIG-27), as the configuration spells them.
const (
	SignatureAnchorKindCA        = "ca"
	SignatureAnchorKindOpenPGP   = "openpgp"
	SignatureAnchorKindPublicKey = "public-key"
	SignatureAnchorKindSigstore  = "sigstore"

	SignatureTrustRootPublic = "public"
	SignatureTrustRootOwn    = "own"
)

// The bounds of a declared anchor's shape (R-SIG-015): the key and the title as signature_anchors stores them, and the keys of a
// public-key anchor.
const (
	maxSignatureAnchorKey   = 64
	maxSignatureAnchorTitle = 128
	maxSignatureAnchorKeys  = 16
)

// signatureAnchorKeyPattern is the rule of signature_anchors.key (R-SIG-015).
var signatureAnchorKeyPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// EffectiveTrustRoot is the trust root the anchor names: own when a trusted root is given inline or as a file, else public; an explicit
// trust_root wins and is checked against the material by [Signatures.Validate].
func (s SignatureAnchorSigstore) EffectiveTrustRoot() string {
	switch {
	case s.TrustRoot != "":
		return s.TrustRoot
	case s.TrustedRoot != "" || s.TrustedRootFile != "":
		return SignatureTrustRootOwn
	}
	return SignatureTrustRootPublic
}

// Validate checks the cross-field rules of the section (R-SIG-040, R-SIG-018): the trust root's URL is an https URL, and every declared
// anchor has a valid key and title, a kind, exactly its kind's block and its material from exactly one place. The bounds of the keys are
// the loader's (the validate tags); the material is checked by quader migrate.
func (s *Signatures) Validate(r *Report) {
	checkTrustRootURL(r, "trust_root.url", s.TrustRoot.URL)
	for _, key := range sortedMapKeys(s.Anchors) {
		s.Anchors[key].validate(r, key)
	}
}

// checkTrustRootURL refuses a TUF repository URL that is not an https URL with a host.
func checkTrustRootURL(r *Report, key, raw string) {
	u, err := url.Parse(raw)
	switch {
	case err != nil || !strings.HasPrefix(raw, "https://") || u.Host == "":
		r.Add(key, "%q must be an https URL with a host, such as https://tuf-repo-cdn.sigstore.dev or a TUF mirror of the company (R-SIG-017)", redactUserinfo(raw))
	case u.User != nil:
		r.Add(key, "%q must not carry a user or a password", redactUserinfo(raw))
	}
}

// anchorBlock is one kind's block of a declared anchor: the kind, the block's key and whether it is set.
type anchorBlock struct {
	kind, name string
	set        bool
}

// validate checks one declared anchor; key is the map key, the anchor's key.
func (a SignatureAnchor) validate(r *Report, key string) {
	at := "anchors." + key
	if len(key) > maxSignatureAnchorKey || !signatureAnchorKeyPattern.MatchString(key) {
		r.Add(at, "the key must match ^[a-z0-9]+(-[a-z0-9]+)*$ and have at most %d characters (R-SIG-015)", maxSignatureAnchorKey)
	}
	if n := utf8.RuneCountInString(a.Title); n < 1 || n > maxSignatureAnchorTitle || strings.TrimSpace(a.Title) == "" {
		r.Add(at+".title", "must have 1 to %d characters (has %d): it is the name shown at a signature (R-SIG-015)", maxSignatureAnchorTitle, n)
	}
	blocks := []anchorBlock{
		{SignatureAnchorKindCA, "ca", a.CA != nil},
		{SignatureAnchorKindOpenPGP, "openpgp", a.OpenPGP != nil},
		{SignatureAnchorKindPublicKey, "public_key", a.PublicKey != nil},
		{SignatureAnchorKindSigstore, "sigstore", a.Sigstore != nil},
	}
	if !slices.ContainsFunc(blocks, func(b anchorBlock) bool { return b.kind == a.Kind }) {
		r.Add(at+".kind", "%q is none of ca, openpgp, public-key and sigstore (R-SIG-015)", a.Kind)
		return
	}
	for _, b := range blocks {
		switch {
		case b.kind == a.Kind && !b.set:
			r.Add(at+"."+b.name, "is required for the kind %s: it holds the anchor's material", a.Kind)
		case b.kind != a.Kind && b.set:
			r.Add(at+"."+b.name, "must not be set for the kind %s: an anchor holds the block of its own kind only", a.Kind)
		}
	}
	switch {
	case a.CA != nil && a.Kind == SignatureAnchorKindCA:
		a.CA.validate(r, at+".ca")
	case a.OpenPGP != nil && a.Kind == SignatureAnchorKindOpenPGP:
		oneSource(r, at+".openpgp", "keys", a.OpenPGP.Keys, a.OpenPGP.KeysFile)
	case a.PublicKey != nil && a.Kind == SignatureAnchorKindPublicKey:
		a.PublicKey.validate(r, at+".public_key")
	case a.Sigstore != nil && a.Kind == SignatureAnchorKindSigstore:
		a.Sigstore.validate(r, at+".sigstore")
	}
}

// validate checks a CA block: the certificates from one place, a known use, and a subject restriction of the attributes O, OU, CN and C.
func (c SignatureAnchorCA) validate(r *Report, at string) {
	oneSource(r, at, "certificates", c.Certificates, c.CertificatesFile)
	switch c.Use {
	case "", "codeSigning", "timestamping":
	default:
		r.Add(at+".use", "%q is neither codeSigning (the default) nor timestamping (Q-SIG-30)", c.Use)
	}
	for _, attr := range sortedMapKeys(c.Subject) {
		switch attr {
		case "O", "OU", "CN", "C":
		default:
			r.Add(at+".subject", "names the attribute %q; a subject restriction names O, OU, CN or C (Q-SIG-30)", attr)
		}
		if strings.TrimSpace(c.Subject[attr]) == "" {
			r.Add(at+".subject."+attr, "is empty: the attribute must match the leaf's subject exactly")
		}
	}
	for _, p := range c.SubjectAltNames {
		if strings.TrimSpace(p) == "" {
			r.Add(at+".subject_alt_names", "holds an empty pattern")
		}
	}
}

// validate checks a public-key block: 1 to 16 keys, each with its PEM from one place and an expiry in RFC 3339 where it has one.
func (p SignatureAnchorPublicKey) validate(r *Report, at string) {
	if n := len(p.Keys); n < 1 || n > maxSignatureAnchorKeys {
		r.Add(at+".keys", "must hold 1 to %d keys (holds %d) (R-SIG-015)", maxSignatureAnchorKeys, n)
	}
	for i, k := range p.Keys {
		ka := fmt.Sprintf("%s.keys[%d]", at, i)
		oneSource(r, ka, "pem", k.PEM, k.PEMFile)
		if k.Expires != "" {
			if _, err := time.Parse(time.RFC3339, k.Expires); err != nil {
				r.Add(ka+".expires", "%q is no RFC 3339 time, such as 2027-01-01T00:00:00Z", k.Expires)
			}
		}
	}
}

// validate checks a Sigstore block: an issuer, and a trust root that agrees with the material given: own needs a trusted root from one
// place, public takes none. The issuer's form and the identity are checked with the material (R-SIG-015).
func (s SignatureAnchorSigstore) validate(r *Report, at string) {
	if strings.TrimSpace(s.Issuer) == "" {
		r.Add(at+".issuer", "is required: the OIDC issuer, an https URL")
	}
	switch s.EffectiveTrustRoot() {
	case SignatureTrustRootOwn:
		oneSource(r, at, "trusted_root", s.TrustedRoot, s.TrustedRootFile)
	case SignatureTrustRootPublic:
		if s.TrustedRoot != "" || s.TrustedRootFile != "" {
			r.Add(at+".trust_root", "is public, but a trusted root is given: leave trust_root out or set it to own for the company's root")
		}
	default:
		r.Add(at+".trust_root", "%q is neither public nor own (Q-SIG-27)", s.TrustRoot)
	}
}

// oneSource requires material from exactly one place: the inline key name or the file key name_file, which must be an absolute path.
func oneSource(r *Report, at, name, inline, file string) {
	switch {
	case inline != "" && file != "":
		r.Add(at+"."+name, "and %s_file are both set: give the material inline or as a file, not both", name)
	case inline == "" && file == "":
		r.Add(at+"."+name, "or %s_file is required", name)
	case file != "" && (!path.IsAbs(file) || path.Clean(file) != file):
		r.Add(at+"."+name+"_file", "%q must be a clean absolute path", file)
	}
}
