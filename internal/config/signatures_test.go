package config

import (
	"strings"
	"testing"
	"time"
)

func loadSignatures(t *testing.T, file string, env ...string) (*Loaded, error) {
	t.Helper()
	return Load(Options{File: "/c.yaml", ReadFile: fakeFS(map[string]string{"/c.yaml": file}), Environ: minimalEnv(env...), Command: CommandMigrate})
}

// TestR_SIG_040_TheKeysHaveTheirDefaults: every key of R-SIG-040 with its default, no declared anchor, and the queue signatures with one
// slot, claimed by a process that names no queue.
func TestR_SIG_040_TheKeysHaveTheirDefaults(t *testing.T) {
	l, err := loadSignatures(t, "")
	if err != nil {
		t.Fatal(err)
	}
	s := l.Config.Signatures
	if !s.Enabled || s.Concurrency != 4 || s.RunSlice != 10*time.Minute || s.MaxAttempts != 5 || s.CheckTimeout != 5*time.Second {
		t.Errorf("job %+v", s)
	}
	if s.MaxObjectSize != 256*KiB || s.MaxPerSubject != 32 || s.MaxChain != 8 || s.MaxContentSize != 8*GiB {
		t.Errorf("bounds %+v", s)
	}
	if r := s.Remote; r.MaxManifestsPerSubject != 4 || r.RefreshAfter != 24*time.Hour || r.RequestsPerHour != 600 {
		t.Errorf("remote %+v", r)
	}
	if tr := s.TrustRoot; !tr.Refresh || tr.URL != "https://tuf-repo-cdn.sigstore.dev" {
		t.Errorf("trust_root %+v", tr)
	}
	if len(s.Anchors) != 0 {
		t.Errorf("anchors %+v", s.Anchors)
	}
	if l.Config.Jobs.Workers.Signatures != 1 || !l.Config.Jobs.Claims("signatures") {
		t.Errorf("jobs %+v", l.Config.Jobs.Workers)
	}
	if k, ok := MustDescribe().ByPath("signatures.anchors"); !ok || k.Env != "QUADER_SIGNATURES_ANCHORS" {
		t.Errorf("signatures.anchors: %+v", k)
	}
}

// TestR_SIG_040_TheThreeRefusalsNameTheKey: a max_object_size above 1 MiB, a concurrency of 0 and a trust_root.url without https:// are
// refused with the key named; 1 MiB itself and a company's TUF mirror are accepted.
func TestR_SIG_040_TheThreeRefusalsNameTheKey(t *testing.T) {
	for file, key := range map[string]string{
		"signatures:\n  max_object_size: 1025KiB\n":                      "signatures.max_object_size",
		"signatures:\n  concurrency: 0\n":                                "signatures.concurrency",
		"signatures:\n  trust_root:\n    url: http://tuf.example.com\n":  "signatures.trust_root.url",
		"signatures:\n  trust_root:\n    url: tuf.example.com\n":         "signatures.trust_root.url",
		"signatures:\n  trust_root:\n    url: 'https://'\n":              "signatures.trust_root.url",
		"signatures:\n  trust_root:\n    url: https://u:p@tuf.example\n": "signatures.trust_root.url",
		"signatures:\n  max_chain: 0\n":                                  "signatures.max_chain",
		"signatures:\n  check_timeout: 10m\n":                            "signatures.check_timeout",
		"signatures:\n  remote:\n    requests_per_hour: 0\n":             "signatures.remote.requests_per_hour",
		"jobs:\n  queues: [signaturez]\n":                                "jobs.queues",
	} {
		_, err := loadSignatures(t, file)
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%q: %v, want %s named", file, err, key)
		}
		if err != nil && strings.Contains(err.Error(), "u:p@") {
			t.Errorf("%q: the password is printed: %v", file, err)
		}
	}
	l, err := loadSignatures(t, "signatures:\n  max_object_size: 1MiB\n  trust_root:\n    refresh: false\n    url: https://tuf.example.com/sigstore\n")
	if err != nil {
		t.Fatalf("1MiB and a mirror: %v", err)
	}
	if s := l.Config.Signatures; s.MaxObjectSize != MiB || s.TrustRoot.Refresh || s.TrustRoot.URL != "https://tuf.example.com/sigstore" {
		t.Errorf("%+v", s)
	}
}

