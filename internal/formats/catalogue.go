package formats

import (
	"context"
	"fmt"
	"strings"

	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// Catalogue is a remote adapter's view of its upstream for a person (remote-curation.md R-CUR-007, Q-CUR-12, Q-CUR-109): the browsing
// calls of the leaf ([discover.Catalogue]: CatalogueCaps, Search, Children, Package), which the catalogue's engine and the expansion of
// a bulk call without importing this package, and the call that names what a download of one version starts from. Every format with
// the class remote declares what its adapter answers in [Descriptor.Catalogue]; [CapabilitiesOf] finds the adapter's implementation as
// [RemoteCaps.Catalogue].
type Catalogue interface {
	discover.Catalogue
	VersionProber
}

// VersionProber is the call of the catalogue that the download queue's run makes (remote-curation.md R-CUR-011, Q-CUR-80): the five
// formats with a remote class implement it on their remote adapters ahead of the browsing calls, so that a download of one version runs
// before the catalogue is built; [CapabilitiesOf] finds it as [RemoteCaps.VersionProbe].
type VersionProber interface {
	// VersionProbe names what a download of one version starts from: the probe the queue's run hands to
	// [RemoteAdapter.ProbeContent] (R-CUR-011). It may read and store the version's documents through u, which in a run is the
	// worker's, never the catalogue's. ok is false when the upstream does not have the version.
	VersionProbe(ctx context.Context, u discover.Upstream, name, version string) (p Probe, ok bool, err error)
}

// CatalogueDeclaration is what a format with the class remote declares of its upstream catalogue (R-CUR-007), as it declares its
// probes: the calls its remote adapter answers and, for each it does not, one sentence why. The lookup by exact name (Package and
// VersionProbe) is the floor every such format declares; Register refuses a format with the class remote that declares no catalogue,
// declares a call and a reason for it, or neither. The screens read the declaration; a repository's own capabilities, which differ
// between the upstreams of one format, are [discover.CatalogueCaps].
type CatalogueDeclaration struct {
	// Lookup: the adapter answers Package and VersionProbe. Every format with the class remote declares it.
	Lookup bool
	// Search: the adapter answers Search for an upstream of the format, by the upstream or over an index Quader holds. NoSearch is
	// the reason it has none.
	Search   bool
	NoSearch string
	// Levels are the kinds of the levels above a package that Children lists, outermost first ("scope"; "suite", "component",
	// "architecture"). NoLevels is the reason the format has none.
	Levels   []string
	NoLevels string
}

// checkCatalogue holds a format with the class remote to a declaration of its upstream catalogue and of its version canon (R-CUR-006,
// R-CUR-007): the lookup, search and levels each declared or given a reason, never both, and a canon. A format without the class
// declares neither.
func checkCatalogue(_ Format, d Descriptor) error {
	c := d.Catalogue
	if !d.Supports(Remote) {
		if c.Lookup || c.Search || len(c.Levels) > 0 || c.NoSearch != "" || c.NoLevels != "" {
			return fmt.Errorf("%q declares an upstream catalogue and has no class remote", d.ID)
		}
		return nil
	}
	switch {
	case !c.Lookup:
		return fmt.Errorf("%q has the class remote and declares no upstream catalogue: the lookup by exact name is the floor (R-CUR-007)", d.ID)
	case c.Search == (strings.TrimSpace(c.NoSearch) != ""):
		return fmt.Errorf("%q declares its catalogue's search and a reason it has none, or neither (R-CUR-007)", d.ID)
	case (len(c.Levels) > 0) == (strings.TrimSpace(c.NoLevels) != ""):
		return fmt.Errorf("%q declares its catalogue's levels and a reason it has none, or neither (R-CUR-007)", d.ID)
	case d.VersionCanon == nil:
		return fmt.Errorf("%q has the class remote and declares no version canon (R-CUR-006)", d.ID)
	}
	for _, l := range c.Levels {
		if strings.TrimSpace(l) == "" {
			return fmt.Errorf("%q declares a level of its catalogue without a name", d.ID)
		}
	}
	return nil
}

// ProbeFromPlan is the probe of one version from a mirror plan of its package (discover.Lister.MirrorPlan): the files of the items of
// that version, in the plan's order. ok is false when the plan has no item of the version. A format whose plan of a package reads
// one or two documents names a version's download with it (npm, NuGet, Debian; R-CUR-011).
func ProbeFromPlan(plan discover.Plan, version string) (Probe, bool) {
	p := Probe{Name: plan.Name, Version: version}
	for _, it := range plan.Items {
		if it.Version == version && len(p.Files) < MaxProbeFiles {
			p.Files = append(p.Files, Path{Path: it.Path})
		}
	}
	return p, len(p.Files) > 0
}
