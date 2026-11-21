package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/formathost"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/packages"
	"github.com/guided-traffic/quader/internal/qname"
	sigstore "github.com/guided-traffic/quader/internal/signatures/store"
	"github.com/guided-traffic/quader/internal/signing"
	"github.com/guided-traffic/quader/internal/signing/attest"
	"github.com/guided-traffic/quader/internal/signing/job"
	"github.com/guided-traffic/quader/internal/signing/statement"
	signingstore "github.com/guided-traffic/quader/internal/signing/store"
	"github.com/guided-traffic/quader/internal/vuln"
	"github.com/guided-traffic/quader/internal/vuln/report"
	vulnstore "github.com/guided-traffic/quader/internal/vuln/store"
)

// The receipt attestations of the job signing (signatures.md R-SIG-051 to R-SIG-054): the hooks the storage and the malware scanning tell
// the job through on every role, and on a role that runs jobs the kind attestation, which asks the item's format through a format host and
// reads a version's vulnerability evaluation through the report of the scanning.

// attestationHooks are the hooks of the receipt attestations, one per replica: the formats' storage hooks and the malware store take them,
// whichever area is wired first.
type attestationHooks struct {
	once sync.Once
	h    *attest.Hooks
}

func (a *App) attestHooks() *attest.Hooks {
	a.attestations.once.Do(func() {
		c := a.cfg.Signing.Attestations
		// signing.enabled false writes no work, as for npm's signatures (Q-SIG-158); discovery attests later what it left.
		a.attestations.h = attest.NewHooks(a.cfg.Signing.Enabled && c.Enabled, c.Fills, job.Wake)
	})
	return a.attestations.h
}

// attestationWork is formathost.AttestationWork over the hooks of the receipt attestations.
type attestationWork struct{ h *attest.Hooks }

func (w attestationWork) Committed(ctx context.Context, tx meta.Tx, repoID, itemID int64, fill bool) error {
	return w.h.Committed(ctx, tx, repoID, itemID, fill)
}

func (w attestationWork) SignatureTagArrived(ctx context.Context, tx meta.Tx, repoID int64, subjects [][32]byte) ([]formathost.DisplacedReferrer, error) {
	got, err := w.h.SignatureTagArrived(ctx, tx, repoID, subjects)
	out := make([]formathost.DisplacedReferrer, len(got))
	for i, u := range got {
		out[i] = formathost.DisplacedReferrer{Path: u.Path, Blob: u.Blob, Manifest: u.Manifest, Bundle: u.Bundle}
	}
	return out, err
}

// attestationKind builds the kind attestation of the job signing on a role that runs jobs: the attester, whose format host and
// vulnerability report are found at its first operation, because the job is built before the areas that own them.
func (a *App) attestationKind(in infra) (job.Kind, error) {
	c := a.cfg.Signing.Attestations
	at, err := attest.New(attest.Options{
		DB: in.db, Formats: &attestFormats{a: a, in: in}, Vulns: &attestVulns{a: a, in: in},
		Signatures: sigstore.New(sigstore.Options{Runner: in.db}), Enabled: c.Enabled, Fills: c.Fills,
		WaitForChecks: c.WaitForChecks, MalwareScanning: a.cfg.Malware.Enabled, BaseURL: a.cfg.Server.BaseURL,
		Log: a.log.With("component", "signing"),
	})
	if err != nil {
		return job.Kind{}, err
	}
	return job.Kind{
		Name: signing.WorkAttestation,
		Sign: func(ctx context.Context, tx meta.Tx, s job.Session, w signingstore.Work) error {
			return at.Sign(ctx, tx, s.Identity, s.Key.Signer(), w)
		},
		Discover: at.Discover,
	}, nil
}

// attestFormats is attest.Formats over the replica's format host: the one of the client path where the role builds it, else a host of
// the item store and the data ports without an engine, which a worker builds for the signing at its first operation.
type attestFormats struct {
	a    *App
	in   infra
	once sync.Once
	h    *formathost.Host
	err  error
}

