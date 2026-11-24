package formathost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/mirror"
	"github.com/guided-traffic/quader/internal/mirror/discover"
	"github.com/guided-traffic/quader/internal/proxy"
	"github.com/guided-traffic/quader/internal/repository"
)

// The upstream the catalogue is lent for one call (remote-curation.md R-CUR-008, Q-CUR-13): a discover.Upstream over the remote's client
// that answers a document from the repository's stored copy (fresh or stale, never revalidated) or reads it from the upstream without
// storing it (proxy.Engine.Peruse), refuses an artifact, and reaches only the upstream, its extraHosts, the mirror's hosts of the format
// and the preset's catalogue hosts, the last two without a credential. Every request to the network first spends a unit of the
// catalogue's budget (Spend), so a stored document costs nothing; a document read twice within the call is read once.

// catalogueFlow marks the context of a catalogue call: the format host builds the request of a document as a cache view and the delivery
// rules refuse nothing, since the catalogue shows a blocked package marked (R-CUR-009).
type catalogueFlow struct{}

// CatalogueLend is what the catalogue asks of the upstream it is lent.
type CatalogueLend struct {
	// MaxBytes bounds a document or an answer (proxy.catalogue.max_document_bytes).
	MaxBytes int64
	// Spend is called before every request to the network; its error is the request's.
	Spend func(ctx context.Context) error
}

// perusor is the remote engine's unstored read of a document; *proxy.Engine implements it.
type perusor interface {
	Peruse(ctx context.Context, rq proxy.Request, g proxy.Get, resolve proxy.DocumentResolver) (*proxy.GetResult, error)
}

// CatalogueUpstream lends the upstream of one catalogue call of the remote repository repoID.
func (m *MirrorHost) CatalogueUpstream(ctx context.Context, repoID int64, o CatalogueLend) (discover.Upstream, error) {
	if m.h.o.Repos == nil {
		return nil, errs.E(errs.NotImplemented, errs.CodeNotImplemented, "This process has no repository reader for the upstream catalogue.")
	}
	repo, err := m.h.o.Repos.ByID(ctx, repoID)
	if err != nil {
		return nil, err
	}
	if repo.Class != repository.Remote {
		return nil, errs.E(errs.Invalid, errs.CodeNotApplicable, "Only a remote repository has an upstream catalogue.")
	}
	rem, err := proxy.NewRemote(repo)
	if err != nil {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "The repository is not a usable remote repository.", errs.WithCause(err))
	}
	share := proxy.BackgroundShare(rem.Settings.Limits.MaxConcurrentFetches, rem.Settings.Mirror.Concurrency)
	pu, err := m.newPassUpstream(repo, rem, mirror.Pass{Ceiling: share}, func(base *url.URL) []string { return catalogueHosts(m, repo, base) })
	if err != nil {
		return nil, err
	}
	max := o.MaxBytes
	if max <= 0 {
		max = proxy.DiscoveryDocumentMaxBytes
	}
	return &catalogueUpstream{pu: pu, max: max, spend: o.Spend, memo: map[string]memoDocument{}}, nil
}

// catalogueHosts are the hosts besides the upstream's and its extraHosts that a catalogue call reaches, none of which receives the
// repository's credential: the mirror's hosts of the format (npm's replication host, Q-NPM-72) and the catalogue hosts of the preset
// whose URL the repository has (R-CUR-008, Q-CUR-15). A repository whose URL is no preset's has no catalogue host.
func catalogueHosts(m *MirrorHost, repo repository.Repository, base *url.URL) []string {
	var hosts []string
	if lister, ok := m.Lister(repo.Format); ok {
		hosts = append(hosts, lister.MirrorHosts(base)...)
	}
	if facts, ok := catalog.Default().Lookup(repo.Format); ok {
		if p, ok := facts.PresetFor(base.String()); ok {
			for _, h := range p.CatalogueHosts {
				if !slices.Contains(hosts, h.Pattern) {
					hosts = append(hosts, h.Pattern)
				}
			}
		}
	}
	return hosts
}

// catalogueUpstream is the [discover.Upstream] of one catalogue call.
type catalogueUpstream struct {
	pu    *passUpstream
	max   int64
	spend func(context.Context) error
	mu    sync.Mutex
	memo  map[string]memoDocument
}

type memoDocument struct {
	doc   []byte
	found bool
}

var (
	_ discover.Upstream       = (*catalogueUpstream)(nil)
	_ discover.Header         = (*catalogueUpstream)(nil)
	_ discover.RepositoryIDer = (*catalogueUpstream)(nil)
)

// Base implements [discover.Upstream].
func (c *catalogueUpstream) Base() *url.URL { return c.pu.Base() }

// Settings implements [discover.Upstream].
func (c *catalogueUpstream) Settings() json.RawMessage { return c.pu.Settings() }

// RepositoryID implements [discover.RepositoryIDer].
func (c *catalogueUpstream) RepositoryID() int64 { return c.pu.RepositoryID() }

func (c *catalogueUpstream) bound(max int64) int64 {
	if max <= 0 || max > c.max {
		return c.max
	}
	return max
}

func (c *catalogueUpstream) pay(ctx context.Context) error {
	if c.spend == nil {
		return nil
	}
	return c.spend(ctx)
}

// Get implements [discover.Upstream]: one request, paid for, within the catalogue's bound.
func (c *catalogueUpstream) Get(ctx context.Context, target *url.URL, accept string, max int64) (*discover.Response, error) {
	if err := c.pay(ctx); err != nil {
		return nil, err
	}
	res, err := c.pu.Get(ctx, target, accept, c.bound(max))
	return res, catalogueError(c.pu.repo, err)
}

