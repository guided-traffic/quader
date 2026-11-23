package nuget

import (
	"context"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/nuget/version"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

var _ formats.VersionProber = remoteAdapter{}

// VersionProbe implements [formats.VersionProber] (remote-curation.md R-CUR-011, Q-CUR-80): the version list and the registration of
// the ID are filled through u, as a mirror's plan fills them, and the version's package is the probe's file. A version the list does
// not hold is not there.
func (a remoteAdapter) VersionProbe(ctx context.Context, u discover.Upstream, name, ver string) (formats.Probe, bool, error) {
	v, err := version.Parse(ver)
	if err != nil {
		return formats.Probe{}, false, nil //nolint:nilerr // a text that is no version names no version of the upstream
	}
	plan, err := a.MirrorPlan(ctx, u, name)
	if err != nil {
		return formats.Probe{}, false, err
	}
	p, ok := formats.ProbeFromPlan(plan, v.URLForm())
	return p, ok, nil
}
