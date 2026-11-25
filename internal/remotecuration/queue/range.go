package queue

import (
	"github.com/guided-traffic/quader/internal/formats/versionkey"
)

// A bulk of a package takes a range of its versions (remote-curation.md R-CUR-010, Q-CUR-190 to Q-CUR-194): from a version, up to a
// version or below it, or both. The order is the format's own, the one every list of the format is sorted by
// ([formats.ViewAdapter.Compare] and its sort key), so one mechanism serves npm, NuGet, OCI, Debian and Ansible Galaxy and every later
// format with a view adapter; no format has range code of its own and no range syntax is parsed.

// Range is a bulk's bound by version. From is the lowest version taken, always included; To the highest, included when ToInclusive,
// else the versions below it. "" is no bound on that side. The bounds are in the format's canonical form (R-CUR-006), so that NuGet's
// `1.0` and `1.0.0` meet.
type Range struct {
	From        string
	To          string
	ToInclusive bool
}

// Set reports whether the range bounds anything.
func (r Range) Set() bool { return r.From != "" || r.To != "" }

// VersionOrder is the part of a format's view adapter a range reads: [formats.ViewAdapter] satisfies it.
type VersionOrder interface {
	// Compare orders two versions by the format's precedence: negative when a is older than b.
	Compare(a, b string) int
	// SortKey is the stored sort key of a version; its first byte is the class of [versionkey].
	SortKey(version string) []byte
}

// Canon is a format's canonical form of a version (R-CUR-006): [catalog.VersionCanon] satisfies it.
type Canon interface {
	// Canonical returns the canonical form of version; ok is false for a string that is no version of the format.
	Canonical(version string) (canonical string, ok bool)
}

// Order is a format's order of versions as a range reads it: the view adapter's order and the format's canon, which folds two
// spellings of one version into one before they are compared. The edge builds it from the registered format (the format host's
// rangeOrder, the API's versionOrder); the domain only compares.
type Order struct {
	View VersionOrder
	// Canon is nil for a format without a canon: the versions are compared as they are spelled.
	Canon Canon
}

// Rank returns the canonical form of v and whether the format ranks it: a version by the format's canon whose sort key has the class
// [versionkey.Version]. An OCI tag such as `latest` or `main` is a tag the canon accepts but no version of the tag order: it is not
// ranked (Q-CUR-192).
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
// does not rank lies in no range that bounds something (Q-CUR-192).
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

// Selection is what a bulk takes of a package's versions, which its planner lists newest first by the format's order (R-CUR-010,
// Q-CUR-191): the range first, then the pre-releases, then the newest Max (0 takes all). maxBytes is the expansion's, after it.
type Selection struct {
	Range              Range
	IncludePrereleases bool
	Max                int
}

// Picker applies a [Selection] to the versions of a package in the order the planner lists them, one at a time, so that a planner
// that pages stops reading once Max versions are taken.
type Picker struct {
	Selection
	Order Order
	// Taken counts the versions taken.
	Taken int
	// NotVersions counts the strings a set range left out because the format does not rank them (an OCI tag such as `latest`):
	// the preview says how many (R-CUR-010, Q-CUR-192).
	NotVersions int
}

// Take reports whether the next version is taken. full is true once Max versions are taken: the version is not taken and no later one
// will be.
func (p *Picker) Take(version string, prerelease bool) (take, full bool) {
	if p.Max > 0 && p.Taken >= p.Max {
		return false, true
	}
	if p.Range.Set() {
		if _, ok := p.Order.Rank(version); !ok {
			p.NotVersions++
			return false, false
		}
		if !p.Range.Contains(p.Order, version) {
			return false, false
		}
	}
	if prerelease && !p.IncludePrereleases {
		return false, false
	}
	p.Taken++
	return true, false
}
