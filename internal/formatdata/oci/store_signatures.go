package oci

import (
	"context"

	"github.com/guided-traffic/quader/internal/formats/oci/rows"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// TagTargets reads the manifests the tags at the paths point at: a local tag item's content, a remote's stored tag document. It reads in
// the transaction of a removal's hook (signatures.md R-SIG-025) and, with tx nil, in a short one of its own: the delivery decision of a
// manifest by digest of an image with version rules has no transaction (formats/oci.md R-OCI-038, Q-CUR-10).
func (s *Store) TagTargets(ctx context.Context, tx meta.Tx, repoID int64, paths []string) (map[string][32]byte, error) {
	out := map[string][32]byte{}
	if len(paths) == 0 {
		return out, nil
	}
	err := s.read(ctx, tx, func(qs *q.Queries) error {
		rs, err := qs.OCITagTargets(ctx, q.OCITagTargetsParams{RepoID: repoID, Paths: paths})
		if err != nil {
			return err
		}
		for _, r := range rs {
			if d, ok := digestOf(r.Digest); ok {
				if _, seen := out[r.Path]; !seen {
					out[r.Path] = d
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GateFacts reads whether each manifest is an attachment and the indexes of its name that list it (signatures.md R-SIG-063), in one
// statement; a manifest that is not recorded is absent.
func (s *Store) GateFacts(ctx context.Context, repoID int64, keys []rows.ManifestKey) (map[rows.ManifestKey]rows.GateFacts, error) {
	out := make(map[rows.ManifestKey]rows.GateFacts, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	asked := make(map[rows.ManifestKey]bool, len(keys))
	names, digests := make([]string, 0, len(keys)), make([][]byte, 0, len(keys))
	for _, k := range keys {
		if !asked[k] {
			asked[k] = true
			names, digests = append(names, k.Name), append(digests, k.Digest[:])
		}
	}
	err := s.read(ctx, nil, func(qs *q.Queries) error {
		clear(out)
		rs, err := qs.OCIGateFacts(ctx, q.OCIGateFactsParams{RepoID: repoID, Names: names, Digests: digests})
		if err != nil {
			return err
		}
		for _, r := range rs {
			d, ok := digestOf(r.Digest)
			k := rows.ManifestKey{Name: r.Name, Digest: d}
			if !ok || !asked[k] {
				continue
			}
			f := rows.GateFacts{Attachment: r.Attachment}
			for _, raw := range r.Indexes {
				if idx, ok := digestOf(raw); ok {
					f.Indexes = append(f.Indexes, idx)
				}
			}
			out[k] = f
		}
		return nil
	})
	return out, err
}

// EnqueueSignatureFetch queues the background fetch of a remote's signatures for a top-level manifest (R-SIG-026), through the queue
// the composition root wired; a replica without it queues nothing.
func (s *Store) EnqueueSignatureFetch(ctx context.Context, tx meta.Tx, repoID int64, target string, subject [32]byte, refresh bool) error {
	if s.o.Signatures == nil {
		return nil
	}
	return s.o.Signatures.Enqueue(ctx, tx, repoID, "oci", target, subject, refresh)
}
