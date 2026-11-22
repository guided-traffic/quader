package store

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
)

// Views answers the signatures of the subjects (and npm's subject names) attached in q.RepoIDs (R-SIG-004, R-SIG-034), each with its
// attachments there, its object's scheme, media type and size, and its trust row in the scope: the project's row of q.ProjectID when
// one exists, else the installation's; Stale when the row was computed under an older anchor epoch (R-SIG-012, H-1175).
func (s *Store) Views(ctx context.Context, vq signatures.ViewQuery) ([]signatures.View, error) {
	out := []signatures.View{}
	if len(vq.RepoIDs) == 0 || (len(vq.Subjects) == 0 && len(vq.SubjectNames) == 0) {
		return out, nil
	}
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := viewRows(ctx, qs, vq)
		if err != nil || len(rows) == 0 {
			return err
		}
		ids := make([]int64, 0, len(rows))
		anchorIDs, blockIDs := []int64{}, []int64{}
		for _, r := range rows {
			ids = append(ids, r.ID)
			anchorIDs = append(anchorIDs, r.TrustAnchorIds...)
			if r.TrustBlockID != 0 {
				blockIDs = append(blockIDs, r.TrustBlockID)
			}
		}
		atts, err := qs.SignatureViewAttachments(ctx, q.SignatureViewAttachmentsParams{SignatureIds: ids, RepoIds: vq.RepoIDs})
		if err != nil {
			return err
		}
		bySig := map[int64][]signatures.Attachment{}
		for _, a := range atts {
			bySig[a.SignatureID] = append(bySig[a.SignatureID], signatures.Attachment{
				RepoID: a.RepoID, SignatureID: a.SignatureID, Location: a.Location, ItemID: a.ItemID, Source: a.Source, Name: a.Name,
				CreatedAt: timeOf(a.CreatedAt.Valid, a.CreatedAt.Time),
			})
		}
		refs, blocks, err := trustNames(ctx, qs, anchorIDs, blockIDs)
		if err != nil {
			return err
		}
		for _, r := range rows {
			v := signatures.View{
				Signature: signatureOf(viewSignature(r)), Scheme: r.Scheme, MediaType: r.MediaType, ObjectSize: r.ObjectSize,
				Attachments: bySig[r.ID],
			}
			v.Signature.Subject = digest(r.SubjectSha256)
			if r.HasTrust {
				v.Trust = trustOfRow(r, refs, blocks)
			}
			if r.IdentityID != 0 {
				v.IssuedBy = &signatures.IssuedBy{
					Identity: r.IdentityID, Subject: r.IdentitySubject, Fingerprint: hex.EncodeToString(r.IdentityCertSha256), State: r.IdentityState,
				}
			}
			if r.ServedAs != "" {
				v.Receipt = &signatures.Receipt{Bundle: r.IssuedBundle, ServedAs: r.ServedAs, Reason: r.ServedReason}
			}
			out = append(out, v)
		}
		return nil
	})
	return out, err
}

func viewRows(ctx context.Context, qs *q.Queries, vq signatures.ViewQuery) ([]q.SignatureViewsRow, error) {
	subjects := make([][]byte, 0, len(vq.Subjects))
	for _, d := range vq.Subjects {
		subjects = append(subjects, d[:])
	}
	names := vq.SubjectNames
	if names == nil {
		names = []string{}
	}
	return qs.SignatureViews(ctx, q.SignatureViewsParams{
		RepoIds: vq.RepoIDs, Subjects: subjects, SubjectNames: names, ProjectID: vq.ProjectID,
	})
}

func viewSignature(r q.SignatureViewsRow) q.SignaturesDueRow {
	return q.SignaturesDueRow{
		ID: r.ID, ObjectSha256: r.ObjectSha256, Part: r.Part, SubjectSha256: r.SubjectSha256, SubjectName: r.SubjectName, Role: r.Role,
		PredicateType: r.PredicateType, ContentSha256: r.ContentSha256, ContentLocator: r.ContentLocator, State: r.State, Reason: r.Reason,
		Signer: r.Signer, KeyIds: r.KeyIds, SignedDigest: r.SignedDigest, DigestAlg: r.DigestAlg, ClaimedAt: r.ClaimedAt,
		ProvenAt: r.ProvenAt, TimeProof: r.TimeProof, ReceivedAt: r.ReceivedAt, CheckedAt: r.CheckedAt, Due: r.Due,
		Priority: r.Priority, Attempts: r.Attempts, NotBefore: r.NotBefore,
	}
}

