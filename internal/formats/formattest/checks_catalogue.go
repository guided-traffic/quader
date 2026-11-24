package formattest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// CheckCatalogue is check 22 (remote-curation.md R-CUR-007, R-CUR-022), for a format with the class remote: against a fake upstream in
// process that answers the fixture's canned documents, each call the repository's capabilities offer answers pages whose names the
// format's name rule accepts, whose cursor continues and that end Done (a search may end Truncated at its window); a call they do not
// offer answers discover.ErrNotOffered, and so does every call the format's declaration gives a reason for; a package lookup answers
// canonical versions and a name the upstream does not have Found false; a level the upstream does not have, where the case names one,
// answers a page that lists nothing and says Unknown within three reads of the upstream (Q-CUR-165); an answer above the catalogue's bound fails with
// catalogue_answer_too_large; and no call reaches a host outside the upstream and the hosts the capabilities declare.
const CheckCatalogue Check = 22

// CatalogueFixture is the fixture of check 22: one case per upstream of the format whose capabilities differ (a preset with its
// operator's API and a registry without).
type CatalogueFixture struct {
	Cases []CatalogueCase
}

// CatalogueCase is one remote repository of the format with a fake upstream.
type CatalogueCase struct {
	Name string
	// Base is the repository's remote.url; Settings the format's member of its document, nil for none.
	Base     string
	Settings json.RawMessage
	// Answers are the fake upstream's answers to Get and Head, by the URL the format sends (URL.String()); a URL not in the map answers
	// 404. Documents are the repository's metadata documents by path, or by path, NUL and variant; a path not in the map is not there.
	Answers   map[string]CannedAnswer
	Documents map[string][]byte
	// Search is the text of the search case, Level the path of the level case ([] for the root), Package the name the lookup reads (at
	// least two versions) and Absent a name the upstream does not have.
	Search  string
	Level   []string
	Package string
	Absent  string
	// Limit is the page size of every call, small enough that the search, the level and the versions each take more than one page.
	Limit int
	// UnknownLevel is the path of a level the upstream does not have, for an upstream that tells an unknown level from an empty one:
	// Children answers a page that lists nothing with Unknown set, reading the upstream at most maxUnknownReads times (Q-CUR-165), and
	// naming UnknownSuggestion as its suggestion ("" for none). Empty for an upstream that does not tell the two apart.
	UnknownLevel      []string
	UnknownSuggestion string
	// ExpectSearchUnavailable says that the case's upstream does not answer the search as the format expects (a registry without the
	// route, an undocumented endpoint that changed): Search answers a *discover.UnavailableError, which the engine turns into "no search"
	// for its cache period (R-CUR-008).
	ExpectSearchUnavailable bool
	// Seed stores what the format reads besides the fake upstream (a Debian remote's parsed indexes) in a Host built for the case and
	// binds the format to it; nil stores nothing.
	Seed func(h *Host) error
}

// CannedAnswer is one answer of the fake upstream.
type CannedAnswer struct {
	Status int
	Body   []byte
	Header http.Header
}

// fakeCatalogueUpstream is the fake upstream of a case: it answers the canned documents and records the hosts the format asks.
type fakeCatalogueUpstream struct {
	base     *url.URL
	settings json.RawMessage
	answers  map[string]CannedAnswer
	docs     map[string][]byte
	repoID   int64
	// bound is the largest body it hands over; a larger one fails with catalogue_answer_too_large, as the catalogue's upstream does.
	bound int64
	mu    sync.Mutex
	hosts []string
	reads int
	asked []string
}

var (
	_ discover.Upstream       = (*fakeCatalogueUpstream)(nil)
	_ discover.Header         = (*fakeCatalogueUpstream)(nil)
	_ discover.RepositoryIDer = (*fakeCatalogueUpstream)(nil)
)

func (u *fakeCatalogueUpstream) Base() *url.URL            { b := *u.base; return &b }
func (u *fakeCatalogueUpstream) Settings() json.RawMessage { return u.settings }
func (u *fakeCatalogueUpstream) RepositoryID() int64       { return u.repoID }

func (u *fakeCatalogueUpstream) tooLarge() error {
	return errs.E(errs.UpstreamFailed, errs.CodeCatalogueAnswerTooLarge, "the kit's catalogue bound is "+strconv.FormatInt(u.bound, 10)+" bytes")
}

