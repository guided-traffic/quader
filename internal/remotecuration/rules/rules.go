// Package rules is the delivery rules of a remote repository (remote-curation.md R-CUR-001 to R-CUR-006): the decision of R-CUR-001 on
// a compiled rule set, the rules' store and API service, the narrowing a write journals, and the record of a client's refusal.
//
// The compiled [Set] is what a replica's repository snapshot holds (R-CUR-006 "Propagation"): the repository model compiles it with
// the format's name rule, version canon and the one matcher of name patterns (Q-STOR-9) and hands it to every place that decides,
// so the decision reads no database (R-ARCH-027, Q-CUR-110). A nil *Set is a repository that delivers everything: a remote in mode
// allExceptBlocked without a rule, and every local and virtual repository, pay nothing (R-CUR-003).
package rules

import (
	"slices"
	"strconv"
	"strings"

	rc "github.com/guided-traffic/quader/internal/remotecuration"
)

// Rule is one delivery rule as the store holds it (R-CUR-006): a subject and an effect. The subject's members are in their canonical
// forms: Pattern folded by the format's name rule, Name normalized, Version in the format's version canon. Range is the range of
// versions of a package's allow (R-CUR-026), its bounds in the version canon; it bounds nothing on any other rule.
type Rule struct {
	Kind    rc.RuleKind
	Pattern string
	Name    string
	Version string
	Effect  rc.Effect
	Range   rc.Range
}

// Subject is the rule's subject as the API and the events name it (R-CUR-006): the pattern of a section, the name of a package, and
// "<name>@<version>" of a version.
func (r Rule) Subject() string { return SubjectOf(r.Kind, r.Pattern, r.Name, r.Version) }

// SubjectOf is the subject of a rule of the kind with the members.
func SubjectOf(kind rc.RuleKind, pattern, name, version string) string {
	switch kind {
	case rc.KindSection:
		return pattern
	case rc.KindVersion:
		return name + "@" + version
	}
	return name
}

// Canon is what a rule set folds what it is asked with: the format's name rule and its version canon (R-CUR-006 "Canonical forms").
// A nil function leaves its input unchanged; a name or version the function refuses is compared as given, so it meets no rule that
// was stored in the canonical form and the mode decides it. Order is the format's order of versions, which a range of a package's allow
// is compared by (R-CUR-026, Q-CUR-221); nil for a format without one, whose set holds no range.
type Canon struct {
	Name    func(string) (string, bool)
	Version func(string) (string, bool)
	Order   rc.VersionOrder
}

func (c Canon) name(s string) string {
	if c.Name != nil {
		if n, ok := c.Name(s); ok {
			return n
		}
	}
	return s
}

func (c Canon) version(s string) string {
	if c.Version != nil && s != "" {
		if v, ok := c.Version(s); ok {
			return v
		}
	}
	return s
}

// Matcher matches a normalized name against one compiled section pattern; the repository model compiles it with the one matcher of
// name patterns (Q-STOR-9), which this package does not import.
type Matcher func(name string) bool

// Compiler compiles a folded section pattern; its error names what is wrong with the pattern.
type Compiler func(pattern string) (Matcher, error)

// section is a compiled section rule.
type section struct {
	pattern string
	effect  rc.Effect
	match   Matcher
}

// versionRule is one version rule of a package as the set holds it.
type versionRule struct {
	version string
	effect  rc.Effect
}

// pkgRules is what a rule set holds of one package name: its package rule and its version rules, sorted by version. A sorted slice and
// not a map (Q-CUR-207): the first version rule of a package would allocate a map's header and its first group of eight slots, about
// 300 bytes, where the slice holds the one rule in 32.
type pkgRules struct {
	effect rc.Effect // "" without a package rule
	// rng is the range of versions of the package's allow, nil without one (R-CUR-026): a pointer, so a package without a range pays
	// eight bytes for it.
	rng      *rc.Range
	versions []versionRule
	// allows is true when a version rule of the package allows: the package's decision then delivers (R-CUR-001).
	allows bool
	// versionAllows and versionBlocks count the version rules by effect (VersionRuleCounts).
	versionAllows, versionBlocks int32
}

