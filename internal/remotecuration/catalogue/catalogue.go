// Package catalogue is the engine of the upstream catalogue of remote repositories (remote-curation.md R-CUR-007 to R-CUR-009, package
// R3 of docs/developer/remote-curation.md): what a person sees of a remote's upstream through the management API.
//
// The engine calls a format's browsing calls (discover.Catalogue) through an upstream the composition root lends it for one call, which
// answers a document from the repository's stored copy or fetches it without storing, and refuses an artifact (R-CUR-008, Q-CUR-13):
// browsing never fills a cache. Around the call it keeps the catalogue's own budget (upstream requests per repository and per person,
// on the cluster limiter), a short per-replica cache of parsed pages, the bounds on untrusted answers (a document's size, the call's
// time, a page's entries, a text's length), and the fallback of a capability whose upstream does not answer as expected (Q-CUR-15).
// Quader's own state of a row (delivery, stored, queued, request) is the API's, added at answer time and never cached.
package catalogue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// The calls, the label call of the metrics.
const (
	CallCaps     = "caps"
	CallSearch   = "search"
	CallChildren = "levels"
	CallPackage  = "package"
)

// The outcomes, the label outcome of quader_remote_catalogue_requests_total (R-CUR-019).
const (
	OutcomeOK            = "ok"
	OutcomeCached        = "cached"
	OutcomeBudget        = "budget"
	OutcomeUpstreamError = "upstream_error"
	OutcomeNotOffered    = "not_offered"
	OutcomeTooLarge      = "too_large"
	OutcomeShape         = "shape"
)

// MaxQueryRunes bounds a search's text after trimming (R-CUR-007).
const MaxQueryRunes = 128

// Settings are proxy.catalogue (R-CUR-020).
type Settings struct {
	// RequestsPerMinute bounds the upstream requests of one repository across replicas; RequestsPerMinutePerPerson those of one person
	// across repositories.
	RequestsPerMinute, RequestsPerMinutePerPerson int
	// CacheBytes bounds the parsed pages a replica keeps, 0 keeps none; CacheTTL is how long a page is kept, 0 keeps none.
	CacheBytes int64
	CacheTTL   time.Duration
	// MaxDocumentBytes bounds a document a call reads; Timeout a call.
	MaxDocumentBytes int64
	Timeout          time.Duration
}

// Target is the repository a call is for and who asks.
type Target struct {
	RepoID int64
	// Repo is the repository's qualified name: the metrics' label and the logs.
	Repo string
	// Format is the repository's format, Upstream its normalized remote.url (part of the cache's key: a changed URL is another upstream).
	Format, Upstream string
	// Person is the caller's principal ID, the bucket of the person's budget; "" for a call that no person makes.
	Person string
}

// Lend is what the engine asks of the upstream it is lent for one call.
type Lend struct {
	// MaxBytes bounds a document or an answer the upstream reads; a larger one fails with upstream_item_too_large or
	// catalogue_answer_too_large.
	MaxBytes int64
	// Spend is called before every request the upstream sends to the network; its error stops the request and is the call's answer.
	// A document answered from the repository's stored copy costs nothing.
	Spend func(ctx context.Context) error
}

// Lender lends the upstream of one call (formathost's catalogue upstream, through the composition root).
type Lender interface {
	Lend(ctx context.Context, t Target, o Lend) (discover.Upstream, error)
}

// Budget takes one unit of a bucket within its window: the cluster limiter (R-ARCH-030) in production, [Window] where none is wired.
type Budget interface {
	Take(ctx context.Context, bucket string) (ok bool, retryAfter time.Duration)
}

// Options build an [Engine].
type Options struct {
	// Catalogues finds a format's browsing calls; ok is false for a format without a remote catalogue.
	Catalogues func(format string) (discover.Catalogue, bool)
	Lender     Lender
	// Settings reads proxy.catalogue at every call.
	Settings func() Settings
	// RepoBudget and PersonBudget take the budget's units; nil takes a window of this process at the settings' values (the share of a
	// replica without Valkey is the limiter's, R-CUR-008).
	RepoBudget, PersonBudget Budget
	Metrics                  *Metrics
	Now                      func() time.Time
	Log                      *slog.Logger
}

