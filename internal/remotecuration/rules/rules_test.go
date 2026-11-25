package rules

import (
	"path"
	"strings"
	"testing"

	rc "github.com/guided-traffic/quader/internal/remotecuration"
)

// globCompiler is the test's matcher of a section pattern: path.Match over the name, which agrees with the one matcher of names for
// the patterns of the table (a literal and a trailing *). The repository model compiles with the real one (Q-STOR-9).
func globCompiler(p string) (Matcher, error) {
	if _, err := path.Match(p, ""); err != nil {
		return nil, err
	}
	return func(n string) bool { ok, _ := path.Match(p, n); return ok }, nil
}

// nugetCanon folds as NuGet does: the ID lower case, the version's URL form (1.0 is 1.0.0, Beta is beta).
var nugetCanon = Canon{
	Name: func(s string) (string, bool) { return strings.ToLower(s), true },
	Version: func(s string) (string, bool) {
		s = strings.ToLower(s)
		if strings.Count(s, ".") == 1 {
			s += ".0"
		}
		return s, true
	},
}

func sec(p string, e rc.Effect) Rule { return Rule{Kind: rc.KindSection, Pattern: p, Effect: e} }
func pkg(n string, e rc.Effect) Rule { return Rule{Kind: rc.KindPackage, Name: n, Effect: e} }
func ver(n, v string, e rc.Effect) Rule {
	return Rule{Kind: rc.KindVersion, Name: n, Version: v, Effect: e}
}

