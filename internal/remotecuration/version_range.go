package remotecuration

import (
	"github.com/guided-traffic/quader/internal/formats/versionkey"
)

// A range of a package's versions (remote-curation.md R-CUR-010, R-CUR-026, Q-CUR-190, Q-CUR-221): from a version, up to a version or
// below it, or both, in the format's own order: the one every list of the format is sorted by ([formats.ViewAdapter.Compare] and its
// sort key). One mechanism serves a bulk's range, a sync's range and the range of a package's allow, for npm, NuGet, OCI, Debian and
// Ansible Galaxy and every later format with a view adapter; no format has range code of its own and no range syntax is parsed.

// Range is a bound by version. From is the lowest version inside, always included; To the highest, included when ToInclusive, else
// the versions below it. "" is no bound on that side. The bounds are in the format's canonical form (R-CUR-006), so that NuGet's `1.0`
// and `1.0.0` meet.
type Range struct {
	From        string
	To          string
	ToInclusive bool
}

// Set reports whether the range bounds anything.
func (r Range) Set() bool { return r.From != "" || r.To != "" }

// Normal returns the one spelling of the range: ToInclusive true where it has a lower bound only, the zero range where it bounds
// nothing; two ranges that hold the same versions by their bounds compare equal.
func (r Range) Normal() Range {
	switch {
	case !r.Set():
		return Range{}
	case r.To == "":
		r.ToInclusive = true
	}
	return r
}

// VersionOrder is the part of a format's view adapter a range reads: [formats.ViewAdapter] satisfies it, and so does the catalog's
// [catalog.VersionOrder].
type VersionOrder interface {
	// Compare orders two versions by the format's precedence: negative when a is older than b.
	Compare(a, b string) int
	// SortKey is the stored sort key of a version; its first byte is the class of [versionkey].
	SortKey(version string) []byte
}

// VersionCanon is a format's canonical form of a version (R-CUR-006): [catalog.VersionCanon] satisfies it.
type VersionCanon interface {
	// Canonical returns the canonical form of version; ok is false for a string that is no version of the format.
	Canonical(version string) (canonical string, ok bool)
}

// Order is a format's order of versions as a range reads it: the view adapter's order and the format's canon, which folds two
// spellings of one version into one before they are compared. The edge builds it from the registered format (the format host's
// rangeOrder, the API's versionOrder, the catalog's VersionOrder for the rules); the domain only compares.
type Order struct {
	View VersionOrder
	// Canon is nil for a format without a canon, and for versions that are canonical already: they are compared as they are spelled.
	Canon VersionCanon
}

// Rank returns the canonical form of v and whether the format ranks it: a version by the format's canon whose sort key has the class
// [versionkey.Version]. An OCI tag such as `latest` or `main` is a tag the canon accepts but no version of the tag order: it is not
// ranked (Q-CUR-192, Q-CUR-223). The class is read before any comparison: a key of the class NotVersion sorts below every version and
// would otherwise lie inside a range without a lower bound.
func (o Order) Rank(v string) (string, bool) {
	c := v
	if o.Canon != nil {
		var ok bool
		if c, ok = o.Canon.Canonical(v); !ok {
			return "", false
		}
	}
	k := o.View.SortKey(c)
	return c, len(k) > 0 && k[0] == versionkey.Version
}

// Contains reports whether v lies in the range by the order. Every string lies in a range that bounds nothing; a string the format
// does not rank lies in no range that bounds something (Q-CUR-192, Q-CUR-223). A version that differs from a bound by build metadata
// only lies after it, as Compare orders them (Q-CUR-229).
func (r Range) Contains(o Order, v string) bool {
	if !r.Set() {
		return true
	}
	c, ok := o.Rank(v)
	if !ok {
		return false
	}
	if r.From != "" && o.View.Compare(c, r.From) < 0 {
		return false
	}
	if r.To != "" {
		d := o.View.Compare(c, r.To)
		if d > 0 || (d == 0 && !r.ToInclusive) {
			return false
		}
	}
	return true
}

// Empty reports whether a range with both bounds holds no version: From above To, or To excluded and equal to From. The bounds are
// ranked and canonical.
func (r Range) Empty(o Order) bool {
	if r.From == "" || r.To == "" {
		return false
	}
	d := o.View.Compare(r.From, r.To)
	return d > 0 || (d == 0 && !r.ToInclusive)
}

// Intersect is the range of the versions both ranges hold, by the order: the higher of the lower bounds, the lower of the upper bounds,
// an upper bound included only where both include it at the same version. The replay of a narrowed range keeps the narrower of the
// restored and the journaled one with it (Q-CUR-225). The bounds are ranked and canonical.
func (r Range) Intersect(o Order, x Range) Range {
	out := r.Normal()
	x = x.Normal()
	if x.From != "" && (out.From == "" || o.View.Compare(x.From, out.From) > 0) {
		out.From = x.From
	}
	switch {
	case x.To == "":
	case out.To == "":
		out.To, out.ToInclusive = x.To, x.ToInclusive
	default:
		d := o.View.Compare(x.To, out.To)
		if d < 0 {
			out.To, out.ToInclusive = x.To, x.ToInclusive
		} else if d == 0 {
			out.ToInclusive = out.ToInclusive && x.ToInclusive
		}
	}
	return out.Normal()
}
