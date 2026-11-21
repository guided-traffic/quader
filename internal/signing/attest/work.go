package attest

import (
	"context"
	"sync"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signing"
)

// Discover is the discovery step of the kind attestation (R-SIG-049, R-SIG-053): among the live files after the cursor that lack an
// attestation of the current identity (stored before an identity existed, loaded by a restore, left by a rotation) or whose referrer was
// deleted, it asks each repository's format which are subjects and writes their work at the priority of discovery. It answers the last
// item it looked at; a step that finds none ends the pass.
func (a *Attester) Discover(ctx context.Context, cur signing.Identity, after int64, limit int) (int64, error) {
	if !a.o.Enabled {
		return after, nil
	}
	var rows []q.SigningDiscoverAttestationsRow
	if err := a.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err = qs.SigningDiscoverAttestations(ctx, q.SigningDiscoverAttestationsParams{AfterID: after, IdentityID: cur.ID, RowLimit: int32(min(limit, 10000))}) //nolint:gosec // bounded
		return err
	}); err != nil || len(rows) == 0 {
		return after, err
	}
	byRepo := map[int64][]int{}
	var order []int64
	for i, r := range rows {
		if !a.o.Fills && r.Class == "remote" {
			continue
		}
		if _, ok := byRepo[r.RepoID]; !ok {
			order = append(order, r.RepoID)
		}
		byRepo[r.RepoID] = append(byRepo[r.RepoID], i)
	}
	reasons := map[string][]int64{}
	for _, repoID := range order {
		at := byRepo[repoID]
		first := rows[at[0]]
		repo := Repo{ID: repoID, Project: first.ProjectKey, Key: first.RepoKey, Format: first.Format, Class: first.Class}
		its := make([]Item, len(at))
		for n, i := range at {
			its[n] = Item{Path: rows[i].Path, Blob: [32]byte(rows[i].BlobSha256)}
		}
		subs, err := a.o.Formats.Subjects(ctx, repo, its)
		if err != nil {
			return after, err
		}
		for n, i := range at {
			if n >= len(subs) || !subs[n].Attested {
				continue
			}
			reason := signing.ReasonDiscovery
			if rows[i].Attested {
				reason = signing.ReasonRotation
			}
			reasons[reason] = append(reasons[reason], rows[i].ID)
		}
	}
	if err := a.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		for _, reason := range []string{signing.ReasonDiscovery, signing.ReasonRotation} {
			if len(reasons[reason]) == 0 {
				continue
			}
			if _, err := qs.SigningEnqueueWork(ctx, q.SigningEnqueueWorkParams{
				Kind: signing.WorkAttestation, Reason: reason, Priority: signing.PriorityDiscovery, ItemIds: reasons[reason],
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return after, err
	}
	return rows[len(rows)-1].ID, nil
}

// Hooks is what the storage hooks and the malware scanning tell the job signing (R-SIG-053, R-SIG-054), in the transaction of the change:
// a committed file of a format that attests something becomes work of the kind attestation (priority 1 for an upload, 2 for a fill), an
// older cosign signature tag takes the referrers of its subjects' attestations (served through the API from then on), and a malware
// verdict that changed makes the attestations that name its blob again. A file of a system repository of upstream is no work (R-SIG-044).
type Hooks struct {
	enabled, fills bool
	wake           func(ctx context.Context, tx meta.Tx) error
	// system caches whether a repository is a system repository of upstream: a repository's kind never changes.
	system sync.Map
}

// NewHooks returns the hooks over signing.enabled with signing.attestations.enabled, and .fills; wake enqueues the job signing in a
// transaction.
func NewHooks(enabled, fills bool, wake func(ctx context.Context, tx meta.Tx) error) *Hooks {
	return &Hooks{enabled: enabled, fills: fills, wake: wake}
}

// Committed implements formathost.AttestationWork.
func (h *Hooks) Committed(ctx context.Context, tx meta.Tx, repoID, itemID int64, fill bool) error {
	if h == nil || !h.enabled || (fill && !h.fills) {
		return nil
	}
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	if sys, err := h.isSystem(ctx, qs, repoID); err != nil || sys {
		return err
	}
	reason, priority := signing.ReasonUpload, signing.PriorityUpload
	if fill {
		reason, priority = signing.ReasonFill, signing.PriorityFill
	}
	// Written only while an identity is current, as npm's work of a publish is (Q-SIG-158): what a repository receives before the
	// installation's first identity is left to discovery, so an installation that never signs keeps no rows.
	n, err := qs.SigningEnqueueWorkWhileCurrent(ctx, q.SigningEnqueueWorkWhileCurrentParams{
		Kind: signing.WorkAttestation, Reason: reason, Priority: int16(priority), ItemIds: []int64{itemID}, //nolint:gosec // 1 or 2
	})
	if err != nil || n == 0 {
		return err
	}
	return h.wakeIn(ctx, tx)
}

// Unplaced is an attestation's referrer that an older cosign signature tag displaced: the attested item, the referrer manifest's digest
// and the bundle's, which name the referrer's paths.
type Unplaced struct {
	Path     string
	Blob     [32]byte
	Manifest [32]byte
	Bundle   [32]byte
}

// SignatureTagArrived is an older cosign signature tag stored for subjects of a repository (R-SIG-054, Q-SIG-66), in the transaction of the
// tag's commit: the attestations of those subjects that stand as referrers are served through the API from now on, and their referrers
// are answered for the caller to remove in the same transaction. It needs no identity, so a referrer never stays beside an older
// signature while no identity is current.
func (h *Hooks) SignatureTagArrived(ctx context.Context, tx meta.Tx, repoID int64, subjects [][32]byte) ([]Unplaced, error) {
	if h == nil || len(subjects) == 0 {
		return nil, nil
	}
	qs, err := queries(tx)
	if err != nil {
		return nil, err
	}
	rows, err := qs.SigningUnplaceReferrers(ctx, q.SigningUnplaceReferrersParams{RepoID: repoID, Subjects: byteSlices(subjects)})
	if err != nil {
		return nil, err
	}
	out := make([]Unplaced, 0, len(rows))
	for _, r := range rows {
		if len(r.Referrer) != 32 || len(r.BlobSha256) != 32 || len(r.ObjectSha256) != 32 {
			continue
		}
		out = append(out, Unplaced{Path: r.Path, Blob: [32]byte(r.BlobSha256), Manifest: [32]byte(r.Referrer), Bundle: [32]byte(r.ObjectSha256)})
	}
	return out, nil
}

// VerdictChanged is the malware scanning's word that a blob's verdict or its release changed, in the transaction that wrote it.
func (h *Hooks) VerdictChanged(ctx context.Context, tx meta.Tx, sha256 [32]byte) error {
	if h == nil || !h.enabled {
		return nil
	}
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	n, err := qs.SigningWakeVerdict(ctx, sha256[:])
	if err != nil || n == 0 {
		return err
	}
	return h.wakeIn(ctx, tx)
}

func (h *Hooks) isSystem(ctx context.Context, qs *q.Queries, repoID int64) (bool, error) {
	if v, ok := h.system.Load(repoID); ok {
		return v.(bool), nil
	}
	scannable, err := qs.MalwareRepoScannable(ctx, repoID)
	if err != nil {
		return false, err
	}
	h.system.Store(repoID, !scannable.Bool)
	return !scannable.Bool, nil
}

func (h *Hooks) wakeIn(ctx context.Context, tx meta.Tx) error {
	if h.wake == nil {
		return nil
	}
	return h.wake(ctx, tx)
}
