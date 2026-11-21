package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signatures"
	"github.com/guided-traffic/quader/internal/signatures/anchors"
	"github.com/guided-traffic/quader/internal/signatures/scheme"
	"github.com/guided-traffic/quader/internal/signatures/shipped"
)

// The reads and writes of the job signature-verify (R-SIG-011).

// npmKeysPath is the path of an npm remote's stored keys document (R-SIG-028): another package's endpoint stores it there.
const npmKeysPath = "-/npm/v1/keys"

// maxKeysDocument bounds an npm remote's stored keys document as the store reads it.
const maxKeysDocument = 256 << 10

// Check is a due signature with what its check needs (R-SIG-011): the row, its object, and what the blobs table knows of its subject.
type Check struct {
	Signature signatures.Signature
	Object    signatures.Object
	// SubjectStored is false when no blob has the subject's digest: an npm registry signature, whose subject is the digest of its
	// message (Q-SIG-50), or a subject not stored yet.
	SubjectStored    bool
	SubjectSHA512    []byte
	SubjectSize      int64
	SubjectCreatedAt time.Time
}

// DueChecks reads at most limit due signatures whose time has come, by priority and then not_before, without those in inFlight, in a
// transaction of its own. Nothing is locked across the check: one job runs at a time (coalesce).
func (s *Store) DueChecks(ctx context.Context, limit int, inFlight []int64) ([]Check, error) {
	if inFlight == nil {
		inFlight = []int64{}
	}
	var out []Check
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := qs.SignatureDueRows(ctx, q.SignatureDueRowsParams{InFlight: inFlight, RowLimit: int32(min(max(limit, 1), 10000))}) //nolint:gosec // bounded
		if err != nil {
			return err
		}
		out = make([]Check, 0, len(rows))
		for _, r := range rows {
			out = append(out, Check{
				Signature: signatures.Signature{
					ID: r.ID, Object: digest(r.ObjectSha256), Part: int(r.Part), Subject: digest(r.SubjectSha256), SubjectName: r.SubjectName,
					Role: r.Role, ContentBlob: digest(r.ContentSha256), ContentLocator: r.ContentLocator, State: signatures.State(r.State),
					SignedDigest: r.SignedDigest, DigestAlg: r.DigestAlg, Due: true, Priority: int(r.Priority), Attempts: int(r.Attempts),
					NotBefore: timeOf(r.NotBefore.Valid, r.NotBefore.Time),
				},
				Object: signatures.Object{
					SHA256: digest(r.ObjectSha256), Scheme: r.Scheme, MediaType: r.MediaType, Raw: r.Raw, ItemBlob: digest(r.ItemBlob),
					Locator: r.Locator, Size: r.Size, FirstSeenAt: timeOf(r.FirstSeenAt.Valid, r.FirstSeenAt.Time),
				},
				SubjectStored: r.SubjectStored, SubjectSHA512: r.SubjectSha512, SubjectSize: r.SubjectSize,
				SubjectCreatedAt: timeOf(r.SubjectCreatedAt.Valid, r.SubjectCreatedAt.Time),
			})
		}
		return nil
	})
	return out, err
}

// WriteCheck writes the verdict of a check in tx (R-SIG-005, R-SIG-006, R-SIG-007): the result with its evidence, and the receipt the
// check used when it is earlier than the row's. It answers the state the row had before; ok is false when the row went meanwhile (the
// orphan removal), and nothing is written then.
func (s *Store) WriteCheck(ctx context.Context, tx meta.Tx, id int64, r signatures.Result, receivedAt time.Time) (before signatures.State, ok bool, err error) {
	qs, err := queries(tx)
	if err != nil {
		return "", false, err
	}
	states, err := qs.SignatureLockState(ctx, id)
	if err != nil || len(states) == 0 {
		return "", false, err
	}
	if err := s.SetResult(ctx, tx, id, r); err != nil {
		return "", false, err
	}
	if !receivedAt.IsZero() {
		if err := qs.SignatureSetReceived(ctx, q.SignatureSetReceivedParams{ID: id, ReceivedAt: rfc3339(receivedAt)}); err != nil {
			return "", false, err
		}
	}
	return signatures.State(states[0]), true, nil
}

