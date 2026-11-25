package rules

import (
	"strconv"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/versionkey"
	"github.com/guided-traffic/quader/internal/qname"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
)

// semverOrder is the test's order of versions: SemVer's precedence over "major.minor.patch[-pre][+build]", the build metadata a
// tie-break after the release, as the npm and Ansible Galaxy view adapters order them; any other string (a tag such as latest) is no
// version. The rules read only the class byte of a sort key and Compare.
type semverOrder struct{}

func (semverOrder) parse(v string) (nums [3]int, pre string, ok bool) {
	core, _, _ := strings.Cut(v, "+")
	core, pre, _ = strings.Cut(core, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return nums, "", false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nums, "", false
		}
		nums[i] = n
	}
	return nums, pre, true
}

func (o semverOrder) SortKey(v string) []byte {
	_, _, ok := o.parse(v)
	return versionkey.Of(nil, ok, v)
}

func (o semverOrder) Compare(a, b string) int {
	na, pa, oka := o.parse(a)
	nb, pb, okb := o.parse(b)
	switch {
	case !oka || !okb:
		return strings.Compare(a, b)
	}
	for i := range na {
		if na[i] != nb[i] {
			return na[i] - nb[i]
		}
	}
	switch {
	case pa == "" && pb != "":
		return 1
	case pa != "" && pb == "":
		return -1
	case pa != pb:
		return strings.Compare(pa, pb)
	}
	return strings.Compare(a, b) // the build metadata after the release
}

var semverCanon = Canon{Order: semverOrder{}}

func ranged(n string, r rc.Range) Rule {
	return Rule{Kind: rc.KindPackage, Name: n, Effect: rc.Allow, Range: r}
}

// TestR_CUR_026_TheRangeDecides holds the range of a package's allow (R-CUR-026, Q-CUR-220 to Q-CUR-224): inside, below and above it,
// at each bound included and excluded, an open lower and an open upper bound, a version rule above the range in both directions, a tag
// that is no version outside it, the package's decision delivered with its range, and the reason of an emptied document.
func TestR_CUR_026_TheRangeDecides(t *testing.T) {
	const (
		ok      = "delivered"
		outside = "outsideRange"
		blocked = "blocked"
	)
	below2 := rc.Range{From: "1.1.0", To: "2.0.0"}
	upTo2 := rc.Range{To: "2.0.0", ToInclusive: true}
	from3 := rc.Range{From: "3.0.0"}
	type ask struct{ version, want string }
	cases := []struct {
		name  string
		mode  rc.Mode
		rules []Rule
		asks  []ask
	}{
		{"from a version below another", rc.AllExceptBlocked, []Rule{ranged("lodash", below2)}, []ask{
			{"1.0.0", outside},
			{"1.1.0", ok},
			{"1.5.0", ok},
			{"2.0.0-rc.1", ok},
			{"2.0.0", outside},
			{"2.1.0", outside},
			{"1.1.0+build.1", ok},
			{"latest", outside},
		}},
		{"up to a version, included", rc.AllExceptBlocked, []Rule{ranged("lodash", upTo2)}, []ask{
			{"0.0.1", ok}, {"2.0.0", ok}, {"2.0.0+build.1", outside}, {"2.0.1", outside}, {"main", outside},
		}},
		{"an open upper bound", rc.AllExceptBlocked, []Rule{ranged("lodash", from3)}, []ask{{"2.9.9", outside}, {"3.0.0", ok}, {"10.0.0", ok}}},
		{"in mode onlyAllowed the same", rc.OnlyAllowed, []Rule{ranged("lodash", below2)}, []ask{{"1.5.0", ok}, {"3.0.0", outside}}},
		{"a version rule above the range, both ways", rc.AllExceptBlocked, []Rule{
			ranged("lodash", below2), ver("lodash", "3.0.0", rc.Allow), ver("lodash", "1.5.0", rc.Block), ver("lodash", "latest", rc.Allow),
		}, []ask{{"3.0.0", ok}, {"1.5.0", blocked}, {"latest", ok}, {"1.6.0", ok}, {"3.0.1", outside}}},
		{
			"a section block under the package's ranged allow", rc.AllExceptBlocked,
			[]Rule{sec("lod*", rc.Block), ranged("lodash", below2)},
			[]ask{{"1.5.0", ok}, {"2.5.0", outside}},
		},
	}
	for _, tc := range cases {
		s := compile(t, tc.mode, semverCanon, tc.rules...)
		for _, a := range tc.asks {
			d := s.Decide("lodash", a.version)
			if got := outcome(d); got != a.want {
				t.Errorf("%s: lodash %s is %s, want %s", tc.name, a.version, got, a.want)
			}
			if a.want == outside && (d.By.Kind != rc.KindPackage || d.By.Range == nil) {
				t.Errorf("%s: lodash %s outside the range names %+v, want the package's allow with its range", tc.name, a.version, d.By)
			}
		}
	}

	s := compile(t, rc.OnlyAllowed, semverCanon, ranged("lodash", below2), pkg("express", rc.Allow))
	if d := s.Package("lodash"); !d.Delivered || d.By.Range == nil || *d.By.Range != below2.Normal() {
		t.Errorf("the package's decision of a ranged allow is %+v, want delivered with its range", d)
	}
	if d := s.Level("lodash"); !d.Delivered || d.By.Range == nil {
		t.Errorf("the package level of a ranged allow is %+v, want delivered with its range", d)
	}
	if d := s.Inherited("lodash", "2.5.0"); d.Delivered || d.Reason != rc.ReasonOutsideRange {
		t.Errorf("a version outside the range inherits %+v, want outsideRange", d)
	}
	if d := s.Inherited("lodash", "1.5.0"); !d.Delivered {
		t.Errorf("a version inside the range inherits %+v, want delivered", d)
	}
	if got := s.EmptyReason("lodash"); got != rc.ReasonOutsideRange {
		t.Errorf("an emptied document of a ranged package is refused with %q, want outsideRange", got)
	}
	if got := s.EmptyReason("express"); got != rc.ReasonNotAllowed {
		t.Errorf("an emptied document of a package without a range is refused with %q, want the mode's", got)
	}
	if !s.NarrowsVersions("lodash") || s.NarrowsVersions("express") {
		t.Error("a range decides the versions apart from the package; a plain allow does not")
	}
	if r, ok := s.Range("lodash"); !ok || r != below2.Normal() {
		t.Errorf("Range(lodash) = %+v, %v", r, ok)
	}
}

