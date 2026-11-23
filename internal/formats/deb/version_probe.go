package deb

import (
	"context"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

var _ formats.VersionProber = remoteAdapter{}

// VersionProbe implements [formats.VersionProber] (remote-curation.md R-CUR-011, Q-CUR-80): the files of the version are the rows of
// every Packages index the remote has parsed below a suite whose release a key verifies, whatever its mirror scope says, as a mirror's
// plan reads the rows of its scope; a version no parsed index lists is not there. The indexes are those the remote has read: a suite no
// client and no pass asked for names nothing yet.
func (a remoteAdapter) VersionProbe(ctx context.Context, u discover.Upstream, name, version string) (formats.Probe, bool, error) {
	st, sc, err := a.mirrorScope(u)
	if err != nil {
		return formats.Probe{}, false, err
	}
	sc.AnyIndex = true
	entries, err := st.MirrorEntries(ctx, sc, name)
	if err != nil {
		return formats.Probe{}, false, err
	}
	plan := discover.Plan{Name: name}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.Version == version && !seen[e.Path] {
			seen[e.Path] = true
			plan.Items = append(plan.Items, discover.Item{Version: e.Version, Path: e.Path, Size: e.Size})
		}
	}
	p, ok := formats.ProbeFromPlan(plan, version)
	return p, ok, nil
}
