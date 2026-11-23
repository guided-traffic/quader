package virtual

import (
	"cmp"
	"context"
	"maps"
	"net/http"
	"strings"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/repository"
)

// ResolvedFromHeader names the member that served an artifact (R-REPO-019) as the member list writes it: the key of a
// member of the virtual repository's project (Q-REPO-217), upstream/<key> for a member of the project upstream
// (R-REPO-071). It is a member the caller can read, so it discloses nothing new; it is never sent on an endpoint with
// exposure rules, because members are not exposed there (R-EP-018).
const ResolvedFromHeader = "Quader-Resolved-From"

// Artifact walks the three tiers of R-REPO-019 step 6 and serves the first hit: the local members
// in order, the caches of the remote members without contacting an upstream, then the upstreams of
// the remote members on which the caller passes the cache-fill check. When nothing was found it
// answers 404 if every consulted member said "not found" and otherwise with the failure of the first
// member that failed transiently, because "not found" would be a false statement about an item that
// may exist (step 7).
func (r *Resolver) Artifact(ctx context.Context, c Caller, p *Plan, t Tiers, w http.ResponseWriter) (err error) {
	// >>> WP2.9 exposure
	defer func() { err = c.asAddressed(p, err) }()
	// <<< WP2.9 exposure
	repo, d := p.Virtual.Name, p.Descriptor
	if !p.Admitted {
		r.m.resolved(repo.String(), sourceNone)
		return notFound()
	}
	serve := func(m Member, hit Hit, source string) error {
		if !c.Rules {
			w.Header().Set(ResolvedFromHeader, repository.MemberRef(repo.Project, m.Repo.Name))
		}
		r.m.resolved(repo.String(), source)
		return hit(w)
	}
	for _, m := range p.Local() {
		hit, ok, err := t.Local(ctx, m, d)
		if err != nil && !errs.IsMemberMiss(err) {
			return err
		}
		if err == nil && ok {
			return serve(m, hit, sourceLocal)
		}
	}
	remotes := p.Remote()
	// failure is the first transient failure of a member, refusal the first refusal of a member's delivery rules: the answer when no
	// member serves the item, in that order (step 7, Q-CUR-8).
	var miss walkMisses
	if done, err := r.cacheTier(ctx, c, p, t, w, &miss); done {
		return err
	}
	failure, refusal := miss.failure, miss.refusal
	for _, m := range remotes {
		if !m.MayFill {
			// A member the caller may not fill counts as a miss (R-AUTHZ-009 rule 3).
			// >>> WP2.9 exposure
			if sub := c.Subject; sub.Endpoint != "" && !sub.CacheFill && r.o.FillRefused != nil {
				r.o.FillRefused(sub.Endpoint) // R-EP-019: the endpoint prevented the contact
			}
			// <<< WP2.9 exposure
			continue
		}
		tw := &trackWriter{ResponseWriter: w}
		if !c.Rules {
			tw.Header().Set(ResolvedFromHeader, repository.MemberRef(repo.Project, m.Repo.Name))
		}
		found, err := t.RemoteFetch(ctx, m, d, tw)
		if found && err == nil {
			r.m.resolved(repo.String(), sourceUpstream)
			return nil
		}
		if !tw.wrote {
			tw.Header().Del(ResolvedFromHeader)
		}
		switch {
		case err == nil, errs.IsMemberMiss(err):
			// This member does not have it, or its rules do not deliver it.
			if refusal == nil && !tw.wrote && errs.IsRuleRefusal(err) {
				refusal = err
			}
		case tw.wrote || !errs.IsMemberFailure(err) || ctx.Err() != nil:
			// A failure after the first byte, or an answer the walk does not name, ends the
			// request: the next member cannot continue it, or must not serve what this one holds back.
			r.m.resolved(repo.String(), sourceNone)
			return err
		case failure == nil:
			failure = err
		}
	}
	r.m.resolved(repo.String(), sourceNone)
	if failure != nil {
		return failure
	}
	if refusal != nil {
		return refusal
	}
	return p.notFound()
}

// Stored walks the first two tiers of R-REPO-019 step 6 as Artifact does (the local members in order, then the caches of the remote
// members) and answers the first hit without running it and without the third tier's fetch: what Artifact would serve first when an
// item is stored, for a check of the item at a document a client reads before it (signatures.md Q-SIG-241). No hit (nothing stored,
// a plan that admits nothing) is ok false and no error; a remote member's cache whose object is lost is passed over, as Artifact's
// walk passes it. It records no resolution in the metrics: nothing is served.
func (r *Resolver) Stored(ctx context.Context, p *Plan, t Tiers) (Hit, bool, error) {
	if !p.Admitted {
		return nil, false, nil
	}
	for _, m := range p.Local() {
		hit, ok, err := t.Local(ctx, m, p.Descriptor)
		if errs.IsMemberMiss(err) {
			continue
		}
		if err != nil || ok {
			return hit, ok, err
		}
	}
	for _, m := range p.Remote() {
		hit, ok, err := t.RemoteCached(ctx, m, p.Descriptor)
		switch {
		case errs.IsMemberMiss(err):
		case err != nil && !contentLost(err):
			return nil, false, err
		case err == nil && ok:
			return hit, true, nil
		}
	}
	return nil, false, nil
}

