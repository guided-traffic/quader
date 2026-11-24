package oci

import (
	"context"
	"encoding/hex"
	"strings"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/oci/rows"
	"github.com/guided-traffic/quader/internal/formats/oci/tagorder"
	"github.com/guided-traffic/quader/internal/formats/versionkey"
)

// The package screens of the format (R-OCI-031, R-API-029): an image name is a package and its tags are its versions, newest first by
// push time in a local repository and by fetch time in a remote one, whose tags the screens read from the cache entries (R-OCI-036).
// Per tag the screen shows the digest, the media type, the summed size and, for an index, its platforms with the digest and the size
// of each child, read from the rows of R-OCI-007. Layer history and labels are not shown (Deferred).

// View returns the format's part of the package screens.
func (f *Format) View() formats.ViewAdapter { return viewAdapter{} }

type viewAdapter struct{}

var _ formats.ViewAdapter = viewAdapter{}

func viewPort(ports formats.DataPorts) (Store, error) {
	if ports != nil {
		if st, ok := ports.FormatData(ID).(Store); ok {
			return st, nil
		}
	}
	return nil, errs.E(errs.Internal, errs.CodeInternal, "oci: the data port is not bound")
}

// Compare orders tags by the tag order of the format (Q-OCI-17): versions by their numbers, then the release before its variants
// before its pre-releases, then every other tag by its bytes. A string that is no tag sorts before every tag, as the contract asks.
func (viewAdapter) Compare(a, b string) int {
	switch okA, okB := ValidTag(a), ValidTag(b); {
	case !okA && !okB:
		return strings.Compare(a, b)
	case !okA:
		return -1
	case !okB:
		return 1
	}
	return tagorder.Compare(a, b)
}

// SortKey is the stored key of Compare's order (Q-API-429): a string that is no tag first, then the key of the tag order. A remote
// repository's tags are cache entries whose key PostgreSQL derives from the tag (oci_tag_sort_key, migration 00116); both give the same
// bytes for every tag, which the listing test of R-OCI-036 checks.
func (viewAdapter) SortKey(tag string) []byte {
	return versionkey.Of(tagorder.Key(tag), ValidTag(tag), tag)
}

// Stored lists the tags a local repository serves of an image name, newest push first, for the exposure check of an endpoint. A remote
// repository answers none here: its tag documents are metadata entries and own no version row (Q-OCI-34, Q-OCI-121); the package screens
// list them from the cache entries (R-OCI-036, Q-OCI-215) and the exposure check does not read the cache.
func (viewAdapter) Stored(ctx context.Context, ports formats.DataPorts, q formats.StoredQuery) (formats.Stored, error) {
	if q.Repo.Class != formats.Local || q.Limit <= 0 {
		return formats.Stored{}, nil
	}
	st, err := viewPort(ports)
	if err != nil {
		return formats.Stored{}, err
	}
	tags, err := st.StoredTags(ctx, q.Repo.ID, q.Package, q.Limit)
	return formats.Stored{Versions: tags}, err
}

// Withheld returns the tags of the repositories that point at a manifest the journal replay withheld (R-OCI-033): the screens list
// none of them.
func (viewAdapter) Withheld(ctx context.Context, ports formats.DataPorts, repos []string) ([]formats.VersionID, error) {
	if len(repos) == 0 {
		return nil, nil
	}
	st, err := viewPort(ports)
	if err != nil {
		return nil, err
	}
	ids, err := st.WithheldVersions(ctx, repos)
	out := make([]formats.VersionID, len(ids))
	for i, id := range ids {
		out[i] = formats.VersionID(id)
	}
	return out, err
}

// Find is empty: an image has no description or keywords to search (the config is not read, R-OCI-031).
func (viewAdapter) Find(context.Context, formats.DataPorts, formats.ViewFind, int) ([]formats.VersionID, error) {
	return nil, nil
}

// Describe reads, for tags, the facts of the manifests they point at, in two statements however many packages the page holds. The
// summary the package list asks for holds a description and keywords, which an image has none of, so it costs nothing; the details ask
// for the metadata of a version:
//
//	digest, mediaType, size (the summed size of the config and the layers; of an index, of its children), artifactType (the
//	manifest's own, else its config's media type) and, for an index, platforms: one object per child with platform, digest,
//	mediaType and size.
//
// A tag the repository holds no recorded manifest for is absent from the answer.
func (viewAdapter) Describe(ctx context.Context, ports formats.DataPorts, vs []formats.ViewVersion, want formats.ViewFields, _ func(string) bool) (map[formats.VersionID]formats.ViewData, error) {
	if !want.Has(formats.ViewDetails) || len(vs) == 0 {
		return map[formats.VersionID]formats.ViewData{}, nil
	}
	st, err := viewPort(ports)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(vs))
	for i, v := range vs {
		ids[i] = int64(v.ID)
	}
	views, err := st.TagViews(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[formats.VersionID]formats.ViewData, len(views))
	for _, tv := range views {
		out[formats.VersionID(tv.VersionID)] = formats.ViewData{Metadata: tagMetadata(tv)}
	}
	return out, nil
}

