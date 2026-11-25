package ansible

import (
	"context"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/ansible/collection"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

var _ formats.VersionProber = remoteAdapter{}

// VersionProbe implements [formats.VersionProber] (remote-curation.md R-CUR-011, Q-CUR-80): the version document of the collection is
// filled through u (it carries the archive's digest the gate checks), and the archive is the probe's file. A version whose document
// the upstream does not have is not there.
func (remoteAdapter) VersionProbe(ctx context.Context, u discover.Upstream, name, version string) (formats.Probe, bool, error) {
	if id := roleIDOf(name); id != "" && version != "" {
		return roleVersionProbe(ctx, u, id, version)
	}
	ns, n, ok := collection.SplitName(name)
	if !ok || version == "" {
		return formats.Probe{}, false, nil
	}
	if _, found, err := u.Document(ctx, VersionDocumentPath(ns, n, version), documentVariant); err != nil || !found {
		return formats.Probe{}, false, err
	}
	return formats.Probe{Name: ns + "." + n, Version: version, Files: []formats.Path{{Path: ArtifactPath(ns, n, version)}}}, true, nil
}