// version is the effect of the package's rule of the version; ok is false without one.
func (p *pkgRules) version(v string) (rc.Effect, bool) {
	i, ok := slices.BinarySearchFunc(p.versions, v, func(r versionRule, v string) int { return strings.Compare(r.version, v) })
	if !ok {
		return "", false
	}
	return p.versions[i].effect, true
}

// Set is the compiled rule set of one remote repository and its mode, immutable once built (R-CUR-006): a map of names to the package
// rule and the version rules, and the section patterns compiled. Its memory is measured in R-CUR-006.
type Set struct {
	mode     rc.Mode
	revision int64
	canon    Canon
	pkgs     map[string]*pkgRules
	sections []section
	rules    int
	// allows and blocks count the package and version rules by effect; the sections are len(sections).
	allows, blocks int
}

// Compile builds the set of a repository in the mode from its rules. It returns nil, a set that delivers everything and costs nothing,
// for the mode allExceptBlocked without a rule (R-CUR-003). A section whose pattern does not compile, and a range the store cannot have
// written (on another rule than a package's allow, in a format without an order, or with a bound the format does not rank), are an
// error, and the repository model then loads the repository disabled (fail closed).
func Compile(mode rc.Mode, revision int64, list []Rule, compile Compiler, canon Canon) (*Set, error) {
	if !mode.Valid() {
		mode = rc.AllExceptBlocked
	}
	if mode == rc.AllExceptBlocked && len(list) == 0 {
		return nil, nil
	}
	s := &Set{mode: mode, revision: revision, canon: canon, pkgs: map[string]*pkgRules{}, rules: len(list)}
	for _, r := range list {
		if r.Kind != rc.KindPackage && r.Range.Set() {
			return nil, errRangeOnRule
		}
		switch r.Kind {
		case rc.KindSection:
			m, err := compile(r.Pattern)
			if err != nil {
				return nil, err
			}
			s.sections = append(s.sections, section{pattern: r.Pattern, effect: r.Effect, match: m})
			continue
		case rc.KindPackage:
			p := s.pkg(r.Name)
			p.effect = r.Effect
			if r.Range.Set() {
				rng, err := canon.compileRange(r)
				if err != nil {
					return nil, err
				}
				p.rng = rng
			}
		case rc.KindVersion:
			p := s.pkg(r.Name)
			p.versions = append(p.versions, versionRule{version: r.Version, effect: r.Effect})
			if r.Effect == rc.Allow {
				p.allows = true
				p.versionAllows++
			} else {
				p.versionBlocks++
			}
		default:
			continue
		}
		if r.Effect == rc.Allow {
			s.allows++
		} else {
			s.blocks++
		}
	}
	for _, p := range s.pkgs {
		if len(p.versions) > 1 {
			slices.SortFunc(p.versions, func(a, b versionRule) int { return strings.Compare(a.version, b.version) })
		}
		// The appends' spare capacity goes: the set lives until the repository's next change.
		p.versions = slices.Clip(p.versions)
	}
	return s, nil
}

func (s *Set) pkg(name string) *pkgRules {
	p := s.pkgs[name]
	if p == nil {
		p = &pkgRules{}
		s.pkgs[name] = p
	}
	return p
}

// Mode is the repository's delivery mode; allExceptBlocked for a nil set.
func (s *Set) Mode() rc.Mode {
	if s == nil {
		return rc.AllExceptBlocked
	}
	return s.mode
}

// Revision is the revision of the rule set (repositories.delivery_rules_revision), which the key and the validator of a filtered
// document carry (R-CUR-003); 0 for a nil set.
func (s *Set) Revision() int64 {
	if s == nil {
		return 0
	}
	return s.revision
}

// Len is the number of rules of the set.
func (s *Set) Len() int {
	if s == nil {
		return 0
	}
	return s.rules
}

// Restricts reports whether the set can refuse anything: false for a nil set.
func (s *Set) Restricts() bool { return s != nil }

