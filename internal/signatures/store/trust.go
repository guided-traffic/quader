package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/shipped"
	"github.com/guided-traffic/quader/internal/signatures/trust"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// The reads and writes of the trust step (R-SIG-008, R-SIG-012).

// ScopedAnchor is an enabled anchor parsed, with the project it belongs to ("" for the installation's scopes). Anchor.Parsed is nil
// when the stored material does not parse any more: the trust step skips the anchor (and the job logs it).
type ScopedAnchor struct {
	Anchor  anchors.Anchor
	Project string
	// Err is why the material did not parse.
	Err error
}

// TrustMaterial is what the job builds its scopes from once per run (R-SIG-008, R-SIG-016, R-SIG-017): the anchor epoch and the switch
// it read them under, every enabled anchor (the project anchors only while the switch is on), the blocks, and the public Sigstore root in
// use (the stored trusted_root.json when the task stored one newer than the release's snapshot, else the snapshot's).
type TrustMaterial struct {
	Epoch          int64
	ProjectAnchors bool
	Anchors        []ScopedAnchor
	Blocks         []trust.Block
	PublicRoot     []byte
}

// TrustMaterial reads the material of the scopes in one transaction. The shipped anchor npmjs takes its keys from the stored
// registry.npmjs.org/keys.json when the task stored one newer than the release's snapshot (R-SIG-017).
func (s *Store) TrustMaterial(ctx context.Context) (TrustMaterial, error) {
	var out TrustMaterial
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		st, err := qs.SignatureEpoch(ctx)
		if err != nil {
			return err
		}
		out.Epoch, out.ProjectAnchors = st.AnchorEpoch, st.ProjectAnchors
		roots, err := trustRoots(ctx, qs)
		if err != nil {
			return err
		}
		rel := shipped.Release()
		out.PublicRoot = rel.TrustedRoot
		if r, ok := roots[shipped.TargetTrustedRoot]; ok && r.TUFVersion > rel.TUFVersion {
			out.PublicRoot = r.Document
		}
		var npm *anchors.PublicKeyMaterial
		if r, ok := roots[shipped.TargetNPMKeys]; ok && r.TUFVersion > rel.TUFVersion {
			if m, err := shipped.NPMKeys(r.Document); err == nil {
				npm = m
			}
		}
		rows, err := qs.SignatureAnchorsEnabled(ctx)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if r.Scope == anchors.ScopeProject && !st.ProjectAnchors {
				continue
			}
			m := storedMaterial(r.Material)
			if r.Scope == anchors.ScopeShipped && r.Key == shipped.KeyNPMJS && npm != nil {
				m = anchors.Material{PublicKey: npm}
			}
			a := ScopedAnchor{
				Anchor:  anchors.Anchor{ID: r.ID, Scope: r.Scope, ProjectID: r.ProjectID, Key: r.Key, Title: r.Title, Kind: r.Kind, Enabled: true, Material: m},
				Project: r.ProjectID,
			}
			a.Anchor.Parsed, a.Err = anchors.Parse(r.Kind, m)
			if a.Err != nil {
				a.Anchor.Parsed = nil
			}
			out.Anchors = append(out.Anchors, a)
		}
		blocks, err := qs.SignatureBlocks(ctx, []int64{})
		if err != nil {
			return err
		}
		for _, b := range blocks {
			out.Blocks = append(out.Blocks, trust.Block{ID: b.ID, Kind: b.Kind, Value: b.Value, Issuer: b.Issuer})
		}
		return nil
	})
	return out, err
}

// TrustBatch reads, in tx, the next signatures in id order after afterID whose state the trust step judges (valid, no_key) and whose
// installation row is missing or computed under an older epoch than epoch, at most limit (R-SIG-012).
func (s *Store) TrustBatch(ctx context.Context, tx meta.Tx, afterID, epoch int64, limit int) ([]trust.Signature, error) {
	qs, err := queries(tx)
	if err != nil {
		return nil, err
	}
	rows, err := qs.SignatureTrustBatch(ctx, q.SignatureTrustBatchParams{AfterID: afterID, Epoch: epoch, RowLimit: int32(min(max(limit, 1), 10000))}) //nolint:gosec // bounded
	if err != nil {
		return nil, err
	}
	out := make([]trust.Signature, 0, len(rows))
	for _, r := range rows {
		out = append(out, trustSignature(q.SignatureTrustOfRow(r)))
	}
	return out, nil
}

// TrustOf reads, in tx, the stored fields the judgement reads of the signatures named, in id order.
func (s *Store) TrustOf(ctx context.Context, tx meta.Tx, ids []int64) ([]trust.Signature, error) {
	qs, err := queries(tx)
	if err != nil {
		return nil, err
	}
	rows, err := qs.SignatureTrustOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]trust.Signature, 0, len(rows))
	for _, r := range rows {
		out = append(out, trustSignature(r))
	}
	return out, nil
}

// trustSignature is a row as the judgement reads it; evidence or a signer that does not read is none.
func trustSignature(r q.SignatureTrustOfRow) trust.Signature {
	sig := trust.Signature{
		ID: r.ID, Scheme: r.Scheme, State: signatures.State(r.State), KeyIDs: r.KeyIds, SignedDigest: r.SignedDigest, DigestAlg: r.DigestAlg,
		ReceivedAt: timeOf(r.ReceivedAt.Valid, r.ReceivedAt.Time), ProvenAt: timeOf(r.ProvenAt.Valid, r.ProvenAt.Time), TimeProof: r.TimeProof,
	}
	if len(r.Evidence) > 0 {
		var ev scheme.Evidence
		if json.Unmarshal(r.Evidence, &ev) == nil {
			sig.Evidence = &ev
		}
	}
	if len(r.Signer) > 0 && string(r.Signer) != "null" {
		var sg signatures.Signer
		if json.Unmarshal(r.Signer, &sg) == nil {
			sig.Signer = &sg
		}
	}
	return sig
}

