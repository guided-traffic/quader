package store

import (
	"context"
	"encoding/hex"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/gate"
)

// The releases of a repository's gate, the reads of a preview and of an impact count, and the list of refused downloads (stage 3,
// package 2: signatures.md R-SIG-070 to R-SIG-073; queries/signature_releases.sql).

// ReleaseWithdrawnEntry is the journal entry of signature.release.withdrawn (R-SIG-076): the repository by id and name and the artifact
// whose release went. The replay withdraws it again by the id; the name is for the record.
type ReleaseWithdrawnEntry struct {
	RepoID     int64  `json:"repoId"`
	Repository string `json:"repository"`
	SHA256     string `json:"sha256"`
}

// GateReleased implements [gate.Store].
func (s *Store) GateReleased(ctx context.Context, repoID int64, subjects [][32]byte) (map[[32]byte]bool, error) {
	out := map[[32]byte]bool{}
	if repoID <= 0 || len(subjects) == 0 {
		return out, nil
	}
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rs, err := qs.SignatureGateReleased(ctx, q.SignatureGateReleasedParams{RepoID: repoID, Subjects: digestsOf(subjects)})
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

// GateItems implements [gate.Store].
func (s *Store) GateItems(ctx context.Context, repoIDs []int64, after gate.ItemCursor, limit int) ([]gate.StoredItem, error) {
	var out []gate.StoredItem
	if len(repoIDs) == 0 || limit <= 0 {
		return out, nil
	}
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rs, err := qs.SignatureGateItems(ctx, q.SignatureGateItemsParams{
			RepoIds: repoIDs, AfterRepo: after.RepoID, AfterPath: after.Path, MaxRows: int32(min(limit, 1<<20)), //nolint:gosec // bounded above
		})
		if err != nil {
			return err
		}
		out = out[:0]
		for _, r := range rs {
			if len(r.BlobSha256) == 32 {
				out = append(out, gate.StoredItem{ID: r.ID, RepoID: r.RepoID, Path: r.Path, Blob: [32]byte(r.BlobSha256)})
			}
		}
		return nil
	})
	return out, err
}

// GateImpact implements [gate.Store].
func (s *Store) GateImpact(ctx context.Context, iq gate.ImpactQuery, repoIDs []int64, limit int) ([]gate.Candidate, error) {
	var out []gate.Candidate
	if len(repoIDs) == 0 || limit <= 0 {
		return out, nil
	}
	maxRows := int32(min(limit, 1<<24)) //nolint:gosec // bounded above
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		out = out[:0]
		add := func(repo int64, subject []byte, identity, issuer string) {
			if len(subject) == 32 {
				out = append(out, gate.Candidate{RepoID: repo, Subject: [32]byte(subject), Identity: identity, OIDCIssuer: issuer})
			}
		}
		switch {
		case len(iq.AnchorIDs) > 0:
			rs, err := qs.SignatureImpactByAnchors(ctx, q.SignatureImpactByAnchorsParams{AnchorIds: iq.AnchorIDs, RepoIds: repoIDs, MaxRows: maxRows})
			for _, r := range rs {
				add(r.RepoID, r.SubjectSha256, "", "")
			}
			return err
		case len(iq.KeyIDs) > 0:
			rs, err := qs.SignatureImpactByKeys(ctx, q.SignatureImpactByKeysParams{KeyIds: iq.KeyIDs, RepoIds: repoIDs, MaxRows: maxRows})
			for _, r := range rs {
				add(r.RepoID, r.SubjectSha256, "", "")
			}
			return err
		case iq.Identity:
			rs, err := qs.SignatureImpactByIdentity(ctx, q.SignatureImpactByIdentityParams{RepoIds: repoIDs, MaxRows: maxRows})
			for _, r := range rs {
				add(r.RepoID, r.SubjectSha256, r.Identity, r.OidcIssuer)
			}
			return err
		}
		return nil
	})
	return out, err
}

// Releases answers the releases of the repositories, newest first, at most limit; subject narrows to one artifact.
func (s *Store) Releases(ctx context.Context, repoIDs []int64, subject *[32]byte, limit int) ([]signatures.GateRelease, error) {
	var out []signatures.GateRelease
	if len(repoIDs) == 0 || limit <= 0 {
		return out, nil
	}
	var one []byte
	if subject != nil {
		one = subject[:]
	}
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rs, err := qs.SignatureReleases(ctx, q.SignatureReleasesParams{
			RepoIds: repoIDs, Subject: append([]byte{}, one...), MaxRows: int32(min(limit, 1<<20)), //nolint:gosec // bounded above
		})
		if err != nil {
			return err
		}
		out = out[:0]
		for _, r := range rs {
			out = append(out, signatures.GateRelease{
				RepoID: r.RepoID, Subject: digest(r.SubjectSha256), Reason: r.Reason, RefusedReason: r.RefusedReason,
				ReleasedBy: r.ReleasedBy, ReleasedByName: r.ReleasedByName, ReleasedAt: r.ReleasedAt.Time.UTC(),
			})
		}
		return nil
	})
	return out, err
}

