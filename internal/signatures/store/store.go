// Package store is the PostgreSQL store of signatures on artifacts (signatures.md R-SIG-003, R-SIG-004, R-SIG-010, R-SIG-011): the
// one door to signature_objects, signatures, signature_attachments and artifact_origins. The commit hook of the item store and the fill
// of a remote's document record through it in their own transaction (Record, SubjectArrived, RecordOrigin); the job of R-SIG-011
// reads the due rows and writes what its read and check found (Due, SetResult, Defer, RemoveOrphans); the API reads the rows of an
// artifact (BySubject, AttachmentsUnder, Object, Origin).
package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signatures"
)

// Options configure [New].
type Options struct {
	// Runner runs the reads that take no transaction of the caller.
	Runner meta.Runner
	// MaxObjectSize is signatures.max_object_size: an object larger is recorded as malformed (limits_exceeded) and read never; 0 is
	// the default (R-SIG-013, Q-SIG-49).
	MaxObjectSize int64
	// MaxPerSubject is signatures.max_per_subject: the signatures one batch records per subject; 0 is the default.
	MaxPerSubject int
	// Wake, when set, wakes the job signature-verify in the transaction of a batch that recorded a candidate (R-SIG-011). The job's
	// package sets it.
	Wake func(ctx context.Context, tx meta.Tx) error
}

// Store is the store.
type Store struct{ o Options }

// New creates the store.
func New(o Options) *Store {
	if o.MaxObjectSize <= 0 {
		o.MaxObjectSize = signatures.DefaultMaxObjectSize
	}
	if o.MaxPerSubject <= 0 {
		o.MaxPerSubject = signatures.DefaultMaxPerSubject
	}
	return &Store{o: o}
}

var errForeignTx = errors.New("signatures: not a PostgreSQL transaction")

func queries(tx meta.Tx) (*q.Queries, error) {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return nil, errForeignTx
	}
	return q.New(ptx), nil
}

// in runs fn in a transaction of the store's runner.
func (s *Store) in(ctx context.Context, fn func(qs *q.Queries, tx meta.Tx) error) error {
	if s.o.Runner == nil {
		return errors.New("signatures: the store has no runner")
	}
	return s.o.Runner.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		return fn(qs, tx)
	})
}

// ---- the commit hook's writes

// Resolve answers what the paths of a repository hold now: a live file or a remote's stored document. A path that holds nothing is
// absent. The hook resolves only paths of the repository of its event (Q-SIG-47).
func (s *Store) Resolve(ctx context.Context, tx meta.Tx, repoID int64, paths []string) (map[string]signatures.Stored, error) {
	out := map[string]signatures.Stored{}
	if len(paths) == 0 {
		return out, nil
	}
	qs, err := queries(tx)
	if err != nil {
		return nil, err
	}
	rows, err := qs.SignatureStoredPaths(ctx, q.SignatureStoredPathsParams{RepoID: repoID, Paths: paths})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if _, seen := out[r.Path]; seen {
			continue
		}
		out[r.Path] = signatures.Stored{Blob: digest(r.BlobSha256), Size: r.Size, ItemID: r.ItemID}
	}
	return out, nil
}

