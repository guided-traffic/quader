package queue

import (
	"github.com/guided-traffic/quader/internal/remotecuration"
)

// A bulk of a package takes a range of its versions (remote-curation.md R-CUR-010, Q-CUR-190 to Q-CUR-194): from a version, up to a
// version or below it, or both, by [remotecuration.Range] in the format's own order ([remotecuration.Order]), the one comparison the
// range of a package's allow reads too (R-CUR-026, Q-CUR-221).

// Selection is what a bulk takes of a package's versions, which its planner lists newest first by the format's order (R-CUR-010,
// Q-CUR-191): the range first, then the pre-releases, then the newest Max (0 takes all). maxBytes is the expansion's, after it.
type Selection struct {
	Range              remotecuration.Range
	IncludePrereleases bool
	Max                int
}

// Picker applies a [Selection] to the versions of a package in the order the planner lists them, one at a time, so that a planner
// that pages stops reading once Max versions are taken.
type Picker struct {
	Selection
	Order remotecuration.Order
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