// trustNames reads the titles and scopes of the anchors and the blocks the trust rows name.
func trustNames(ctx context.Context, qs *q.Queries, anchorIDs, blockIDs []int64) (map[int64]signatures.AnchorRef, map[int64]signatures.BlockRecord, error) {
	refs := map[int64]signatures.AnchorRef{}
	blocks := map[int64]signatures.BlockRecord{}
	if len(anchorIDs) > 0 {
		rows, err := qs.SignatureAnchorRefs(ctx, anchorIDs)
		if err != nil {
			return nil, nil, err
		}
		for _, r := range rows {
			refs[r.ID] = signatures.AnchorRef{ID: r.ID, Title: r.Title, Scope: r.Scope}
		}
	}
	if len(blockIDs) > 0 {
		recs, err := blockRecords(ctx, qs, blockIDs)
		if err != nil {
			return nil, nil, err
		}
		for _, b := range recs {
			blocks[b.ID] = b
		}
	}
	return refs, blocks, nil
}

func trustOfRow(r q.SignatureViewsRow, refs map[int64]signatures.AnchorRef, blocks map[int64]signatures.BlockRecord) *signatures.Trust {
	t := &signatures.Trust{
		State: r.TrustState, Epoch: r.TrustEpoch,
		ComputedAt: timeOf(r.TrustComputedAt.Valid, r.TrustComputedAt.Time), Stale: r.TrustEpoch < r.CurrentEpoch,
		Anchors: make([]signatures.AnchorRef, 0, len(r.TrustAnchorIds)),
	}
	t.ProjectID = r.TrustProjectID
	for _, id := range r.TrustAnchorIds {
		if ref, ok := refs[id]; ok {
			t.Anchors = append(t.Anchors, ref)
		}
	}
	if b, ok := blocks[r.TrustBlockID]; ok {
		t.Block = &b
	}
	return t
}

// Counts answers what the badge reads of the signatures of each subject attached in q.RepoIDs (R-SIG-009, R-SIG-034): the role, the
// integrity state, the trust state in q's scope and whether that row is stale. A signature found by npm's subject name counts under its
// own subject (the digest of its message, Q-SIG-50).
func (s *Store) Counts(ctx context.Context, vq signatures.ViewQuery) (map[[32]byte][]signatures.Counted, error) {
	out := map[[32]byte][]signatures.Counted{}
	if len(vq.RepoIDs) == 0 || (len(vq.Subjects) == 0 && len(vq.SubjectNames) == 0) {
		return out, nil
	}
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := viewRows(ctx, qs, vq)
		if err != nil {
			return err
		}
		for _, r := range rows {
			c := signatures.Counted{Role: r.Role, State: signatures.State(r.State)}
			if r.HasTrust {
				c.Trust, c.Stale = r.TrustState, r.TrustEpoch < r.CurrentEpoch
			}
			d := digest(r.SubjectSha256)
			out[d] = append(out[d], c)
		}
		return nil
	})
	return out, err
}

// ObjectHolders answers the repositories that hold an attachment of a signature of the object (the permission of its download,
// R-SIG-034).
func (s *Store) ObjectHolders(ctx context.Context, sha256 [32]byte) ([]signatures.Holder, error) {
	var out []signatures.Holder
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := qs.SignatureObjectHolders(ctx, sha256[:])
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

// ObjectBytes answers the original bytes of a signature object and its media type ("" when none is known) (R-SIG-034): the bytes
// PostgreSQL keeps, or the stored file's blob through the blob store, its zip entry ("entry:<name>", read with the bounded zip reader)
// or the value at a JSON pointer ("#/…"). An object larger than signatures.max_object_size is not read (TooLarge); a blob that is not
// stored is NotFound.
func (s *Store) ObjectBytes(ctx context.Context, sha256 [32]byte) ([]byte, string, error) {
	o, ok, err := s.Object(ctx, sha256)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, "", errs.E(errs.NotFound, errs.CodeNotFound, "No signature object has this digest.")
	}
	if o.Raw != nil {
		return o.Raw, o.MediaType, nil
	}
	if o.Size > s.o.MaxObjectSize {
		return nil, "", errs.E(errs.TooLarge, errs.CodeTooLarge, "The signature object is larger than signatures.max_object_size and is not read.")
	}
	b, err := s.readLocated(ctx, o.ItemBlob, o.Locator, s.o.MaxObjectSize)
	if err != nil {
		return nil, "", err
	}
	return b, o.MediaType, nil
}

