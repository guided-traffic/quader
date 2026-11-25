package oci

import (
	"context"
	"encoding/json"
	"io"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/oci/manifest"
)

// Reading an image's configuration (R-OCI-040, Q-OCI-271). The config blob is read where the registry holds it already, never fetched
// for it: at a push, the client uploaded it before the manifest, so the push reads it from the repository before the commit. Every other
// way a configuration comes to be stored (a remote's fill, whose client fetches the manifest before the config; a mirror's download and a
// download ahead, which no client fetches; a mount) passes the commit of the blob, which queues the data port's job oci-describe for the
// manifests that wait for it (OnCommit); a manifest stored while its config is stored already queues it in the manifest's commit. A
// config that cannot be opened leaves the row unread for the job; one that cannot be read is recorded as unreadable, with the reason, and
// is not read again. Nothing of a configuration is logged.

// readStoredConfig reads the configuration of an image manifest that is about to be committed from the repository of the request. nil
// for a manifest that is no image, a config of another kind, and a config the repository cannot open now.
func readStoredConfig(ctx context.Context, rq *formats.Request, name string, m *manifest.Manifest) []byte {
	if m.Type != manifest.TypeImage || m.Config == nil {
		return nil
	}
	kind := manifest.ConfigKind(m.Config.MediaType)
	if kind == "" {
		return nil
	}
	return readConfigItem(ctx, rq, rq.RepoRef(), BlobPath(name, m.Config.Digest), kind)
}

// readConfigItem reads the config blob at the path of the repository: nil when it is not stored or cannot be opened, the facts
// otherwise (one that is too large is recorded as such without a byte of it read).
func readConfigItem(ctx context.Context, rq *formats.Request, repo formats.RepoRef, path, kind string) []byte {
	it, found, err := rq.Host.Items().Stat(ctx, repo, formats.Path{Path: path})
	if err != nil || !found || it.Folder {
		return nil
	}
	facts, err := manifest.ReadStoredConfig(kind, it.Size, func() (io.ReadCloser, error) { return rq.Host.Items().Open(ctx, it) })
	if err != nil {
		return nil
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		return nil
	}
	return raw
}
