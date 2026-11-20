package store

import (
	"context"

	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/qname"
)

// Unanalysed returns the due manifests whose analysis by the analyser version is not stored yet (Q-VULN-168): the work of a run
// without a database, which has the scanner analyse them and leaves their rows pending.
func (s *Images) Unanalysed(ctx context.Context, analyser string, maxAttempts, limit int) ([]ImageDue, error) {
	var out []ImageDue
	err := s.tx(ctx, func(qs *q.Queries) error {
		rows, err := qs.VulnImageUnanalysed(ctx, q.VulnImageUnanalysedParams{Analyser: analyser, MaxAttempts: rows32(maxAttempts), MaxRows: rows32(limit)})
		if err != nil {
			return err
		}
		out = make([]ImageDue, 0, len(rows))
		for _, r := range rows {
			out = append(out, imageDue(r.RepoID, r.Name, r.Digest, r.Attempts, r.ProjectKey, r.RepoKey))
		}
		return nil
	})
	return out, err
}

func imageDue(repoID int64, name string, digest []byte, attempts int32, project, key string) ImageDue {
	d := ImageDue{RepoID: repoID, Name: name, Attempts: int(attempts), Repo: qname.Repo{Project: project, Key: key}}
	copy(d.Digest[:], digest)
	return d
}
