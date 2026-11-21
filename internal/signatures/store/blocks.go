package store

import (
	"context"
	"regexp"
	"strings"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
	"github.com/guided-traffic/quader/internal/signatures/trust"
)

// BlockEntry is the journal entry of signature.signer.blocked (Q-SIG-42): the replay blocks the signer again.
type BlockEntry struct {
	Kind   string `json:"kind"`
	Value  string `json:"value"`
	Issuer string `json:"issuer,omitempty"`
	Reason string `json:"reason"`
}

// Blocks lists the blocks of R-SIG-020, newest first, with the signatures each blocks in the installation's scope.
func (s *Store) Blocks(ctx context.Context) ([]signatures.BlockRecord, error) {
	var out []signatures.BlockRecord
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		var err error
		out, err = blockRecords(ctx, qs, []int64{})
		return err
	})
	return out, err
}

func blockRecords(ctx context.Context, qs *q.Queries, ids []int64) ([]signatures.BlockRecord, error) {
	rows, err := qs.SignatureBlocks(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]signatures.BlockRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, signatures.BlockRecord{
			ID: r.ID, Kind: r.Kind, Value: r.Value, Issuer: r.Issuer, Reason: r.Reason,
			CreatedAt: timeOf(r.CreatedAt.Valid, r.CreatedAt.Time), CreatedBy: r.CreatedByName, Signatures: r.Signatures,
		})
	}
	return out, nil
}

// checkBlock refuses a block whose members are out of their bounds (R-SIG-020): the kind, a value of 1 to 1024 characters, an
// identity's pattern that compiles as RE2 (it is matched anchored), an issuer only for an identity and of at most 512 characters, and a
// reason of 1 to 1000 characters.
func checkBlock(in signatures.BlockInput) error {
	var fe fieldErrors
	switch in.Kind {
	case trust.BlockFingerprint, trust.BlockSerial, trust.BlockKeyID, trust.BlockIdentity:
	default:
		fe.add("/kind", anchors.CodeInvalidValue, "The kind is one of fingerprint, serial, keyid and identity.")
	}
	if chars(in.Value) < 1 || chars(in.Value) > maxBlockValue || strings.TrimSpace(in.Value) == "" {
		fe.add("/value", anchors.CodeInvalidValue, "The value is 1 to 1024 characters.")
	} else if in.Kind == trust.BlockIdentity {
		if _, err := regexp.Compile(`^(?:` + in.Value + `)$`); err != nil {
			fe.add("/value", anchors.CodeInvalidValue, "The identity pattern does not compile as a regular expression: "+err.Error())
		}
	}
	switch {
	case in.Issuer != "" && in.Kind != trust.BlockIdentity:
		fe.add("/issuer", anchors.CodeInvalidValue, "Only a block of an identity names an issuer.")
	case chars(in.Issuer) > maxIssuer:
		fe.add("/issuer", anchors.CodeInvalidValue, "The issuer is at most 512 characters.")
	}
	if chars(in.Reason) < 1 || chars(in.Reason) > maxReason || strings.TrimSpace(in.Reason) == "" {
		fe.add("/reason", anchors.CodeInvalidValue, "The reason is 1 to 1000 characters.")
	}
	return fe.err()
}

// CreateBlock blocks a signer (R-SIG-020): Invalid for a member out of its bounds, Conflict (block_exists) for a signer blocked
// already. It records signature.signer.blocked with its journal entry, raises the anchor epoch and wakes the trust job, which sets
// blocked on every trust row of a matching signature.
func (s *Store) CreateBlock(ctx context.Context, actor audit.Actor, in signatures.BlockInput) (signatures.BlockRecord, error) {
	if err := checkBlock(in); err != nil {
		return signatures.BlockRecord{}, err
	}
	var out signatures.BlockRecord
	err := s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		ids, err := qs.SignatureBlockInsert(ctx, q.SignatureBlockInsertParams{
			Kind: in.Kind, Value: in.Value, Issuer: in.Issuer, Reason: in.Reason, Actor: principalOf(actor),
		})
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return errs.E(errs.Conflict, errs.CodeBlockExists, "A block of this signer exists.")
		}
		recs, err := blockRecords(ctx, qs, ids)
		if err != nil || len(recs) == 0 {
			return err
		}
		out = recs[0]
		det := map[string]any{"id": out.ID, "kind": in.Kind, "value": in.Value, "reason": in.Reason}
		if in.Issuer != "" {
			det["issuer"] = in.Issuer
		}
		if err := s.o.Audit.RecordTx(ctx, tx, audit.Event{
			Type: audit.EventSignatureSignerBlocked, Actor: actor, Outcome: audit.Success,
			Subject: audit.Subject{Kind: "signature_block", ID: idText(out.ID)}, Details: details(det),
			Journal: details(BlockEntry{Kind: in.Kind, Value: in.Value, Issuer: in.Issuer, Reason: in.Reason}),
		}); err != nil {
			return err
		}
		epoch, err := s.changed(ctx, qs, tx)
		if err != nil {
			return err
		}
		// The gate of stage 3 applies the block to every trust row computed before this epoch (R-SIG-066).
		return qs.SignatureBlockEpoch(ctx, q.SignatureBlockEpochParams{ID: out.ID, Epoch: epoch})
	})
	if err != nil {
		return signatures.BlockRecord{}, err
	}
	return out, nil
}

// DeleteBlock lifts a block (R-SIG-020): NotFound when there is none. It records signature.signer.unblocked, raises the anchor epoch
// and wakes the trust job.
func (s *Store) DeleteBlock(ctx context.Context, actor audit.Actor, id int64) error {
	return s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		rows, err := qs.SignatureBlockDelete(ctx, id)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return errs.E(errs.NotFound, errs.CodeNotFound, "No block has this ID.")
		}
		r := rows[0]
		det := map[string]any{"id": id, "kind": r.Kind, "value": r.Value}
		if r.Issuer != "" {
			det["issuer"] = r.Issuer
		}
		if err := s.o.Audit.RecordTx(ctx, tx, audit.Event{
			Type: audit.EventSignatureSignerUnblocked, Actor: actor, Outcome: audit.Success,
			Subject: audit.Subject{Kind: "signature_block", ID: idText(id)}, Details: details(det),
		}); err != nil {
			return err
		}
		_, err = s.changed(ctx, qs, tx)
		return err
	})
}