// ReachingProjects answers, in tx, which of the projects reach each signature (R-SIG-008, Q-SIG-26): an attachment in one of their
// repositories, or in a repository of the project upstream that one of their virtual repositories lists.
func (s *Store) ReachingProjects(ctx context.Context, tx meta.Tx, signatureIDs []int64, projects []string) (map[int64][]string, error) {
	out := map[int64][]string{}
	if len(signatureIDs) == 0 || len(projects) == 0 {
		return out, nil
	}
	qs, err := queries(tx)
	if err != nil {
		return nil, err
	}
	rows, err := qs.SignatureReachingProjects(ctx, q.SignatureReachingProjectsParams{
		SignatureIds: signatureIDs, ProjectIds: projects, UpstreamKey: wellknown.ProjectKey,
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.SignatureID] = append(out[r.SignatureID], r.ProjectID)
	}
	return out, nil
}

// TrustRow is the verdict of one signature in one scope (R-SIG-008): Project "" for the installation's.
type TrustRow struct {
	SignatureID int64
	Project     string
	State       string
	AnchorIDs   []int64
	BlockID     int64
}

// WriteTrust writes, in tx, the trust rows of the signatures named under epoch and removes their project rows the batch does not write
// again (a project that no longer reaches a signature, or whose anchors no longer apply).
func (s *Store) WriteTrust(ctx context.Context, tx meta.Tx, epoch int64, signatureIDs []int64, rows []TrustRow) error {
	if len(signatureIDs) == 0 {
		return nil
	}
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	up := q.SignatureTrustUpsertParams{Epoch: epoch}
	keep := q.SignatureTrustPruneParams{SignatureIds: signatureIDs, KeepSignatureIds: []int64{}, KeepProjectIds: []string{}}
	for _, r := range rows {
		up.SignatureIds, up.ProjectIds = append(up.SignatureIds, r.SignatureID), append(up.ProjectIds, r.Project)
		up.States, up.AnchorIds = append(up.States, r.State), append(up.AnchorIds, int64Array(r.AnchorIDs))
		up.BlockIds = append(up.BlockIds, r.BlockID)
		if r.Project != "" {
			keep.KeepSignatureIds, keep.KeepProjectIds = append(keep.KeepSignatureIds, r.SignatureID), append(keep.KeepProjectIds, r.Project)
		}
	}
	if len(rows) > 0 {
		if err := qs.SignatureTrustUpsert(ctx, up); err != nil {
			return fmt.Errorf("write the trust rows: %w", err)
		}
	}
	if _, err := qs.SignatureTrustPrune(ctx, keep); err != nil {
		return fmt.Errorf("remove the trust rows of projects that no longer reach: %w", err)
	}
	return nil
}

// DeleteTrust removes, in tx, every trust row of the signatures named (an integrity verdict that has no trust step).
func (s *Store) DeleteTrust(ctx context.Context, tx meta.Tx, signatureIDs []int64) error {
	if len(signatureIDs) == 0 {
		return nil
	}
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	_, err = qs.SignatureTrustDelete(ctx, signatureIDs)
	return err
}

// SetIntegrity writes, in tx, the integrity verdict an anchor's key decided for a no_key signature (R-SIG-012): valid, or invalid with
// the reason. It reports whether the row was no_key and changed.
func (s *Store) SetIntegrity(ctx context.Context, tx meta.Tx, id int64, state signatures.State, reason string) (bool, error) {
	qs, err := queries(tx)
	if err != nil {
		return false, err
	}
	n, err := qs.SignatureSetIntegrity(ctx, q.SignatureSetIntegrityParams{ID: id, State: string(state), Reason: reason})
	return n > 0, err
}

// DropOtherTrust removes, in its own transaction, at most limit trust rows of signatures in a state without a trust step, and answers
// how many (R-SIG-008: anchors never repair the bytes).
func (s *Store) DropOtherTrust(ctx context.Context, limit int) (int64, error) {
	var n int64
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) (err error) {
		n, err = qs.SignatureTrustDropOther(ctx, int32(min(max(limit, 1), 10000))) //nolint:gosec // bounded
		return err
	})
	return n, err
}

// TrustPending reports whether the trust step has work: a valid or no_key signature without an installation row at the current epoch
// (after a restore every one, R-SIG-041), or a trust row of a signature in another state.
func (s *Store) TrustPending(ctx context.Context) (bool, error) {
	var pending bool
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) (err error) {
		pending, err = qs.SignatureTrustBacklog(ctx)
		return err
	})
	return pending, err
}

// StoredRoot is a TUF target the task stored (R-SIG-017).
type StoredRoot struct {
	Document   []byte
	TUFVersion int64
	FetchedAt  time.Time
}

func trustRoots(ctx context.Context, qs *q.Queries) (map[string]StoredRoot, error) {
	rows, err := qs.SignatureTrustRoots(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]StoredRoot, len(rows))
	for _, r := range rows {
		out[r.Name] = StoredRoot{Document: r.Document, TUFVersion: r.TufVersion, FetchedAt: timeOf(r.FetchedAt.Valid, r.FetchedAt.Time)}
	}
	return out, nil
}