// trackWriter notes whether the response has been started, so that a fetch which failed after the
// first byte is not followed by another member's.
type trackWriter struct {
	http.ResponseWriter
	wrote bool
}

func (t *trackWriter) WriteHeader(code int) {
	t.wrote = true
	t.ResponseWriter.WriteHeader(code)
}

func (t *trackWriter) Write(b []byte) (int, error) {
	t.wrote = true
	return t.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (t *trackWriter) Unwrap() http.ResponseWriter { return t.ResponseWriter }

// >>> WP2.9 exposure

// asAddressed reports a failure as the addressed repository's on an endpoint with exposure rules
// (R-EP-018): no problem body there names a repository other than the addressed one, and a member's
// failure names its member ("The upstream of npm-remote did not answer in time."). The kind, the
// code, Retry-After and the headers of the answer stay, so a client acts on it as before; the
// detail names the virtual repository and the code's title; the member's error stays the cause,
// which is logged and never rendered. A missing item is the engine's one 404 (R-EP-013). On any
// other endpoint the error is unchanged: there the members are the caller's to see (R-REPO-019).
func (c Caller) asAddressed(p *Plan, err error) error {
	if !c.Rules || err == nil {
		return err
	}
	e, ok := errs.As(err)
	if !ok {
		return err // rendered as an internal error, with the generic detail
	}
	if e.Kind == errs.NotFound {
		return notFound()
	}
	title := http.StatusText(e.Status())
	if info, found := errs.Lookup(e.Code); found {
		title = info.Title
	}
	out := errs.E(e.Kind, e.Code, p.Virtual.Name.String()+" could not answer the request: "+strings.ToLower(title)+".",
		errs.WithCause(err), errs.WithRetryAfter(e.RetryAfter))
	out.Headers = maps.Clone(e.Headers)
	return out
}

// <<< WP2.9 exposure

// contentLost reports whether an error refuses a stored item because its object is missing or failed verification
// (content_unavailable, storage.md R-STOR-012).
func contentLost(err error) bool {
	e, ok := errs.As(err)
	return ok && e.Code == errs.CodeContentUnavailable
}

// walkMisses is what the walk keeps of the members that did not serve: the first transient failure and the first refusal of a member's
// delivery rules (step 7, Q-CUR-8).
type walkMisses struct{ failure, refusal error }

// cacheTier is the second tier of R-REPO-019 step 6: the caches of the remote members, without contacting an upstream. done reports that
// the walk ends with err (a member served the item, or an answer the walk does not name); otherwise ms holds what the next tier and the
// end of the walk need.
func (r *Resolver) cacheTier(ctx context.Context, c Caller, p *Plan, t Tiers, w http.ResponseWriter, ms *walkMisses) (done bool, err error) {
	repo, d := p.Virtual.Name, p.Descriptor
	for _, m := range p.Remote() {
		hit, ok, err := t.RemoteCached(ctx, m, d)
		if errs.IsMemberMiss(err) {
			if ms.refusal == nil && errs.IsRuleRefusal(err) {
				ms.refusal = err
			}
			continue // the member's cache does not have it for the caller
		}
		if err != nil && !contentLost(err) {
			return true, err
		}
		if err != nil {
			// The member's cache holds the item and its object is lost (Q-REPO-205): the walk goes on, its fetch tier
			// included, which fills it again for a caller who may; a caller who may not gets this 503, not a 404.
			ms.failure = cmp.Or(ms.failure, err)
			continue
		}
		if !ok {
			continue
		}
		tw := &trackWriter{ResponseWriter: w}
		if !c.Rules {
			tw.Header().Set(ResolvedFromHeader, repository.MemberRef(repo.Project, m.Repo.Name))
		}
		herr := hit(tw)
		if !tw.wrote && errs.IsRuleRefusal(herr) {
			// The member's rules do not deliver what its cache holds (R-CUR-003 point 2): a miss of this member, and the walk goes on
			// to the next one, which may deliver it under its own rules (R-CUR-004).
			ms.refusal = cmp.Or(ms.refusal, herr)
			tw.Header().Del(ResolvedFromHeader)
			continue
		}
		if herr == nil || tw.wrote || !contentLost(herr) {
			r.m.resolved(repo.String(), sourceCache)
			return true, herr
		}
		ms.failure = cmp.Or(ms.failure, herr)
		// The read found the cached copy's object missing or damaged and marked it, before anything was written: the
		// member's cache does not have the item, and the walk goes on, its fetch tier included, which fills it again
		// (storage.md R-STOR-027, Q-REPO-204); the loss is the answer when nothing serves it (Q-REPO-205). A local member's
		// lost object stays its answer: that is data loss.
		tw.Header().Del(ResolvedFromHeader)
	}
	return false, nil
}