// AddParts makes the rows of parts 1 to parts-1 of the object of a part-0 row, against the same subject and subject name, due at the
// part-0 row's priority, with copies of its attachments (R-SIG-003): the parts a detector could not name. parts is bounded by
// signatures.max_per_subject. It answers the rows made.
func (s *Store) AddParts(ctx context.Context, tx meta.Tx, id int64, parts int) (int64, error) {
	parts = min(parts, s.o.MaxPerSubject)
	if parts < 2 {
		return 0, nil
	}
	qs, err := queries(tx)
	if err != nil {
		return 0, err
	}
	n, err := qs.SignatureAddParts(ctx, q.SignatureAddPartsParams{ID: id, Parts: int32(parts)}) //nolint:gosec // bounded above
	if err != nil {
		return 0, err
	}
	if _, err := qs.SignatureCopyPartAttachments(ctx, q.SignatureCopyPartAttachmentsParams{ID: id, Parts: int32(parts)}); err != nil { //nolint:gosec // bounded above
		return 0, err
	}
	return n, nil
}

// RecordInvalid records signature.invalid for a signature a check or an anchor's key made invalid (R-SIG-006, R-SIG-039), in tx, with
// actor system:signatures: the object, the subject, the scheme, the role, the reason, the signer and the repositories that hold an
// attachment (at most 100) with their count.
func (s *Store) RecordInvalid(ctx context.Context, tx meta.Tx, id int64) error {
	qs, err := queries(tx)
	if err != nil {
		return err
	}
	r, err := qs.SignatureInvalidDetails(ctx, q.SignatureInvalidDetailsParams{ID: id, MaxRows: maxHolderNames})
	if err != nil {
		return fmt.Errorf("read what signature.invalid names: %w", err)
	}
	det := map[string]any{
		"object": hexOf(r.ObjectSha256), "subject": hexOf(r.SubjectSha256), "scheme": r.Scheme, "role": r.Role, "reason": r.Reason,
		"repositories": r.Repositories, "repositoryCount": r.RepositoryCount,
	}
	if r.Repositories == nil {
		det["repositories"] = []string{}
	}
	if r.SubjectName != "" {
		det["subjectName"] = r.SubjectName
	}
	if len(r.Signer) > 0 && string(r.Signer) != "null" {
		det["signer"] = r.Signer
	}
	return s.o.Audit.RecordTx(ctx, tx, audit.Event{
		Type: audit.EventSignatureInvalid, Actor: SystemActor(), Outcome: audit.Success,
		Subject: audit.Subject{Kind: "signature", ID: idText(id)}, Details: details(det),
	})
}

// UpstreamKeys answers, per signature, the keys the upstreams of the repositories that attach it announce, for the integrity check
// alone (R-SIG-006, Q-SIG-24): a Debian remote's deb.upstreamKeys, and an npm remote's stored keys document at -/npm/v1/keys converted
// with shipped.NPMKeys. Keys that do not parse are left out; an error of the blob store is an error (the check spends an attempt).
func (s *Store) UpstreamKeys(ctx context.Context, signatureIDs []int64) (map[int64]scheme.Keys, error) {
	out := map[int64]scheme.Keys{}
	if len(signatureIDs) == 0 {
		return out, nil
	}
	var rows []q.SignatureUpstreamKeysRow
	if err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) (err error) {
		rows, err = qs.SignatureUpstreamKeys(ctx, q.SignatureUpstreamKeysParams{SignatureIds: signatureIDs, KeysPath: npmKeysPath})
		return err
	}); err != nil {
		return nil, err
	}
	for _, r := range rows {
		k := out[r.SignatureID]
		if r.DebKeys != "" {
			if p, err := anchors.Parse(anchors.KindOpenPGP, anchors.Material{OpenPGP: &anchors.OpenPGPMaterial{Keys: r.DebKeys}}); err == nil {
				k.OpenPGP = append(k.OpenPGP, p.OpenPGP...)
			}
		}
		if len(r.NpmKeysBlob) == 32 {
			keys, err := s.npmKeys(ctx, digest(r.NpmKeysBlob))
			if err != nil {
				return nil, err
			}
			k.Public = append(k.Public, keys...)
		}
		out[r.SignatureID] = k
	}
	return out, nil
}