func compile(t *testing.T, mode rc.Mode, canon Canon, rules ...Rule) *Set {
	t.Helper()
	s, err := Compile(mode, 7, rules, globCompiler, canon)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestR_CUR_001_TheDecisionTable holds the decision of R-CUR-001 to its table and the cases its acceptance criteria name.
func TestR_CUR_001_TheDecisionTable(t *testing.T) {
	const (
		ok      = "delivered"
		blocked = "blocked"
		notAll  = "notAllowed"
	)
	type ask struct{ name, version, want, by string }
	cases := []struct {
		name  string
		mode  rc.Mode
		canon Canon
		rules []Rule
		asks  []ask
	}{
		{"the spec's row 1: a package allowed", rc.OnlyAllowed, Canon{}, []Rule{pkg("lodash", rc.Allow)}, []ask{
			{"lodash", "4.17.21", ok, "package lodash"},
			{"lodash", "4.17.20", ok, "package lodash"},
			{"@acme/ui", "2.0.0", notAll, "mode"},
			{"left-pad", "1.3.0", notAll, "mode"},
		}},
		{"the spec's row 2: one version allowed", rc.OnlyAllowed, Canon{}, []Rule{ver("lodash", "4.17.21", rc.Allow)}, []ask{
			{"lodash", "4.17.21", ok, "version lodash@4.17.21"},
			{"lodash", "4.17.20", notAll, "mode"},
			{"@acme/ui", "2.0.0", notAll, "mode"},
			{"left-pad", "1.3.0", notAll, "mode"},
		}},
		{
			"the spec's row 3: a package allowed, one version blocked", rc.OnlyAllowed,
			Canon{},
			[]Rule{pkg("lodash", rc.Allow), ver("lodash", "4.17.20", rc.Block)},
			[]ask{
				{"lodash", "4.17.21", ok, "package lodash"},
				{"lodash", "4.17.20", blocked, "version lodash@4.17.20"},
				{"@acme/ui", "2.0.0", notAll, "mode"},
			},
		},
		{
			"the spec's row 4: the block wins at equal width", rc.OnlyAllowed,
			Canon{},
			[]Rule{sec("@acme/*", rc.Allow), sec("@acme/ui*", rc.Block)},
			[]ask{
				{"@acme/ui", "2.0.0", blocked, "section @acme/ui*"},
				{"@acme/uikit", "1.0.0", blocked, "section @acme/ui*"},
				{"@acme/api", "1.0.0", ok, "section @acme/*"},
				{"lodash", "4.17.21", notAll, "mode"},
			},
		},
		{
			"the spec's row 5: the package is narrower than the section", rc.AllExceptBlocked,
			Canon{},
			[]Rule{sec("@acme/*", rc.Block), pkg("@acme/ui", rc.Allow)},
			[]ask{
				{"@acme/ui", "2.0.0", ok, "package @acme/ui"},
				{"@acme/api", "1.0.0", blocked, "section @acme/*"},
				{"lodash", "4.17.21", ok, "mode"},
			},
		},
		{
			"a section and a package of the same name", rc.AllExceptBlocked,
			Canon{},
			[]Rule{sec("lodash", rc.Block), pkg("lodash", rc.Allow)},
			[]ask{{"lodash", "1.0.0", ok, "package lodash"}},
		},
		{
			"two sections with different effects, the allow alone", rc.OnlyAllowed,
			Canon{},
			[]Rule{sec("@a/*", rc.Allow), sec("@b/*", rc.Block)},
			[]ask{
				{"@a/x", "1.0.0", ok, "section @a/*"}, {"@b/x", "1.0.0", blocked, "section @b/*"}, {"@c/x", "1.0.0", notAll, "mode"},
			},
		},
		{
			"a version rule beside a package rule of the other effect", rc.AllExceptBlocked,
			Canon{},
			[]Rule{pkg("left-pad", rc.Block), ver("left-pad", "1.3.0", rc.Allow)},
			[]ask{
				{"left-pad", "1.3.0", ok, "version left-pad@1.3.0"}, {"left-pad", "1.2.0", blocked, "package left-pad"},
			},
		},
		{"an empty rule set, mode onlyAllowed", rc.OnlyAllowed, Canon{}, nil, []ask{{"lodash", "1.0.0", notAll, "mode"}}},
		{"a NuGet name the rule folds", rc.OnlyAllowed, nugetCanon, []Rule{pkg("newtonsoft.json", rc.Allow)}, []ask{
			{"Newtonsoft.Json", "13.0.1", ok, "package newtonsoft.json"}, {"newtonsoft.json", "13.0.1", ok, "package newtonsoft.json"},
		}},
		{"a NuGet version in a non-canonical form", rc.OnlyAllowed, nugetCanon, []Rule{ver("contoso.lib", "1.0.0", rc.Allow), ver("contoso.lib", "2.0.0-beta", rc.Block)}, []ask{
			{"Contoso.Lib", "1.0", ok, "version contoso.lib@1.0.0"},
			{"contoso.lib", "2.0.0-Beta", blocked, "version contoso.lib@2.0.0-beta"},
			{"contoso.lib", "3.0.0", notAll, "mode"},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := compile(t, c.mode, c.canon, c.rules...)
			for _, a := range c.asks {
				d := s.Decide(a.name, a.version)
				if got := outcome(d); got != a.want {
					t.Errorf("%s@%s: %s, want %s (%+v)", a.name, a.version, got, a.want, d)
				}
				if got := by(d); got != a.by {
					t.Errorf("%s@%s: decided by %q, want %q", a.name, a.version, got, a.by)
				}
			}
		})
	}
}

// TestR_CUR_001_ThePackagesDecision holds the decision for a request without a version: delivered when the package level allows or
// a version rule of the package allows.
func TestR_CUR_001_ThePackagesDecision(t *testing.T) {
	cases := []struct {
		name  string
		mode  rc.Mode
		rules []Rule
		pkg   string
		want  string
		by    string
	}{
		{"only a version rule allows", rc.OnlyAllowed, []Rule{ver("lodash", "4.17.21", rc.Allow)}, "lodash", "delivered", "version lodash@4.17.21"},
		{"a blocked package with an allowed version", rc.AllExceptBlocked, []Rule{pkg("lodash", rc.Block), ver("lodash", "1.0.0", rc.Allow)}, "lodash", "delivered", "version lodash@1.0.0"},
		{"a blocked package, a blocked version", rc.AllExceptBlocked, []Rule{pkg("lodash", rc.Block), ver("lodash", "1.0.0", rc.Block)}, "lodash", "blocked", "package lodash"},
		{"a version blocked in the default mode", rc.AllExceptBlocked, []Rule{ver("lodash", "1.0.0", rc.Block)}, "lodash", "delivered", "mode"},
		{"a section blocks", rc.AllExceptBlocked, []Rule{sec("@acme/*", rc.Block)}, "@acme/a", "blocked", "section @acme/*"},
		{"nothing allows", rc.OnlyAllowed, nil, "lodash", "notAllowed", "mode"},
		{"the least allowed version names it", rc.OnlyAllowed, []Rule{ver("a", "2.0.0", rc.Allow), ver("a", "1.0.0", rc.Allow)}, "a", "delivered", "version a@1.0.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := compile(t, c.mode, Canon{}, c.rules...).Package(c.pkg)
			if outcome(d) != c.want || by(d) != c.by {
				t.Errorf("got %s by %q, want %s by %q", outcome(d), by(d), c.want, c.by)
			}
		})
	}
}

