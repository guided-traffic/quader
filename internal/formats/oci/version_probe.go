package oci

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

var _ formats.VersionProber = remoteAdapter{}

// VersionProbe implements [formats.VersionProber] (remote-curation.md R-CUR-011, Q-CUR-80): the version is a tag. Its document is
// filled through u, as a mirror's plan fills it, and the probe is the one a client's GET of the tag gives (probeTag): the manifest
// under its digest, which ProbeContent expands to the platforms of oci.platforms, and the tag's entry with the digest of what it held.
// A tag the upstream does not have is not there; on Docker Hub a one-component name is the name below library/ (R-OCI-025).
func (a remoteAdapter) VersionProbe(ctx context.Context, u discover.Upstream, name, tag string) (formats.Probe, bool, error) {
	if !ValidName(name) || tag == "" {
		return formats.Probe{}, false, nil
	}
	upName := libraryName(u.Base().String(), name)
	doc, found, err := u.Document(ctx, TagPath(upName, tag), "")
	if err != nil || !found {
		return formats.Probe{}, false, err
	}
	sum := sha256.Sum256(doc)
	return formats.Probe{
		Name: upName, Version: tag, Files: []formats.Path{{Path: ManifestPath(upName, "sha256:"+hex.EncodeToString(sum[:]))}},
		Document: formats.Path{Path: TagPath(upName, tag)}, Digest: sum,
	}, true, nil
}
