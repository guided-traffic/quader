package oci

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/oci/rows"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The statements of the manifests a push writes and of the checks and reads around them (internal/meta/postgres/queries/
// oci_manifests.sql). The rows carry no blob reference: the items hold the bytes (R-OCI-007).

var errForeignTx = errors.New("oci: the transaction is not one of internal/meta/postgres")

func txQueries(tx meta.Tx) (*q.Queries, error) {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return nil, errForeignTx
	}
	return q.New(ptx), nil
}

// read runs fn in the transaction when there is one and in a short one of its own otherwise.
func (s *Store) read(ctx context.Context, tx meta.Tx, fn func(qs *q.Queries) error) error {
	run := func(tx meta.Tx) error {
		qs, err := txQueries(tx)
		if err != nil {
			return err
		}
		return fn(qs)
	}
	if tx != nil {
		return run(tx)
	}
	return s.o.DB.InTx(ctx, run)
}

// PutManifest records a manifest row and the references it names, in the transaction that writes the manifest's item. A manifest
// recorded already changes nothing; a target named twice is one edge, the first occurrence's.
func (s *Store) PutManifest(ctx context.Context, tx meta.Tx, in rows.ManifestInput) error {
	qs, err := txQueries(tx)
	if err != nil {
		return err
	}
	var subject []byte
	if in.Subject != nil {
		subject = in.Subject[:]
	}
	annotations := in.Annotations
	if len(annotations) == 0 {
		annotations = nil
	}
	var configDigest []byte
	if in.ConfigDigest != nil {
		configDigest = in.ConfigDigest[:]
	}
	if err := qs.OCIPutManifest(ctx, q.OCIPutManifestParams{
		RepoID: in.RepoID, Name: in.Name, Digest: in.Digest[:], MediaType: in.MediaType, ArtifactType: in.ArtifactType,
		Subject: subject, Size: in.Size, ContentSize: in.ContentSize, Annotations: annotations, AnnotationsCut: in.AnnotationsCut,
		Attachment: in.Attachment, ConfigDigest: configDigest, Blobs: nilIfEmpty(in.Blobs), ConfigFacts: nilIfEmpty(in.ConfigFacts),
	}); err != nil {
		return err
	}
	if len(in.References) == 0 {
		return nil
	}
	type edge struct {
		Target   string `json:"target"`
		Kind     string `json:"kind"`
		Platform string `json:"platform"`
	}
	seen := make(map[[32]byte]bool, len(in.References))
	edges := make([]edge, 0, len(in.References))
	for _, r := range in.References {
		if seen[r.Target] {
			continue
		}
		seen[r.Target] = true
		if r.Kind != rows.KindManifest && r.Kind != rows.KindBlob {
			return errs.E(errs.Internal, errs.CodeInternal, "oci: a reference of kind "+r.Kind)
		}
		edges = append(edges, edge{Target: hex.EncodeToString(r.Target[:]), Kind: r.Kind, Platform: r.Platform})
	}
	raw, err := json.Marshal(edges)
	if err != nil {
		return err
	}
	if err := qs.OCIPutReferences(ctx, q.OCIPutReferencesParams{RepoID: in.RepoID, Name: in.Name, Manifest: in.Digest[:], Refs: raw}); err != nil {
		return err
	}
	for _, e := range edges {
		if e.Kind == rows.KindManifest {
			// An index: its content size is the sum of its children's, at any depth (Q-OCI-130).
			return qs.OCISetIndexContentSize(ctx, q.OCISetIndexContentSizeParams{RepoID: in.RepoID, Name: in.Name, Digest: in.Digest[:]})
		}
	}
	return nil
}

// TagSizes reads what the size of the image of each tag with the given version id is made of, in one statement; a version that is
// not a stored tag of a recorded manifest is absent.
func (s *Store) TagSizes(ctx context.Context, versionIDs []int64) (map[int64]rows.TagSize, error) {
	out := make(map[int64]rows.TagSize, len(versionIDs))
	if len(versionIDs) == 0 {
		return out, nil
	}
	err := s.read(ctx, nil, func(qs *q.Queries) error {
		clear(out)
		rs, err := qs.OCITagSizes(ctx, versionIDs)
		for _, r := range rs {
			out[r.VersionID] = rows.TagSize{
				ContentSize: r.ContentSize,
				Children:    r.Children, ChildrenStored: r.ChildrenStored, ChildrenSize: r.ChildrenSize,
				Platforms: r.Platforms, PlatformsStored: r.PlatformsStored, PlatformsSize: r.PlatformsSize,
			}
		}
		return err
	})
	return out, err
}