var _ formats.ViewSizer = (*Format)(nil)

// ViewSizes implements [formats.ViewSizer]: the size of a tag, as the package screens show it for the version, is the size of its
// image, not of the tag's file, which is the manifest document (R-OCI-031, Q-OCI-130, Q-OCI-221). An image manifest names the size of
// its config and layers, so its size is known once the manifest is; an index's is the sum over the platforms the repository holds,
// partial while some are missing and not known while none is (imageSize). One read answers for all the tags asked for, and for no
// other id. A tag with no recorded manifest is absent.
func (*Format) ViewSizes(ctx context.Context, ports formats.DataPorts, vs []formats.ViewVersion) (map[formats.VersionID]formats.ViewSize, error) {
	if len(vs) == 0 {
		return map[formats.VersionID]formats.ViewSize{}, nil
	}
	st, err := viewPort(ports)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(vs))
	asked := make(map[int64]bool, len(vs))
	for i, v := range vs {
		ids[i] = int64(v.ID)
		asked[int64(v.ID)] = true
	}
	sizes, err := st.TagSizes(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[formats.VersionID]formats.ViewSize, len(sizes))
	for id, ts := range sizes {
		if asked[id] {
			out[formats.VersionID(id)] = imageSize(ts)
		}
	}
	return out, nil
}

// imageSize is the size of a tag's image as the screens show it (Q-OCI-221). An image manifest's is the config and the layers it
// names: the manifest states them and its digest fixes them, so the size is known whether the layers are stored or not. An index's is
// the sum over the images of its platforms that the repository holds: all of them in a local repository, those a client fetched in a
// remote one. While some are missing the sum is partial; while none is, the size is not known. Attestation manifests (the platform
// unknown/unknown) are left out of the count, because a pull of a platform never fetches them; an index that names nothing else is
// counted over all its children.
func imageSize(ts rows.TagSize) formats.ViewSize {
	if ts.Children == 0 {
		return formats.ViewSize{Bytes: max(ts.ContentSize, 0)}
	}
	total, stored, size := ts.Platforms, ts.PlatformsStored, ts.PlatformsSize
	if total == 0 {
		total, stored, size = ts.Children, ts.ChildrenStored, ts.ChildrenSize
	}
	switch {
	case stored == 0:
		return formats.ViewSize{Unknown: true}
	case stored < total:
		return formats.ViewSize{Bytes: max(size, 0), Partial: true}
	}
	return formats.ViewSize{Bytes: max(size, 0)}
}

func tagMetadata(tv rows.TagView) map[string]any {
	md := map[string]any{
		"digest":    "sha256:" + hex.EncodeToString(tv.Digest[:]),
		"mediaType": tv.MediaType,
	}
	// The size of the image follows the version's (imageSize): absent while it is not known, with sizePartial while it counts only
	// the platforms the repository holds.
	if size := imageSize(rows.SizeOf(tv)); !size.Unknown {
		md["size"] = size.Bytes
		if size.Partial {
			md["sizePartial"] = true
		}
	}
	if tv.ArtifactType != "" {
		md["artifactType"] = tv.ArtifactType
	}
	if len(tv.Children) == 0 {
		return md
	}
	children := tv.Children // in platform order, as the statement read them
	platforms := make([]map[string]any, 0, len(children))
	var attestations []map[string]any
	for _, c := range children {
		p := map[string]any{"platform": c.Platform, "digest": "sha256:" + hex.EncodeToString(c.Digest[:]), "stored": c.Stored}
		if c.Stored {
			// A platform the repository does not hold has no media type or size it could state (Q-OCI-221).
			p["mediaType"], p["size"] = c.MediaType, c.ContentSize
		}
		if c.Platform == rows.AttestationPlatform {
			// BuildKit's attestation entries are no platform: they stand at the image with its attestations (signatures.md R-SIG-024).
			delete(p, "platform")
			attestations = append(attestations, p)
			continue
		}
		platforms = append(platforms, p)
	}
	md["platforms"] = platforms
	if len(attestations) > 0 {
		md["attestations"] = attestations
	}
	return md
}

const maxInt64 = int64(^uint64(0) >> 1)
