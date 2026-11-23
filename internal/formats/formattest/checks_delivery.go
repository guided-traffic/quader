package formattest

import (
	"context"
	"net/http"
	"strings"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
	"github.com/guided-traffic/quader/internal/repository"
)

// CheckDelivery is check 23 (remote-curation.md R-CUR-003, R-CUR-004, R-CUR-022), for a format with the class remote: against a remote
// repository whose rules refuse one version of the fixture's package, no document of the format lists that version and the version's
// file answers 403 with Cache-Control: no-store in the format's shape; against one whose rules refuse the package whole, every document
// of the package answers that refusal. Checks 21 and 22 of R-ARCH-010 were taken (the package shapes, the upstream catalogue), so the
// delivery rules are the next free number (R-ARCH-010).
const CheckDelivery Check = 23

// DeliveryFixture is the fixture of check 23: a remote repository's upstream as an in-memory fake, a package with at least two
// versions in it, and the requests that read the package's documents and the refused version's file.
type DeliveryFixture struct {
	// Remote returns the fake upstream: the package's documents and the files of both versions, by canonical path. A new one is made
	// for every request the check sends.
	Remote func() *MemRemote
	// Package is the normalized name of the package; Refused the version the check's rule refuses, Kept one it keeps. The check reads
	// a document's body for Refused as text, so it is chosen not to occur in the documents otherwise (no other version or digest
	// contains it).
	Package, Refused, Kept string
	// Documents are the escaped request paths of every document of the format that lists the package's versions (a package document, a
	// version list, a tag list, a registration): each answers 200 without Refused under the version rule, and the refusal under the
	// package rule.
	Documents []string
	// Passthrough are the escaped request paths of documents the remote passes unchanged because the upstream signs them (a Debian
	// remote's InRelease and Packages, Q-CUR-9): each answers 200 with exactly the fake upstream's bytes under both rules, and the
	// filtered view of such a format is a virtual repository over the remote. A fixture names Documents, Passthrough or both.
	Passthrough []string
	// Artifact is the escaped request path of a file of the refused version.
	Artifact string
	// Seed stores what the format reads besides the fake upstream (a remote's parsed rows), in a Host built for the request; nil
	// stores nothing.
	Seed func(h *Host) error
}

// DeliveryRules are the delivery mode and rules of the kit's remote repository (HostOptions.Delivery); the rules are in their
// canonical forms.
type DeliveryRules struct {
	Mode  rc.Mode
	Rules []rules.Rule
}

// withDelivery compiles the options' rules into the Host's repository, with the format's name rule and version canon, and makes the
// in-memory remote answer step 5a of the remote engine (R-REPO-025): a document or a file that the rules do not deliver is refused
// before the fake upstream is read, as formathost refuses it before the engine asks the upstream.
func (h *Host) withDelivery(o HostOptions) {
	if o.Delivery == nil || o.Class != formats.Remote {
		return
	}
	d := h.f.Descriptor()
	canon := rules.Canon{}
	if d.Names != nil {
		canon.Name = d.Names.Normalize
	}
	if d.VersionCanon != nil {
		canon.Version = d.VersionCanon.Canonical
	}
	repo, err := repository.WithRules(*h.repo, o.Delivery.Mode, o.Delivery.Rules, canon)
	if err != nil {
		panic("formattest: the delivery rules do not compile: " + err.Error())
	}
	h.repo = &repo
	if o.Remote != nil {
		h.opts.Remote = rulesRemote{RemoteFetcher: o.Remote, f: h.f}
	}
}

// rulesRemote is step 5a over a remote fetcher of the kit.
type rulesRemote struct {
	formats.RemoteFetcher
	f formats.Format
}

func (r rulesRemote) refusal(rq *formats.Request, p formats.Path, kind rules.RefusalKind) error {
	info := r.f.Classify(p)
	if info.Package == "" {
		return nil
	}
	return formats.Undelivered(rq, info.Package, info.Version, kind)
}

func (r rulesRemote) Metadata(ctx context.Context, rq *formats.Request, p formats.Path, variant string) (*formats.RemoteDocument, error) {
	if err := r.refusal(rq, p, rules.KindDocument); err != nil {
		return nil, err
	}
	return r.RemoteFetcher.Metadata(ctx, rq, p, variant)
}

func (r rulesRemote) EnsureCurrent(ctx context.Context, rq *formats.Request, p formats.Path, variant string) (*formats.RemoteDocument, formats.Currency, error) {
	if err := r.refusal(rq, p, rules.KindDocument); err != nil {
		return nil, formats.Unknown, err
	}
	return r.RemoteFetcher.EnsureCurrent(ctx, rq, p, variant)
}

func (r rulesRemote) Artifact(ctx context.Context, rq *formats.Request, p formats.Path, d formats.Digests, w http.ResponseWriter) error {
	if err := r.refusal(rq, p, rules.KindArtifact); err != nil {
		return err
	}
	return r.RemoteFetcher.Artifact(ctx, rq, p, d, w)
}

