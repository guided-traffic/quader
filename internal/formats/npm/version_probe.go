package npm

import (
	"context"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/npm/semver"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

var _ formats.VersionProber = remoteAdapter{}

// VersionProbe implements [formats.VersionProber] (remote-curation.md R-CUR-011, Q-CUR-80): the full package document is filled
// through u, as a mirror's plan fills it, and the version's tarball is the probe's file. A version the document does not list is not
// there.
func (a remoteAdapter) VersionProbe(ctx context.Context, u discover.Upstream, name, version string) (formats.Probe, bool, error) {
	v, err := semver.Parse(version)
	if err != nil {
		return formats.Probe{}, false, nil //nolint:nilerr // a text that is no version names no version of the upstream
	}
	plan, err := a.MirrorPlan(ctx, u, name)
	if err != nil {
		return formats.Probe{}, false, err
	}
	p, ok := formats.ProbeFromPlan(plan, v.Canonical())
	return p, ok, nil
}