var errNoAttestationHost = errors.New("signing: this replica has no item store and data ports to attest with")

func (f *attestFormats) host() (*formathost.Host, error) {
	if h, ok := f.a.host.get(); ok {
		if fh, ok := h.(*formathost.Host); ok {
			return fh, nil
		}
	}
	f.once.Do(func() {
		fa, fm, repo := part[filesArea](f.a.parts), part[formatsArea](f.a.parts), part[repositoryArea](f.a.parts)
		if fa.items == nil || fm.deps.Bindings == nil || repo.reader == nil {
			f.err = errNoAttestationHost
			return
		}
		pkgs, err := packages.NewStore(f.in.db)
		if err != nil {
			f.err = err
			return
		}
		f.h, f.err = formathost.New(formathost.Options{
			Runner: f.in.db, Decider: fa.decider, Items: fa.items, Packages: pkgs, Deps: fm.deps, Repos: repo.reader,
			Log: f.a.log.With("component", "signing"),
		})
	})
	return f.h, f.err
}

func repoRef(r attest.Repo) formats.RepoRef {
	return formats.RepoRef{ID: r.ID, Name: qname.Repo{Project: r.Project, Key: r.Key}, Class: formats.Class(r.Class), Format: formats.ID(r.Format)}
}

func attestItem(it attest.Item) formats.AttestationItem {
	return formats.AttestationItem{Path: it.Path, Blob: it.Blob, Size: it.Size}
}

func formatSubject(s attest.Subject) formats.AttestationSubject {
	return formats.AttestationSubject{Attested: s.Attested, Name: s.Name, Version: s.Version, MediaType: s.MediaType, Scanned: s.Scanned}
}

// Subjects implements attest.Formats.
func (f *attestFormats) Subjects(ctx context.Context, repo attest.Repo, its []attest.Item) ([]attest.Subject, error) {
	h, err := f.host()
	if err != nil {
		return nil, err
	}
	in := make([]formats.AttestationItem, len(its))
	for i, it := range its {
		in[i] = attestItem(it)
	}
	got, err := h.AttestationSubjects(ctx, repoRef(repo), in)
	if err != nil {
		return nil, err
	}
	out := make([]attest.Subject, len(got))
	for i, s := range got {
		out[i] = attest.Subject{Attested: s.Attested, Name: s.Name, Version: s.Version, MediaType: s.MediaType, Scanned: s.Scanned}
	}
	return out, nil
}

// Placement implements attest.Formats.
func (f *attestFormats) Placement(ctx context.Context, repo attest.Repo, it attest.Item) (attest.Placement, error) {
	h, err := f.host()
	if err != nil {
		return attest.Placement{}, err
	}
	refer, reason, err := h.ReferrerPlacement(ctx, repoRef(repo), attestItem(it))
	return attest.Placement{Refer: refer, Reason: reason}, err
}

// PutReferrer implements attest.Formats.
func (f *attestFormats) PutReferrer(ctx context.Context, repo attest.Repo, sub attest.Subject, it attest.Item, bundle []byte, issuedAt time.Time) ([32]byte, error) {
	h, err := f.host()
	if err != nil {
		return [32]byte{}, err
	}
	return h.PutReferrer(ctx, repoRef(repo), formatSubject(sub), attestItem(it), bundle, statement.PredicateType, issuedAt)
}

// RemoveReferrer implements attest.Formats.
func (f *attestFormats) RemoveReferrer(ctx context.Context, tx meta.Tx, repo attest.Repo, it attest.Item, manifest, bundle [32]byte) error {
	h, err := f.host()
	if err != nil {
		return err
	}
	return h.RemoveReferrer(ctx, tx, repoRef(repo), attestItem(it), manifest, bundle)
}