// TestR_SIG_018_DeclaredAnchorsAreDecodedWithTheirBlocks: the four kinds, material inline and from files, from the file and from the
// environment's JSON, and the trust root a Sigstore anchor names.
func TestR_SIG_018_DeclaredAnchorsAreDecodedWithTheirBlocks(t *testing.T) {
	file := `signatures:
  anchors:
    gitlab-ci-signing:
      title: GitLab CI signing CA
      kind: ca
      ca:
        certificates_file: /etc/quader/signature-anchors/gitlab-ci-signing/certificates.pem
        use: codeSigning
        subject: {O: Example GmbH}
        subject_alt_names: ['https://gitlab\.example\.com/.+']
    release-key:
      title: Release key
      kind: openpgp
      openpgp:
        keys: |
          -----BEGIN PGP PUBLIC KEY BLOCK-----
          -----END PGP PUBLIC KEY BLOCK-----
    npm-mirror:
      title: npm mirror keys
      kind: public-key
      public_key:
        keys:
          - pem_file: /etc/quader/signature-anchors/npm-mirror/key-0.pem
            keyid: SHA256:abc
            expires: 2027-01-01T00:00:00Z
    company-sigstore:
      title: Company Sigstore
      kind: sigstore
      sigstore:
        issuer: https://gitlab.example.com
        identity_pattern: '^https://gitlab\.example\.com/platform/.+$'
        trusted_root_file: /etc/quader/signature-anchors/company-sigstore/trusted_root.json
    public-sigstore:
      title: Public Sigstore, our CI
      kind: sigstore
      sigstore:
        issuer: https://token.actions.githubusercontent.com
        identity: https://github.com/example/app/.github/workflows/release.yml@refs/heads/main
`
	l, err := loadSignatures(t, file)
	if err != nil {
		t.Fatal(err)
	}
	a := l.Config.Signatures.Anchors
	if len(a) != 5 {
		t.Fatalf("anchors %+v", a)
	}
	ca := a["gitlab-ci-signing"]
	if ca.Kind != "ca" || ca.CA == nil || ca.CA.CertificatesFile == "" || ca.CA.Subject["O"] != "Example GmbH" || len(ca.CA.SubjectAltNames) != 1 {
		t.Errorf("ca %+v %+v", ca, ca.CA)
	}
	if pgp := a["release-key"]; pgp.OpenPGP == nil || !strings.Contains(pgp.OpenPGP.Keys, "BEGIN PGP PUBLIC KEY BLOCK") {
		t.Errorf("openpgp %+v", pgp)
	}
	if pk := a["npm-mirror"]; pk.PublicKey == nil || len(pk.PublicKey.Keys) != 1 || pk.PublicKey.Keys[0].KeyID != "SHA256:abc" {
		t.Errorf("public-key %+v", pk)
	}
	if own := a["company-sigstore"].Sigstore; own == nil || own.EffectiveTrustRoot() != SignatureTrustRootOwn {
		t.Errorf("own root %+v", own)
	}
	if pub := a["public-sigstore"].Sigstore; pub == nil || pub.EffectiveTrustRoot() != SignatureTrustRootPublic {
		t.Errorf("public root %+v", pub)
	}
	if l.Sources["signatures.anchors"].Kind != "file" {
		t.Errorf("source %+v", l.Sources["signatures.anchors"])
	}

	env := `QUADER_SIGNATURES_ANCHORS={"company-ca":{"title":"Company CA","kind":"ca","ca":{"certificates_file":"/etc/quader/signature-anchors/company-ca/certificates.pem"}}}`
	l, err = loadSignatures(t, "", env)
	if err != nil {
		t.Fatalf("from the environment: %v", err)
	}
	if got := l.Config.Signatures.Anchors["company-ca"]; got.CA == nil || got.Title != "Company CA" {
		t.Errorf("from the environment: %+v", got)
	}
}

