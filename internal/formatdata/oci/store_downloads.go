package oci

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// DownloadQuery asks the version of a client's download of an image (operations-and-observability.md R-OPS-049 "A client's pull of an
// image") by its remote repository, its image name and the path of its first file.
type DownloadQuery struct {
	RepoID     int64
	Name, Path string
}

// DownloadVersions answers each query with the version of the image its file belongs to: the most recently validated tag whose manifest
// is the file, names it, or names a manifest that names it; else the digest of the topmost stored manifest the file belongs to (a pull
// by digest); "" for a path that names no digest or a file of no stored manifest (OCIDownloadVersions). One statement for all queries,
// outside the path of a fetch: the publisher of the downloads asks it every publication for the units it has not resolved.
func (s *Store) DownloadVersions(ctx context.Context, qs []DownloadQuery) ([]string, error) {
	out := make([]string, len(qs))
	type query struct {
		Ord    int    `json:"ord"`
		RepoID int64  `json:"repo_id"`
		Name   string `json:"name"`
		Digest string `json:"digest"`
	}
	var asked []query
	for i, dq := range qs {
		if hex, ok := contentDigest(dq.Name, dq.Path); ok {
			asked = append(asked, query{Ord: i, RepoID: dq.RepoID, Name: dq.Name, Digest: hex})
		}
	}
	if len(asked) == 0 {
		return out, nil
	}
	b, err := json.Marshal(asked)
	if err != nil {
		return nil, err
	}
	var rows []q.OCIDownloadVersionsRow
	err = s.read(ctx, nil, func(qs *q.Queries) error {
		rows, err = qs.OCIDownloadVersions(ctx, string(b))
		return err
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.Ord >= 0 && int(r.Ord) < len(out) {
			out[r.Ord] = r.Version
		}
	}
	return out, nil
}

// contentDigest is the hexadecimal SHA-256 of a manifest or blob path of name in the layout of R-OCI-007
// (`<name>/_manifests/sha256:<hex>`, `<name>/_blobs/sha256:<hex>`); ok is false for any other path.
func contentDigest(name, path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, name+"/")
	if !ok {
		return "", false
	}
	for _, dir := range []string{dirManifests + "/sha256:", "_blobs/sha256:"} {
		if hex, ok := strings.CutPrefix(rest, dir); ok && len(hex) == 64 && strings.Trim(hex, "0123456789abcdef") == "" {
			return hex, true
		}
	}
	return "", false
}