// Head implements [discover.Header].
func (c *catalogueUpstream) Head(ctx context.Context, target *url.URL, accept string) (*discover.Response, error) {
	if err := c.pay(ctx); err != nil {
		return nil, err
	}
	res, err := c.pu.Head(ctx, target, accept)
	return res, catalogueError(c.pu.repo, err)
}

// Document implements [discover.Upstream]: the stored copy, whatever its age, else the upstream's answer, which is not stored. found
// is false when neither has the document (the upstream answered 404 or 410).
func (c *catalogueUpstream) Document(ctx context.Context, path, variant string) ([]byte, bool, error) {
	key := path + "\x00" + variant
	c.mu.Lock()
	if d, ok := c.memo[key]; ok {
		c.mu.Unlock()
		return d.doc, d.found, nil
	}
	c.mu.Unlock()
	ctx = context.WithValue(ctx, catalogueFlow{}, true)
	doc, found, err := c.pu.stored(ctx, path, variant)
	if err != nil {
		return nil, false, catalogueError(c.pu.repo, err)
	}
	if found {
		if int64(len(doc)) > c.max {
			return nil, false, tooLargeForCatalogue(c.max)
		}
	} else if doc, found, err = c.read(ctx, path, variant); err != nil {
		return nil, false, err
	}
	c.mu.Lock()
	c.memo[key] = memoDocument{doc: doc, found: found}
	c.mu.Unlock()
	return doc, found, nil
}

// read reads a document from the upstream without storing it.
func (c *catalogueUpstream) read(ctx context.Context, path, variant string) ([]byte, bool, error) {
	pe, ok := c.pu.m.e.(perusor)
	if !ok {
		return nil, false, errs.E(errs.NotImplemented, errs.CodeNotImplemented, "This process's remote engine cannot read a document without storing it.")
	}
	p := formats.Path{Path: path}
	rq := c.pu.m.request(ctx, c.pu.repo, p)
	preq, err := (&remote{h: c.pu.m.h, e: c.pu.m.e}).request(ctx, rq, p, variant, proxy.Metadata)
	if err != nil {
		return nil, false, err
	}
	if err := c.pay(ctx); err != nil {
		return nil, false, err
	}
	caps := formats.RemoteCapabilities(formats.ID(c.pu.repo.Format))
	res, err := pe.Peruse(ctx, preq, proxy.Get{
		Max: c.max, MirrorHosts: c.pu.hosts, Ceiling: c.pu.pass.Ceiling,
		Auth:     handshakeOf(caps, &c.pu.repo, c.pu.settings, nil),
		Exchange: exchangerOf(caps, &c.pu.repo, c.pu.settings),
	}, c.Document)
	if err != nil {
		return nil, false, catalogueError(c.pu.repo, err)
	}
	switch res.Status {
	case http.StatusOK:
		return res.Body, true, nil
	case http.StatusNotFound, http.StatusGone:
		return nil, false, nil
	}
	return nil, false, errs.E(errs.UpstreamFailed, errs.CodeUpstreamError,
		fmt.Sprintf("The upstream of %s answered %d to the document %s.", c.pu.repo.Name, res.Status, path))
}

// Artifact implements [discover.Upstream]: the catalogue stores nothing, so it reads no artifact (R-CUR-008).
func (c *catalogueUpstream) Artifact(context.Context, string, int64) ([]byte, bool, error) {
	return nil, false, errs.E(errs.Internal, errs.CodeInternal, "The upstream catalogue stores nothing in the cache, so it reads no artifact.")
}

// catalogueError turns the outcomes of a background request into the catalogue's answers: an open circuit is 503
// upstream_assumed_offline, a pre-emption by a client's fetch 503 with a short Retry-After, the offline mode 409 offline. A rate limit
// stays the typed error, which the engine answers 429; anything else stays as it is.
func catalogueError(repo repository.Repository, err error) error {
	if err == nil {
		return nil
	}
	var w *mirror.WaitError
	var pw *proxy.BackgroundWait
	switch {
	case errors.As(err, &w):
		return errs.E(errs.Unavailable, errs.CodeUpstreamAssumedOffline,
			"The upstream of "+repo.Name.String()+" is assumed to be offline after repeated failures; the catalogue sends nothing until the circuit closes.",
			errs.WithRetryAfter(max(w.After, time.Second)))
	case errors.As(err, &pw):
		return errs.E(errs.Unavailable, errs.CodeUpstreamAssumedOffline,
			"The upstream of "+repo.Name.String()+" is assumed to be offline after repeated failures; the catalogue sends nothing until the circuit closes.",
			errs.WithRetryAfter(max(pw.After, time.Second)))
	case errors.Is(err, mirror.ErrPreempted), errors.Is(err, proxy.ErrPreempted):
		return errs.E(errs.Unavailable, errs.CodeUnavailable,
			"A client's fetch of "+repo.Name.String()+" took the catalogue's place at the upstream; try again.", errs.WithRetryAfter(time.Second))
	}
	if ee, ok := errs.As(err); ok && ee.Code == errs.CodeNotCachedOffline {
		return errs.E(errs.Conflict, errs.CodeOffline,
			repo.Name.String()+" is offline: the catalogue sends no request to its upstream and shows only what the repository has stored.")
	}
	return err
}

func tooLargeForCatalogue(bound int64) error {
	return errs.E(errs.UpstreamFailed, errs.CodeCatalogueAnswerTooLarge,
		fmt.Sprintf("A document of the upstream is larger than proxy.catalogue.max_document_bytes (%d bytes).", bound))
}
