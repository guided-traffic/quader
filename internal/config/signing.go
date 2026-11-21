package config

import (
	"path"
	"time"
)

// Signing is the signing section (signatures.md R-SIG-058): whether Quader signs, its signing identity from the files of the Helm values
// (R-SIG-046) with the reload and the announce period (R-SIG-048), and the bounds of the job signing (R-SIG-049). The queue signing has
// its slots in jobs.workers.signing. The keys of the receipt attestations join the section with the package that builds them.
type Signing struct {
	Enabled     bool            `yaml:"enabled" default:"true" doc:"false: Quader signs nothing, also with an identity; the job signing takes no work, the identities stay manageable, and what is stored stays and is served (R-SIG-058)."`
	Identity    SigningIdentity `yaml:"identity"`
	Concurrency int             `yaml:"concurrency" default:"4" validate:"min=1,max=64" doc:"Signing operations the one job of the installation has in flight at a time (R-SIG-049)."`
	MaxAttempts int             `yaml:"max_attempts" default:"5" validate:"min=1,max=20" doc:"Attempts at an operation that could not run (the database, the object store), with a doubling back-off from one minute; after the last the work is failed, signing.failed is recorded once an hour per kind and code, and the job makes it due again once a day (R-SIG-049)."`
}

// SigningIdentity is the signing identity of the Helm values (R-SIG-046): the files of a Secret the chart mounts into the worker pods
// alone, and how a change of them reaches the installation (R-SIG-048). Only the roles worker and all read the three files.
type SigningIdentity struct {
	CertFile       string        `yaml:"cert_file" default:"" doc:"PEM file of the identity's certificate and its intermediates, the leaf first (tls.crt of a cert-manager Secret). Empty: the identity comes from the admin area (R-SIG-047). While set, the identity of the files is the installation's: uploads and key requests are refused with 409 signing_identity_managed. The chart sets /var/run/secrets/quader/signing/tls.crt while signing.identity.existingSecret or signing.identity.certManager names one (R-HA-071)."`
	KeyFile        string        `yaml:"key_file" default:"" secret:"file" doc:"PEM file of the identity's ECDSA P-256 private key (tls.key), unencrypted PKCS#8 or SEC 1. Read by the worker alone, held in its memory and never stored (R-SIG-046). Set with cert_file or not at all; the chart sets /var/run/secrets/quader/signing/tls.key."`
	CAFile         string        `yaml:"ca_file" default:"" doc:"PEM file of the root the chain ends in (ca.crt), when tls.crt does not end in it; it may hold intermediates too. Only with cert_file; the chart sets /var/run/secrets/quader/signing/ca.crt."`
	ReloadInterval time.Duration `yaml:"reload_interval" default:"1m" validate:"min=1s,max=1h" doc:"How often a worker reads the three files again and compares their content (R-SIG-046): a pair cert-manager renewed is announced within this interval, without a restart (Q-SIG-151)."`
	AnnouncePeriod time.Duration `yaml:"announce_period" default:"10m" validate:"min=0s,max=24h" doc:"How long a new identity beside the current one is listed at npm's keys endpoint before it signs, so that a cache of the keys document learns the key before a signature made with it arrives (R-SIG-048). 0s: a new identity is current at once."`
}

// Validate checks the cross-field rules of the section (R-SIG-058): cert_file and key_file both or neither, ca_file only with them, each
// a clean absolute path.
func (s *Signing) Validate(r *Report) {
	id := s.Identity
	switch {
	case (id.CertFile == "") != (id.KeyFile == ""):
		r.Add("identity.cert_file", "and identity.key_file are set both or neither: the identity of the files is a certificate and its key (R-SIG-046)")
	case id.CAFile != "" && id.CertFile == "":
		r.Add("identity.ca_file", "is set without identity.cert_file: the root belongs to the certificate of the files (R-SIG-046)")
	}
	for _, f := range []struct{ key, file string }{
		{"identity.cert_file", id.CertFile}, {"identity.key_file", id.KeyFile}, {"identity.ca_file", id.CAFile},
	} {
		if f.file != "" && (!path.IsAbs(f.file) || path.Clean(f.file) != f.file) {
			r.Add(f.key, "%q must be a clean absolute path", f.file)
		}
	}
}

// Files reports whether the configuration names the identity's files: the Helm values name the installation's identity (R-SIG-046).
func (s SigningIdentity) Files() bool { return s.CertFile != "" }
