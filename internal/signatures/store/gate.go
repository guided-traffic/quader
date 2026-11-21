package store

import (
	"context"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/fetch"
	"github.com/guided-traffic/quader/internal/signatures/gate"
)

// The reads and writes of the gate per repository, stage 3 (signatures.md R-SIG-062 to R-SIG-067, R-SIG-073;
// queries/signature_gate.sql). The store implements gate.Store.

var _ gate.Store = (*Store)(nil)

func digestsOf(ds [][32]byte) [][]byte {
	out := make([][]byte, len(ds))
	for i := range ds {
		out[i] = ds[i][:]
	}
	return out
}

// GateRows implements [gate.Store]: the counting signatures of the subjects attached in the repositories, with their trust row in the
// scope of the project (the installation's for "").
func (s *Store) GateRows(ctx context.Context, projectID string, repoIDs []int64, subjects [][32]byte) ([]gate.Row, error) {
	var out []gate.Row
	if len(repoIDs) == 0 || len(subjects) == 0 {
		return out, nil
	}
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rs, err := qs.SignatureGateRows(ctx, q.SignatureGateRowsParams{ProjectID: projectID, Subjects: digestsOf(subjects), RepoIds: repoIDs})
		if err != nil {
			return err
		}
		out = out[:0]
		for _, r := range rs {
			if len(r.SubjectSha256) != 32 {
				continue
			}
			out = append(out, gate.Row{
				ID: r.ID, Subject: [32]byte(r.SubjectSha256), State: signatures.State(r.State), KeyIDs: r.KeyIds, OIDCIssuer: r.OidcIssuer,
				Identity: r.Identity, Issued: r.Issued, HasTrust: r.HasTrust, TrustState: r.TrustState, TrustAnchors: r.TrustAnchorIds, TrustEpoch: r.TrustEpoch,
			})
		}
		return nil
	})
	return out, err
}

// GateFetches implements [gate.Store].
func (s *Store) GateFetches(ctx context.Context, repoIDs []int64, subjects [][32]byte) ([]gate.Fetch, error) {
	var out []gate.Fetch
	if len(repoIDs) == 0 || len(subjects) == 0 {
		return out, nil
	}
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rs, err := qs.SignatureGateFetches(ctx, q.SignatureGateFetchesParams{RepoIds: repoIDs, Subjects: digestsOf(subjects)})
		if err != nil {
			return err
		}
		out = out[:0]
		for _, r := range rs {
			if len(r.SubjectSha256) != 32 {
				continue
			}
			out = append(out, gate.Fetch{
				RepoID: r.RepoID, Target: r.Target, Subject: [32]byte(r.SubjectSha256), Queued: r.Queued, Fetched: r.Fetched, Failed: r.Failed,
			})
		}
		return nil
	})
	return out, err
}

// GateObjectsRead implements [gate.Store].
func (s *Store) GateObjectsRead(ctx context.Context, digests [][32]byte, maxSize int64) (map[[32]byte]bool, error) {
	out := map[[32]byte]bool{}
	if len(digests) == 0 {
		return out, nil
	}
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rs, err := qs.SignatureGateObjectsRead(ctx, q.SignatureGateObjectsReadParams{Digests: digestsOf(digests), MaxSize: maxSize})
		if err != nil {
			return err
		}
		clear(out)
		for _, d := range rs {
			if len(d) == 32 {
				out[[32]byte(d)] = true
			}
		}
		return nil
	})
	return out, err
}

// GateSnapshot implements [gate.Store]: the anchors, the blocks and the switch in one transaction.
func (s *Store) GateSnapshot(ctx context.Context) (*gate.Snapshot, error) {
	var out *gate.Snapshot
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		st, err := qs.SignatureGateSettings(ctx)
		if err != nil {
			return err
		}
		as, err := qs.SignatureGateAnchors(ctx)
		if err != nil {
			return err
		}
		bs, err := qs.SignatureGateBlocks(ctx)
		if err != nil {
			return err
		}
		snap := &gate.Snapshot{Anchors: make(map[int64]gate.Anchor, len(as)), ProjectAnchors: st.ProjectAnchors, Epoch: st.AnchorEpoch}
		for _, a := range as {
			snap.Anchors[a.ID] = gate.Anchor{
				ID: a.ID, Scope: a.Scope, ProjectID: a.ProjectID, Key: a.Key, Enabled: a.Enabled, NarrowedEpoch: a.NarrowedEpoch,
			}
		}
		for _, b := range bs {
			snap.Blocks = append(snap.Blocks, gate.Block{ID: b.ID, Kind: b.Kind, Value: b.Value, Issuer: b.Issuer, Epoch: b.AnchorEpoch})
		}
		out = snap
		return nil
	})
	return out, err
}

// GatePrioritize implements [gate.Store] (R-SIG-067): the subjects' pending signatures are due at priority 0 now, the waiting jobs of
// the due fetches run first, and the job signature-verify is woken when a signature was made due.
func (s *Store) GatePrioritize(ctx context.Context, subjects [][32]byte, due []gate.Fetch) error {
	return s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		n, err := qs.SignatureGatePrioritize(ctx, digestsOf(subjects))
		if err != nil {
			return err
		}
		if n > 0 && s.o.Wake != nil {
			if err := s.o.Wake(ctx, tx); err != nil {
				return err
			}
		}
		if len(due) == 0 {
			return nil
		}
		keys := make([]string, 0, len(due))
		for _, f := range due {
			keys = append(keys, fetch.Key(f.RepoID, f.Target))
		}
		_, err = qs.SignatureGateRaiseFetch(ctx, keys)
		return err
	})
}

// RecordRefusal implements [gate.Store].
func (s *Store) RecordRefusal(ctx context.Context, repoID int64, subject [32]byte, path string, reason gate.Reason) error {
	if repoID <= 0 || path == "" {
		return nil
	}
	if len(path) > 2048 {
		path = path[:2048]
	}
	return s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		return qs.SignatureRefusalRecord(ctx, q.SignatureRefusalRecordParams{RepoID: repoID, Subject: subject[:], Path: path, Reason: string(reason)})
	})
}

// ExpireRefusals removes the refused downloads whose last refusal is older than the retention (R-SIG-073); discovery calls it.
func (s *Store) ExpireRefusals(ctx context.Context, tx meta.Tx, retention time.Duration) (int64, error) {
	qs, err := queries(tx)
	if err != nil {
		return 0, err
	}
	return qs.SignatureRefusalsExpire(ctx, retention.Seconds())
}