// TestR_CUR_003_ADefaultRepositoryPaysNothing holds that the mode allExceptBlocked without a rule compiles to nil, which delivers
// everything and has no fingerprint (R-CUR-003), and that a set's fingerprint names its mode and revision.
func TestR_CUR_003_ADefaultRepositoryPaysNothing(t *testing.T) {
	s, err := Compile(rc.AllExceptBlocked, 3, nil, globCompiler, Canon{})
	if err != nil || s != nil {
		t.Fatalf("Compile = %v, %v; want nil", s, err)
	}
	if !s.Decide("x", "1").Delivered || !s.Package("x").Delivered || s.Fingerprint() != "" || s.KeepsAway("x") {
		t.Fatal("a nil set refuses or is keyed")
	}
	a := compile(t, rc.OnlyAllowed, Canon{})
	if a.Fingerprint() != "do7" {
		t.Fatalf("fingerprint %q", a.Fingerprint())
	}
	if _, err := Compile(rc.OnlyAllowed, 1, []Rule{sec("[", rc.Block)}, globCompiler, Canon{}); err == nil {
		t.Fatal("a pattern that does not compile is no error")
	}
}

// TestR_CUR_002_TheSearchGuard holds the guard of a forwarded search term (R-CUR-002, R-NUGET-016).
func TestR_CUR_002_TheSearchGuard(t *testing.T) {
	d := compile(t, rc.AllExceptBlocked, nugetCanon, sec("contoso.*", rc.Block), pkg("internal.lib", rc.Block))
	for term, want := range map[string]bool{"Contoso.Secret": true, "internal.lib": true, "newtonsoft.json": false, "json": false} {
		if got := d.KeepsAway(term); got != want {
			t.Errorf("allExceptBlocked KeepsAway(%q) = %v", term, got)
		}
	}
	o := compile(t, rc.OnlyAllowed, nugetCanon, sec("contoso.*", rc.Allow), pkg("newtonsoft.json", rc.Allow), ver("serilog", "3.0.0", rc.Allow))
	for term, want := range map[string]bool{"Contoso.Lib": false, "Newtonsoft.Json": false, "serilog": false, "json": true, "secret": true} {
		if got := o.KeepsAway(term); got != want {
			t.Errorf("onlyAllowed KeepsAway(%q) = %v", term, got)
		}
	}
	if got := o.Names(); strings.Join(got, ",") != "newtonsoft.json,serilog" {
		t.Errorf("Names = %v", got)
	}
}

// TestR_CUR_004_TheRefusalsText holds the text of a refusal and its error (R-CUR-004).
func TestR_CUR_004_TheRefusalsText(t *testing.T) {
	r := &Refusal{Name: "left-pad", Version: "1.3.0", Reason: rc.ReasonBlocked}
	r.Repo.Project, r.Repo.Key = "web", "npm-remote"
	want := "left-pad 1.3.0 is not allowed in web/npm-remote: blocked by a rule of the repository. Ask for it at " +
		"https://q.example/ui/projects/web/repositories/npm-remote/upstream?name=left-pad."
	if got := r.Text("https://q.example"); got != want {
		t.Errorf("text\n%s\nwant\n%s", got, want)
	}
	r.Version, r.Reason, r.Name = "", rc.ReasonNotAllowed, "@acme/ui"
	if got := r.Text("https://q.example"); !strings.Contains(got, "@acme/ui any version is not allowed") ||
		!strings.Contains(got, "delivers only what is allowed") || !strings.Contains(got, "name=%40acme%2Fui") {
		t.Errorf("text %s", got)
	}
	err := r.Err("https://q.example")
	if got, ok := RefusalOf(err); !ok || got != r {
		t.Fatal("the refusal is not the error's cause")
	}
}

func outcome(d rc.Decision) string {
	if d.Delivered {
		return "delivered"
	}
	return string(d.Reason)
}

func by(d rc.Decision) string {
	if d.By.Kind == "" {
		return "mode"
	}
	return string(d.By.Kind) + " " + d.By.Subject
}

