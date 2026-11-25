package items

import (
	"time"

	"github.com/guided-traffic/quader/internal/qname"

	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/malware"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/servefacts"
	"github.com/guided-traffic/quader/internal/vuln"
	"github.com/guided-traffic/quader/internal/vuln/advisory"
)

// Kinds of an item.
const (
	KindFile   = "file"
	KindFolder = "folder"
)

// Item is a live row of the items table with what the descriptions need of its
// blob.
type Item struct {
	ID         int64
	RepoID     int64
	Repository qname.Repo
	Path       string
	Kind       string
	Size       int64
	MediaType  string
	// Digest is the SHA-256 of the content; the other digests are those of the
	// blob row. All are zero for a folder.
	Digest blob.Digest
	SHA1   []byte
	MD5    []byte
	SHA512 []byte
	// Declared digests are those the client announced (Repr-Digest), nil if none.
	DeclaredSHA256, DeclaredSHA512, DeclaredSHA1, DeclaredMD5 []byte
	DeclaredMismatch                                          bool
	Explicit                                                  bool
	Generated                                                 bool
	ContentStatus                                             string
	// UpstreamURL is where the content of a remote repository's item came from;
	// "" for an item that was uploaded.
	UpstreamURL                      string
	CreatedAt, ModifiedAt, UpdatedAt time.Time
	// CreatedBy and ModifiedBy are "<kind>:<name>" of the principal, "" when the
	// row has none (a break-glass administrator, an erased principal).
	CreatedBy, ModifiedBy string
	// VersionID is the package_versions.id of the stored version the item belongs to, 0 for an item that belongs to
	// none (R-VULN-015 applies to items that have one).
	VersionID int64
	// Block is the finding that blocks the item's version in its repository, read in the lookup with the policy of
	// RepoRef.Vuln; the zero value is none. Policy is the policy it was read with. [vuln.Verdict] turns both into the
	// decision on a download.
	Block  vuln.BlockRow
	Policy vuln.Policy
	// Facts are the rows the lookup read for RepoRef.Facts (zero key: none). EntryStored is true when RepoRef.Entry was set and
	// the cache entry of the artifact exists and is not marked gone.
	Facts       servefacts.Facts
	EntryStored bool
	// Malware is the verdict of the malware scanning on the item's blob and its release, read in the lookup (R-MAL-013,
	// Q-MAL-23); the zero value is a blob without a row, which is pending. A folder has none.
	Malware malware.Verdict
	// Copies are the copy rows of the item's blob, read in the lookup when CopiesRead is set (storage.md R-STOR-055,
	// Q-STOR-275): Open hands them to the blob store, so that a download costs the lookup alone (R-ARCH-027). An item built
	// another way has none read, and Open then asks for them.
	Copies     []blob.Copy
	CopiesRead bool
}

