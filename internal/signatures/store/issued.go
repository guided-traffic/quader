package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signatures"
)

// SourceIssued is the source of the attachment of a signature Quader made itself (R-SIG-054): written and removed by the job signing
// alone, never replaced by a commit's or a fill's batch.
const SourceIssued = "issued"

// Issued is a signature Quader made itself with its signing identity (stage 2, R-SIG-054): its original, what it covers, and the item
// it is attached to in that item's repository.
type Issued struct {
	// Scheme is the object's scheme (npm-registry), Role the signature's (registry).
	Scheme string
	Role   string
	// Raw is the original: npm's dist.signatures entry. MediaType is its media type.
	Raw       []byte
	MediaType string
	// Subject is what the signature covers; for npm the SHA-256 of the signed message <name>@<version>:<integrity>, with SubjectName
	// <name>@<version> and SignedDigest the message's SHA-256, as stage 1 records an upstream's registry signature (Q-SIG-50).
	Subject      [32]byte
	SubjectName  string
	SignedDigest []byte
	DigestAlg    string
	// KeyIDs are the identifiers the signature is known by: npm's keyid.
	KeyIDs []string
	// IdentityID is the identity that made it.
	IdentityID int64
	// RepoID, ItemID and Location are the attachment: the signed item, at its path; Name the package it is under.
	RepoID   int64
	ItemID   int64
	Location string
	Name     string
	// PredicateType, Signer and ClaimedAt are an attestation's (R-SIG-051): its predicate type, the identity its certificate names, and
	// the moment its statement claims; empty for npm's registry signature.
	PredicateType string
	Signer        *signatures.Signer
	ClaimedAt     time.Time
	// Replace removes, in the same transaction, the attachments of the item's other signatures of the scheme Quader made: an
	// attestation made again replaces the earlier one whichever identity made it (R-SIG-053).
	Replace bool
}

// RecordIssued writes a signature Quader made in the caller's transaction (R-SIG-054): the object once per its SHA-256, the signature
// row valid and never due (Quader checks no signature it made, as for Debian's indexes, Q-SIG-31), with identity_id, and the attachment
// issued to the item. It returns the signature's id.
func (s *Store) RecordIssued(ctx context.Context, tx meta.Tx, in Issued) (int64, error) {
	switch {
	case len(in.Raw) == 0 || len(in.Raw) > signatures.MaxRaw:
		return 0, errors.New("signatures: an issued signature's original is empty or above the bound")
	case in.IdentityID == 0 || in.ItemID == 0 || in.RepoID == 0 || in.Location == "":
		return 0, errors.New("signatures: an issued signature names its identity, its item, its repository and its location")
	case !signatures.RoleValid(in.Role):
		return 0, fmt.Errorf("signatures: role %q", in.Role)
	}
	qs, err := queries(tx)
	if err != nil {
		return 0, err
	}
	obj := sha256.Sum256(in.Raw)
	if err := qs.SignatureInsertIssuedObject(ctx, q.SignatureInsertIssuedObjectParams{
		Sha256: obj[:], Scheme: in.Scheme, MediaType: in.MediaType, Raw: in.Raw,
	}); err != nil {
		return 0, fmt.Errorf("record an issued signature's object: %w", err)
	}
	keys := in.KeyIDs
	if keys == nil {
		keys = []string{}
	}
	var signer []byte
	if in.Signer != nil {
		if signer, err = json.Marshal(in.Signer); err != nil {
			return 0, err
		}
	}
	claimed := ""
	if !in.ClaimedAt.IsZero() {
		claimed = in.ClaimedAt.UTC().Format(time.RFC3339Nano)
	}
	id, err := qs.SignatureInsertIssued(ctx, q.SignatureInsertIssuedParams{
		ObjectSha256: obj[:], SubjectSha256: in.Subject[:], SubjectName: in.SubjectName, Role: in.Role, PredicateType: in.PredicateType,
		SignedDigest: in.SignedDigest, DigestAlg: in.DigestAlg, KeyIds: keys, IdentityID: in.IdentityID, Signer: signer, ClaimedAt: claimed,
	})
	if err != nil {
		return 0, fmt.Errorf("record an issued signature: %w", err)
	}
	if err := qs.SignatureAttachIssued(ctx, q.SignatureAttachIssuedParams{
		RepoID: in.RepoID, SignatureID: id, Location: in.Location, ItemID: in.ItemID, Name: in.Name,
	}); err != nil {
		return 0, fmt.Errorf("attach an issued signature: %w", err)
	}
	if in.Replace {
		if _, err := qs.SignatureDetachIssuedExcept(ctx, q.SignatureDetachIssuedExceptParams{ItemID: in.ItemID, Keep: id, Scheme: in.Scheme}); err != nil {
			return 0, fmt.Errorf("detach an item's earlier issued signatures: %w", err)
		}
	}
	return id, nil
}

// DetachRetiredIssued removes, in the caller's transaction, the attachments of the item's issued signatures of the scheme whose
// identity is retired (R-SIG-048): the signature the current identity makes again replaces them. The job signature-verify removes a
// signature that lost its last attachment (R-SIG-011). It answers how many it removed.
func (s *Store) DetachRetiredIssued(ctx context.Context, tx meta.Tx, itemID int64, scheme string) (int64, error) {
	qs, err := queries(tx)
	if err != nil {
		return 0, err
	}
	return qs.SignatureDetachRetiredIssued(ctx, q.SignatureDetachRetiredIssuedParams{ItemID: itemID, Scheme: scheme})
}
