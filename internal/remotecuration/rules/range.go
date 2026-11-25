package rules

import (
	"errors"

	rc "github.com/guided-traffic/quader/internal/remotecuration"
)

// The range of versions of a package's allow (remote-curation.md R-CUR-026): one optional pair of bounds on the package's allow, in the
// format's order (Q-CUR-220, Q-CUR-221). A version inside it is delivered by the rule, a version outside it is not, with the reason
// outsideRange, in either mode (Q-CUR-222); a string the format does not rank as a version (an OCI tag such as latest) lies outside it
// and is switched on alone by its own version rule (Q-CUR-223). A version rule stays the exception above it.

// The errors of a range the store cannot have written: the repository model loads the repository disabled (fail closed).
var (
	errRangeOnRule  = errors.New("rules: a range of versions is part of a package's allow only")
	errRangeNoOrder = errors.New("rules: the format has no order of versions to compare a range by")
	errRangeBound   = errors.New("rules: a bound of a range is no version the format ranks")
)

// order is the order a set compares its ranges by: the format's, over versions that are canonical already.
func (c Canon) order() rc.Order { return rc.Order{View: c.Order} }

// compileRange checks the range of a package rule as the store writes it: an allow's, in a format with an order, each bound a version
// the format ranks and the range not empty.
func (c Canon) compileRange(r Rule) (*rc.Range, error) {
	if r.Effect != rc.Allow {
		return nil, errRangeOnRule
	}
	if c.Order == nil {
		return nil, errRangeNoOrder
	}
	o := c.order()
	for _, b := range []string{r.Range.From, r.Range.To} {
		if b == "" {
			continue
		}
		if _, ok := o.Rank(b); !ok {
			return nil, errRangeBound
		}
	}
	rng := r.Range.Normal()
	if rng.Empty(o) {
		return nil, errRangeBound
	}
	return &rng, nil
}

// inRange reports whether the package's range admits the folded version: true without a range, and for the package itself (no
// version): the package's decision of a ranged allow is delivered, and its documents decide per version (Q-CUR-224).
func (s *Set) inRange(p *pkgRules, version string) bool {
	if p == nil || p.rng == nil || p.effect != rc.Allow || version == "" {
		return true
	}
	return p.rng.Contains(s.canon.order(), version)
}

// outside is the decision of a version outside the range of the package's allow.
func outside(name string, rng *rc.Range) rc.Decision {
	return rc.Decision{Reason: rc.ReasonOutsideRange, By: rc.DecidedBy{Kind: rc.KindPackage, Subject: name, Effect: rc.Allow, Range: rng}}
}

// Range is the range of versions of the package's allow; ok is false where its allow has none, or it has no allow.
func (s *Set) Range(name string) (rc.Range, bool) {
	if s == nil {
		return rc.Range{}, false
	}
	p := s.pkgs[s.canon.name(name)]
	if p == nil || p.rng == nil || p.effect != rc.Allow {
		return rc.Range{}, false
	}
	return *p.rng, true
}

// NarrowsVersions reports whether the set decides the package's versions apart from the package (R-OCI-038, Q-CUR-10, Q-CUR-230): it
// holds a version rule of the package, or the package's allow carries a range. A manifest by digest of such an image is delivered only
// through a delivered tag that names it.
func (s *Set) NarrowsVersions(name string) bool {
	if s == nil {
		return false
	}
	p := s.pkgs[s.canon.name(name)]
	return p != nil && (len(p.versions) > 0 || (p.rng != nil && p.effect == rc.Allow))
}

// EmptyReason is the reason a package's document is refused for when its own decision delivers it and none of its versions is
// delivered (R-CUR-004, Q-CUR-224): outsideRange where the package's allow carries a range, else the reason the mode gives a version no
// rule names (a version rule allows a version the upstream does not hold).
func (s *Set) EmptyReason(name string) rc.Reason {
	if _, ok := s.Range(name); ok {
		return rc.ReasonOutsideRange
	}
	if s.Mode() == rc.OnlyAllowed {
		return rc.ReasonNotAllowed
	}
	return rc.ReasonBlocked
}