// Release grants a release of one artifact in one repository (R-SIG-070): Conflict already_released when one passes it. The caller has
// decided that the gate does not pass the artifact (409 nothing_to_release is the caller's). It records signature.release.granted, which
// widens and is not journaled.
func (s *Store) Release(ctx context.Context, actor audit.Actor, in signatures.GateReleaseInput) (signatures.GateRelease, error) {
	err := s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		n, err := qs.SignatureReleaseInsert(ctx, q.SignatureReleaseInsertParams{
			RepoID: in.RepoID, Subject: in.Subject[:], Reason: in.Reason, RefusedReason: in.RefusedReason, Actor: principalOf(actor),
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return errs.E(errs.Conflict, errs.CodeAlreadyReleased,
				"A release already passes this artifact in this repository; withdraw it first to grant another.")
		}
		return s.o.Audit.RecordTx(ctx, tx, audit.Event{
			Type: audit.EventSignatureReleaseGranted, Actor: actor, Outcome: audit.Success,
			Subject: audit.Subject{Kind: "blob", ID: hex.EncodeToString(in.Subject[:])}, Repository: in.Repository, Project: projectOf(in.Repository),
			Details: details(map[string]string{
				"repository": in.Repository, "sha256": hex.EncodeToString(in.Subject[:]), "refusedReason": in.RefusedReason, "reason": in.Reason,
			}),
		})
	})
	if err != nil {
		return signatures.GateRelease{}, err
	}
	got, err := s.Releases(ctx, []int64{in.RepoID}, &in.Subject, 1)
	if err != nil || len(got) == 0 {
		return signatures.GateRelease{
			RepoID: in.RepoID, Subject: in.Subject, Reason: in.Reason, RefusedReason: in.RefusedReason,
			ReleasedAt: time.Now().UTC(),
		}, err
	}
	return got[0], nil
}

// Withdraw withdraws a release (R-SIG-070): NotFound when none passes the artifact in the repository. It records
// signature.release.withdrawn with its journal entry (R-SIG-076), so a restore keeps it withdrawn and the Bastions drop the repository's
// answers (R-SIG-075).
func (s *Store) Withdraw(ctx context.Context, actor audit.Actor, repoID int64, repository string, subject [32]byte) error {
	return s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		reasons, err := qs.SignatureReleaseDelete(ctx, q.SignatureReleaseDeleteParams{RepoID: repoID, Subject: subject[:]})
		if err != nil {
			return err
		}
		if len(reasons) == 0 {
			return errs.E(errs.NotFound, errs.CodeNotFound, "No release passes this artifact in this repository.")
		}
		sum := hex.EncodeToString(subject[:])
		return s.o.Audit.RecordTx(ctx, tx, audit.Event{
			Type: audit.EventSignatureReleaseWithdrawn, Actor: actor, Outcome: audit.Success,
			Subject: audit.Subject{Kind: "blob", ID: sum}, Repository: repository, Project: projectOf(repository),
			Journal: details(ReleaseWithdrawnEntry{RepoID: repoID, Repository: repository, SHA256: sum}),
			Details: details(map[string]string{"repository": repository, "sha256": sum, "reason": reasons[0]}),
		})
	})
}

// RefusalHolders answers the repositories with a refused download, whose readable ones scope the list (R-SIG-073).
func (s *Store) RefusalHolders(ctx context.Context) ([]signatures.Holder, error) {
	var out []signatures.Holder
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rs, err := qs.SignatureRefusalHolders(ctx)
		if err != nil {
			return err
		}
		out = out[:0]
		for _, r := range rs {
			out = append(out, signatures.Holder{RepoID: r.RepoID, ProjectKey: r.ProjectKey, RepoKey: r.RepoKey, Format: r.Format})
		}
		return nil
	})
	return out, err
}

// Refusals answers one page of refused downloads (Limit+1 rows when more follow).
func (s *Store) Refusals(ctx context.Context, rq signatures.RefusalsQuery) ([]signatures.Refusal, error) {
	var out []signatures.Refusal
	if len(rq.RepoIDs) == 0 {
		return out, nil
	}
	after := ""
	if !rq.AfterAt.IsZero() {
		after = rq.AfterAt.UTC().Format(time.RFC3339Nano)
	}
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rs, err := qs.SignatureRefusals(ctx, q.SignatureRefusalsParams{
			RepoIds: rq.RepoIDs, RepoID: rq.RepoID, Reason: rq.Reason, AfterAt: after, AfterRepo: rq.AfterRepo,
			AfterSubject: rq.AfterSubject[:], MaxRows: int32(min(rq.Limit, 1<<20)), //nolint:gosec // bounded above
		})
		if err != nil {
			return err
		}
		out = out[:0]
		for _, r := range rs {
			f := signatures.Refusal{
				Holder:  signatures.Holder{RepoID: r.RepoID, ProjectKey: r.ProjectKey, RepoKey: r.RepoKey, Format: r.Format, Path: r.Path},
				Subject: digest(r.SubjectSha256), Reason: r.Reason, FirstAt: r.FirstAt.Time.UTC(), LastAt: r.LastAt.Time.UTC(), Count: r.Count,
			}
			if r.Released {
				f.Release = &signatures.GateRelease{
					RepoID: r.RepoID, Subject: f.Subject, Reason: r.ReleaseReason, ReleasedByName: r.ReleasedByName, ReleasedAt: r.ReleasedAt.Time.UTC(),
				}
			}
			out = append(out, f)
		}
		return nil
	})
	return out, err
}

// projectOf is the project key of a qualified repository name.
func projectOf(repository string) string {
	if project, _, ok := strings.Cut(repository, "/"); ok {
		return project
	}
	return ""
}

// BlobItem answers a live stored file of the repositories with the blob: the item a release names (R-SIG-070); ok is false for none.
func (s *Store) BlobItem(ctx context.Context, repoIDs []int64, blob [32]byte) (repoID int64, path string, ok bool, err error) {
	if len(repoIDs) == 0 {
		return 0, "", false, nil
	}
	err = s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rs, err := qs.SignatureGateBlobItem(ctx, q.SignatureGateBlobItemParams{Blob: blob[:], RepoIds: repoIDs})
		if err != nil || len(rs) == 0 {
			return err
		}
		repoID, path, ok = rs[0].RepoID, rs[0].Path, true
		return nil
	})
	return repoID, path, ok, err
}
