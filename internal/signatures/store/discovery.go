package store

import (
	"context"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// DiscoveryItem is a live file discovery asks the formats' detectors about (R-SIG-011): its item, path, blob and media type, the
// upstream it was fetched from ("" for an upload), and its repository.
type DiscoveryItem struct {
	ID          int64
	Path        string
	Blob        [32]byte
	Size        int64
	MediaType   string
	UpstreamURL string
	RepoID      int64
	ProjectKey  string
	RepoKey     string
	Format      string
	Class       string
}

// DiscoveryItems answers at most limit live files after the item after, in item order, that a client or a fill stored (a file Quader
// derived is not asked, Q-SIG-52).
func (s *Store) DiscoveryItems(ctx context.Context, after int64, limit int) ([]DiscoveryItem, error) {
	var out []DiscoveryItem
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := qs.SignatureDiscoverItems(ctx, q.SignatureDiscoverItemsParams{After: after, RowLimit: int32(min(limit, 10000))}) //nolint:gosec // bounded
		if err != nil {
			return err
		}
		out = make([]DiscoveryItem, len(rows))
		for i, r := range rows {
			out[i] = DiscoveryItem{
				ID: r.ID, Path: r.Path, Blob: digest(r.BlobSha256), Size: r.Size, MediaType: r.MediaType, UpstreamURL: r.UpstreamUrl,
				RepoID: r.RepoID, ProjectKey: r.ProjectKey, RepoKey: r.RepoKey, Format: r.Format, Class: r.Class,
			}
		}
		return nil
	})
	return out, err
}