// TestR_CUR_026_ARangeTheStoreCannotWriteDisablesTheRepository: a range on another rule than a package's allow, in a format without an
// order, with a bound the format does not rank or holding no version does not compile (fail closed, R-CUR-026).
func TestR_CUR_026_ARangeTheStoreCannotWriteDisablesTheRepository(t *testing.T) {
	r := rc.Range{From: "1.0.0"}
	for name, c := range map[string]struct {
		canon Canon
		rule  Rule
	}{
		"a block":           {semverCanon, Rule{Kind: rc.KindPackage, Name: "a", Effect: rc.Block, Range: r}},
		"a version rule":    {semverCanon, Rule{Kind: rc.KindVersion, Name: "a", Version: "1.0.0", Effect: rc.Allow, Range: r}},
		"a section":         {semverCanon, Rule{Kind: rc.KindSection, Pattern: "a*", Effect: rc.Allow, Range: r}},
		"no order":          {Canon{}, ranged("a", r)},
		"a bound unranked":  {semverCanon, ranged("a", rc.Range{To: "latest"})},
		"a range of none":   {semverCanon, ranged("a", rc.Range{From: "2.0.0", To: "1.0.0", ToInclusive: true})},
		"one bound, closed": {semverCanon, ranged("a", rc.Range{From: "2.0.0", To: "2.0.0"})},
	} {
		if _, err := Compile(rc.AllExceptBlocked, 1, []Rule{c.rule}, globCompiler, c.canon); err == nil {
			t.Errorf("%s: compiled, want an error", name)
		}
	}
}

// TestR_CUR_026_TheRefusalSaysOutsideTheRange: the client's text and the event's reason of a version outside the range (R-CUR-004).
func TestR_CUR_026_TheRefusalSaysOutsideTheRange(t *testing.T) {
	rf := &Refusal{Repo: qname.Repo{Project: "web", Key: "npm-remote"}, Name: "lodash", Version: "2.0.0", Reason: rc.ReasonOutsideRange}
	want := "lodash 2.0.0 is not allowed in web/npm-remote: outside the versions a rule of the repository allows. Ask for it at"
	if got := rf.Text("https://q.example"); !strings.HasPrefix(got, want) {
		t.Errorf("text %q", got)
	}
	if got := EventReason(rc.ReasonOutsideRange); got != "outside_range" {
		t.Errorf("event reason %q", got)
	}
}
