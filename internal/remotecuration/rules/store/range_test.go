package store

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/formats/versionkey"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
)

// dotted is the test format's order: dotted numbers, compared part by part; any other string is no version.
type dotted struct{}

func (dotted) parts(v string) ([]int, bool) {
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

func (d dotted) SortKey(v string) []byte {
	_, ok := d.parts(v)
	return versionkey.Of(nil, ok, v)
}

func (d dotted) Compare(a, b string) int {
	pa, oka := d.parts(a)
	pb, okb := d.parts(b)
	if !oka || !okb {
		return strings.Compare(a, b)
	}
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] != pb[i] {
			return pa[i] - pb[i]
		}
	}
	return len(pa) - len(pb)
}

// threeParts is the test format's canon: a version of two parts gets its third (NuGet's 1.0 is 1.0.0); a tag that is no version passes,
// as OCI's canon passes latest.
type threeParts struct{}

func (threeParts) Canonical(v string) (string, bool) {
	if v == "" || strings.ContainsAny(v, " /") {
		return "", false
	}
	if _, ok := (dotted{}).parts(v); ok && strings.Count(v, ".") == 1 {
		return v + ".0", true
	}
	return v, true
}

type lower struct{}

func (lower) Normalize(n string) (string, bool) { return strings.ToLower(n), n != "" }

func rangeStore() *Store {
	reg := catalog.NewRegistry()
	facts := func(id string) catalog.Facts {
		return catalog.Facts{
			ID: catalog.ID(id), Classes: []catalog.Class{catalog.Remote}, Layout: id, ProtocolBasePath: "/api/" + id + "/{project}/{key}/",
			PatternSubject: catalog.SubjectName, PackageNames: true,
		}
	}
	reg.Register(catalog.Entry{Facts: facts("ordered"), Names: lower{}, VersionCanon: threeParts{}, VersionOrder: dotted{}})
	reg.Register(catalog.Entry{Facts: facts("unordered"), Names: lower{}, VersionCanon: threeParts{}})
	return &Store{o: Options{Catalog: reg, Compile: func(string) (rules.Matcher, error) { return func(string) bool { return false }, nil }}}
}

// TestR_CUR_026_TheStoreChecksARangeAndWritesItCanonical holds the store's check of a range (R-CUR-026, Q-CUR-221, Q-CUR-229): the
// bounds in the format's canon and order, the refusals of the bulk's range, and the pointers below the switch's /range.
func TestR_CUR_026_TheStoreChecksARangeAndWritesItCanonical(t *testing.T) {
	s := rangeStore()
	ordered := Target{Format: "ordered"}
	allow := func(r rc.Range) Input { return Input{Kind: rc.KindPackage, Name: "Lodash", Effect: rc.Allow, Range: r} }

	r, err := s.Canonical(ordered, allow(rc.Range{From: " 1.0 ", To: "2.0.0"}))
	if err != nil {
		t.Fatal(err)
	}
	if want := (rc.Range{From: "1.0.0", To: "2.0.0"}); r.Range != want || r.Name != "lodash" {
		t.Errorf("canonical %+v, want %+v of lodash", r, want)
	}
	if r, _ := s.Canonical(ordered, allow(rc.Range{From: "3.0.0"})); r.Range != (rc.Range{From: "3.0.0", ToInclusive: true}) {
		t.Errorf("a range without an upper bound is spelled %+v", r.Range)
	}

	for _, c := range []struct {
		name               string
		t                  Target
		in                 Input
		at                 string
		code, ptr, ptrCode string
	}{
		{"a tag that is no version", ordered, allow(rc.Range{From: "latest"}), "", errs.CodeInvalidVersion, "/fromVersion", errs.CodeInvalidVersion},
		{"a range of none", ordered, allow(rc.Range{From: "2.0.0", To: "1.0.0", ToInclusive: true}), "", errs.CodeValidation, "/toVersion", "out_of_range"},
		{"below its own lower bound", ordered, allow(rc.Range{From: "2.0", To: "2.0.0"}), "", errs.CodeValidation, "/toVersion", "out_of_range"},
		{"on a block", ordered, Input{Kind: rc.KindPackage, Name: "a", Effect: rc.Block, Range: rc.Range{To: "1.0.0"}}, "", errs.CodeValidation, "/toVersion", errs.CodeNotApplicable},
		{"on a version", ordered, Input{Kind: rc.KindVersion, Name: "a", Version: "1.0.0", Effect: rc.Allow, Range: rc.Range{From: "1.0.0"}}, "", errs.CodeValidation, "/fromVersion", errs.CodeNotApplicable},
		{"a format without an order", Target{Format: "unordered"}, allow(rc.Range{From: "1.0.0"}), "", errs.CodeValidation, "/fromVersion", errs.CodeNotApplicable},
		{"below the switch's range", ordered, allow(rc.Range{To: "main"}), "/range", errs.CodeInvalidVersion, "/range/toVersion", errs.CodeInvalidVersion},
	} {
		_, err := s.canonical(c.t, c.in, c.at)
		var e *errs.Error
		if !errors.As(err, &e) {
			t.Errorf("%s: %v, want a refusal", c.name, err)
			continue
		}
		if e.Kind != errs.Unprocessable || e.Code != c.code || len(e.Fields) != 1 || e.Fields[0].Pointer != c.ptr || e.Fields[0].Code != c.ptrCode {
			t.Errorf("%s: %s %s %+v, want 422 %s at %s (%s)", c.name, e.Kind, e.Code, e.Fields, c.code, c.ptr, c.ptrCode)
		}
	}
}

