package catalogue

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// documented is a format whose upstream serves READMEs: "handed" carries its README in the lookup's answers (npm's package document),
// "asked" costs a request (NuGet's flat container), "far" names it on a host the repository does not reach, and "broken" fails.
type documented struct{ fakeCatalogue }

func (d *documented) Package(ctx context.Context, u discover.Upstream, name string, rq discover.VersionsRequest) (discover.PackageInfo, error) {
	p, err := d.fakeCatalogue.Package(ctx, u, name, rq)
	p.About = discover.About{Version: "1.0.0", License: "MIT", Links: []discover.Link{
		{Kind: discover.LinkHomepage, URL: "javascript:alert(1)"}, {Kind: discover.LinkRepository, URL: "git+https://github.com/acme/a.git"},
	}}
	if name == "handed" {
		p.Readme = &discover.Readme{Version: "1.0.0", Found: true, Text: "# handed"}
	}
	return p, err
}

func (d *documented) Readme(ctx context.Context, u discover.Upstream, name, version string) (discover.Readme, error) {
	switch name {
	case "far":
		return discover.Readme{Version: version, Unreachable: "readme.example"}, nil
	case "broken":
		return discover.Readme{}, errs.E(errs.UpstreamFailed, errs.CodeUpstreamError, "The upstream answered 500 to the README.")
	}
	if _, err := u.Get(ctx, u.Base(), "", 0); err != nil {
		return discover.Readme{}, err
	}
	return discover.Readme{Version: version, Found: true, Text: "# " + name + "\n" + strings.Repeat("x", discover.MaxReadmeBytes)}, nil
}

// TestR_CUR_028_TheReadmeCall holds the engine's README call to R-CUR-028: a README the lookup handed over costs no request and is not
// answered with the package; an asked one is read once within the period and cut at the bound; a host the repository does not reach,
// a failure and a format without READMEs are answered as such, and a failure is not kept. The lookup's facts are bounded: an address
// that is no http or https address is dropped, npm's git form is the https address. Milliseconds.
func TestR_CUR_028_TheReadmeCall(t *testing.T) {
	ctx := context.Background()
	tg := Target{RepoID: 7, Repo: "it/npm-remote", Format: "npm", Upstream: "https://registry.example/", Person: "p1"}
	l := &fakeLender{}
	e := newEngine(t, &documented{}, l, defaults(), time.Now)

	t.Run("R-CUR-028 a README the lookup handed over costs no request and is not on the package", func(t *testing.T) {
		p, err := e.Package(ctx, tg, "handed", discover.VersionsRequest{})
		if err != nil || p.Readme != nil {
			t.Fatalf("the lookup: %v, readme %v", err, p.Readme)
		}
		if len(p.About.Links) != 1 || p.About.Links[0].URL != "https://github.com/acme/a" {
			t.Fatalf("the facts' links %v, want the repository's https address alone", p.About.Links)
		}
		before, _ := l.totals()
		for _, v := range []string{"1.0.0", ""} {
			r, err := e.Readme(ctx, tg, "handed", v)
			if err != nil || !r.Found || r.Text != "# handed" {
				t.Fatalf("the README of %q: %+v %v", v, r, err)
			}
		}
		if after, _ := l.totals(); after != before {
			t.Fatalf("the README call sent %d requests, want none", after-before)
		}
	})
	t.Run("R-CUR-028 an asked README is read once within the period and cut at the bound", func(t *testing.T) {
		before, _ := l.totals()
		for range 2 {
			r, err := e.Readme(ctx, tg, "asked", "1.0.0")
			if err != nil || !r.Found || !r.Truncated || len(r.Text) > discover.MaxReadmeBytes {
				t.Fatalf("the README: found %v truncated %v %d bytes, %v", r.Found, r.Truncated, len(r.Text), err)
			}
		}
		if after, _ := l.totals(); after-before != 1 {
			t.Fatalf("two README calls sent %d requests, want 1", after-before)
		}
	})
	t.Run("R-CUR-028 a host the repository does not reach, a failure and a format without READMEs", func(t *testing.T) {
		if r, err := e.Readme(ctx, tg, "far", "1.0.0"); err != nil || r.Found || r.Unreachable != "readme.example" {
			t.Fatalf("far: %+v %v", r, err)
		}
		for range 2 {
			if _, err := e.Readme(ctx, tg, "broken", "1.0.0"); code(err) != errs.CodeUpstreamError {
				t.Fatalf("broken: %v", err)
			}
		}
		plain := newEngine(t, &fakeCatalogue{}, &fakeLender{}, defaults(), time.Now)
		if _, err := plain.Readme(ctx, tg, "a", ""); code(err) != errs.CodeCatalogueNotOffered || plain.ServesReadme("npm") {
			t.Fatalf("a format without READMEs: %v", err)
		}
		if !e.ServesReadme("npm") {
			t.Fatal("ServesReadme of a documented format is false")
		}
	})
	t.Run("R-CUR-028 the README spends the budget like every request", func(t *testing.T) {
		st := defaults()
		st.RequestsPerMinutePerPerson = 1
		tight := newEngine(t, &documented{}, &fakeLender{}, st, time.Now)
		if _, err := tight.Readme(ctx, tg, "one", "1.0.0"); err != nil {
			t.Fatal(err)
		}
		_, err := tight.Readme(ctx, tg, "two", "1.0.0")
		var ee *errs.Error
		if code(err) != errs.CodeCatalogueBudgetExhausted || !errors.As(err, &ee) {
			t.Fatalf("the second README of the minute: %v, want catalogue_budget_exhausted", err)
		}
	})
}
