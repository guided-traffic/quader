package proxy

import (
	"context"
	"net/url"

	"github.com/guided-traffic/quader/internal/errs"
)

// The upstream catalogue's read of a document (remote-curation.md R-CUR-008, Q-CUR-13): a metadata document of a remote repository read
// from the upstream without storing it. The catalogue reads the stored copy where the repository holds one (the cache view, which never
// contacts the upstream) and asks Peruse for the rest: the format's own upstream request is built as a fetch would build it, with the
// URL map and the discovery document, and sent as a discovery request ([Engine.Get]): the egress rules, the TLS verification, the
// redirect rules and the credential policy apply, it takes a background slot that yields to clients (R-REPO-055), it reads the circuit
// and never moves it, and nothing of its answer is written: no cache entry, no item, no negative entry, no URL-map row.

// DocumentResolver answers a metadata document of the repository for the discovery document a format's addressing follows (NuGet's
// service index): the stored copy or an unstored read of it, as the caller reads every document.
type DocumentResolver func(ctx context.Context, path, variant string) (doc []byte, found bool, err error)

// Peruse reads the metadata document rq names from the upstream without storing it. g carries what a discovery request carries (the
// format's handshake and exchange, the hosts besides the upstream's, the bound of the body); its URL and Accept are the format's request's.
// resolve answers the discovery document of a format that follows one. The answer is the upstream's, whatever its status, for the
// caller to judge; a rate limit is a *upstream.RateLimitError, an open circuit a *BackgroundWait, the offline mode not_cached_offline.
func (e *Engine) Peruse(ctx context.Context, rq Request, g Get, resolve DocumentResolver) (*GetResult, error) {
	if rq.Adapter == nil {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "proxy: a catalogue read needs the format's adapter")
	}
	rem := rq.Remote
	adapter := rq.Adapter
	if path, variant, ok := adapter.DiscoveryDocument(); ok && (path != rq.Path || variant != rq.Variant) {
		doc, found, err := resolve(ctx, path, variant)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, errs.E(errs.UpstreamFailed, errs.CodeUpstreamError,
				"The upstream of "+rem.Repo.Name.String()+" has no discovery document, so the addresses of its resources are unknown.")
		}
		bound, err := adapter.Discover(ctx, doc)
		if err != nil {
			return nil, asUpstreamError(err)
		}
		adapter = bound
	}
	cl, err := e.o.Clients.For(ctx, rem.Repo, rem.Settings)
	if err != nil {
		return nil, err
	}
	req, err := adapter.UpstreamRequest(ctx, UpstreamBase{URL: cl.Origin()}, rq.Path, rq.Variant)
	if err != nil {
		return nil, err
	}
	target := req.URL
	if mapped, err := e.upstreamURL(ctx, rem.id, rq.Path); err != nil {
		return nil, err
	} else if mapped != "" {
		u, err := url.Parse(mapped)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, errs.E(errs.UpstreamFailed, errs.CodeUpstreamError, "The recorded upstream URL of the document is not usable.")
		}
		target = u
	}
	g.Remote, g.URL, g.Method = rem, target, ""
	if a := req.Header.Get("Accept"); a != "" {
		g.Accept = a
	}
	return e.Get(ctx, g)
}