// TestR_CUR_024_WhatASubjectInherits holds what a switch shows and departs from (R-CUR-024): a version inherits its package level (the
// package's rule, else the sections, else the mode), the level inherits the sections, else the mode; a version's own rule moves neither,
// and the counts of the version rules are what the package's switch asks about.
func TestR_CUR_024_WhatASubjectInherits(t *testing.T) {
	type want struct{ inherited, level, levelInherited string }
	cases := []struct {
		name         string
		mode         rc.Mode
		rules        []Rule
		pkg, version string
		want         want
		modeDecides  bool
		allow, block int
	}{
		{"no rule, everything", rc.AllExceptBlocked, []Rule{ver("other", "1", rc.Block)}, "lodash", "1.0.0", want{"delivered mode", "delivered mode", "delivered mode"}, true, 0, 0},
		{"no rule, only allowed", rc.OnlyAllowed, nil, "lodash", "1.0.0", want{"notAllowed mode", "notAllowed mode", "notAllowed mode"}, true, 0, 0},
		{"a version block does not move the level", rc.AllExceptBlocked, []Rule{ver("lodash", "1.0.0", rc.Block)}, "lodash", "1.0.0", want{"delivered mode", "delivered mode", "delivered mode"}, true, 0, 1},
		{"a version allow does not move the level", rc.OnlyAllowed, []Rule{ver("lodash", "1.0.0", rc.Allow)}, "lodash", "1.0.0", want{"notAllowed mode", "notAllowed mode", "notAllowed mode"}, true, 1, 0},
		{"a package allow is what a version inherits", rc.OnlyAllowed, []Rule{pkg("lodash", rc.Allow)}, "lodash", "1.0.0", want{"delivered package lodash", "delivered package lodash", "notAllowed mode"}, false, 0, 0},
		{"a package block beside a version allow", rc.AllExceptBlocked, []Rule{pkg("lodash", rc.Block), ver("lodash", "1.0.0", rc.Allow), ver("lodash", "2.0.0", rc.Block)}, "lodash", "1.0.0", want{"blocked package lodash", "blocked package lodash", "delivered mode"}, false, 1, 1},
		{"a section allow beside a package", rc.OnlyAllowed, []Rule{sec("@acme/*", rc.Allow)}, "@acme/ui", "2.0.0", want{"delivered section @acme/*", "delivered section @acme/*", "delivered section @acme/*"}, false, 0, 0},
		{"a package rule beside a section", rc.OnlyAllowed, []Rule{sec("@acme/*", rc.Allow), pkg("@acme/ui", rc.Block)}, "@acme/ui", "2.0.0", want{"blocked package @acme/ui", "blocked package @acme/ui", "delivered section @acme/*"}, false, 0, 0},
		{"a section block wins over a section allow", rc.AllExceptBlocked, []Rule{sec("@acme/*", rc.Allow), sec("@acme/u*", rc.Block)}, "@acme/ui", "", want{"blocked section @acme/u*", "blocked section @acme/u*", "blocked section @acme/u*"}, false, 0, 0},
		{"another package's rules decide nothing here", rc.OnlyAllowed, []Rule{pkg("a", rc.Allow), ver("a", "1", rc.Allow)}, "b", "1", want{"notAllowed mode", "notAllowed mode", "notAllowed mode"}, true, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := compile(t, c.mode, Canon{}, c.rules...)
			got := want{
				outcome(s.Inherited(c.pkg, c.version)) + " " + by(s.Inherited(c.pkg, c.version)),
				outcome(s.Level(c.pkg)) + " " + by(s.Level(c.pkg)),
				outcome(s.LevelInherited(c.pkg)) + " " + by(s.LevelInherited(c.pkg)),
			}
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
			if s.ModeDecides(c.pkg) != c.modeDecides {
				t.Errorf("ModeDecides = %v", !c.modeDecides)
			}
			if a, b := s.VersionRuleCounts(c.pkg); a != c.allow || b != c.block {
				t.Errorf("VersionRuleCounts = %d, %d; want %d, %d", a, b, c.allow, c.block)
			}
		})
	}
	// A nil set delivers everything, the mode decides every package, and it counts nothing.
	var n *Set
	if !n.Inherited("x", "1").Delivered || !n.Level("x").Delivered || !n.LevelInherited("x").Delivered || !n.ModeDecides("x") {
		t.Fatal("a nil set refuses")
	}
	if a, b, s := n.Counts(); a+b+s != 0 {
		t.Fatal("a nil set counts rules")
	}
	// The counts of a set: package and version rules by effect, and the sections.
	s := compile(t, rc.OnlyAllowed, Canon{}, pkg("a", rc.Allow), ver("a", "1", rc.Block), ver("b", "2", rc.Allow), sec("c*", rc.Block))
	if a, b, sec := s.Counts(); a != 2 || b != 1 || sec != 1 {
		t.Fatalf("Counts = %d, %d, %d", a, b, sec)
	}
	// The version rules are found by a binary search over the sorted slice, whatever order they were read in.
	vs := compile(t, rc.AllExceptBlocked, Canon{}, ver("p", "3.0.0", rc.Block), ver("p", "1.0.0", rc.Allow), ver("p", "2.0.0", rc.Block))
	for v, w := range map[string]string{"1.0.0": "delivered", "2.0.0": "blocked", "3.0.0": "blocked", "1.5.0": "delivered"} {
		if got := outcome(vs.Decide("p", v)); got != w {
			t.Errorf("Decide(p, %s) = %s, want %s", v, got, w)
		}
	}
}