// Fingerprint names the set in a rendition's key and validator (R-CUR-003): "" for a nil set, so that a document of a repository that
// delivers everything is keyed as before, and otherwise the mode and the revision.
func (s *Set) Fingerprint() string {
	if s == nil {
		return ""
	}
	return "d" + string(s.mode[0]) + strconv.FormatInt(s.revision, 10)
}

// Fold returns the name and the version in the forms the set compares: the format's normalized name and its version canon.
func (s *Set) Fold(name, version string) (string, string) {
	if s == nil {
		return name, version
	}
	return s.canon.name(name), s.canon.version(version)
}

// VersionRuleCounts is the number of the package's version rules that allow and that block: the package's switch asks whether they are
// kept when it is turned (R-CUR-024). Zero for a nil set.
func (s *Set) VersionRuleCounts(name string) (allow, block int) {
	if s == nil {
		return 0, 0
	}
	if p := s.pkgs[s.canon.name(name)]; p != nil {
		return int(p.versionAllows), int(p.versionBlocks)
	}
	return 0, 0
}

// Counts is the number of the set's package and version rules that allow and that block, and of its sections (R-CUR-025's summary).
func (s *Set) Counts() (allow, block, sections int) {
	if s == nil {
		return 0, 0, 0
	}
	return s.allows, s.blocks, len(s.sections)
}

// Decide is the decision of R-CUR-001 for one version of a package, steps 1 to 3: the narrowest kind that matches decides, a block
// wins at equal width, and without a matching rule the mode decides. A package's allow with a range delivers only the versions inside
// it (R-CUR-026). name and version are folded by the format's rules first.
func (s *Set) Decide(name, version string) rc.Decision {
	if s == nil {
		return rc.Delivered()
	}
	name, version = s.canon.name(name), s.canon.version(version)
	if p := s.pkgs[name]; p != nil && version != "" {
		if e, ok := p.version(version); ok {
			return decided(rc.KindVersion, SubjectOf(rc.KindVersion, "", name, version), e)
		}
	}
	return s.inherited(name, version)
}

// Inherited is what a version of the package inherits (R-CUR-024): the decision of R-CUR-001 without the version's own rule: the package
// level's, and outside the range of the package's allow not delivered (R-CUR-026). A version's switch writes a rule only where it
// departs from it, so a version outside the range, or a tag that is no version, is switched on by a version rule of its own (Q-CUR-223).
func (s *Set) Inherited(name, version string) rc.Decision {
	if s == nil {
		return rc.Delivered()
	}
	return s.inherited(s.canon.name(name), s.canon.version(version))
}

// inherited is Inherited of a folded name and version.
func (s *Set) inherited(name, version string) rc.Decision {
	if p := s.pkgs[name]; !s.inRange(p, version) {
		return outside(name, p.rng)
	}
	return s.level(name)
}

// Level is the decision of the package level (R-CUR-024): the package's rule, else the sections, else the mode. A version's own rule
// does not move it, unlike [Set.Package]; the package's switch shows it.
func (s *Set) Level(name string) rc.Decision {
	if s == nil {
		return rc.Delivered()
	}
	return s.level(s.canon.name(name))
}

// LevelInherited is what the package level inherits (R-CUR-024): the sections, else the mode, without the package's own rule.
func (s *Set) LevelInherited(name string) rc.Decision {
	if s == nil {
		return rc.Delivered()
	}
	if d, ok := s.sectionDecision(s.canon.name(name)); ok {
		return d
	}
	return s.modeDecision()
}

// ModeDecides reports whether the mode decides the package level (R-CUR-025): no package rule and no section matches the name, so a
// change of the mode changes what such a package delivers. True for a nil set.
func (s *Set) ModeDecides(name string) bool {
	if s == nil {
		return true
	}
	name = s.canon.name(name)
	if p := s.pkgs[name]; p != nil && p.effect != "" {
		return false
	}
	_, ok := s.sectionDecision(name)
	return !ok
}

// level is the package level of a folded name: a package's allow names its range (R-CUR-026).
func (s *Set) level(name string) rc.Decision {
	if p := s.pkgs[name]; p != nil && p.effect != "" {
		d := decided(rc.KindPackage, name, p.effect)
		if p.effect == rc.Allow {
			d.By.Range = p.rng
		}
		return d
	}
	if d, ok := s.sectionDecision(name); ok {
		return d
	}
	return s.modeDecision()
}