// Record stores a batch in tx (R-SIG-003, R-SIG-004): the objects and the signatures once (an existing row keeps what it has), and
// the attachments of the batch in place of those its carrier and its locations had. At most MaxPerSubject candidates per subject are
// recorded. An object larger than MaxObjectSize is recorded with a signature that is born malformed (limits_exceeded) and not due.
func (s *Store) Record(ctx context.Context, tx meta.Tx, b signatures.Batch) error {
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	cands := s.bounded(b.Candidates)
	if err := s.insert(ctx, qs, cands, b.Priority); err != nil {
		return err
	}
	ids, err := s.ids(ctx, qs, cands)
	if err != nil {
		return err
	}
	at := q.SignatureUpsertAttachmentsParams{RepoID: b.RepoID}
	rp := q.SignatureReplaceAttachmentsParams{
		RepoID: b.RepoID, ItemID: b.CarrierItem, Document: b.Document, Locations: []string{}, Objects: [][]byte{}, Subjects: [][]byte{},
		KeepIds: []int64{}, KeepLocations: []string{},
	}
	for i, c := range cands {
		rp.Locations, rp.Objects, rp.Subjects = append(rp.Locations, c.Location), append(rp.Objects, c.Object.SHA256[:]), append(rp.Subjects, c.Subject[:])
		rp.KeepIds, rp.KeepLocations = append(rp.KeepIds, ids[i]), append(rp.KeepLocations, c.Location)
		at.Ids, at.Locations, at.ItemIds = append(at.Ids, ids[i]), append(at.Locations, c.Location), append(at.ItemIds, c.ItemID)
		at.Sources, at.Names = append(at.Sources, c.Source), append(at.Names, c.Name)
	}
	if len(cands) == 0 && (b.Fresh || (b.CarrierItem == 0 && b.Document == "")) {
		return nil // a new item carried nothing before, and the batch makes nothing: nothing to replace
	}
	if err := qs.SignatureReplaceAttachments(ctx, rp); err != nil {
		return err
	}
	if len(cands) == 0 {
		return nil
	}
	if err := qs.SignatureUpsertAttachments(ctx, at); err != nil {
		return err
	}
	if s.o.Wake != nil {
		return s.o.Wake(ctx, tx)
	}
	return nil
}

// bounded keeps the candidates that can be stored, at most MaxPerSubject per subject, each once.
func (s *Store) bounded(in []signatures.Candidate) []signatures.Candidate {
	per := map[[32]byte]int{}
	seen := map[string]bool{}
	out := make([]signatures.Candidate, 0, len(in))
	for _, c := range in {
		key := fmt.Sprintf("%x|%d|%x|%s|%s", c.Object.SHA256, c.Part, c.Subject, c.SubjectName, c.Location)
		switch {
		case seen[key], per[c.Subject] >= s.o.MaxPerSubject, c.Object.Size < 1, len(c.Object.Raw) > signatures.MaxRaw,
			(c.Object.Raw == nil) == (c.Object.ItemBlob == [32]byte{}):
			continue
		}
		seen[key] = true
		per[c.Subject]++
		out = append(out, c)
	}
	return out
}

// insert writes the objects and the signatures of a batch, each once.
func (s *Store) insert(ctx context.Context, qs *q.Queries, cands []signatures.Candidate, priority int) error {
	if len(cands) == 0 {
		return nil
	}
	op := q.SignatureInsertObjectsParams{}
	rp := q.SignatureInsertRowsParams{}
	for _, c := range cands {
		o := c.Object
		scheme := o.Scheme
		if scheme == "" {
			scheme = "unknown"
		}
		op.Sha256s, op.Schemes, op.MediaTypes = append(op.Sha256s, o.SHA256[:]), append(op.Schemes, scheme), append(op.MediaTypes, o.MediaType)
		op.Raws, op.ItemBlobs = append(op.Raws, o.Raw), append(op.ItemBlobs, optional(o.ItemBlob))
		op.Locators, op.Sizes = append(op.Locators, o.Locator), append(op.Sizes, o.Size)
		rp.Objects, rp.Parts = append(rp.Objects, o.SHA256[:]), append(rp.Parts, int16(min(c.Part, 32767))) //nolint:gosec // bounded above
		rp.Subjects, rp.SubjectNames = append(rp.Subjects, c.Subject[:]), append(rp.SubjectNames, c.SubjectName)
		rp.Roles, rp.ContentSha256s = append(rp.Roles, c.Role), append(rp.ContentSha256s, optional(c.ContentBlob))
		rp.ContentLocators, rp.SignedDigests = append(rp.ContentLocators, c.ContentLocator), append(rp.SignedDigests, c.SignedDigest)
		rp.DigestAlgs, rp.TooLarge = append(rp.DigestAlgs, c.DigestAlg), append(rp.TooLarge, o.Size > s.o.MaxObjectSize)
		rp.Priorities = append(rp.Priorities, int16(priority)) //nolint:gosec // 0 to 3
	}
	if err := qs.SignatureInsertObjects(ctx, op); err != nil {
		return err
	}
	return qs.SignatureInsertRows(ctx, rp)
}