// readLocated reads a stored blob, or the part of it a locator names, at most max bytes.
func (s *Store) readLocated(ctx context.Context, blob [32]byte, locator string, max int64) ([]byte, error) {
	if s.o.Blobs == nil {
		return nil, errs.E(errs.Unavailable, errs.CodeUnavailable, "The signature store has no blob reader.")
	}
	c, err := s.o.Blobs.Content(ctx, blob)
	if errors.Is(err, scheme.ErrBlobMissing) {
		return nil, errs.E(errs.NotFound, errs.CodeNotFound, "The stored file of the signature object is gone.")
	}
	if err != nil {
		return nil, fmt.Errorf("signatures: read the blob of a signature object: %w", err)
	}
	switch {
	case strings.HasPrefix(locator, "entry:"):
		return zipEntry(ctx, c, strings.TrimPrefix(locator, "entry:"), max)
	case strings.HasPrefix(locator, "#"):
		whole, err := c.Bytes(max * 4)
		if err != nil {
			return nil, errs.E(errs.TooLarge, errs.CodeTooLarge, "The document that holds the signature object is too large to read.")
		}
		return jsonPointer(whole, strings.TrimPrefix(locator, "#"), max)
	}
	b, err := c.Bytes(max)
	if err != nil {
		return nil, errs.E(errs.TooLarge, errs.CodeTooLarge, "The signature object is too large to read.")
	}
	return b, nil
}

// zipEntry reads one entry of a zip archive with the bounded reader of R-ARCH-028.
func zipEntry(ctx context.Context, c scheme.Content, name string, max int64) ([]byte, error) {
	ra, err := c.ReaderAt()
	if err != nil {
		return nil, fmt.Errorf("signatures: read the archive of a signature object: %w", err)
	}
	z, err := untrusted.NewZip(ctx, ra, c.Size(), untrusted.DefaultLimits())
	if err != nil {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "The archive that holds the signature object does not read.", errs.WithCause(err))
	}
	for _, e := range z.Entries() {
		if e.Name == name && !e.Dir {
			b, err := z.ReadEntry(e, max)
			if err != nil {
				return nil, errs.E(errs.TooLarge, errs.CodeTooLarge, "The signature entry of the archive is too large to read.", errs.WithCause(err))
			}
			return b, nil
		}
	}
	return nil, errs.E(errs.NotFound, errs.CodeNotFound, "The archive holds no entry "+name+".")
}

// jsonPointer answers the JSON value at an RFC 6901 pointer of a document, checked against the bounds of R-SIG-013 first.
func jsonPointer(doc []byte, pointer string, max int64) ([]byte, error) {
	if err := untrusted.CheckJSON(doc, untrusted.Limits{}); err != nil {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "The document that holds the signature object does not read.", errs.WithCause(err))
	}
	cur := json.RawMessage(doc)
	if pointer != "" {
		for _, tok := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
			tok = strings.ReplaceAll(strings.ReplaceAll(tok, "~1", "/"), "~0", "~")
			next, ok := step(cur, tok)
			if !ok {
				return nil, errs.E(errs.NotFound, errs.CodeNotFound, "The document holds nothing at "+pointer+".")
			}
			cur = next
		}
	}
	if int64(len(cur)) > max {
		return nil, errs.E(errs.TooLarge, errs.CodeTooLarge, "The signature object is too large to read.")
	}
	return cur, nil
}

// step is one token of a JSON pointer: a member of an object or an index of an array.
func step(cur json.RawMessage, tok string) (json.RawMessage, bool) {
	trimmed := strings.TrimSpace(string(cur))
	if strings.HasPrefix(trimmed, "[") {
		var arr []json.RawMessage
		i, err := strconv.Atoi(tok)
		if err != nil || json.Unmarshal(cur, &arr) != nil || i < 0 || i >= len(arr) {
			return nil, false
		}
		return arr[i], true
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(cur, &obj) != nil {
		return nil, false
	}
	v, ok := obj[tok]
	return v, ok
}