func (u *fakeCatalogueUpstream) Get(_ context.Context, target *url.URL, _ string, max int64) (*discover.Response, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.reads++
	u.hosts = append(u.hosts, strings.ToLower(target.Hostname()))
	u.asked = append(u.asked, target.String())
	a, ok := u.answers[target.String()]
	if !ok {
		return &discover.Response{Status: http.StatusNotFound, Header: http.Header{}}, nil
	}
	bound := u.bound
	if max > 0 && max < bound {
		bound = max
	}
	if int64(len(a.Body)) > bound {
		return nil, u.tooLarge()
	}
	st := a.Status
	if st == 0 {
		st = http.StatusOK
	}
	h := a.Header
	if h == nil {
		h = http.Header{}
	}
	return &discover.Response{Status: st, Header: h, Body: a.Body}, nil
}

func (u *fakeCatalogueUpstream) Head(ctx context.Context, target *url.URL, accept string) (*discover.Response, error) {
	res, err := u.Get(ctx, target, accept, 0)
	if res != nil {
		res.Body = nil
	}
	return res, err
}

func (u *fakeCatalogueUpstream) Document(_ context.Context, path, variant string) ([]byte, bool, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.reads++
	d, ok := u.docs[path+"\x00"+variant]
	if !ok {
		d, ok = u.docs[path]
	}
	if !ok {
		return nil, false, nil
	}
	if int64(len(d)) > u.bound {
		return nil, false, u.tooLarge()
	}
	return d, true, nil
}

func (*fakeCatalogueUpstream) Artifact(context.Context, string, int64) ([]byte, bool, error) {
	return nil, false, errors.New("the upstream catalogue stores nothing: it reads no artifact (R-CUR-008)")
}

// catalogue is check 22.
func (r *run) catalogue() {
	if r.f.Remote() == nil {
		r.failf("check 22 has nothing to run over: the format has no remote adapter (skip it with that reason)")
		return
	}
	cat := formats.CapabilitiesOf(r.f).Remote.Catalogue
	if cat == nil {
		r.failf("the format has the class remote and its remote adapter does not implement formats.Catalogue (R-CUR-007)")
		return
	}
	fx := r.fx.Catalogue
	if fx == nil || len(fx.Cases) == 0 {
		r.failf("the format has the class remote and no Catalogue fixture: check 22 cannot hold its catalogue to its declaration")
		return
	}
	for _, c := range fx.Cases {
		r.catalogueCase(cat, c)
	}
}

func (r *run) newCatalogueUpstream(c CatalogueCase, bound int64) (*fakeCatalogueUpstream, error) {
	base, err := url.Parse(c.Base)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("the base %q is no URL", c.Base)
	}
	return &fakeCatalogueUpstream{base: base, settings: c.Settings, answers: c.Answers, docs: c.Documents, repoID: 1, bound: bound}, nil
}

// catalogueCase runs the calls of one case.
func (r *run) catalogueCase(cat formats.Catalogue, c CatalogueCase) {
	ctx := context.Background()
	failf := func(format string, a ...any) { r.failf("catalogue case %q: "+format, append([]any{c.Name}, a...)...) }
	if c.Seed != nil {
		if err := c.Seed(r.host(formats.Remote)); err != nil {
			failf("the seed fails: %v", err)
			return
		}
	}
	u, err := r.newCatalogueUpstream(c, 64<<20)
	if err != nil {
		failf("%v", err)
		return
	}
	caps, err := cat.CatalogueCaps(ctx, u)
	if err != nil {
		failf("CatalogueCaps: %v", err)
		return
	}
	decl := r.d.Catalogue
	switch {
	case decl.NoSearch != "" && caps.Search != discover.SearchNone:
		failf("the declaration gives a reason for no search and the capabilities say %q", caps.Search)
	case decl.NoLevels != "" && len(caps.Levels) > 0:
		failf("the declaration gives a reason for no levels and the capabilities list %v", caps.Levels)
	case !caps.SearchHint.Valid():
		failf("the capabilities' search hint is %q, want one of discover.SearchHints (the screen says a sentence for each)", caps.SearchHint)
	case caps.Sizes != "known" && caps.Sizes != "perVersion" && caps.Sizes != "unknown":
		failf("the capabilities' sizes are %q, want known, perVersion or unknown", caps.Sizes)
	}
	limit := max(c.Limit, 1)
	r.catalogueSearch(ctx, cat, u, c, caps, limit, failf)
	r.catalogueLevel(ctx, cat, u, c, caps, limit, failf)
	r.cataloguePackage(ctx, cat, u, c, limit, failf)
	allowed := []string{strings.ToLower(u.base.Hostname())}
	for _, h := range caps.Hosts {
		allowed = append(allowed, strings.ToLower(h))
	}
	for _, h := range u.hosts {
		if !slices.Contains(allowed, h) {
			failf("a call reached the host %q, which is neither the upstream's nor one the capabilities declare (%v)", h, caps.Hosts)
		}
	}
	r.catalogueTooLarge(ctx, cat, c, failf)
}

