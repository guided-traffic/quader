package store

import (
	"context"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signatures/shipped"
)

// The writes of the task signature-trust-root (R-SIG-017, R-SIG-039).

// TrustRoots answers the TUF targets the task stored, by name (shipped.TargetTrustedRoot, shipped.TargetNPMKeys).
func (s *Store) TrustRoots(ctx context.Context) (map[string]StoredRoot, error) {
	var out map[string]StoredRoot
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) (err error) {
		out, err = trustRoots(ctx, qs)
		return err
	})
	return out, err
}

// StoreTrustRoot stores the two targets of a refresh when its TUF version is newer than the stored one and the release's (R-SIG-017),
// in one transaction: the documents, the last failure cleared, the anchor epoch raised, the change area announced, the trust job woken
// and signature.trust_root.updated recorded. A refresh that is not newer clears the last failure only. It reports whether it stored.
func (s *Store) StoreTrustRoot(ctx context.Context, r shipped.Refreshed) (bool, error) {
	stored := false
	err := s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		if err := qs.SignatureTrustRootSucceeded(ctx); err != nil {
			return err
		}
		have, err := trustRoots(ctx, qs)
		if err != nil {
			return err
		}
		newest := shipped.Release().TUFVersion
		for _, name := range []string{shipped.TargetTrustedRoot, shipped.TargetNPMKeys} {
			newest = max(newest, have[name].TUFVersion)
		}
		if r.TUFVersion <= newest {
			return nil
		}
		for name, doc := range map[string][]byte{shipped.TargetTrustedRoot: r.TrustedRoot, shipped.TargetNPMKeys: r.NPMKeys} {
			if err := qs.SignatureStoreTrustRoot(ctx, q.SignatureStoreTrustRootParams{Name: name, Document: doc, TufVersionNumber: r.TUFVersion}); err != nil {
				return err
			}
		}
		if err := s.o.Audit.RecordTx(ctx, tx, audit.Event{
			Type: audit.EventSignatureTrustRootUpdated, Actor: SystemActor(), Outcome: audit.Success,
			Details: details(map[string]any{
				"tufVersion": r.TUFVersion, "targets": []string{shipped.TargetTrustedRoot, shipped.TargetNPMKeys},
			}),
		}); err != nil {
			return err
		}
		stored = true
		_, err = s.changed(ctx, qs, tx)
		return err
	})
	return stored && err == nil, err
}

// TrustRootFailed keeps a failure of the task (the stored documents stay) and records signature.trust_root.failed at most once a day
// for the installation (R-SIG-039, mode A: the event goes through the bounded buffer after the commit).
func (s *Store) TrustRootFailed(ctx context.Context, code, detail string) error {
	report := false
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) (err error) {
		report, err = qs.SignatureTrustRootFailed(ctx, detail)
		return err
	})
	if err != nil {
		return err
	}
	if report {
		s.o.Audit.Record(ctx, audit.Event{
			Type: audit.EventSignatureTrustRootFailed, Actor: SystemActor(), Outcome: audit.Failure, Reason: code,
			Details: details(map[string]string{"code": code, "detail": detail}),
		})
	}
	return nil
}