func manifestRow(m q.OCIGetManifestRow) rows.Manifest {
	out := rows.Manifest{
		RepoID: m.RepoID, Name: m.Name, MediaType: m.MediaType, ArtifactType: m.ArtifactType.String, Size: m.Size,
		ContentSize: m.ContentSize, Annotations: m.Annotations, AnnotationsCut: m.AnnotationsCut, CreatedAt: m.CreatedAt.Time,
		Attachment: m.Attachment,
	}
	copy(out.Digest[:], m.Digest)
	if len(m.Subject) == 32 {
		var s [32]byte
		copy(s[:], m.Subject)
		out.Subject = &s
	}
	return out
}

// Manifest reads the row of a manifest.
func (s *Store) Manifest(ctx context.Context, repoID int64, name string, digest [32]byte) (rows.Manifest, bool, error) {
	var out rows.Manifest
	found := false
	err := s.read(ctx, nil, func(qs *q.Queries) error {
		out, found = rows.Manifest{}, false
		m, err := qs.OCIGetManifest(ctx, q.OCIGetManifestParams{RepoID: repoID, Name: name, Digest: digest[:]})
		if postgres.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		out, found = manifestRow(m), true
		return nil
	})
	return out, found, err
}

// References lists what a manifest names.
func (s *Store) References(ctx context.Context, repoID int64, name string, manifest [32]byte) ([]rows.Reference, error) {
	var out []rows.Reference
	err := s.read(ctx, nil, func(qs *q.Queries) error {
		out = out[:0]
		rs, err := qs.OCIListReferences(ctx, q.OCIListReferencesParams{RepoID: repoID, Name: name, Manifest: manifest[:]})
		if err != nil {
			return err
		}
		for _, r := range rs {
			ref := rows.Reference{Kind: r.Kind, Platform: r.Platform}
			copy(ref.Target[:], r.Target)
			out = append(out, ref)
		}
		return nil
	})
	return out, err
}

// Tombstone reads the withheld mark of a digest under an image name.
func (s *Store) Tombstone(ctx context.Context, repoID int64, name string, digest [32]byte) (rows.Tombstone, bool, error) {
	var out rows.Tombstone
	found := false
	err := s.read(ctx, nil, func(qs *q.Queries) error {
		out, found = rows.Tombstone{}, false
		at, err := qs.OCIGetTombstone(ctx, q.OCIGetTombstoneParams{RepoID: repoID, Name: name, Digest: digest[:]})
		if postgres.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		out, found = rows.Tombstone{RepoID: repoID, Name: name, Digest: digest, WithheldAt: at.Time}, true
		return nil
	})
	return out, found, err
}

// ExistingItems returns the paths among the given ones that are live files of the repository. In a transaction the rows are held
// against a concurrent removal until it ends.
func (s *Store) ExistingItems(ctx context.Context, tx meta.Tx, repoID int64, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	var out []string
	err := s.read(ctx, tx, func(qs *q.Queries) (err error) {
		if tx != nil {
			out, err = qs.OCILockExistingItems(ctx, q.OCILockExistingItemsParams{RepoID: repoID, Paths: paths})
		} else {
			out, err = qs.OCIExistingItems(ctx, q.OCIExistingItemsParams{RepoID: repoID, Paths: paths})
		}
		return err
	})
	return out, err
}

// WithheldAmong returns the digests among the given ones that carry a withheld mark under the image name.
func (s *Store) WithheldAmong(ctx context.Context, tx meta.Tx, repoID int64, name string, digests [][32]byte) ([][32]byte, error) {
	if len(digests) == 0 {
		return nil, nil
	}
	in := make([][]byte, len(digests))
	for i := range digests {
		in[i] = digests[i][:]
	}
	var out [][32]byte
	err := s.read(ctx, tx, func(qs *q.Queries) error {
		out = out[:0]
		rs, err := qs.OCIWithheldAmong(ctx, q.OCIWithheldAmongParams{RepoID: repoID, Name: name, Digests: in})
		if err != nil {
			return err
		}
		for _, r := range rs {
			var d [32]byte
			copy(d[:], r)
			out = append(out, d)
		}
		return nil
	})
	return out, err
}

