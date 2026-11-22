package bastionmain

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/guided-traffic/quader/internal/qname"

	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/bastion/iface"
)

type fakeReacher struct {
	out  map[qname.Repo]authz.ReachResult
	pkgs map[string]authz.Reach
	err  error
}

func (f fakeReacher) ReachOn(context.Context, *authz.Index, authz.Subject, []qname.Repo) (map[qname.Repo]authz.ReachResult, error) {
	return f.out, f.err
}

func (f fakeReacher) PackageReachOn(context.Context, *authz.Index, authz.Subject, qname.Repo, []string) (map[string]authz.Reach, error) {
	return f.pkgs, f.err
}

// TestR_BAS_080_ShapeResolverMapsPackageReachToTheWire: a package shape is full where the decision point's package reach is full and
// partial for every other name, one the decision point did not answer included; never none (Q-BAS-243).
func TestR_BAS_080_ShapeResolverMapsPackageReachToTheWire(t *testing.T) {
	r := NewShapeResolver(fakeReacher{pkgs: map[string]authz.Reach{"@acme/sdk": authz.ReachFull, "x": authz.ReachPartial, "gone": authz.ReachNone}})
	got, err := r.PackageShapes(t.Context(), nil, authz.Subject{}, qr("npm-local"), []string{"@acme/sdk", "x", "gone", "unanswered"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]iface.Read{"@acme/sdk": iface.ReadFull, "x": iface.ReadPartial, "gone": iface.ReadPartial, "unanswered": iface.ReadPartial}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: %s, want %s", k, got[k], w)
		}
	}
	boom := errors.New("stale")
	if _, err := NewShapeResolver(fakeReacher{err: boom}).PackageShapes(t.Context(), nil, authz.Subject{}, qr("a"), []string{"p"}); !errors.Is(err, boom) {
		t.Fatalf("%v", err)
	}
}

// TestR_BAS_082_ARegistryGrantLimitsAPackageShape: a package shape is full or partial; a registry token's is full only on an image its
// grant holds pull on, whatever the decision point computes for the token it was issued for.
func TestR_BAS_082_ARegistryGrantLimitsAPackageShape(t *testing.T) {
	for _, c := range []struct {
		read           iface.Read
		granted, pulls bool
		want           iface.Read
	}{
		{iface.ReadFull, false, false, iface.ReadFull},
		{iface.ReadPartial, false, false, iface.ReadPartial},
		{iface.ReadNone, false, false, iface.ReadPartial},
		{"", false, false, iface.ReadPartial},
		{iface.ReadFull, true, true, iface.ReadFull},
		{iface.ReadFull, true, false, iface.ReadPartial},
		{iface.ReadPartial, true, true, iface.ReadPartial},
		{iface.ReadNone, true, true, iface.ReadPartial},
	} {
		if got := CapRegistryPackage(c.read, c.granted, c.pulls); got != c.want {
			t.Errorf("%q granted %v pulls %v: %s, want %s", c.read, c.granted, c.pulls, got, c.want)
		}
	}
}

// TestR_BAS_081_TheResponsesPackageIsTheOnePackageEveryDecisionNamed: P exactly when at least one decision named P, every decision
// named P and none named no package; a decision without a package or with another one clears the record for good.
func TestR_BAS_081_TheResponsesPackageIsTheOnePackageEveryDecisionNamed(t *testing.T) {
	for _, c := range []struct {
		name  string
		notes []string
		want  string
	}{
		{"no decision", nil, ""},
		{"one decision", []string{"@acme/sdk"}, "@acme/sdk"},
		{"the same package twice", []string{"@acme/sdk", "@acme/sdk"}, "@acme/sdk"},
		{"a second package", []string{"@acme/sdk", "x"}, ""},
		{"a package-less decision after it", []string{"@acme/sdk", ""}, ""},
		{"a package-less decision before it", []string{"", "@acme/sdk"}, ""},
		{"the first package again after another", []string{"@acme/sdk", "x", "@acme/sdk"}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx, a := WithAbsence(t.Context())
			if !NotesPackages(ctx) {
				t.Fatal("a recognized request records its decisions")
			}
			for _, p := range c.notes {
				NotePackage(ctx, p)
			}
			if got := a.Package(); got != c.want {
				t.Fatalf("%q, want %q", got, c.want)
			}
		})
	}
	var none *Absence
	if none.Package() != "" || NotesPackages(t.Context()) {
		t.Fatal("no record names no package")
	}
	NotePackage(t.Context(), "x") // no record on the context: a no-op
}

// TestR_BAS_012_ShapeResolverMapsReachToTheWire: full and partial carry no status, none carries the status a
// refused read has (R-AUTHZ-016): 401 for an anonymous subject, 403 for a visible refusal, 404 for a hidden or
// missing repository.
func TestR_BAS_012_ShapeResolverMapsReachToTheWire(t *testing.T) {
	r := NewShapeResolver(fakeReacher{out: map[qname.Repo]authz.ReachResult{
		qr("a"): {Reach: authz.ReachFull},
		qr("b"): {Reach: authz.ReachPartial},
		qr("c"): {Reach: authz.ReachNone, Denial: authz.DenyUnauthenticated},
		qr("d"): {Reach: authz.ReachNone, Denial: authz.DenyVisible},
		qr("e"): {Reach: authz.ReachNone, Denial: authz.DenyHidden},
		qr("f"): {Reach: authz.ReachNone},
	}})
	got, err := r.Shapes(t.Context(), nil, authz.Subject{}, []qname.Repo{qr("a"), qr("b"), qr("c"), qr("d"), qr("e"), qr("f")})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Shape{
		"a": {Read: iface.ReadFull},
		"b": {Read: iface.ReadPartial},
		"c": {Read: iface.ReadNone, Status: http.StatusUnauthorized},
		"d": {Read: iface.ReadNone, Status: http.StatusForbidden},
		"e": {Read: iface.ReadNone, Status: http.StatusNotFound},
		"f": {Read: iface.ReadNone, Status: http.StatusNotFound},
	}
	for k, w := range want {
		if got[qr(k)] != w {
			t.Errorf("%s: %+v, want %+v", k, got[qr(k)], w)
		}
	}
	t.Run("a refusal to decide is the error of the resolver", func(t *testing.T) {
		boom := errors.New("stale")
		if _, err := NewShapeResolver(fakeReacher{err: boom}).Shapes(t.Context(), nil, authz.Subject{}, []qname.Repo{qr("a")}); !errors.Is(err, boom) {
			t.Fatalf("%v", err)
		}
	})
}

// TestR_BAS_013_AbsenceIsAbsentOnlyWhenSomethingWasMissingAndNothingWasUnknown: the record of the lookups of one
// request, and the notes on a request that has none.
func TestR_BAS_013_AbsenceIsAbsentOnlyWhenSomethingWasMissingAndNothingWasUnknown(t *testing.T) {
	ctx, a := WithAbsence(t.Context())
	if a.Absent() {
		t.Fatal("nothing noted is not absent")
	}
	NoteUnknown(ctx)
	if a.Absent() {
		t.Fatal("unknown alone is not absent")
	}
	ctx, a = WithAbsence(t.Context())
	NoteMissing(ctx)
	if !a.Absent() {
		t.Fatal("a missing item is absent")
	}
	NoteUnknown(ctx)
	if a.Absent() {
		t.Fatal("a refusal after a missing lookup makes the 404 mean less")
	}
	var nilAbsence *Absence
	if nilAbsence.Absent() {
		t.Fatal("no record is not absent")
	}
	NoteMissing(t.Context()) // no record on the context: a no-op
	NoteUnknown(t.Context())
}
