package v1

import (
	"context"
	"net/http"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/secrets"
)

// The status of the stored-secret key ring (R-API-049, R-ARCH-021). The report itself is internal/secrets (KeyRing.BuildReport, shared
// with `quader admin secrets status`); this file reads its inputs (the canaries, the replicas' rings, the last re-encryption) and
// renders it.

// KindSecretsReencrypt is the job kind and task name of the re-encryption
// (R-ARCH-021, R-API-049).
const KindSecretsReencrypt = "secrets-reencrypt" //nolint:gosec // a job kind, not a credential

// keyRingPrecondition builds the precondition of the secrets-reencrypt trigger:
// 409 key_ring_incompatible while the rings are not compatible (R-OPS-019).
func (s *Server) keyRingPrecondition(ctx context.Context) error {
	rep, err := s.keyRingReport(ctx)
	if err != nil {
		return err
	}
	if !rep.RingsCompatible {
		return errs.E(errs.Conflict, errs.CodeKeyRingIncompatible,
			"A live replica's key ring lacks a key that is active on another replica or referenced by a sealed value; roll the key out everywhere first.")
	}
	return nil
}

// keyRingReport is the answering replica's report: its ring, the sealed counts, the canaries and the rings of the registry.
func (s *Server) keyRingReport(ctx context.Context) (secrets.Report, error) {
	if s.o.API.KeyRing == nil || s.o.API.Sealed == nil || s.o.API.Rings == nil || s.o.API.Store == nil {
		return secrets.Report{}, unavailable("The key-ring status")
	}
	canaries := map[string]string{}
	err := s.o.API.Store.Read(ctx, func(db q.DBTX) error {
		rows, err := q.New(db).ListSecretKeyCanaries(ctx)
		for _, row := range rows {
			canaries[row.KeyID] = row.Ciphertext
		}
		return err
	})
	if err != nil {
		return secrets.Report{}, err
	}
	rings, err := s.o.API.Rings.Rings(ctx)
	if err != nil {
		return secrets.Report{}, err
	}
	return s.o.API.KeyRing.BuildReport(ctx, s.o.API.Sealed, canaries, rings)
}

// GetEncryptionStatus implements GET /api/v1/system/encryption (R-API-049).
func (s *Server) GetEncryptionStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rep, err := s.keyRingReport(ctx)
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	var last *gen.LastReencryption
	err = s.o.API.Store.Read(ctx, func(db q.DBTX) error {
		runs, err := q.New(db).LastJobPerKind(ctx, []string{KindSecretsReencrypt})
		if err != nil || len(runs) == 0 {
			return err
		}
		l := gen.LastReencryption{JobId: parseUUID(runs[0].ID.String()), State: gen.JobState(runs[0].State)}
		if runs[0].FinishedAt.Valid {
			t := runs[0].FinishedAt.Time.UTC()
			l.FinishedAt = &t
		}
		last = &l
		return nil
	})
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	out := gen.EncryptionStatus{
		AnsweredBy: s.o.Identity.Replica, ActiveKeyId: rep.ActiveKeyID, Keys: make([]gen.EncryptionKey, 0, len(rep.Keys)),
		Replicas: make([]gen.EncryptionReplica, 0, len(rep.Replicas)), RingsCompatible: rep.RingsCompatible, LastReencryption: last,
	}
	for _, k := range rep.Keys {
		out.Keys = append(out.Keys, gen.EncryptionKey{Id: k.ID, Active: k.Active, SealedValues: k.SealedValues, Canary: gen.EncryptionKeyCanary(k.Canary)})
	}
	for _, rr := range rep.Replicas {
		out.Replicas = append(out.Replicas, gen.EncryptionReplica{
			Name: rr.Name, Role: gen.EncryptionReplicaRole(rr.Role), ActiveKeyId: rr.ActiveKeyID, KeyIds: rr.KeyIDs,
		})
	}
	writeJSON(w, r, http.StatusOK, out)
}