// TagViews reads the tags with the given version ids with the facts of their manifests and, for an index, its children, in two
// statements however many packages the ids belong to.
func (s *Store) TagViews(ctx context.Context, versionIDs []int64) ([]rows.TagView, error) {
	if len(versionIDs) == 0 {
		return nil, nil
	}
	var out []rows.TagView
	err := s.read(ctx, nil, func(qs *q.Queries) error {
		out = out[:0]
		rs, err := qs.OCITagViews(ctx, versionIDs)
		if err != nil {
			return err
		}
		at := make(map[int64]int, len(rs))
		for _, r := range rs {
			v := rows.TagView{
				VersionID: r.VersionID, Tag: r.Tag, MediaType: r.MediaType, ArtifactType: r.ArtifactType, Size: r.Size, ContentSize: r.ContentSize,
			}
			copy(v.Digest[:], r.Digest)
			at[v.VersionID] = len(out)
			out = append(out, v)
		}
		if len(out) == 0 {
			return nil
		}
		children, err := qs.OCIChildPlatforms(ctx, versionIDs)
		if err != nil {
			return err
		}
		for _, c := range children {
			i, ok := at[c.VersionID]
			if !ok {
				continue
			}
			child := rows.Child{Platform: c.Platform, Stored: c.Stored, MediaType: c.MediaType, Size: c.Size, ContentSize: c.ContentSize}
			copy(child.Digest[:], c.Digest)
			out[i].Children = append(out[i].Children, child)
		}
		return nil
	})
	return out, err
}

// StoredTags lists the tags a local repository serves of an image name, newest push first.
func (s *Store) StoredTags(ctx context.Context, repoID int64, name string, limit int) ([]string, error) {
	var out []string
	err := s.read(ctx, nil, func(qs *q.Queries) (err error) {
		out, err = qs.OCIStoredTags(ctx, q.OCIStoredTagsParams{RepoID: repoID, Name: name, MaxRows: int32(min(max(limit, 0), math.MaxInt32))})
		return err
	})
	return out, err
}

// WithheldVersions returns the live tags of the repositories that point at a withheld manifest.
func (s *Store) WithheldVersions(ctx context.Context, repos []string) ([]int64, error) {
	var out []int64
	err := s.read(ctx, nil, func(qs *q.Queries) (err error) {
		out, err = qs.OCIWithheldVersions(ctx, repos)
		return err
	})
	return out, err
}

func nilIfEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

// ConfigUnread reports whether a manifest of the name names the blob as an image's configuration that is not read yet (R-OCI-040).
func (s *Store) ConfigUnread(ctx context.Context, repoID int64, name string, config [32]byte) (bool, error) {
	var out bool
	err := s.read(ctx, nil, func(qs *q.Queries) (err error) {
		out, err = qs.OCIConfigUnread(ctx, q.OCIConfigUnreadParams{RepoID: repoID, Name: name, ConfigDigest: config[:]})
		return err
	})
	return out, err
}

// PutConfigFacts records what the reader read of a configuration in the manifests of the name that name it and have none yet.
func (s *Store) PutConfigFacts(ctx context.Context, repoID int64, name string, config [32]byte, facts json.RawMessage) error {
	return s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := txQueries(tx)
		if err != nil {
			return err
		}
		return qs.OCIPutConfigFacts(ctx, q.OCIPutConfigFactsParams{RepoID: repoID, Name: name, ConfigDigest: config[:], ConfigFacts: facts})
	})
}

// TagFacts reads, for the tags with the given version ids, the facts of the manifests they point at and of the children an index
// names that the repository holds, in platform order (R-OCI-039, R-OCI-040); full adds the blobs and the configurations.
func (s *Store) TagFacts(ctx context.Context, versionIDs []int64, full bool) ([]rows.TagFacts, error) {
	if len(versionIDs) == 0 {
		return nil, nil
	}
	var out []rows.TagFacts
	err := s.read(ctx, nil, func(qs *q.Queries) error {
		out = out[:0]
		rs, err := qs.OCITagFacts(ctx, q.OCITagFactsParams{VersionIds: versionIDs, WithFacts: full})
		if err != nil {
			return err
		}
		at := map[int64]int{}
		for _, r := range rs {
			f := rows.ManifestFacts{Platform: r.Platform, Annotations: r.Annotations, Blobs: r.Blobs, ConfigFacts: r.ConfigFacts}
			copy(f.Digest[:], r.Digest)
			if d, ok := digestOf(r.ConfigDigest); ok {
				f.ConfigDigest = &d
			}
			i, seen := at[r.VersionID]
			if !seen {
				i = len(out)
				at[r.VersionID] = i
				out = append(out, rows.TagFacts{VersionID: r.VersionID})
			}
			if r.Child {
				out[i].Children = append(out[i].Children, f)
			} else {
				out[i].Image = f
			}
		}
		return nil
	})
	return out, err
}