// Engine is the catalogue's engine of one process. It is safe for concurrent use.
type Engine struct {
	o     Options
	cache *pageCache
	mu    sync.Mutex
	// off are the capabilities turned off by an answer that was not as expected, until their time (R-CUR-008).
	off map[string]offEntry
}

type offEntry struct {
	reason string
	hint   discover.SearchHint
	until  time.Time
}

// maxOffEntries bounds the capabilities turned off that the engine remembers; at the bound the expired ones go, then all.
const maxOffEntries = 10000

// New builds the engine.
func New(o Options) (*Engine, error) {
	switch {
	case o.Catalogues == nil:
		return nil, errors.New("catalogue: the engine needs the formats' catalogues")
	case o.Lender == nil:
		return nil, errors.New("catalogue: the engine needs the lender of upstreams")
	case o.Settings == nil:
		return nil, errors.New("catalogue: the engine needs its settings")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.RepoBudget == nil {
		o.RepoBudget = NewWindow(func() int { return o.Settings().RequestsPerMinute }, time.Minute, o.Now)
	}
	if o.PersonBudget == nil {
		o.PersonBudget = NewWindow(func() int { return o.Settings().RequestsPerMinutePerPerson }, time.Minute, o.Now)
	}
	return &Engine{o: o, cache: newPageCache(o.Now), off: map[string]offEntry{}}, nil
}

// Caps answers the repository's capabilities (R-CUR-007), with a capability an unexpected answer turned off for its period.
func (e *Engine) Caps(ctx context.Context, t Target) (discover.CatalogueCaps, error) {
	cat, err := e.catalogue(t)
	if err != nil {
		return discover.CatalogueCaps{}, err
	}
	var caps discover.CatalogueCaps
	if err := e.call(ctx, t, CallCaps, nil, &caps, func(ctx context.Context, u discover.Upstream) (any, error) {
		c, err := cat.CatalogueCaps(ctx, u)
		return c, err
	}); err != nil {
		return discover.CatalogueCaps{}, err
	}
	if o, off := e.isOff(t, CallSearch); off && caps.Search != discover.SearchNone {
		caps.Search, caps.SearchHint, caps.SearchWindow = discover.SearchNone, o.hint, 0
	}
	if !caps.SearchHint.Valid() {
		// A format that names no hint, or one the API does not know, shows nothing rather than a code the screen cannot say.
		caps.SearchHint = ""
	}
	caps.LevelsReason = cut(caps.LevelsReason)
	return caps, nil
}

// Search answers one page of a text query (R-CUR-007): 400 catalogue_not_offered where the repository has no search.
func (e *Engine) Search(ctx context.Context, t Target, q discover.SearchQuery) (discover.SearchPage, error) {
	q.Text = strings.TrimSpace(q.Text)
	if n := utf8.RuneCountInString(q.Text); n < 1 || n > MaxQueryRunes {
		return discover.SearchPage{}, errs.E(errs.Invalid, errs.CodeValidation, fmt.Sprintf("A search's text is 1 to %d characters.", MaxQueryRunes),
			errs.WithFields(errs.FieldError{Pointer: "/q", Code: errs.CodeValidation, Detail: "1 to 128 characters"}))
	}
	q.Limit = clamp(q.Limit, discover.MaxSearchHits)
	caps, err := e.Caps(ctx, t)
	if err != nil {
		return discover.SearchPage{}, err
	}
	if caps.Search == discover.SearchNone {
		e.o.Metrics.count(t.Repo, CallSearch, OutcomeNotOffered)
		o, _ := e.isOff(t, CallSearch)
		return discover.SearchPage{}, notOffered(o.reason, "This repository's upstream offers no search; look a package up by its exact name.")
	}
	cat, err := e.catalogue(t)
	if err != nil {
		return discover.SearchPage{}, err
	}
	var page discover.SearchPage
	err = e.call(ctx, t, CallSearch, q, &page, func(ctx context.Context, u discover.Upstream) (any, error) {
		p, err := cat.Search(ctx, u, q)
		if err != nil {
			return nil, err
		}
		p.Hits = cutHits(p.Hits, q.Limit)
		p.Note = cut(p.Note)
		return p, nil
	})
	return page, err
}

// Children answers one page of a level (R-CUR-007): 400 catalogue_not_offered where the repository's upstream lists none.
func (e *Engine) Children(ctx context.Context, t Target, rq discover.LevelRequest) (discover.LevelPage, error) {
	rq.Limit = clamp(rq.Limit, discover.MaxLevelEntries)
	for _, p := range rq.Level.Path {
		if strings.TrimSpace(p) == "" || len(p) > 512 {
			return discover.LevelPage{}, errs.E(errs.Invalid, errs.CodeValidation, "A level's name is not empty and at most 512 bytes.")
		}
	}
	caps, err := e.Caps(ctx, t)
	if err != nil {
		return discover.LevelPage{}, err
	}
	if len(caps.Levels) == 0 && caps.LevelsReason != "" {
		e.o.Metrics.count(t.Repo, CallChildren, OutcomeNotOffered)
		return discover.LevelPage{}, notOffered(caps.LevelsReason, "")
	}
	cat, err := e.catalogue(t)
	if err != nil {
		return discover.LevelPage{}, err
	}
	var page discover.LevelPage
	err = e.call(ctx, t, CallChildren, rq, &page, func(ctx context.Context, u discover.Upstream) (any, error) {
		p, err := cat.Children(ctx, u, rq)
		if err != nil {
			return nil, err
		}
		if len(p.Levels)+len(p.Packages) > rq.Limit {
			p.Packages = p.Packages[:max(0, rq.Limit-len(p.Levels))]
			p.Levels = p.Levels[:min(len(p.Levels), rq.Limit)]
		}
		p.Packages = cutHits(p.Packages, rq.Limit)
		if p.Unknown != nil && len(p.Levels)+len(p.Packages) > 0 {
			p.Unknown = nil // a level that lists something is known, whatever the format said (Q-CUR-165)
		}
		return p, nil
	})
	return page, err
}

// Package looks a package up by its exact name and answers one page of its versions (R-CUR-007): 404 catalogue_not_found when the
// upstream does not have it.
func (e *Engine) Package(ctx context.Context, t Target, name string, rq discover.VersionsRequest) (discover.PackageInfo, error) {
	return e.pkg(ctx, t, name, rq, discover.MaxVersionsOnPage)
}

// Versions reads one page of a package's versions of any size, for a preview that counts them (R-CUR-010): a page of the bulk's bound,
// so that a document is read once where the format holds the list whole.
func (e *Engine) Versions(ctx context.Context, t Target, name string, rq discover.VersionsRequest) (discover.PackageInfo, error) {
	return e.pkg(ctx, t, name, rq, max(rq.Limit, 1))
}

func (e *Engine) pkg(ctx context.Context, t Target, name string, rq discover.VersionsRequest, bound int) (discover.PackageInfo, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 512 {
		return discover.PackageInfo{}, errs.E(errs.Invalid, errs.CodeValidation, "A lookup names a package of at most 512 bytes.",
			errs.WithFields(errs.FieldError{Pointer: "/name", Code: errs.CodeValidation, Detail: "a package name"}))
	}
	rq.Limit = clamp(rq.Limit, bound)
	if len(rq.Filter) > 128 {
		rq.Filter = rq.Filter[:128]
	}
	cat, err := e.catalogue(t)
	if err != nil {
		return discover.PackageInfo{}, err
	}
	var info discover.PackageInfo
	err = e.call(ctx, t, CallPackage, struct {
		Name string
		RQ   discover.VersionsRequest
	}{name, rq}, &info, func(ctx context.Context, u discover.Upstream) (any, error) {
		p, err := cat.Package(ctx, u, name, rq)
		if err != nil {
			return nil, err
		}
		p.Description = discover.CutText(p.Description)
		if len(p.Versions) > rq.Limit {
			p.Versions = p.Versions[:rq.Limit]
		}
		// An attestation manifest is no platform of the image, whichever format's catalogue named it.
		for i := range p.Versions {
			p.Versions[i].Platforms = platformsOf(p.Versions[i].Platforms)
		}
		return p, nil
	})
	if err != nil {
		return discover.PackageInfo{}, err
	}
	if !info.Found {
		return info, errs.E(errs.NotFound, errs.CodeCatalogueNotFound, "The upstream of "+t.Repo+" does not have a package named "+name+".")
	}
	return info, nil
}

// catalogue finds the format's browsing calls.
func (e *Engine) catalogue(t Target) (discover.Catalogue, error) {
	c, ok := e.o.Catalogues(t.Format)
	if !ok || c == nil {
		return nil, errs.E(errs.Invalid, errs.CodeCatalogueNotOffered, "The format "+t.Format+" has no upstream catalogue.")
	}
	return c, nil
}

// call runs one call: from the cache when it holds the page, else through a lent upstream within the budget, the timeout and the
// bounds, keeping a page it answers unless the page found nothing and no request reached the network (foundNothing). into receives
// the page.
func (e *Engine) call(ctx context.Context, t Target, call string, args any, into any, run func(context.Context, discover.Upstream) (any, error)) error {
	st := e.o.Settings()
	key, err := cacheKey(t, call, args)
	if err != nil {
		return err
	}
	if v, ok := e.cache.get(key, st.CacheTTL); ok {
		e.o.Metrics.count(t.Repo, call, OutcomeCached)
		return assign(v, into)
	}
	start := e.o.Now()
	defer func() { e.o.Metrics.observe(call, e.o.Now().Sub(start)) }()
	timeout := st.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var asked atomic.Bool
	spend := e.spender(t)
	u, err := e.o.Lender.Lend(cctx, t, Lend{MaxBytes: st.MaxDocumentBytes, Spend: func(ctx context.Context) error {
		asked.Store(true)
		return spend(ctx)
	}})
	if err != nil {
		return e.fail(t, call, err, st, cctx)
	}
	v, err := run(cctx, u)
	if err != nil {
		return e.fail(t, call, err, st, cctx)
	}
	e.o.Metrics.count(t.Repo, call, OutcomeOK)
	if asked.Load() || !foundNothing(v) {
		e.cache.put(key, v, st.CacheBytes, st.CacheTTL)
	}
	return assign(v, into)
}

// foundNothing says that a page found nothing: a package not found, a search without a hit, a level without an entry. Such a page is
// kept only when the upstream was asked for it (Q-CUR-164): a format that answers from what Quader holds of the upstream (Debian's
// parsed indexes, R-DEB-036) finds the package once its index is parsed, and keeping "nothing" would hide it for the cache's period,
// while a request it does not send needs no budget protected.
func foundNothing(v any) bool {
	switch p := v.(type) {
	case discover.PackageInfo:
		return !p.Found
	case discover.SearchPage:
		return len(p.Hits) == 0
	case discover.LevelPage:
		return len(p.Levels) == 0 && len(p.Packages) == 0
	}
	return false
}

// spender takes one unit of the person's budget and one of the repository's before a request goes to the network (R-CUR-008).
func (e *Engine) spender(t Target) func(context.Context) error {
	return func(ctx context.Context) error {
		if t.Person != "" {
			if ok, after := e.o.PersonBudget.Take(ctx, "person:"+t.Person); !ok {
				return budgetError(after, fmt.Sprintf("Your upstream catalogue requests of the last minute reached proxy.catalogue.requests_per_minute_per_person (%d).",
					e.o.Settings().RequestsPerMinutePerPerson))
			}
		}
		if ok, after := e.o.RepoBudget.Take(ctx, fmt.Sprintf("repo:%d", t.RepoID)); !ok {
			return budgetError(after, fmt.Sprintf("The upstream catalogue requests of %s in the last minute reached proxy.catalogue.requests_per_minute (%d).",
				t.Repo, e.o.Settings().RequestsPerMinute))
		}
		return nil
	}
}

func budgetError(after time.Duration, detail string) error {
	return errs.E(errs.RateLimited, errs.CodeCatalogueBudgetExhausted, detail, errs.WithRetryAfter(max(after, time.Second)))
}

// fail maps the error of a call to the API's answer and counts its outcome.
func (e *Engine) fail(t Target, call string, err error, st Settings, ctx context.Context) error { //nolint:revive // the context is the call's, read for its deadline
	var unavailable *discover.UnavailableError
	switch {
	case errors.As(err, &unavailable):
		e.turnOff(t, call, offEntry{reason: unavailable.Reason, hint: unavailable.Hint}, st.CacheTTL)
		outcome := OutcomeNotOffered
		if unavailable.Shape {
			outcome = OutcomeShape
		}
		e.o.Metrics.count(t.Repo, call, outcome)
		return notOffered(unavailable.Reason, "")
	case errors.Is(err, discover.ErrNotOffered):
		e.o.Metrics.count(t.Repo, call, OutcomeNotOffered)
		return notOffered("", "This repository's upstream does not answer this call of the catalogue.")
	case ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)):
		e.o.Metrics.count(t.Repo, call, OutcomeTooLarge)
		return errs.E(errs.UpstreamFailed, errs.CodeCatalogueAnswerTooLarge,
			fmt.Sprintf("The call took longer than proxy.catalogue.timeout (%s); Quader does not wait longer for the upstream's answer.", st.Timeout))
	}
	if rl, ok := discover.AsRateLimited(err); ok {
		e.o.Metrics.count(t.Repo, call, OutcomeBudget)
		after, _ := rl.RetryAfter()
		return budgetError(after, "The upstream of "+t.Repo+" rate limits Quader; the catalogue does not retry within the call.")
	}
	if ee, ok := errs.As(err); ok {
		switch ee.Code {
		case errs.CodeCatalogueBudgetExhausted:
			e.o.Metrics.count(t.Repo, call, OutcomeBudget)
			return ee
		case errs.CodeCatalogueAnswerTooLarge, errs.CodeUpstreamItemTooLarge:
			e.o.Metrics.count(t.Repo, call, OutcomeTooLarge)
			return errs.E(errs.UpstreamFailed, errs.CodeCatalogueAnswerTooLarge,
				fmt.Sprintf("An answer of the upstream is larger than proxy.catalogue.max_document_bytes (%d bytes); Quader does not read it for browsing.", st.MaxDocumentBytes))
		case errs.CodeCatalogueNotOffered:
			e.o.Metrics.count(t.Repo, call, OutcomeNotOffered)
			return ee
		}
		e.o.Metrics.count(t.Repo, call, OutcomeUpstreamError)
		return ee
	}
	e.o.Metrics.count(t.Repo, call, OutcomeUpstreamError)
	e.o.Log.Warn("a call of the upstream catalogue failed", "repository", t.Repo, "call", call, "error", err)
	return errs.E(errs.UpstreamFailed, errs.CodeUpstreamError, "The upstream's answer could not be read for the catalogue: "+cut(err.Error()))
}