// attestVulns is attest.Vulnerabilities over the report of the vulnerability scanning (R-VULN-001, R-VULN-038), built at its first use
// with a scope that reads every repository: the job attests what the installation received, not what a caller may read.
type attestVulns struct {
	a    *App
	in   infra
	once sync.Once
	rep  *report.Service
	err  error
}

func (v *attestVulns) report() (*report.Service, error) {
	v.once.Do(func() {
		repo, vz := part[repositoryArea](v.a.parts), part[vulnArea](v.a.parts)
		if repo.reader == nil {
			v.err = errors.New("signing: no repository reader for the vulnerability report")
			return
		}
		cfg := v.a.cfg.Vulnerabilities
		v.rep, v.err = report.New(report.Options{
			Store: vulnstore.NewAPI(v.in.db), Repositories: repo.reader, Advisories: vz.advisories, Rule: vz.rule, Images: vulnstore.NewImages(v.in.db),
			Config: report.Config{
				Enabled: cfg.Enabled, Enforcement: vuln.Enforcement(cfg.Enforcement), StaleAfter: cfg.StaleAfter,
				PublicUpstreams: cfg.Matching.PublicUpstreams, ImagesEnabled: cfg.Images.Enabled, Ecosystems: cfg.Sources.OSV.EffectiveEcosystems(),
			},
		})
	})
	return v.rep, v.err
}

// Version implements attest.Vulnerabilities: the version's evaluation in its repository, its grade, its open findings per severity and
// the date of its data; pending while it is pending_scan or pending_data.
func (v *attestVulns) Version(ctx context.Context, repo attest.Repo, name, version string) (statement.Vulnerabilities, bool, error) {
	rep, err := v.report()
	if err != nil {
		return statement.Vulnerabilities{}, false, err
	}
	sn, err := rep.Snapshot(ctx)
	if err != nil {
		return statement.Vulnerabilities{}, false, err
	}
	key := qname.Repo{Project: repo.Project, Key: repo.Key}
	sc, err := rep.PackageScope(ctx, sn, report.Caller{Reads: readsEverything{}}, repo.Format, name, version)
	if err != nil {
		return statement.Vulnerabilities{}, false, err
	}
	rvs, found, err := rep.Version(ctx, sc, repo.Format, name, version)
	if err != nil || !found {
		return statement.Vulnerabilities{State: string(vuln.EvaluationNotCovered)}, false, err
	}
	for _, rv := range rvs {
		if rv.Repository != key.String() {
			continue
		}
		out := statement.Vulnerabilities{State: string(rv.Evaluation)}
		if g := rv.Grade; g.Grade != "" && (rv.Evaluation == vuln.EvaluationEvaluated || rv.Evaluation == vuln.EvaluationPendingData) {
			out.Grade = string(g.Grade)
			out.Counts = &statement.Counts{
				Critical: int(g.Open.Critical), High: int(g.Open.High), Medium: int(g.Open.Medium), Low: int(g.Open.Low), Unknown: int(g.Open.Unknown),
			}
			if g.CheckedAt != nil {
				out.DataAt = *g.CheckedAt
			}
		}
		pending := rv.Evaluation == vuln.EvaluationPendingScan || rv.Evaluation == vuln.EvaluationPendingData
		return out, pending, nil
	}
	return statement.Vulnerabilities{State: string(vuln.EvaluationNotCovered)}, false, nil
}

// readsEverything is the read scope of the job signing's question to the report: every repository whole.
type readsEverything struct{}

func (readsEverything) Shape(qname.Repo) authz.ReadShape            { return authz.ReadAll }
func (readsEverything) MayRead(qname.Repo, string, string) bool     { return true }
func (readsEverything) MayReadWiki(qname.Repo, string, string) bool { return true }
func (readsEverything) VersionsByPath(qname.Repo) bool              { return false }
func (readsEverything) VersionIndependent() bool                    { return true }
