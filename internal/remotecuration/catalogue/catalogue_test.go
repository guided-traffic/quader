package catalogue

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// fakeUpstream counts what a format sends through the lent upstream and spends the budget per request, as formathost's does.
type fakeUpstream struct {
	spend     func(context.Context) error
	mu        sync.Mutex
	gets      int
	artifacts int
	err       error
}

func (u *fakeUpstream) Base() *url.URL            { return &url.URL{Scheme: "https", Host: "registry.example"} }
func (u *fakeUpstream) Settings() json.RawMessage { return nil }

func (u *fakeUpstream) Get(ctx context.Context, _ *url.URL, _ string, _ int64) (*discover.Response, error) {
	if err := u.spend(ctx); err != nil {
		return nil, err
	}
	u.mu.Lock()
	u.gets++
	u.mu.Unlock()
	if u.err != nil {
		return nil, u.err
	}
	return &discover.Response{Status: 200}, nil
}

func (u *fakeUpstream) Document(ctx context.Context, _, _ string) ([]byte, bool, error) {
	_, err := u.Get(ctx, nil, "", 0)
	return []byte("{}"), err == nil, err
}

func (u *fakeUpstream) Artifact(context.Context, string, int64) ([]byte, bool, error) {
	u.mu.Lock()
	u.artifacts++
	u.mu.Unlock()
	return nil, false, errors.New("the catalogue stores nothing")
}

// fakeLender lends one fakeUpstream per call, all counting into the same totals.
type fakeLender struct {
	mu   sync.Mutex
	lent []*fakeUpstream
	err  error
}

func (l *fakeLender) Lend(_ context.Context, _ Target, o Lend) (discover.Upstream, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	u := &fakeUpstream{spend: o.Spend, err: l.err}
	l.lent = append(l.lent, u)
	return u, nil
}

func (l *fakeLender) totals() (gets, artifacts int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, u := range l.lent {
		gets += u.gets
		artifacts += u.artifacts
	}
	return gets, artifacts
}

// fakeCatalogue answers through the upstream: a search reads one document, a package reads one, and the search fails by shape on demand.
type fakeCatalogue struct {
	shape bool
}

func (fakeCatalogue) CatalogueCaps(context.Context, discover.Upstream) (discover.CatalogueCaps, error) {
	return discover.CatalogueCaps{Search: discover.SearchUpstream, Levels: []string{"namespace"}, Sizes: "unknown"}, nil
}

func (c *fakeCatalogue) Search(ctx context.Context, u discover.Upstream, _ discover.SearchQuery) (discover.SearchPage, error) {
	if c.shape {
		return discover.SearchPage{}, &discover.UnavailableError{Reason: "The search did not answer as expected.", Shape: true}
	}
	if _, err := u.Get(ctx, u.Base(), "", 0); err != nil {
		return discover.SearchPage{}, err
	}
	return discover.SearchPage{Hits: []discover.Hit{{Name: "a", Description: strings.Repeat("x", 2000)}}, Total: 1, Done: true}, nil
}

func (fakeCatalogue) Children(context.Context, discover.Upstream, discover.LevelRequest) (discover.LevelPage, error) {
	return discover.LevelPage{Done: true}, nil
}

func (fakeCatalogue) Package(ctx context.Context, u discover.Upstream, name string, _ discover.VersionsRequest) (discover.PackageInfo, error) {
	if _, _, err := u.Document(ctx, name, ""); err != nil {
		return discover.PackageInfo{}, err
	}
	if name == "absent" {
		return discover.PackageInfo{}, nil
	}
	return discover.PackageInfo{Found: true, Name: name, Versions: []discover.VersionFacts{{Version: "1.0.0"}}, Done: true}, nil
}

// indexCatalogue answers from rows Quader holds of the upstream, as Debian's parsed indexes (R-DEB-036): no request reaches the network.
// parsed is the package the index holds once the remote parsed it; reads counts the answers it read from its rows.
type indexCatalogue struct {
	mu     sync.Mutex
	parsed string
	reads  int
}