// turnOff turns a capability off for the cache period: the next calls answer catalogue_not_offered with the reason, and the caps name
// the hint (SearchHintNoSearch where the format named none).
func (e *Engine) turnOff(t Target, call string, o offEntry, ttl time.Duration) {
	if ttl <= 0 {
		ttl = time.Minute // the cache keeps nothing; a minute still spares the upstream a request a call
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.o.Now()
	if len(e.off) >= maxOffEntries {
		for k, v := range e.off {
			if now.After(v.until) {
				delete(e.off, k)
			}
		}
		if len(e.off) >= maxOffEntries {
			clear(e.off)
		}
	}
	if o.hint == "" {
		o.hint = discover.SearchHintNoSearch
	}
	o.until = now.Add(ttl)
	e.off[offKey(t, call)] = o
	e.cache.dropCaps(t)
}

func (e *Engine) isOff(t Target, call string) (offEntry, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	o, ok := e.off[offKey(t, call)]
	if !ok {
		return offEntry{}, false
	}
	if e.o.Now().After(o.until) {
		delete(e.off, offKey(t, call))
		return offEntry{}, false
	}
	return o, true
}

func offKey(t Target, call string) string {
	return fmt.Sprintf("%d\x00%s\x00%s", t.RepoID, t.Upstream, call)
}

func notOffered(reason, deflt string) error {
	if reason == "" {
		reason = deflt
	}
	if reason == "" {
		reason = "This repository's upstream does not answer this call of the catalogue."
	}
	return errs.E(errs.Invalid, errs.CodeCatalogueNotOffered, reason)
}

// clamp is a page's limit: the bound when it is 0 or larger.
func clamp(limit, bound int) int {
	if limit <= 0 || limit > bound {
		return bound
	}
	return limit
}

// cutHits keeps at most limit hits and cuts their texts.
func cutHits(hits []discover.Hit, limit int) []discover.Hit {
	if len(hits) > limit {
		hits = hits[:limit]
	}
	for i := range hits {
		hits[i].Description = discover.CutText(hits[i].Description)
		hits[i].Latest = cut(hits[i].Latest)
	}
	return hits
}

func cut(s string) string { return discover.CutText(s) }

// cacheKey is the key of a page: the repository, its upstream, the call and its arguments.
func cacheKey(t Target, call string, args any) (string, error) {
	b, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d\x00%s\x00%s\x00%s", t.RepoID, t.Upstream, call, b), nil
}

// assign hands a page to the caller: the cache holds values of the call's own type.
func assign(v any, into any) error {
	switch dst := into.(type) {
	case *discover.CatalogueCaps:
		c, ok := v.(discover.CatalogueCaps)
		if !ok {
			return errors.New("catalogue: the cached capabilities are of another type")
		}
		c.Levels, c.Hosts = append([]string(nil), c.Levels...), append([]string(nil), c.Hosts...)
		*dst = c
	case *discover.SearchPage:
		p, ok := v.(discover.SearchPage)
		if !ok {
			return errors.New("catalogue: the cached search page is of another type")
		}
		p.Hits = append([]discover.Hit(nil), p.Hits...)
		*dst = p
	case *discover.LevelPage:
		p, ok := v.(discover.LevelPage)
		if !ok {
			return errors.New("catalogue: the cached level page is of another type")
		}
		p.Levels, p.Packages = append([]string(nil), p.Levels...), append([]discover.Hit(nil), p.Packages...)
		if p.Unknown != nil {
			u := *p.Unknown
			p.Unknown = &u
		}
		*dst = p
	case *discover.PackageInfo:
		p, ok := v.(discover.PackageInfo)
		if !ok {
			return errors.New("catalogue: the cached package is of another type")
		}
		p.Versions = append([]discover.VersionFacts(nil), p.Versions...)
		*dst = p
	default:
		return fmt.Errorf("catalogue: no page of type %T", into)
	}
	return nil
}

// attestationPlatform is the platform an OCI image index names an attestation manifest with (a provenance, an SBOM): no image of a
// platform, which an upstream's tag list names among the platforms all the same (Docker Hub's "images").
const attestationPlatform = "unknown/unknown"

// platformsOf is the platforms a version's row shows (R-CUR-009): without attestationPlatform, for every format's catalogue that names
// platforms. Nil stays nil.
func platformsOf(ps []string) []string {
	if !slices.Contains(ps, attestationPlatform) {
		return ps
	}
	return slices.DeleteFunc(slices.Clone(ps), func(p string) bool { return p == attestationPlatform })
}