// delivery is check 23.
func (r *run) delivery() {
	fx := r.fx.Delivery
	if fx == nil {
		r.failf("the format has the class remote and no Delivery fixture: check 23 cannot hold its documents to the delivery rules")
		return
	}
	if fx.Remote == nil || fx.Package == "" || fx.Refused == "" || fx.Kept == "" || len(fx.Documents)+len(fx.Passthrough) == 0 || fx.Artifact == "" {
		r.failf("the Delivery fixture needs Remote, Package, Refused, Kept, Documents or Passthrough, and Artifact")
		return
	}
	version := &DeliveryRules{Mode: rc.AllExceptBlocked, Rules: []rules.Rule{{Kind: rc.KindVersion, Name: fx.Package, Version: r.canonical(fx.Refused), Effect: rc.Block}}}
	whole := &DeliveryRules{Mode: rc.AllExceptBlocked, Rules: []rules.Rule{{Kind: rc.KindPackage, Name: fx.Package, Effect: rc.Block}}}
	for _, p := range fx.Documents {
		resp, ok := r.deliveryRequest(fx, version, p)
		if !ok {
			continue
		}
		switch body := resp.Body.String(); {
		case resp.Code != http.StatusOK:
			r.failf("document %s answered %d under a rule that refuses %s alone, want 200", p, resp.Code, fx.Refused)
		case strings.Contains(body, fx.Refused):
			r.failf("document %s lists %s, which the rules refuse", p, fx.Refused)
		case !strings.Contains(body, fx.Kept):
			r.failf("document %s does not list %s, which the rules deliver", p, fx.Kept)
		}
		resp, ok = r.deliveryRequest(fx, whole, p)
		if ok {
			r.refused(resp, "document "+p+" of a package the rules refuse whole")
		}
	}
	for _, p := range fx.Passthrough {
		var o HostOptions
		if r.fx.Host != nil {
			o = r.fx.Host()
		}
		o.Class = formats.Remote
		rq, err := NewHost(r.f, o).NewRequest(context.Background(), http.MethodGet, p, nil)
		if err != nil {
			r.failf("the Delivery fixture's path %s matches no route: %v", p, err)
			continue
		}
		want, stored := fx.Remote().Docs[rq.Path.Path]
		for _, d := range []*DeliveryRules{version, whole} {
			resp, ok := r.deliveryRequest(fx, d, p)
			switch {
			case !ok:
			case !stored:
				r.failf("passthrough document %s is not in the fixture's upstream (Docs[%q])", p, rq.Path.Path)
			case resp.Code != http.StatusOK || resp.Body.String() != string(want):
				r.failf("passthrough document %s answered %d and not the upstream's bytes under the rules: a signed document passes unchanged", p, resp.Code)
			}
		}
	}
	if resp, ok := r.deliveryRequest(fx, version, fx.Artifact); ok {
		r.refused(resp, "the file "+fx.Artifact+" of the refused version")
	}
}

// canonical is the version in the format's canon, as the store keeps a rule's version.
func (r *run) canonical(v string) string {
	if c := r.d.VersionCanon; c != nil {
		if out, ok := c.Canonical(v); ok {
			return out
		}
	}
	return v
}

// deliveryRequest sends a GET of the path to a remote repository with the rules.
func (r *run) deliveryRequest(fx *DeliveryFixture, d *DeliveryRules, path string) (*Response, bool) {
	var o HostOptions
	if r.fx.Host != nil {
		o = r.fx.Host()
	}
	o.Class, o.Remote, o.Delivery = formats.Remote, fx.Remote(), d
	h := NewHost(r.f, o)
	if fx.Seed != nil {
		if err := fx.Seed(h); err != nil {
			r.failf("the Delivery fixture's seed fails: %v", err)
			return nil, false
		}
	}
	rq, err := h.NewRequest(context.Background(), http.MethodGet, path, nil)
	if err != nil {
		r.failf("the Delivery fixture's path %s matches no route: %v", path, err)
		return nil, false
	}
	return h.ServeRequest(rq, false), true
}

// refused holds an answer to the refusal of R-CUR-004: 403, Cache-Control: no-store, and the code or the refusal's text in the body.
func (r *run) refused(resp *Response, what string) {
	body := resp.Body.String()
	switch {
	case resp.Code != http.StatusForbidden:
		r.failf("%s answered %d, want 403 %s", what, resp.Code, errs.CodeNotAllowedInRepository)
	case !strings.Contains(resp.Header().Get("Cache-Control"), "no-store"):
		r.failf("%s answered 403 without Cache-Control: no-store (%q)", what, resp.Header().Get("Cache-Control"))
	case !strings.Contains(body, errs.CodeNotAllowedInRepository) && !strings.Contains(body, "is not allowed in"):
		r.failf("%s answered 403 without the code or the refusal's text: %s", what, snippet(body))
	}
}