// TestR_SIG_018_TheShapeOfADeclaredAnchorIsChecked: the key, the title, the kind, exactly the kind's block, material from exactly one
// place, and the forms the configuration checks itself, each refused with the key named. The material's own checks are quader migrate's.
func TestR_SIG_018_TheShapeOfADeclaredAnchorIsChecked(t *testing.T) {
	const ca = "ca: {certificates_file: /etc/a/c.pem}"
	for _, tc := range []struct{ name, entry, want string }{
		{"a key with capitals", "Company_CA: {title: t, kind: ca, " + ca + "}", "signatures.anchors.Company_CA the key must match"},
		{"a key of 65 characters", strings.Repeat("a", 65) + ": {title: t, kind: ca, " + ca + "}", "at most 64 characters"},
		{"no title", "a: {kind: ca, " + ca + "}", "signatures.anchors.a.title must have 1 to 128 characters"},
		{"a title of 129 characters", "a: {title: " + strings.Repeat("x", 129) + ", kind: ca, " + ca + "}", "signatures.anchors.a.title"},
		{"an unknown kind", "a: {title: t, kind: x509, " + ca + "}", `signatures.anchors.a.kind "x509" is none of`},
		{"the kind's block missing", "a: {title: t, kind: openpgp}", "signatures.anchors.a.openpgp is required for the kind openpgp"},
		{"another kind's block", "a: {title: t, kind: ca, " + ca + ", openpgp: {keys: k}}", "signatures.anchors.a.openpgp must not be set for the kind ca"},
		{"inline and a file", "a: {title: t, kind: ca, ca: {certificates: c, certificates_file: /etc/a/c.pem}}", "signatures.anchors.a.ca.certificates and certificates_file are both set"},
		{"no material", "a: {title: t, kind: openpgp, openpgp: {}}", "signatures.anchors.a.openpgp.keys or keys_file is required"},
		{"a relative file", "a: {title: t, kind: openpgp, openpgp: {keys_file: keys.asc}}", "signatures.anchors.a.openpgp.keys_file"},
		{"an unknown use", "a: {title: t, kind: ca, ca: {certificates: c, use: serverAuth}}", "signatures.anchors.a.ca.use"},
		{"a subject attribute that is not restricted", "a: {title: t, kind: ca, ca: {certificates: c, subject: {L: Berlin}}}", `names the attribute "L"`},
		{"no public key", "a: {title: t, kind: public-key, public_key: {keys: []}}", "signatures.anchors.a.public_key.keys must hold 1 to 16 keys"},
		{"a key without its PEM", "a: {title: t, kind: public-key, public_key: {keys: [{keyid: k}]}}", "signatures.anchors.a.public_key.keys[0].pem or pem_file is required"},
		{"an expiry that is no time", "a: {title: t, kind: public-key, public_key: {keys: [{pem: p, expires: tomorrow}]}}", "signatures.anchors.a.public_key.keys[0].expires"},
		{"a Sigstore anchor without issuer", "a: {title: t, kind: sigstore, sigstore: {identity: i}}", "signatures.anchors.a.sigstore.issuer is required"},
		{"an own root without its document", "a: {title: t, kind: sigstore, sigstore: {issuer: 'https://i', identity: i, trust_root: own}}", "signatures.anchors.a.sigstore.trusted_root or trusted_root_file is required"},
		{"a public root with a document", "a: {title: t, kind: sigstore, sigstore: {issuer: 'https://i', identity: i, trust_root: public, trusted_root: '{}'}}", "signatures.anchors.a.sigstore.trust_root is public, but a trusted root is given"},
		{"an unknown root", "a: {title: t, kind: sigstore, sigstore: {issuer: 'https://i', identity: i, trust_root: corp}}", `signatures.anchors.a.sigstore.trust_root "corp" is neither public nor own`},
		{"an unknown member", "a: {title: t, kind: ca, ca: {certificate: c}}", "certificate"},
		{"a list instead of a map", "- {title: t}", "signatures.anchors"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadSignatures(t, "signatures:\n  anchors:\n    "+tc.entry+"\n")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%v, want %q", err, tc.want)
			}
		})
	}
}

// TestR_SIG_076_TheGateKeysHaveTheirDefaultsAndBounds: the four keys of signatures.gate with their defaults, and each refusal of the
// validation with the key named; the edges of each bound are accepted.
func TestR_SIG_076_TheGateKeysHaveTheirDefaultsAndBounds(t *testing.T) {
	l, err := loadSignatures(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if g := l.Config.Signatures.Gate; g.DownloadWait != 25*time.Second || g.MaxWaiting != 512 || g.PreviewLimit != 100000 ||
		g.RefusalsRetention != 720*time.Hour {
		t.Errorf("gate %+v", g)
	}
	for file, key := range map[string]string{
		"signatures:\n  gate:\n    download_wait: 61s\n":        "signatures.gate.download_wait",
		"signatures:\n  gate:\n    max_waiting: 0\n":            "signatures.gate.max_waiting",
		"signatures:\n  gate:\n    max_waiting: 65537\n":        "signatures.gate.max_waiting",
		"signatures:\n  gate:\n    preview_limit: 999\n":        "signatures.gate.preview_limit",
		"signatures:\n  gate:\n    preview_limit: 10000001\n":   "signatures.gate.preview_limit",
		"signatures:\n  gate:\n    refusals_retention: 59m\n":   "signatures.gate.refusals_retention",
		"signatures:\n  gate:\n    refusals_retention: 8761h\n": "signatures.gate.refusals_retention",
	} {
		if _, err := loadSignatures(t, file); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%q: %v, want %s named", file, err, key)
		}
	}
	l, err = loadSignatures(t, "signatures:\n  gate:\n    download_wait: 0s\n    max_waiting: 65536\n    preview_limit: 1000\n    refusals_retention: 1h\n")
	if err != nil {
		t.Fatalf("the edges: %v", err)
	}
	if g := l.Config.Signatures.Gate; g.DownloadWait != 0 || g.MaxWaiting != 65536 || g.PreviewLimit != 1000 || g.RefusalsRetention != time.Hour {
		t.Errorf("edges %+v", g)
	}
}