func (r *run) validName(name string) bool {
	if r.d.Names == nil {
		return name != ""
	}
	n, ok := r.d.Names.Normalize(name)
	return ok && n == name
}

func (r *run) catalogueSearch(ctx context.Context, cat formats.Catalogue, u *fakeCatalogueUpstream, c CatalogueCase, caps discover.CatalogueCaps, limit int, failf func(string, ...any)) {
	q := discover.SearchQuery{Text: c.Search, Limit: limit}
	if caps.Search == discover.SearchNone {
		if q.Text == "" {
			q.Text = "kit"
		}
		if _, err := cat.Search(ctx, u, q); !errors.Is(err, discover.ErrNotOffered) {
			failf("the capabilities say no search and Search answers %v, want discover.ErrNotOffered", err)
		}
		return
	}
	if c.Search == "" {
		failf("the capabilities offer a search and the case names no Search text")
		return
	}
	if c.ExpectSearchUnavailable {
		var ue *discover.UnavailableError
		if _, err := cat.Search(ctx, u, q); !errors.As(err, &ue) || ue.Reason == "" || !ue.Hint.Valid() {
			failf("the case's upstream does not answer the search and Search answers %v, want a *discover.UnavailableError with a reason and a hint", err)
		}
		return
	}
	hits := 0
	for i := 0; ; i++ {
		if i == 10 {
			failf("the search did not end within 10 pages of %d", limit)
			return
		}
		p, err := cat.Search(ctx, u, q)
		if err != nil {
			failf("Search page %d: %v", i+1, err)
			return
		}
		if len(p.Hits) > limit {
			failf("Search page %d holds %d hits, the limit is %d", i+1, len(p.Hits), limit)
		}
		for _, h := range p.Hits {
			if !r.validName(h.Name) {
				failf("Search answers the hit %q, which the format's name rule does not take as a normalized name", h.Name)
			}
		}
		hits += len(p.Hits)
		if p.Done || p.Truncated {
			if i == 0 {
				failf("the search ends on its first page: the case's Limit %d does not make it page", limit)
			}
			break
		}
		if len(p.Cursor) == 0 {
			failf("Search page %d is not Done and has no cursor", i+1)
			return
		}
		q.Cursor = p.Cursor
	}
	if hits == 0 {
		failf("the search found nothing")
	}
}

func (r *run) catalogueLevel(ctx context.Context, cat formats.Catalogue, u *fakeCatalogueUpstream, c CatalogueCase, caps discover.CatalogueCaps, limit int, failf func(string, ...any)) {
	rq := discover.LevelRequest{Level: discover.LevelRef{Path: c.Level}, Limit: limit}
	if caps.LevelsReason != "" {
		if _, err := cat.Children(ctx, u, rq); !errors.Is(err, discover.ErrNotOffered) {
			failf("the capabilities list no levels and Children answers %v, want discover.ErrNotOffered", err)
		}
		return
	}
	entries := 0
	for i := 0; ; i++ {
		if i == 10 {
			failf("the level did not end within 10 pages of %d", limit)
			return
		}
		p, err := cat.Children(ctx, u, rq)
		if err != nil {
			failf("Children page %d: %v", i+1, err)
			return
		}
		if len(p.Levels)+len(p.Packages) > limit {
			failf("Children page %d holds %d entries, the limit is %d", i+1, len(p.Levels)+len(p.Packages), limit)
		}
		for _, h := range p.Packages {
			if !r.validName(h.Name) {
				failf("Children answers the package %q, which the format's name rule does not take as a normalized name", h.Name)
			}
		}
		entries += len(p.Levels) + len(p.Packages)
		if p.Done {
			if i == 0 {
				failf("the level ends on its first page: the case's Limit %d does not make it page", limit)
			}
			break
		}
		if len(p.Cursor) == 0 {
			failf("Children page %d is not Done and has no cursor", i+1)
			return
		}
		rq.Cursor = p.Cursor
	}
	if entries == 0 {
		failf("the level %v lists nothing", c.Level)
	}
	r.catalogueUnknownLevel(ctx, cat, u, c, limit, failf)
}

// maxUnknownReads bounds what telling an unknown level from an empty one costs: the listing and at most two accounts of the name (Quay's
// organization and user, Debian's InRelease and Release), a constant per format (Q-CUR-165).
const maxUnknownReads = 3