// ids reads the ids of the signatures of a batch, in its order.
func (s *Store) ids(ctx context.Context, qs *q.Queries, cands []signatures.Candidate) ([]int64, error) {
	if len(cands) == 0 {
		return nil, nil
	}
	p := q.SignatureIDsParams{}
	for _, c := range cands {
		p.Objects, p.Parts = append(p.Objects, c.Object.SHA256[:]), append(p.Parts, int16(min(c.Part, 32767))) //nolint:gosec // bounded
		p.Subjects, p.SubjectNames = append(p.Subjects, c.Subject[:]), append(p.SubjectNames, c.SubjectName)
	}
	rows, err := qs.SignatureIDs(ctx, p)
	if err != nil {
		return nil, err
	}
	if len(rows) != len(cands) {
		return nil, fmt.Errorf("signatures: %d ids for %d signatures", len(rows), len(cands))
	}
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out, nil
}

// SubjectArrived makes the signatures that wait for the subject due (R-SIG-011) and reports how many.
func (s *Store) SubjectArrived(ctx context.Context, tx meta.Tx, subject [32]byte) (int64, error) {
	qs, err := queries(tx)
	if err != nil {
		return 0, err
	}
	n, err := qs.SignatureSubjectArrived(ctx, subject[:])
	if err == nil && n > 0 && s.o.Wake != nil {
		err = s.o.Wake(ctx, tx)
	}
	return n, err
}

// tokenID is the form of a credential's ID the origin keeps; anything else is kept as none.
var tokenID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// RecordOrigin stores the origin of an uploaded item in tx (R-SIG-010), with the claims of a CI job's issuance record as they are now.
func (s *Store) RecordOrigin(ctx context.Context, tx meta.Tx, o signatures.Origin) error {
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	tok := o.TokenID
	if !tokenID.MatchString(tok) {
		tok = ""
	}
	kind := o.PrincipalKind
	if kind == "" {
		kind = "anonymous"
	}
	return qs.SignatureUpsertOrigin(ctx, q.SignatureUpsertOriginParams{
		ItemID: o.ItemID, PrincipalKind: truncate(kind, 32), PrincipalName: truncate(o.PrincipalName, 255), TokenID: tok,
	})
}

// ---- reads

// Origin returns the origin of an item; ok is false for an item no upload recorded one of (a remote's fill).
func (s *Store) Origin(ctx context.Context, itemID int64) (o signatures.StoredOrigin, ok bool, err error) {
	err = s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := qs.SignatureOrigin(ctx, itemID)
		if err != nil || len(rows) == 0 {
			return err
		}
		r := rows[0]
		o, ok = signatures.StoredOrigin{
			PrincipalKind: r.PrincipalKind, PrincipalName: r.PrincipalName, TokenID: r.TokenID, CI: r.Ci,
			UploadedAt: timeOf(r.UploadedAt.Valid, r.UploadedAt.Time),
		}, true
		return nil
	})
	return o, ok, err
}

// Object returns a signature object; ok is false when there is none.
func (s *Store) Object(ctx context.Context, sha256 [32]byte) (o signatures.Object, ok bool, err error) {
	err = s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := qs.SignatureObject(ctx, sha256[:])
		if err != nil || len(rows) == 0 {
			return err
		}
		r := rows[0]
		o, ok = signatures.Object{
			SHA256: digest(r.Sha256), Scheme: r.Scheme, MediaType: r.MediaType, Raw: r.Raw, ItemBlob: digest(r.ItemBlob),
			Locator: r.Locator, Size: r.Size, FirstSeenAt: timeOf(r.FirstSeenAt.Valid, r.FirstSeenAt.Time),
		}, true
		return nil
	})
	return o, ok, err
}