// TestR_CUR_026_ASwitchKeepsOrWritesTheRange holds what a package's switch leaves of its own rule (R-CUR-026, Q-CUR-200): a range asked
// is written whatever the package inherits, one that bounds nothing removes the range, none asked keeps the allow's range, and a switch
// turned off drops it with the allow.
func TestR_CUR_026_ASwitchKeepsOrWritesTheRange(t *testing.T) {
	rng := rc.Range{From: "2.0.0", To: "3.0.0"}
	allowRow := &q.DeliveryRulesOfPackageRow{
		Kind: "package", Name: pgtype.Text{String: "lodash", Valid: true}, Effect: "allow",
		FromVersion: pgtype.Text{String: "1.0.0", Valid: true}, ToInclusive: true,
	}
	on := rules.Rule{Kind: rc.KindPackage, Name: "lodash", Effect: rc.Allow}
	cases := []struct {
		name     string
		in       SwitchInput
		r        rules.Rule
		inherits bool
		have     *q.DeliveryRulesOfPackageRow
		keep     bool
		want     rc.Range
	}{
		{"a range asked where the mode delivers", SwitchInput{Delivered: true, Range: &rng}, rules.Rule{Kind: rc.KindPackage, Name: "lodash", Effect: rc.Allow, Range: rng}, true, nil, true, rng},
		{"every version asked where the mode delivers", SwitchInput{Delivered: true, Range: &rc.Range{}}, on, true, allowRow, false, rc.Range{}},
		{"every version asked where the mode refuses", SwitchInput{Delivered: true, Range: &rc.Range{}}, on, false, allowRow, true, rc.Range{}},
		{"no range asked keeps the allow's", SwitchInput{Delivered: true}, on, true, allowRow, true, rc.Range{From: "1.0.0", ToInclusive: true}},
		{"turned off drops it", SwitchInput{Delivered: false}, rules.Rule{Kind: rc.KindPackage, Name: "lodash", Effect: rc.Block}, true, allowRow, true, rc.Range{}},
	}
	for _, c := range cases {
		got, keep := ownRuleWanted(c.in, c.r, c.inherits, c.have)
		if keep != c.keep || (keep && got.Range != c.want) {
			t.Errorf("%s: keep %v with %+v, want %v with %+v", c.name, keep, got.Range, c.keep, c.want)
		}
	}
	if !narrowsRange(rules.Rule{Kind: rc.KindPackage, Effect: rc.Allow, Range: rng}, rc.Allow, rc.Range{}) ||
		narrowsRange(rules.Rule{Kind: rc.KindPackage, Effect: rc.Allow, Range: rng}, rc.Allow, rng) ||
		narrowsRange(rules.Rule{Kind: rc.KindPackage, Effect: rc.Allow}, rc.Allow, rng) {
		t.Error("a range written where the allow had another or none is journaled; the same range and a range removed are not")
	}
}