func (c *indexCatalogue) read() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	return c.parsed
}

func (c *indexCatalogue) parse(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.parsed = name
}

func (c *indexCatalogue) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads
}

func (*indexCatalogue) CatalogueCaps(context.Context, discover.Upstream) (discover.CatalogueCaps, error) {
	return discover.CatalogueCaps{Search: discover.SearchIndex, Levels: []string{"suite"}, Sizes: "known"}, nil
}

func (c *indexCatalogue) Search(_ context.Context, _ discover.Upstream, q discover.SearchQuery) (discover.SearchPage, error) {
	if p := c.read(); p != "" && strings.Contains(p, q.Text) {
		return discover.SearchPage{Hits: []discover.Hit{{Name: p}}, Total: -1, Done: true}, nil
	}
	return discover.SearchPage{Total: -1, Done: true}, nil
}

func (c *indexCatalogue) Children(context.Context, discover.Upstream, discover.LevelRequest) (discover.LevelPage, error) {
	if p := c.read(); p != "" {
		return discover.LevelPage{Packages: []discover.Hit{{Name: p}}, Total: -1, Done: true}, nil
	}
	return discover.LevelPage{Total: -1, Done: true}, nil
}

func (c *indexCatalogue) Package(_ context.Context, _ discover.Upstream, name string, _ discover.VersionsRequest) (discover.PackageInfo, error) {
	if c.read() != name {
		return discover.PackageInfo{}, nil
	}
	return discover.PackageInfo{Found: true, Name: name, Versions: []discover.VersionFacts{{Version: "1.0-1"}}, Done: true}, nil
}

