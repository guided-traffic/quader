package formathost

import (
	"context"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/packages"
	"github.com/guided-traffic/quader/internal/qname"
)

// pkgFormat places a path in the package of its first segment, as a format's Classify does.
type pkgFormat struct{ portFormat }

func (pkgFormat) Classify(p formats.Path) formats.PathInfo {
	name, _, _ := strings.Cut(p.Path, "/")
	return formats.PathInfo{Kind: formats.Content, Package: name}
}

// notingDecider allows every decision but those of the packages in deny.
type notingDecider struct {
	fakeDecider
	deny map[string]bool
}

func (d *notingDecider) DecideOn(_ context.Context, _ *authz.Index, _ authz.Subject, c authz.Coordinate) authz.Decision {
	if d.deny[c.Package] {
		return authz.Decision{Outcome: authz.DenyHidden}
	}
	return authz.Decision{}
}

func (d *notingDecider) WikiReadableOn(_ *authz.Index, _ authz.Subject, c authz.Coordinate) bool {
	return !d.deny[c.Package]
}

func (d *notingDecider) PathsAllowedOn(_ context.Context, _ *authz.Index, _ authz.Subject, _ authz.Coordinate, _ authz.Action, paths []string, _ authz.Classifier) ([]bool, error) {
	return make([]bool, len(paths)), nil
}

// rowScope is a read scope whose rows the decision point allows.
type rowScope struct{ authz.ReadScope }

func (rowScope) MayRead(qname.Repo, string, string) bool { return true }

type rowScoper struct{}

func (rowScoper) ReadScopeOn(context.Context, *authz.Index, authz.Subject, qname.Repo, []qname.Repo) (authz.ReadScope, error) {
	return rowScope{}, nil
}

// TestR_BAS_081_TheHostNotesThePackageOfEveryDecision: Authorize, allowed or refused, WikiReadable, each path of PathsAllowed in the
// package Classify places it in, and each MayRead row of the read scope the host hands out are the decisions of a response; the
// response's package is the one package every one of them named (R-BAS-081). A request that is no recognized Bastion request records
// nothing.
func TestR_BAS_081_TheHostNotesThePackageOfEveryDecision(t *testing.T) {
	d := &notingDecider{deny: map[string]bool{"secret": true}}
	pk, err := packages.NewStore(nopRunner{})
	if err != nil {
		t.Fatal(err)
	}
	f := pkgFormat{portFormat{id: "probe"}}
	h, err := New(Options{Runner: nopRunner{}, Decider: d, Items: (*items.Store)(nil), Packages: pk, Deps: Deps{Formats: func(id string) (formats.Format, bool) {
		return f, id == "probe"
	}}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := NewResolver(struct{ VirtualEngine }{}, rowScoper{}, func(string) (formats.Format, bool) { return f, true })
	if err != nil {
		t.Fatal(err)
	}
	read := []formats.Action{formats.ActionRead}
	authorize := func(pkg string) func(context.Context, *formats.Request) {
		return func(ctx context.Context, rq *formats.Request) {
			_ = h.Authorize(ctx, rq, formats.Target{Package: pkg, Actions: read})
		}
	}
	mayRead := func(pkg string) func(context.Context, *formats.Request) {
		return func(ctx context.Context, rq *formats.Request) {
			scope, err := res.ReadScope(ctx, rq, []qname.Repo{rq.Repo.Name})
			if err != nil {
				t.Fatal(err)
			}
			scope.MayRead(rq.Repo.Name, pkg, "")
		}
	}
	paths := func(ps ...string) func(context.Context, *formats.Request) {
		return func(ctx context.Context, rq *formats.Request) { _, _ = h.PathsAllowed(ctx, rq, formats.ActionRead, ps) }
	}
	wiki := func(pkg string) func(context.Context, *formats.Request) {
		return func(ctx context.Context, rq *formats.Request) { h.WikiReadable(ctx, rq, formats.Target{Package: pkg}) }
	}
	for _, c := range []struct {
		name      string
		decisions []func(context.Context, *formats.Request)
		want      string
	}{
		{"Authorize(P) twice", []func(context.Context, *formats.Request){authorize("left-pad"), authorize("left-pad")}, "left-pad"},
		{"Authorize(P) and MayRead(Q)", []func(context.Context, *formats.Request){authorize("left-pad"), mayRead("right-pad")}, ""},
		{"Authorize(P) and MayRead(P)", []func(context.Context, *formats.Request){authorize("left-pad"), mayRead("left-pad")}, "left-pad"},
		{"Authorize(P) and a package-less Authorize", []func(context.Context, *formats.Request){authorize("left-pad"), authorize("")}, ""},
		{"a refused Authorize(Q) after P", []func(context.Context, *formats.Request){authorize("left-pad"), authorize("secret")}, ""},
		{"a refused Authorize(P) alone", []func(context.Context, *formats.Request){authorize("secret")}, "secret"},
		{"PathsAllowed over paths of P", []func(context.Context, *formats.Request){paths("left-pad/1.0.0/a.tgz", "left-pad/2.0.0/a.tgz")}, "left-pad"},
		{"PathsAllowed over paths of P and Q", []func(context.Context, *formats.Request){paths("left-pad/a.tgz", "right-pad/a.tgz")}, ""},
		{"WikiReadable(P)", []func(context.Context, *formats.Request){wiki("left-pad")}, "left-pad"},
		{"WikiReadable(P) and Authorize(Q)", []func(context.Context, *formats.Request){wiki("left-pad"), authorize("right-pad")}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx, rec := bastionmain.WithAbsence(context.Background())
			rq := testRequest(formats.Metadata)
			for _, dec := range c.decisions {
				dec(ctx, rq)
			}
			if got := rec.Package(); got != c.want {
				t.Fatalf("package %q, want %q", got, c.want)
			}
		})
	}
	t.Run("a path of a format the host does not know names no package", func(t *testing.T) {
		ctx, rec := bastionmain.WithAbsence(context.Background())
		rq := testRequest(formats.Metadata)
		rq.Repo.Format = "ghost"
		_, _ = h.PathsAllowed(ctx, rq, formats.ActionRead, []string{"left-pad/a.tgz"})
		if rec.Package() != "" {
			t.Fatalf("package %q", rec.Package())
		}
	})
	t.Run("an ordinary request records nothing and is decided as before", func(t *testing.T) {
		rq := testRequest(formats.Metadata)
		if err := h.Authorize(context.Background(), rq, formats.Target{Package: "left-pad", Actions: read}); err != nil {
			t.Fatal(err)
		}
		if ok, err := h.PathsAllowed(context.Background(), rq, formats.ActionRead, []string{"left-pad/a.tgz"}); err != nil || len(ok) != 1 {
			t.Fatalf("%v %v", ok, err)
		}
	})
}
