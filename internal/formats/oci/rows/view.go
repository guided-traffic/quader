package rows

// TagView is a tag with the facts of the manifest it points at, for the package view (R-OCI-031).
type TagView struct {
	// VersionID is the id of the version row of the tag.
	VersionID int64
	Tag       string
	Digest    [32]byte
	MediaType string
	// ArtifactType is the manifest's own artifactType, else its config's media type; "" for none.
	ArtifactType string
	// Size is the size of the manifest document; ContentSize the size of the image: the summed size of the config and layers of an image manifest, the sum of its
	// children's ContentSize for an index (Q-OCI-130).
	Size        int64
	ContentSize int64
	// Children are what an index names, in platform order; empty for an image manifest.
	Children []Child
}

// Child is a manifest an index names: the platform of the edge and, when the repository holds it, the child's own facts.
type Child struct {
	// Platform is "os/arch[/variant]", "" when the index names none for the child.
	Platform string
	Digest   [32]byte
	// Stored says that the repository holds the child's manifest; MediaType, Size and ContentSize are known only then. A remote
	// repository holds a platform once a client fetched it (Q-OCI-221).
	Stored      bool
	MediaType   string
	Size        int64
	ContentSize int64
}

// AttestationPlatform is the platform an index names an attestation manifest with: it is no image of a platform.
const AttestationPlatform = "unknown/unknown"

// TagSize is what the size of a tag's image is made of (R-OCI-031, Q-OCI-221): the content size of its manifest and, for an index,
// its children, counted once over all of them and once over the images of a platform.
type TagSize struct {
	ContentSize int64
	// Children counts the manifests an index names, ChildrenStored those the repository holds and ChildrenSize their summed content
	// size; Platforms, PlatformsStored and PlatformsSize the same over the children whose platform is not AttestationPlatform.
	Children, ChildrenStored, ChildrenSize    int64
	Platforms, PlatformsStored, PlatformsSize int64
}

// SizeOf counts the children of a TagView the way the statement of TagSizes counts them.
func SizeOf(v TagView) TagSize {
	out := TagSize{ContentSize: v.ContentSize}
	for _, c := range v.Children {
		out.Children++
		attestation := c.Platform == AttestationPlatform
		if !attestation {
			out.Platforms++
		}
		if !c.Stored {
			continue
		}
		out.ChildrenStored++
		out.ChildrenSize += c.ContentSize
		if !attestation {
			out.PlatformsStored++
			out.PlatformsSize += c.ContentSize
		}
	}
	return out
}
