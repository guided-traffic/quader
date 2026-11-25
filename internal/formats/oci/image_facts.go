package oci

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/oci/manifest"
	"github.com/guided-traffic/quader/internal/formats/oci/rows"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
)

// Reading an image's configuration (R-OCI-040, Q-OCI-271). The config blob is read where the registry holds it already, never fetched
// for it: at a push, the client uploaded it before the manifest, so the push reads it from the repository before the commit; at a
// remote's fill, the client fetches the manifest before the config, so the manifest's row waits with its config unread, and the first
// fetch of the config through the repository reads the stored blob and completes the row (completeConfig). A config that cannot be
// opened (a store that does not answer) leaves the row unread for the next fetch; one that cannot be read is recorded as unreadable,
// with the reason, and is not read again. Nothing of a configuration is logged.

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
	var facts rows.ConfigFacts
	if it.Size > manifest.MaxConfigBytes {
		facts = rows.ConfigFacts{Kind: kind, Unreadable: rows.UnreadableTooLarge}
	} else {
		r, err := rq.Host.Items().Open(ctx, it)
		if err != nil {
			return nil
		}
		data, err := untrusted.ReadAll(r, manifest.MaxConfigBytes+1)
		_ = r.Close()
		switch {
		case untrusted.IsTooLarge(err):
			facts = rows.ConfigFacts{Kind: kind, Unreadable: rows.UnreadableTooLarge}
		case err != nil:
			return nil
		default:
			facts = manifest.ReadConfig(kind, data)
		}
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		return nil
	}
	return raw
}

// completeConfig reads, after a fetch of a blob, the configuration of the manifests of the name that name the blob as an image's
// configuration and whose configuration is not read yet: a remote's manifest is fetched before its config (R-OCI-040). The question is
// one read of a partial index that holds only such manifests, so a fetch of a layer costs that read and nothing else. A failure changes
// nothing of the answer the client got.
func completeConfig(ctx context.Context, rq *formats.Request, name, digest string) {
	if rq.Method != http.MethodGet {
		return
	}
	sum, ok := manifest.ParseDigest(digest)
	if !ok {
		return
	}
	st, err := storeOf(rq.Host)
	if err != nil {
		return
	}
	repo := rq.RepoRef()
	mediaType, err := st.ConfigUnread(ctx, repo.ID, name, sum)
	kind := manifest.ConfigKind(mediaType)
	if err != nil || kind == "" {
		return
	}
	raw := readConfigItem(ctx, rq, repo, BlobPath(name, digest), kind)
	if raw == nil {
		return
	}
	_ = st.PutConfigFacts(ctx, repo.ID, name, sum, raw)
}