// Signature returns one signature; ok is false when there is none.
func (s *Store) Signature(ctx context.Context, id int64) (sig signatures.Signature, ok bool, err error) {
	err = s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := qs.SignatureByID(ctx, id)
		if err != nil || len(rows) == 0 {
			return err
		}
		sig, ok = signatureOf(q.SignaturesDueRow(rows[0])), true
		return nil
	})
	return sig, ok, err
}

// BySubject returns the signatures of a subject in the whole installation, in id order. What a reader may see of them is the
// attachments in the repositories it reads (R-SIG-004): the caller joins them.
func (s *Store) BySubject(ctx context.Context, subject [32]byte) ([]signatures.Signature, error) {
	var out []signatures.Signature
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := qs.SignaturesBySubject(ctx, subject[:])
		if err != nil {
			return err
		}
		out = make([]signatures.Signature, len(rows))
		for i, r := range rows {
			out[i] = signatureOf(q.SignaturesDueRow(r))
		}
		return nil
	})
	return out, err
}

// Attachments returns where a signature is attached, by repository and location.
func (s *Store) Attachments(ctx context.Context, signatureID int64) ([]signatures.Attachment, error) {
	var out []signatures.Attachment
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := qs.SignatureAttachments(ctx, signatureID)
		if err != nil {
			return err
		}
		out = make([]signatures.Attachment, len(rows))
		for i, r := range rows {
			out[i] = signatures.Attachment{
				RepoID: r.RepoID, SignatureID: r.SignatureID, Location: r.Location, ItemID: r.ItemID,
				Source: r.Source, Name: r.Name, CreatedAt: timeOf(r.CreatedAt.Valid, r.CreatedAt.Time),
			}
		}
		return nil
	})
	return out, err
}

// AttachmentsUnder returns the attachments of a repository under a package or image name, with their signatures' subjects.
func (s *Store) AttachmentsUnder(ctx context.Context, repoID int64, name string) ([]signatures.Attachment, error) {
	var out []signatures.Attachment
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := qs.SignatureAttachmentsUnder(ctx, q.SignatureAttachmentsUnderParams{RepoID: repoID, Name: name})
		if err != nil {
			return err
		}
		out = make([]signatures.Attachment, len(rows))
		for i, r := range rows {
			out[i] = signatures.Attachment{
				RepoID: r.RepoID, SignatureID: r.SignatureID, Location: r.Location, ItemID: r.ItemID,
				Source: r.Source, Name: r.Name, CreatedAt: timeOf(r.CreatedAt.Valid, r.CreatedAt.Time), Subject: digest(r.SubjectSha256),
			}
		}
		return nil
	})
	return out, err
}

// ---- the job's reads and writes

// Due locks and returns at most limit due signatures whose time has come, by priority and time (R-SIG-011); another transaction skips
// them while tx holds them.
func (s *Store) Due(ctx context.Context, tx meta.Tx, limit int) ([]signatures.Signature, error) {
	qs, err := queries(tx)
	if err != nil {
		return nil, err
	}
	rows, err := qs.SignaturesDue(ctx, int32(min(limit, 10000))) //nolint:gosec // bounded
	if err != nil {
		return nil, err
	}
	out := make([]signatures.Signature, len(rows))
	for i, r := range rows {
		out[i] = signatureOf(r)
	}
	return out, nil
}