// itemFrom converts the row of GetLiveItem.
func itemFrom(repoKey qname.Repo, key servefacts.Key, r q.GetLiveItemRow) Item {
	it := Item{
		ID: r.ID, RepoID: r.RepoID, Repository: repoKey, Path: r.Path, Kind: r.Kind,
		DeclaredSHA256: r.DeclaredSha256, DeclaredSHA512: r.DeclaredSha512, DeclaredSHA1: r.DeclaredSha1, DeclaredMD5: r.DeclaredMd5,
		DeclaredMismatch: r.DeclaredMismatch, Explicit: r.ExplicitFolder, Generated: r.Generated, UpstreamURL: r.UpstreamUrl.String,
		CreatedAt: r.CreatedAt.Time.UTC(), ModifiedAt: r.ModifiedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(),
		CreatedBy: r.CreatedByName, ModifiedBy: r.ModifiedByName, VersionID: r.VersionID,
	}
	it.EntryStored = r.EntryStored
	// The statement says which kind it evaluated; facts for a kind it has no branch for would be all false, so they are not stamped
	// with the key and the serve check reads the rows itself.
	if !key.Zero() && string(key.Kind) == r.FactKind {
		it.Facts = servefacts.Facts{
			Key: key, Tombstoned: r.FactTombstoned, OCIManifest: r.FactOciManifest, Stored: r.FactNugetStored, Listed: r.FactNugetListed,
			NPMRemote: servefacts.NPMRemoteFacts{
				Known: r.FactNpmKnown, Listed: r.FactNpmListed, Integrity: r.FactNpmIntegrity, Shasum: r.FactNpmShasum,
				DocStored: r.FactNpmDocStored, DocCurrent: r.FactNpmDocCurrent, DocGoneUs: r.FactNpmGoneUs, ServeRemoved: r.FactNpmServeRemoved,
			},
			DebRemote: debRemoteFacts(r),
		}
	}
	if r.BlockFindingID != "" {
		it.Block = vuln.BlockRow{
			FindingID: r.BlockFindingID, AdvisoryID: r.BlockAdvisoryID, Kind: advisory.Kind(r.BlockKind), Status: vuln.Status(r.BlockStatus),
		}
	}
	if r.Kind != KindFile {
		return it
	}
	it.Size = r.Size.Int64
	it.MediaType = r.MediaType.String
	copy(it.Digest[:], r.BlobSha256)
	it.SHA1, it.MD5, it.SHA512 = r.BlobSha1, r.BlobMd5, r.BlobSha512
	it.ContentStatus = r.ContentStatus.String
	it.Malware = malwareVerdict(it.Digest, r)
	// An array the store cannot read leaves the copies unread; Open then asks for them with a statement of its own.
	if cs, err := blob.ParseCopies(r.BlobCopies); err == nil {
		it.Copies, it.CopiesRead = cs, true
	}
	return it
}

// malwareVerdict reads the verdict columns of the lookup.
func malwareVerdict(d blob.Digest, r q.GetLiveItemRow) malware.Verdict {
	v := malware.Verdict{Known: r.MalwareKnown}
	if r.MalwareKnown {
		v.State, v.Reason, v.Signature, v.Part = malware.State(r.MalwareState), r.MalwareReason, r.MalwareSignature, r.MalwarePart
		v.Limit, v.Limits = malware.Limit(r.MalwareExceededLimit), r.MalwareLimits
		v.Database, v.DatabaseBuiltAt = r.MalwareDbVersion, r.MalwareDbBuiltAt.Time.UTC()
		v.ScannedAt, v.FirstFoundAt, v.Due = r.MalwareScannedAt.Time.UTC(), r.MalwareFirstFoundAt.Time.UTC(), r.MalwareDue
	}
	if r.MalwareReleased {
		v.Release = &malware.Release{
			Digest: d, Reason: r.MalwareReleaseReason, State: malware.State(r.MalwareReleaseState), ReleasedBy: r.MalwareReleasedBy,
			ReleasedByName: r.MalwareReleasedByName, ReleasedAt: r.MalwareReleasedAt.Time.UTC(),
		}
	}
	return v
}

// ETag is the strong entity tag of a file: the content address (R-STOR-012).
func (it Item) ETag() string { return `"` + it.Digest.Hex() + `"` }

// MediaTypeOrDefault is the media type served for the item (R-GENERIC-005): the
// one recorded at deploy time, or application/octet-stream.
func (it Item) MediaTypeOrDefault() string {
	if it.MediaType == "" {
		return "application/octet-stream"
	}
	return it.MediaType
}

// Root is the synthetic folder of a repository root.
func Root(key qname.Repo, created, updated time.Time) Item {
	return Item{
		Repository: key, Kind: KindFolder, Explicit: true,
		CreatedAt: created.UTC(), ModifiedAt: updated.UTC(), UpdatedAt: updated.UTC(),
	}
}

// debRemoteFacts reads the columns of the deb_remote kind: the digest and size the newest parsed index lists for the item's path.
func debRemoteFacts(r q.GetLiveItemRow) servefacts.DebRemoteFacts {
	f := servefacts.DebRemoteFacts{Known: r.FactDebRemoteKnown, Size: r.FactDebRemoteSize}
	copy(f.SHA256[:], r.FactDebRemoteSha256)
	return f
}
