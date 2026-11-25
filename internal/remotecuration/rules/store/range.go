package store

import (
	"strconv"
	"strings"

	"github.com/guided-traffic/quader/internal/errs"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
)

// The range of versions of a package's allow as the store writes it (remote-curation.md R-CUR-026, Q-CUR-221): the bounds in the
// format's canonical form, each a version the format ranks, compared in the format's order (the catalog's VersionOrder, the view
// adapter's), never a range syntax. The refusals are the bulk's of R-CUR-010 (Q-CUR-192), so that the two ranges read alike: a bound the
// format does not rank answers 422 invalid_version at its pointer, a range that holds no version 422 validation_failed at toVersion with
// the field code out_of_range, a range on any other rule 422 not_applicable.

// canonicalRange checks the range of in and returns it in the format's canonical form; a finding is added at its pointer below at.
func (s *Store) canonicalRange(format string, in Input, at string, add func(ptr, code, detail string)) rc.Range {
	fromAt, toAt := at+"/fromVersion", at+"/toVersion"
	first := fromAt
	if in.Range.From == "" {
		first = toAt
	}
	if in.Kind != rc.KindPackage || in.Effect != rc.Allow {
		add(first, errs.CodeNotApplicable, "a range of versions is part of a package's allow; a block, a section and a version have none")
		return rc.Range{}
	}
	view, ok := s.o.Catalog.VersionOrderOf(format)
	if !ok {
		add(first, errs.CodeNotApplicable, "the format "+format+" has no order of versions to bound an allow by")
		return rc.Range{}
	}
	o := rc.Order{View: view, Canon: canonOf{s, format}}
	out := rc.Range{ToInclusive: in.Range.ToInclusive}
	bad := false
	for _, b := range []struct {
		ptr, name string
		in        string
		out       *string
	}{{fromAt, "fromVersion", in.Range.From, &out.From}, {toAt, "toVersion", in.Range.To, &out.To}} {
		v := strings.TrimSpace(b.in)
		if v == "" {
			continue
		}
		c, ranked := o.Rank(v)
		if !ranked || len(c) > rules.MaxVersionLength || hasControl(c) {
			add(b.ptr, errs.CodeInvalidVersion, b.name+" "+strconv.Quote(v)+" is no version the format "+format+
				" orders: a range is bounded by versions, not by a tag such as latest or by a range expression")
			bad = true
			continue
		}
		*b.out = c
	}
	if bad {
		return rc.Range{}
	}
	out = out.Normal()
	if out.Empty(rc.Order{View: view}) {
		why := "lies below fromVersion " + out.From
		if view.Compare(out.From, out.To) == 0 {
			why = "equals fromVersion " + out.From + " and is excluded (toInclusive false)"
		}
		add(toAt, "out_of_range", "toVersion "+out.To+" "+why+": the range holds no version")
		return rc.Range{}
	}
	return out
}

// canonOf is the catalog's canon of one format as the order reads it.
type canonOf struct {
	s      *Store
	format string
}

func (c canonOf) Canonical(v string) (string, bool) {
	return c.s.o.Catalog.CanonicalVersion(c.format, v)
}

// rangeOfColumns is the range of a row's three columns, a NULL bound read as "".
func rangeOfColumns(from, to string, inclusive bool) rc.Range {
	return rc.Range{From: from, To: to, ToInclusive: inclusive}.Normal()
}

// rangeDetails is a range as the rule's event names it (R-CUR-019): a bound null is no bound on that side.
func rangeDetails(r rc.Range) map[string]any {
	bound := func(v string) any {
		if v == "" {
			return nil
		}
		return v
	}
	r = r.Normal()
	return map[string]any{"fromVersion": bound(r.From), "toVersion": bound(r.To), "toInclusive": r.ToInclusive}
}

// rangeNarrowed is the journal's part of a range a write set on a package's allow (Q-CUR-225).
func rangeNarrowed(name string, r rc.Range) rules.RangeNarrowing {
	r = r.Normal()
	return rules.RangeNarrowing{Name: name, FromVersion: r.From, ToVersion: r.To, ToInclusive: r.ToInclusive}
}

// narrowsRange reports whether a write of the rule over a rule of the previous effect and range sets a range the repository did not
// have: a range written where the allow had another or none. The replay keeps the narrower of two ranges, so a range that widened an
// earlier one is journaled too and changes nothing a restore holds.
func narrowsRange(r rules.Rule, previous rc.Effect, previousRange rc.Range) bool {
	if r.Kind != rc.KindPackage || r.Effect != rc.Allow || !r.Range.Set() {
		return false
	}
	return previous != rc.Allow || previousRange.Normal() != r.Range.Normal()
}
