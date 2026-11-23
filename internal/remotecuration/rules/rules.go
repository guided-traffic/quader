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

	rc "github.com/guided-traffic/quader/internal/remotecuration"
)

// Rule is one delivery rule as the store holds it (R-CUR-006): a subject and an effect. The subject's members are in their canonical
// forms: Pattern folded by the format's name rule, Name normalized, Version in the format's version canon.
type Rule struct {
	Kind    rc.RuleKind
	Pattern string
	Name    string
	Version string
	Effect  rc.Effect
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
// was stored in the canonical form and the mode decides it.
type Canon struct {
	Name    func(string) (string, bool)
	Version func(string) (string, bool)
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

// pkgRules is what a rule set holds of one package name: its package rule and its version rules.
type pkgRules struct {
	effect   rc.Effect // "" without a package rule
	versions map[string]rc.Effect
	// allows is true when a version rule of the package allows: the package's decision then delivers (R-CUR-001).
	allows bool
}

// Set is the compiled rule set of one remote repository and its mode, immutable once built (R-CUR-006): a map of names to the package
// rule and the version rules, and the section patterns compiled. About 150 bytes a rule.
type Set struct {
	mode     rc.Mode
	revision int64
	canon    Canon
	pkgs     map[string]*pkgRules
	sections []section
	rules    int
}

// Compile builds the set of a repository in the mode from its rules. It returns nil, a set that delivers everything and costs nothing,
// for the mode allExceptBlocked without a rule (R-CUR-003). A section whose pattern does not compile cannot have been written by the
// store; it is an error, and the repository model then loads the repository disabled (fail closed).
func Compile(mode rc.Mode, revision int64, list []Rule, compile Compiler, canon Canon) (*Set, error) {
	if !mode.Valid() {
		mode = rc.AllExceptBlocked
	}
	if mode == rc.AllExceptBlocked && len(list) == 0 {
		return nil, nil
	}
	s := &Set{mode: mode, revision: revision, canon: canon, pkgs: map[string]*pkgRules{}, rules: len(list)}
	for _, r := range list {
		switch r.Kind {
		case rc.KindSection:
			m, err := compile(r.Pattern)
			if err != nil {
				return nil, err
			}
			s.sections = append(s.sections, section{pattern: r.Pattern, effect: r.Effect, match: m})
		case rc.KindPackage:
			s.pkg(r.Name).effect = r.Effect
		case rc.KindVersion:
			p := s.pkg(r.Name)
			if p.versions == nil {
				p.versions = map[string]rc.Effect{}
			}
			p.versions[r.Version] = r.Effect
			if r.Effect == rc.Allow {
				p.allows = true
			}
		}
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

// VersionRules reports whether the set holds a version rule of the package: a manifest by digest of an image with version rules is
// delivered only through a delivered tag that names it (R-OCI-038, Q-CUR-10).
func (s *Set) VersionRules(name string) bool {
	if s == nil {
		return false
	}
	p := s.pkgs[s.canon.name(name)]
	return p != nil && len(p.versions) > 0
}

// Decide is the decision of R-CUR-001 for one version of a package, steps 1 to 3: the narrowest kind that matches decides, a block
// wins at equal width, and without a matching rule the mode decides. name and version are folded by the format's rules first.
func (s *Set) Decide(name, version string) rc.Decision {
	if s == nil {
		return rc.Delivered()
	}
	name, version = s.canon.name(name), s.canon.version(version)
	if p := s.pkgs[name]; p != nil {
		if e, ok := p.versions[version]; ok && version != "" {
			return decided(rc.KindVersion, SubjectOf(rc.KindVersion, "", name, version), e)
		}
		if p.effect != "" {
			return decided(rc.KindPackage, name, p.effect)
		}
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
	p := s.pkgs[name]
	var level rc.Decision
	switch d, ok := s.sectionDecision(name); {
	case p != nil && p.effect != "":
		level = decided(rc.KindPackage, name, p.effect)
	case ok:
		level = d
	default:
		level = s.modeDecision()
	}
	if level.Delivered || p == nil || !p.allows {
		return level
	}
	// Any allowing version rule makes the package delivered; the subject names the least of them, so the answer does not depend on the
	// map's order.
	best := ""
	for v, e := range p.versions {
		if e == rc.Allow && (best == "" || v < best) {
			best = v
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