func newEngine(t *testing.T, cat discover.Catalogue, l *fakeLender, st Settings, now func() time.Time) *Engine {
	t.Helper()
	e, err := New(Options{
		Catalogues: func(string) (discover.Catalogue, bool) { return cat, true },
		Lender:     l, Settings: func() Settings { return st }, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func defaults() Settings {
	return Settings{
		RequestsPerMinute: 120, RequestsPerMinutePerPerson: 30, CacheBytes: 32 << 20, CacheTTL: 300 * time.Second,
		MaxDocumentBytes: 64 << 20, Timeout: 20 * time.Second,
	}
}

func code(err error) string {
	if e, ok := errs.As(err); ok {
		return e.Code
	}
	return ""
}

// TestR_CUR_008_TheCatalogueEngine holds the engine to R-CUR-008: nothing is stored, an identical call within the period sends nothing,
// the budgets refuse with 429, an answer above the bound is 502, a host the upstream refuses stays refused, and an answer that is not of
// the expected shape turns the capability off for the period. A fake format and a fake upstream in process; milliseconds, no data.
func TestR_CUR_008_TheCatalogueEngine(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	tgt := Target{RepoID: 7, Repo: "it/npm-remote", Format: "npm", Upstream: "https://registry.example/"}

	t.Run("R-CUR-008 a call stores nothing and a second identical call within the period sends nothing", func(t *testing.T) {
		l := &fakeLender{}
		e := newEngine(t, &fakeCatalogue{}, l, defaults(), func() time.Time { return t0 })
		for range 2 {
			p, err := e.Search(ctx, tgt, discover.SearchQuery{Text: "left"})
			if err != nil || len(p.Hits) != 1 {
				t.Fatalf("%v %v", p, err)
			}
			if n := len([]rune(p.Hits[0].Description)); n != discover.MaxTextRunes {
				t.Fatalf("the description is %d runes, the bound is %d", n, discover.MaxTextRunes)
			}
		}
		if gets, artifacts := l.totals(); gets != 1 || artifacts != 0 {
			t.Fatalf("two identical searches sent %d requests and %d artifact reads, want 1 and 0", gets, artifacts)
		}
	})
	t.Run("R-CUR-008 the cache's period ends and 0s keeps nothing", func(t *testing.T) {
		now := t0
		l := &fakeLender{}
		e := newEngine(t, &fakeCatalogue{}, l, defaults(), func() time.Time { return now })
		_, _ = e.Package(ctx, tgt, "left-pad", discover.VersionsRequest{})
		now = now.Add(301 * time.Second)
		_, _ = e.Package(ctx, tgt, "left-pad", discover.VersionsRequest{})
		st := defaults()
		st.CacheTTL = 0
		e2 := newEngine(t, &fakeCatalogue{}, l, st, func() time.Time { return t0 })
		_, _ = e2.Package(ctx, tgt, "left-pad", discover.VersionsRequest{})
		_, _ = e2.Package(ctx, tgt, "left-pad", discover.VersionsRequest{})
		if gets, _ := l.totals(); gets != 4 {
			t.Fatalf("requests = %d, want 4", gets)
		}
	})
	t.Run("R-CUR-008 the 121st upstream request of a repository within a minute answers 429 with Retry-After", func(t *testing.T) {
		now := t0
		l := &fakeLender{}
		st := defaults()
		st.RequestsPerMinutePerPerson, st.CacheTTL = 100000, 0
		e := newEngine(t, &fakeCatalogue{}, l, st, func() time.Time { return now })
		for i := range 120 {
			if _, err := e.Package(ctx, tgt, "left-pad", discover.VersionsRequest{}); err != nil {
				t.Fatalf("request %d: %v", i+1, err)
			}
		}
		_, err := e.Package(ctx, tgt, "left-pad", discover.VersionsRequest{})
		if e, ok := errs.As(err); !ok || e.Code != errs.CodeCatalogueBudgetExhausted || e.RetryAfter <= 0 {
			t.Fatalf("the 121st request: %v", err)
		}
		now = now.Add(time.Minute)
		if _, err := e.Package(ctx, tgt, "left-pad", discover.VersionsRequest{}); err != nil {
			t.Fatalf("after the window: %v", err)
		}
	})
	t.Run("R-CUR-008 one person's budget is spent across repositories, another person keeps theirs", func(t *testing.T) {
		l := &fakeLender{}
		st := defaults()
		st.RequestsPerMinutePerPerson, st.CacheTTL = 2, 0
		e := newEngine(t, &fakeCatalogue{}, l, st, func() time.Time { return t0 })
		a := tgt
		a.Person = "alice"
		b := a
		b.RepoID = 8
		_, _ = e.Package(ctx, a, "x", discover.VersionsRequest{})
		_, _ = e.Package(ctx, b, "x", discover.VersionsRequest{})
		if _, err := e.Package(ctx, a, "x", discover.VersionsRequest{}); code(err) != errs.CodeCatalogueBudgetExhausted {
			t.Fatalf("alice's third request: %v", err)
		}
		bob := a
		bob.Person = "bob"
		if _, err := e.Package(ctx, bob, "x", discover.VersionsRequest{}); err != nil {
			t.Fatalf("bob's request: %v", err)
		}
	})
	t.Run("R-CUR-008 an answer above the bound answers 502 catalogue_answer_too_large", func(t *testing.T) {
		l := &fakeLender{err: errs.E(errs.UpstreamFailed, errs.CodeUpstreamItemTooLarge, "65 MiB")}
		e := newEngine(t, &fakeCatalogue{}, l, defaults(), func() time.Time { return t0 })
		_, err := e.Package(ctx, tgt, "big", discover.VersionsRequest{})
		if code(err) != errs.CodeCatalogueAnswerTooLarge || !strings.Contains(err.Error(), "max_document_bytes") {
			t.Fatalf("%v", err)
		}
	})
	t.Run("R-CUR-008 a host the upstream refuses stays refused as foreign_host_denied", func(t *testing.T) {
		l := &fakeLender{err: errs.E(errs.UpstreamFailed, errs.CodeForeignHostDenied, "elsewhere.example")}
		e := newEngine(t, &fakeCatalogue{}, l, defaults(), func() time.Time { return t0 })
		if _, err := e.Search(ctx, tgt, discover.SearchQuery{Text: "x"}); code(err) != errs.CodeForeignHostDenied {
			t.Fatalf("%v", err)
		}
	})
	t.Run("R-CUR-008 a shape failure of the search turns it off for the period, and it comes back after", func(t *testing.T) {
		now := t0
		l := &fakeLender{}
		cat := &fakeCatalogue{shape: true}
		e := newEngine(t, cat, l, defaults(), func() time.Time { return now })
		if _, err := e.Search(ctx, tgt, discover.SearchQuery{Text: "x"}); code(err) != errs.CodeCatalogueNotOffered {
			t.Fatalf("the shape failure: %v", err)
		}
		caps, err := e.Caps(ctx, tgt)
		if err != nil || caps.Search != discover.SearchNone || caps.SearchReason == "" {
			t.Fatalf("caps within the period: %+v %v", caps, err)
		}
		cat.shape = false
		now = now.Add(301 * time.Second)
		if caps, _ := e.Caps(ctx, tgt); caps.Search != discover.SearchUpstream {
			t.Fatalf("caps after the period: %+v", caps)
		}
	})
	t.Run("R-CUR-009 a package the upstream does not have answers 404 catalogue_not_found", func(t *testing.T) {
		e := newEngine(t, &fakeCatalogue{}, &fakeLender{}, defaults(), func() time.Time { return t0 })
		if _, err := e.Package(ctx, tgt, "absent", discover.VersionsRequest{}); code(err) != errs.CodeCatalogueNotFound {
			t.Fatalf("%v", err)
		}
	})
	t.Run("R-CUR-008 a not-found the upstream answered is kept for the period", func(t *testing.T) {
		l := &fakeLender{}
		e := newEngine(t, &fakeCatalogue{}, l, defaults(), func() time.Time { return t0 })
		for range 2 {
			if _, err := e.Package(ctx, tgt, "absent", discover.VersionsRequest{}); code(err) != errs.CodeCatalogueNotFound {
				t.Fatalf("%v", err)
			}
		}
		if gets, _ := l.totals(); gets != 1 {
			t.Fatalf("two lookups of a package the upstream does not have sent %d requests, want 1", gets)
		}
	})
	t.Run("R-CUR-008 R-DEB-036 a format that answers from its index finds what the index's parse added within the period", func(t *testing.T) {
		l := &fakeLender{}
		cat := &indexCatalogue{}
		e := newEngine(t, cat, l, defaults(), func() time.Time { return t0 })
		deb := tgt
		deb.Format = "debian"
		root := discover.LevelRequest{}
		if _, err := e.Package(ctx, deb, "hello", discover.VersionsRequest{}); code(err) != errs.CodeCatalogueNotFound {
			t.Fatalf("the lookup before the parse: %v", err)
		}
		if p, err := e.Search(ctx, deb, discover.SearchQuery{Text: "hello"}); err != nil || len(p.Hits) != 0 {
			t.Fatalf("the search before the parse: %+v %v", p, err)
		}
		if p, err := e.Children(ctx, deb, root); err != nil || len(p.Packages) != 0 {
			t.Fatalf("the level before the parse: %+v %v", p, err)
		}
		cat.parse("hello")
		if p, err := e.Package(ctx, deb, "hello", discover.VersionsRequest{}); err != nil || !p.Found {
			t.Fatalf("the lookup after the parse, within the period: %+v %v", p, err)
		}
		if p, err := e.Search(ctx, deb, discover.SearchQuery{Text: "hello"}); err != nil || len(p.Hits) != 1 {
			t.Fatalf("the search after the parse, within the period: %+v %v", p, err)
		}
		if p, err := e.Children(ctx, deb, root); err != nil || len(p.Packages) != 1 {
			t.Fatalf("the level after the parse, within the period: %+v %v", p, err)
		}
		reads := cat.count()
		_, _ = e.Package(ctx, deb, "hello", discover.VersionsRequest{})
		_, _ = e.Search(ctx, deb, discover.SearchQuery{Text: "hello"})
		_, _ = e.Children(ctx, deb, root)
		if n := cat.count(); n != reads {
			t.Fatalf("the pages that found something read the rows again (%d reads, want %d): they are kept for the period", n, reads)
		}
		if gets, artifacts := l.totals(); gets != 0 || artifacts != 0 {
			t.Fatalf("a catalogue of the index sent %d requests and %d artifact reads, want none", gets, artifacts)
		}
	})
	t.Run("R-CUR-007 a search's text is 1 to 128 characters", func(t *testing.T) {
		e := newEngine(t, &fakeCatalogue{}, &fakeLender{}, defaults(), func() time.Time { return t0 })
		for _, q := range []string{"  ", strings.Repeat("a", 129)} {
			if _, err := e.Search(ctx, tgt, discover.SearchQuery{Text: q}); code(err) != errs.CodeValidation {
				t.Errorf("%q: %v", q, err)
			}
		}
	})
}

// unknownCatalogue answers a level as a format that tells an unknown level from an empty one: one request, then the page it holds.
type unknownCatalogue struct {
	fakeCatalogue
	page discover.LevelPage
}

func (c *unknownCatalogue) Children(ctx context.Context, u discover.Upstream, _ discover.LevelRequest) (discover.LevelPage, error) {
	if _, err := u.Get(ctx, u.Base(), "", 0); err != nil {
		return discover.LevelPage{}, err
	}
	p := c.page
	if p.Unknown != nil {
		unknown := *p.Unknown
		p.Unknown = &unknown
	}
	return p, nil
}

// TestR_CUR_007_AnUnknownLevelIsKeptAndAPageWithEntriesIsKnown: a level the format says the upstream does not have is kept for the
// period like any page an asked upstream answered, and handed out as a copy; a page that lists something is known whatever the format
// set (Q-CUR-165). A fake format and a fake upstream in process; milliseconds, no data.
func TestR_CUR_007_AnUnknownLevelIsKeptAndAPageWithEntriesIsKnown(t *testing.T) {
	ctx := context.Background()
	t0 := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	tgt := Target{RepoID: 7, Repo: "it/oci-remote", Format: "oci", Upstream: "https://registry-1.docker.io/"}
	rq := discover.LevelRequest{Level: discover.LevelRef{Path: []string{"guided-traffic"}}}

	t.Run("R-CUR-007 an unknown level is kept for the period and copied", func(t *testing.T) {
		l := &fakeLender{}
		cat := &unknownCatalogue{page: discover.LevelPage{Done: true, Unknown: &discover.UnknownLevel{
			Rule: discover.NameRuleLowercaseDigits, Forbidden: "-", Suggestion: "guidedtraffic",
		}}}
		e := newEngine(t, cat, l, defaults(), func() time.Time { return t0 })
		first, err := e.Children(ctx, tgt, rq)
		if err != nil || first.Unknown == nil || first.Unknown.Suggestion != "guidedtraffic" {
			t.Fatalf("%+v %v", first, err)
		}
		first.Unknown.Suggestion = "changed by a caller"
		second, err := e.Children(ctx, tgt, rq)
		if err != nil || second.Unknown == nil || second.Unknown.Suggestion != "guidedtraffic" {
			t.Fatalf("the cached page is %+v %v", second.Unknown, err)
		}
		if gets, _ := l.totals(); gets != 1 {
			t.Fatalf("two identical calls sent %d requests, want 1", gets)
		}
	})
	t.Run("R-CUR-007 a page that lists something is known", func(t *testing.T) {
		cat := &unknownCatalogue{page: discover.LevelPage{Packages: []discover.Hit{{Name: "a/b"}}, Done: true, Unknown: &discover.UnknownLevel{}}}
		e := newEngine(t, cat, &fakeLender{}, defaults(), func() time.Time { return t0 })
		if p, err := e.Children(ctx, tgt, rq); err != nil || p.Unknown != nil {
			t.Fatalf("%+v %v", p, err)
		}
	})
}
