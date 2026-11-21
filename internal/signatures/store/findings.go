package store

import (
	"context"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signatures"
)

// kindsOf is the kinds of findings asked; none is both.
func kindsOf(kinds []string) []string {
	out := make([]string, 0, 2)
	for _, k := range kinds {
		if k == signatures.FindingInvalid || k == signatures.FindingBlocked {
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		return []string{signatures.FindingInvalid, signatures.FindingBlocked}
	}
	return out
}

// FindingHolders answers the repositories that hold an attachment of a finding of the kinds (empty: both), so that the caller's
// readable ones scope the list (R-SIG-038).
func (s *Store) FindingHolders(ctx context.Context, kinds []string) ([]signatures.Holder, error) {
	var out []signatures.Holder
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := qs.SignatureFindingHolders(ctx, kindsOf(kinds))
		if err != nil {
			return err
		}
		out = make([]signatures.Holder, 0, len(rows))
		for _, r := range rows {
			out = append(out, signatures.Holder{RepoID: r.RepoID, ProjectKey: r.ProjectKey, RepoKey: r.RepoKey, Format: r.Format})
		}
		return nil
	})
	return out, err
}

// Findings answers a page of the security dashboard's view (R-SIG-038): invalid signatures with an attachment and signatures whose
// installation trust row is blocked, within q.RepoIDs (nil: every repository), newest first, after the cursor; q.Limit+1 rows when more
// follow. Each carries the files of its subject in the repositories of the scope that hold an attachment, and its block.
func (s *Store) Findings(ctx context.Context, fq signatures.FindingsQuery) ([]signatures.Finding, error) {
	out := []signatures.Finding{}
	if fq.RepoIDs != nil && len(fq.RepoIDs) == 0 {
		return out, nil
	}
	limit := fq.Limit
	if limit <= 0 {
		limit = 50
	}
	repoIDs := fq.RepoIDs
	if repoIDs == nil {
		repoIDs = []int64{}
	}
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := qs.SignatureFindings(ctx, q.SignatureFindingsParams{
			Kinds: kindsOf(fq.Kinds), AllRepos: fq.RepoIDs == nil, RepoIds: repoIDs, RepoID: fq.RepoID, Q: fq.Q,
			AfterAt: rfc3339(fq.AfterAt), AfterID: fq.AfterID, MaxRows: int32(min(limit, 1000) + 1), //nolint:gosec // bounded
		})
		if err != nil || len(rows) == 0 {
			return err
		}
		ids := make([]int64, 0, len(rows))
		var blockIDs []int64
		for _, r := range rows {
			ids = append(ids, r.SignatureID)
			if r.BlockID != 0 {
				blockIDs = append(blockIDs, r.BlockID)
			}
		}
		files, err := qs.SignatureFindingFiles(ctx, q.SignatureFindingFilesParams{
			SignatureIds: ids, AllRepos: fq.RepoIDs == nil, RepoIds: repoIDs, PerSignature: maxFilesPerFinding,
		})
		if err != nil {
			return err
		}
		holders := map[int64][]signatures.Holder{}
		for _, f := range files {
			holders[f.SignatureID] = append(holders[f.SignatureID], signatures.Holder{
				RepoID: f.RepoID, ProjectKey: f.ProjectKey, RepoKey: f.RepoKey, Format: f.Format, Path: f.Path,
			})
		}
		blocks := map[int64]signatures.BlockRecord{}
		if len(blockIDs) > 0 {
			recs, err := blockRecords(ctx, qs, blockIDs)
			if err != nil {
				return err
			}
			for _, b := range recs {
				blocks[b.ID] = b
			}
		}
		for _, r := range rows {
			f := signatures.Finding{
				SignatureID: r.SignatureID, Kind: r.Kind, Scheme: r.Scheme, Role: r.Role, Reason: r.Reason, Signer: r.Signer,
				Subject: digest(r.SubjectSha256), SubjectName: r.SubjectName, FoundAt: timeOf(r.FoundAt.Valid, r.FoundAt.Time),
				Holders: holders[r.SignatureID],
			}
			if b, ok := blocks[r.BlockID]; ok {
				f.Block = &b
			}
			out = append(out, f)
		}
		return nil
	})
	return out, err
}
