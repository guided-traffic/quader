package oci

import (
	"context"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// TagTargets reads the manifests the tags at the paths point at, in the transaction of the hook (signatures.md R-SIG-025): a local
// tag item's content, a remote's stored tag document.
func (s *Store) TagTargets(ctx context.Context, tx meta.Tx, repoID int64, paths []string) (map[string][32]byte, error) {
	out := map[string][32]byte{}
	if len(paths) == 0 {
		return out, nil
	}
	qs, err := txQueries(tx)
	if err != nil {
		return nil, err
	}
	rs, err := qs.OCITagTargets(ctx, q.OCITagTargetsParams{RepoID: repoID, Paths: paths})
	if err != nil {
		return nil, err
	}
	for _, r := range rs {
		if d, ok := digestOf(r.Digest); ok {
			if _, seen := out[r.Path]; !seen {
				out[r.Path] = d
			}
		}
	}
	return out, nil
}

// EnqueueSignatureFetch queues the background fetch of a remote's signatures for a top-level manifest (R-SIG-026), through the queue
// the composition root wired; a replica without it queues nothing.
func (s *Store) EnqueueSignatureFetch(ctx context.Context, tx meta.Tx, repoID int64, target string, subject [32]byte, refresh bool) error {
	if s.o.Signatures == nil {
		return nil
	}
	return s.o.Signatures.Enqueue(ctx, tx, repoID, "oci", target, subject, refresh)
}