// SetResult writes what the read and the integrity check of a signature found; the row is no longer due.
func (s *Store) SetResult(ctx context.Context, tx meta.Tx, id int64, r signatures.Result) error {
	if !r.State.Valid() || r.State == signatures.StatePending {
		return fmt.Errorf("signatures: %q is no result of a check", r.State)
	}
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	keys := r.KeyIDs
	if keys == nil {
		keys = []string{}
	}
	_, err = qs.SignatureSetResult(ctx, q.SignatureSetResultParams{
		ID: id, State: string(r.State), Reason: r.Reason, Role: r.Role, PredicateType: r.PredicateType, Signer: r.Signer, KeyIds: keys,
		SignedDigest: r.SignedDigest, DigestAlg: r.DigestAlg, ClaimedAt: rfc3339(r.ClaimedAt), ProvenAt: rfc3339(r.ProvenAt), TimeProof: r.TimeProof,
	})
	return err
}

// Defer records a check that did not end in a result: spend attempts and be due again at notBefore, or wait (due false) in state with
// the reason, as a signature whose subject is not stored waits for it (pending, subject_missing) and one whose attempts ran out is
// failed (R-SIG-011).
func (s *Store) Defer(ctx context.Context, tx meta.Tx, id int64, spend int, notBefore time.Time, due bool, state signatures.State, reason string) error {
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	_, err = qs.SignatureDefer(ctx, q.SignatureDeferParams{
		ID: id, Spend: int32(spend), NotBefore: rfc3339(notBefore), Due: due, State: string(state), Reason: reason, //nolint:gosec // small
	})
	return err
}

// RemoveOrphans removes at most limit signatures without an attachment and then at most limit objects without a signature, and
// reports both counts (R-SIG-003, R-SIG-011).
func (s *Store) RemoveOrphans(ctx context.Context, tx meta.Tx, limit int) (sigs, objects int64, err error) {
	qs, err := queries(tx)
	if err != nil {
		return 0, 0, err
	}
	n := int32(min(limit, 10000)) //nolint:gosec // bounded
	if sigs, err = qs.SignatureRemoveOrphans(ctx, n); err != nil {
		return 0, 0, err
	}
	objects, err = qs.SignatureRemoveOrphanObjects(ctx, n)
	return sigs, objects, err
}

// ---- helpers

func signatureOf(r q.SignaturesDueRow) signatures.Signature {
	return signatures.Signature{
		ID: r.ID, Object: digest(r.ObjectSha256), Part: int(r.Part), Subject: digest(r.SubjectSha256), SubjectName: r.SubjectName,
		Role: r.Role, PredicateType: r.PredicateType, ContentBlob: digest(r.ContentSha256), ContentLocator: r.ContentLocator,
		State: signatures.State(r.State), Reason: r.Reason, Signer: r.Signer, KeyIDs: r.KeyIds, SignedDigest: r.SignedDigest,
		DigestAlg: r.DigestAlg, ClaimedAt: timeOf(r.ClaimedAt.Valid, r.ClaimedAt.Time), ProvenAt: timeOf(r.ProvenAt.Valid, r.ProvenAt.Time), TimeProof: r.TimeProof,
		ReceivedAt: timeOf(r.ReceivedAt.Valid, r.ReceivedAt.Time), CheckedAt: timeOf(r.CheckedAt.Valid, r.CheckedAt.Time), Due: r.Due, Priority: int(r.Priority), Attempts: int(r.Attempts),
		NotBefore: timeOf(r.NotBefore.Valid, r.NotBefore.Time),
	}
}

func digest(b []byte) [32]byte {
	var d [32]byte
	copy(d[:], b)
	return d
}

// optional is a digest as a nullable column takes it: nil for the zero digest.
func optional(d [32]byte) []byte {
	if d == [32]byte{} {
		return nil
	}
	return d[:]
}

// timeOf reads a nullable time column by its Valid and its Time, so that the driver's type is not named here (R-ARCH-002): the zero
// time for NULL.
func timeOf(valid bool, t time.Time) time.Time {
	if !valid {
		return time.Time{}
	}
	return t.UTC()
}

// rfc3339 renders a time for a text parameter; the zero time is "", which the statements read as NULL.
func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