// catalogueUnknownLevel asks the case's unknown level: a page that lists nothing, says Unknown and names the expected suggestion.
func (r *run) catalogueUnknownLevel(ctx context.Context, cat formats.Catalogue, u *fakeCatalogueUpstream, c CatalogueCase, limit int, failf func(string, ...any)) {
	if len(c.UnknownLevel) == 0 {
		return
	}
	u.mu.Lock()
	before := u.reads
	u.mu.Unlock()
	p, err := cat.Children(ctx, u, discover.LevelRequest{Level: discover.LevelRef{Path: c.UnknownLevel}, Limit: limit})
	u.mu.Lock()
	reads := u.reads - before
	u.mu.Unlock()
	switch {
	case err != nil:
		failf("Children of the unknown level %v: %v", c.UnknownLevel, err)
	case p.Unknown == nil:
		failf("Children of the unknown level %v does not say it is unknown (it lists %d entries)", c.UnknownLevel, len(p.Levels)+len(p.Packages))
	case len(p.Levels)+len(p.Packages) > 0:
		failf("Children of the unknown level %v lists %d entries beside Unknown", c.UnknownLevel, len(p.Levels)+len(p.Packages))
	case p.Unknown.Suggestion != c.UnknownSuggestion:
		failf("Children of the unknown level %v suggests %q, want %q", c.UnknownLevel, p.Unknown.Suggestion, c.UnknownSuggestion)
	case p.Unknown.Rule == "" && p.Unknown.Forbidden != "":
		failf("Children of the unknown level %v names forbidden characters %q without a rule", c.UnknownLevel, p.Unknown.Forbidden)
	}
	if reads > maxUnknownReads {
		failf("Children of the unknown level %v read the upstream %d times, at most %d tell an unknown level from an empty one", c.UnknownLevel, reads, maxUnknownReads)
	}
}

func (r *run) cataloguePackage(ctx context.Context, cat formats.Catalogue, u *fakeCatalogueUpstream, c CatalogueCase, limit int, failf func(string, ...any)) {
	if c.Package == "" || c.Absent == "" {
		failf("the case names no Package or no Absent name")
		return
	}
	rq := discover.VersionsRequest{Limit: limit}
	var versions []string
	for i := 0; ; i++ {
		if i == 50 {
			failf("the versions of %s did not end within 50 pages of %d", c.Package, limit)
			return
		}
		p, err := cat.Package(ctx, u, c.Package, rq)
		if err != nil {
			failf("Package %s page %d: %v", c.Package, i+1, err)
			return
		}
		if !p.Found {
			failf("Package %s is not found", c.Package)
			return
		}
		if p.Name != c.Package {
			failf("Package %s answers the name %q", c.Package, p.Name)
		}
		if len(p.Versions) > limit {
			failf("Package page %d holds %d versions, the limit is %d", i+1, len(p.Versions), limit)
		}
		for _, v := range p.Versions {
			if r.d.VersionCanon != nil {
				if cv, ok := r.d.VersionCanon.Canonical(v.Version); !ok || cv != v.Version {
					failf("Package %s answers the version %q, which is not canonical (R-CUR-006)", c.Package, v.Version)
				}
			}
			versions = append(versions, v.Version)
		}
		if p.Done {
			break
		}
		if len(p.Cursor) == 0 {
			failf("Package page %d is not Done and has no cursor", i+1)
			return
		}
		rq.Cursor = p.Cursor
	}
	if len(versions) < 2 {
		failf("Package %s answers %d versions, the case needs at least two", c.Package, len(versions))
	}
	if p, err := cat.Package(ctx, u, c.Absent, discover.VersionsRequest{Limit: limit}); err != nil || p.Found {
		failf("Package %s, which the upstream does not have, answers found %v and %v, want not found and no error", c.Absent, p.Found, err)
	}
}

// catalogueTooLarge reads the package again through an upstream whose bound is below the documents it read: the call fails with
// catalogue_answer_too_large instead of reading less (R-CUR-008). A case whose lookup reads nothing through the upstream (a Debian
// remote's rows) has nothing to bound.
func (r *run) catalogueTooLarge(ctx context.Context, cat formats.Catalogue, c CatalogueCase, failf func(string, ...any)) {
	u, err := r.newCatalogueUpstream(c, 16)
	if err != nil {
		return
	}
	_, err = cat.Package(ctx, u, c.Package, discover.VersionsRequest{Limit: max(c.Limit, 1)})
	if u.reads == 0 {
		return
	}
	if ee, ok := errs.As(err); !ok || ee.Code != errs.CodeCatalogueAnswerTooLarge {
		failf("Package %s through an upstream bounded to 16 bytes answers %v, want catalogue_answer_too_large", c.Package, err)
	}
}
