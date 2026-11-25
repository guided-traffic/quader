package manifest

import (
	"encoding/json"
	"io"

	"github.com/guided-traffic/quader/internal/formats/oci/rows"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
)

// What the registry keeps of a stored image manifest and its configuration (formats/oci.md R-OCI-040, Q-OCI-271): the format writes both
// at a push, and the data port's job oci-describe writes them for a manifest whose configuration was stored by a fill, a mount or before
// the rows existed. Both read the same bytes the same way.

// maxBlobsJSON bounds the JSON of an image's blobs below the column's check (00132: 256 KiB).
const maxBlobsJSON = 200 << 10

// ImageBlobs is the JSON of the config's and the layers' descriptors of an image manifest in its order (rows.ImageBlobs), at most
// rows.MaxLayers layers and at most maxBlobsJSON bytes of JSON; nil for a manifest that is no image. Every value passed the parser
// (digests, media types of at most 255 bytes and sizes of the form it checks), so a descriptor is at most about 400 bytes and a
// realistic one a third of that.
func ImageBlobs(m *Manifest) []byte {
	if m.Type != TypeImage {
		return nil
	}
	desc := func(d Descriptor) rows.BlobDescriptor {
		return rows.BlobDescriptor{Digest: d.Digest, MediaType: d.MediaType, Size: d.Size, Foreign: d.Foreign()}
	}
	out := rows.ImageBlobs{Layers: make([]rows.BlobDescriptor, 0, min(len(m.Layers), rows.MaxLayers))}
	if m.Config != nil {
		c := desc(*m.Config)
		out.Config = &c
	}
	used := 512
	for i, l := range m.Layers {
		d := desc(l)
		// The JSON of a descriptor: its strings quoted (the parser admits no character json escapes) and its keys and number.
		used += len(d.Digest) + len(d.MediaType) + 80
		if i >= rows.MaxLayers || used > maxBlobsJSON {
			out.More = len(m.Layers) - i
			break
		}
		out.Layers = append(out.Layers, d)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return raw
}

// ConfigDigest is the digest of an image's or a chart's configuration the manifest names, ok false for a manifest whose config no
// reader reads (ConfigKind).
func ConfigDigest(m *Manifest) (d [32]byte, ok bool) {
	if m.Type != TypeImage || m.Config == nil || ConfigKind(m.Config.MediaType) == "" {
		return d, false
	}
	return ParseDigest(m.Config.Digest)
}

// ReadStoredConfig reads a stored configuration of the kind ConfigKind named, of the stored size: one larger than MaxConfigBytes is
// recorded as too large without a byte of it opened; open opens it otherwise. The error is that of open or of the read, which a caller
// leaves unrecorded so that a later read tries again; a configuration the reader refuses is a ConfigFacts that says why.
func ReadStoredConfig(kind string, size int64, open func() (io.ReadCloser, error)) (rows.ConfigFacts, error) {
	if size > MaxConfigBytes {
		return rows.ConfigFacts{Kind: kind, Unreadable: rows.UnreadableTooLarge}, nil
	}
	r, err := open()
	if err != nil {
		return rows.ConfigFacts{}, err
	}
	defer func() { _ = r.Close() }()
	data, err := untrusted.ReadAll(r, MaxConfigBytes+1)
	switch {
	case untrusted.IsTooLarge(err):
		return rows.ConfigFacts{Kind: kind, Unreadable: rows.UnreadableTooLarge}, nil
	case err != nil:
		return rows.ConfigFacts{}, err
	}
	return ReadConfig(kind, data), nil
}
