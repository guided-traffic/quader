package oci

import (
	"context"
	"encoding/hex"
	"net/http"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/oci/manifest"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// The probes of the format (repositories-and-proxy.md R-REPO-065, Q-OCI-224): the manifest of a tag, by GET or HEAD, names one
// image without fetching its content, the configs and layers; a HEAD of a manifest by digest names one image the same way; a HEAD of
// a blob names one file. A pull by digest and a GET of a blob fetch the content themselves and are none. The content of an index is the
// manifests of the platforms of oci.platforms with their attestations, and of every image manifest its config and layers (Q-OCI-223).

// probes are the format's declaration of its probes (formats.Descriptor.Probes).
func probes() []formats.ProbeRoute {
	return []formats.ProbeRoute{
		{Route: routeManifestGetTag, Method: http.MethodGet, What: "the manifest of a tag"},
		{Route: routeManifestGetTag, Method: http.MethodHead, What: "a HEAD of the manifest of a tag"},
		{Route: routeManifestGetDgs, Method: http.MethodHead, What: "a HEAD of a manifest by its digest"},
		{Route: routeBlobGet, Method: http.MethodHead, What: "a HEAD of a blob"},
	}
}

// probeTag tells the host of the probe of a tag whose manifest the request was answered with: the files are the manifest under its
// digest (the fill entered it beside the tag) and what the manifest names, an image's config and layers or an index's manifests of
// the repository's platforms. A manifest the registry does not read is no probe.
func (f *Format) probeTag(ctx context.Context, rq *formats.Request, name, tag string, digest [32]byte, body []byte) {
	ref := "sha256:" + hex.EncodeToString(digest[:])
	b := &itemBuilder{name: name, version: tag, seen: map[string]bool{}}
	b.add(ManifestPath(name, ref), int64(len(body)))
	fm, err := f.readFetched(name, body)
	if err != nil {
		return
	}
	switch fm.m.Type {
	case manifest.TypeImage:
		b.image(fm.m)
	case manifest.TypeIndex:
		children, err := selectChildren(body, fm.m, mirrorSettingsOf(rq.Repo.Config).Platforms)
		if err != nil {
			return
		}
		for _, d := range children {
			b.manifest(d)
		}
	}
	rq.Host.Remote().Probed(ctx, rq, formats.Probe{
		Name: name, Version: tag, Files: probePaths(b.items), Document: formats.Path{Path: TagPath(name, tag)}, Digest: digest,
	})
}

// probeFile tells the host of a HEAD of a manifest by digest or of a blob: the probe names that one file, whose content a manifest's
// expands to in ProbeContent.
func probeFile(ctx context.Context, rq *formats.Request, name, version, path string) {
	if rq.Method != http.MethodHead {
		return
	}
	rq.Host.Remote().Probed(ctx, rq, formats.Probe{Name: name, Version: version, Files: []formats.Path{{Path: path}}})
}

func probePaths(items []discover.Item) []formats.Path {
	out := make([]formats.Path, len(items))
	for i, it := range items {
		out[i] = formats.Path{Path: it.Path}
	}
	return out
}

// ProbeContent implements [formats.RemoteAdapter] (R-REPO-065, Q-OCI-224): the files of the probe, and below every manifest among
// them what it names, read through u (u.Artifact stores the manifest it reads): an index's manifests of the platforms of oci.platforms
// with their attestations, an image's config and layers without the foreign ones. An index of more than 1,000 children is refused as a
// mirror's plan refuses it; the content is cut at formats.MaxProbeFiles files.
func (a remoteAdapter) ProbeContent(ctx context.Context, u discover.Upstream, p formats.Probe) ([]discover.Item, error) {
	platforms := mirrorMemberOf(u.Settings()).Platforms
	b := &itemBuilder{name: p.Name, version: p.Version, seen: map[string]bool{}}
	for _, f := range p.Files {
		b.add(f.Path, 0)
	}
	for i := 0; i < len(b.items) && len(b.items) < formats.MaxProbeFiles; i++ {
		path := b.items[i].Path
		name, kind, _, ok := splitLayout(path)
		if !ok || kind != dirManifests {
			continue
		}
		body, found, err := u.Artifact(ctx, path, MaxDocumentBytes)
		if err != nil {
			if invalidManifest(err) {
				continue
			}
			return nil, err
		}
		if !found || body == nil {
			continue // gone upstream, or too large to read: what it names cannot be found
		}
		fm, err := a.f.readFetched(name, body)
		if err != nil {
			continue
		}
		b.name = name
		switch fm.m.Type {
		case manifest.TypeImage:
			b.image(fm.m)
		case manifest.TypeIndex:
			children, err := selectChildren(body, fm.m, platforms)
			if err != nil {
				return nil, err
			}
			for _, d := range children {
				b.manifest(d)
			}
		}
	}
	return b.items[:min(len(b.items), formats.MaxProbeFiles)], nil
}