// npmKeys reads a stored keys document and parses its keys; a document that is gone or does not parse names none.
func (s *Store) npmKeys(ctx context.Context, blob [32]byte) ([]anchors.ParsedKey, error) {
	if s.o.Blobs == nil {
		return nil, nil
	}
	c, err := s.o.Blobs.Content(ctx, blob)
	if errors.Is(err, scheme.ErrBlobMissing) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read an npm remote's keys document: %w", err)
	}
	doc, err := c.Bytes(maxKeysDocument)
	if err != nil {
		return nil, nil //nolint:nilerr // a document above the bound names no key
	}
	m, err := shipped.NPMKeys(doc)
	if err != nil {
		return nil, nil //nolint:nilerr // a document that does not parse names no key
	}
	p, err := anchors.Parse(anchors.KindPublicKey, anchors.Material{PublicKey: m})
	if err != nil {
		return nil, nil //nolint:nilerr // keys that do not parse name no key
	}
	return p.Public, nil
}

// Discovery answers discovery's cursor over items in id order and when its pass started (R-SIG-011).
func (s *Store) Discovery(ctx context.Context) (after int64, passStarted time.Time, err error) {
	err = s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		r, err := qs.SignatureDiscovery(ctx)
		after, passStarted = r.DiscoveryAfter, timeOf(r.DiscoveryPassAt.Valid, r.DiscoveryPassAt.Time)
		return err
	})
	return after, passStarted, err
}

// SetDiscovery advances discovery's cursor; restart begins a new pass from the first item (after is then 0).
func (s *Store) SetDiscovery(ctx context.Context, after int64, restart bool) error {
	return s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		return qs.SignatureSetDiscovery(ctx, q.SignatureSetDiscoveryParams{After: after, Restart: restart})
	})
}

// RetryFailed makes the failed signatures due again at the priority of discovery with their attempts anew, at most once a day for the
// installation (R-SIG-011), and answers how many.
func (s *Store) RetryFailed(ctx context.Context) (int64, error) {
	var n int64
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) (err error) {
		n, err = qs.SignatureRetryFailed(ctx)
		return err
	})
	return n, err
}

// Stats is what the worker's gauges show after a run (R-SIG-039).
type Stats struct {
	// Due are the due rows by priority 0 to 3.
	Due [4]int64
	// OldestPending is the age in seconds of the oldest due row's receipt.
	OldestPending float64
	// Invalid are the invalid signatures with an attachment; Stale the trust rows under an older epoch.
	Invalid int64
	Stale   int64
	// RootFetchedAt is the fetch of the public root the task stored; zero when the release's snapshot is in use.
	RootFetchedAt time.Time
}

// Stats reads the gauges' values.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var out Stats
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		r, err := qs.SignatureStats(ctx)
		if err != nil {
			return err
		}
		out = Stats{
			Due: [4]int64{r.Due0, r.Due1, r.Due2, r.Due3}, OldestPending: r.OldestPending, Invalid: r.Invalid, Stale: r.Stale,
			RootFetchedAt: timeOf(r.RootFetchedAt.Valid, r.RootFetchedAt.Time),
		}
		return nil
	})
	return out, err
}