// Package is the package's decision of R-CUR-001, for a request that names no version (a package document, a version or tag list):
// delivered when the package level (a package rule, else the sections, else the mode) allows, or when a version rule of the package
// allows. A package that is not delivered is refused whole; a delivered one has its documents filtered to the delivered versions.
func (s *Set) Package(name string) rc.Decision {
	if s == nil {
		return rc.Delivered()
	}
	name = s.canon.name(name)
	level := s.level(name)
	p := s.pkgs[name]
	if level.Delivered || p == nil || !p.allows {
		return level
	}
	// Any allowing version rule makes the package delivered; the subject names the least of them (the versions are sorted), so the
	// answer does not depend on the order the rules were read in.
	best := ""
	for _, r := range p.versions {
		if r.effect == rc.Allow {
			best = r.version
			break
		}
	}
	return decided(rc.KindVersion, SubjectOf(rc.KindVersion, "", name, best), rc.Allow)
}

// sectionDecision is step 1 and 2 for the sections: among the sections whose pattern matches the name, a block wins; ok is false when
// none matches.
func (s *Set) sectionDecision(name string) (rc.Decision, bool) {
	var allow *section
	for i := range s.sections {
		sec := &s.sections[i]
		if !sec.match(name) {
			continue
		}
		if sec.effect == rc.Block {
			return decided(rc.KindSection, sec.pattern, rc.Block), true
		}
		if allow == nil {
			allow = sec
		}
	}
	if allow != nil {
		return decided(rc.KindSection, allow.pattern, rc.Allow), true
	}
	return rc.Decision{}, false
}

// modeDecision is step 3.
func (s *Set) modeDecision() rc.Decision {
	if s.mode == rc.OnlyAllowed {
		return rc.Decision{Reason: rc.ReasonNotAllowed, By: rc.DecidedBy{Mode: rc.OnlyAllowed}}
	}
	return rc.Delivered()
}

func decided(kind rc.RuleKind, subject string, e rc.Effect) rc.Decision {
	d := rc.Decision{Delivered: e == rc.Allow, By: rc.DecidedBy{Kind: kind, Subject: subject, Effect: e}}
	if !d.Delivered {
		d.Reason = rc.ReasonBlocked
	}
	return d
}

// Names lists the names of the package and version rules of the set that allow, sorted: what the mirror's names method takes in mode
// onlyAllowed, so an allow list is mirrored by naming it (R-CUR-017).
func (s *Set) Names() []string {
	if s == nil {
		return nil
	}
	var out []string
	for n, p := range s.pkgs {
		if p.effect == rc.Allow || p.allows {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}

// Blocks reports whether a package rule or a section rule with the effect block matches the term, folded by the format's name rule:
// the half of the search guard that also protects the stem of a blocked prefix (R-NUGET-016, `contoso.` of `contoso.*`).
func (s *Set) Blocks(term string) bool {
	if s == nil || term == "" {
		return false
	}
	t := s.canon.name(term)
	if p := s.pkgs[t]; p != nil && p.effect == rc.Block {
		return true
	}
	for _, sec := range s.sections {
		if sec.effect == rc.Block && sec.match(t) {
			return true
		}
	}
	return false
}

// KeepsAway reports whether a free-text search term must not be sent to the upstream (R-CUR-002 "The search guard", R-NUGET-016): a
// section or package rule with the effect block matches it, or, in mode onlyAllowed, no allowing section pattern or package name
// covers it. term is folded by the format's name rule.
func (s *Set) KeepsAway(term string) bool {
	if s == nil || term == "" {
		return false
	}
	t := s.canon.name(term)
	if s.Blocks(t) {
		return true
	}
	covered := false
	for _, sec := range s.sections {
		if sec.match(t) {
			covered = true
		}
	}
	if s.mode != rc.OnlyAllowed {
		return false
	}
	if p := s.pkgs[t]; p != nil && (p.effect == rc.Allow || p.allows) {
		return false
	}
	return !covered
}
