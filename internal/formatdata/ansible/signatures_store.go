package ansible

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/ansible/collection"
	"github.com/guided-traffic/quader/internal/formats/ansible/rows"
	"github.com/guided-traffic/quader/internal/formats/untrusted"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signatures"
)

// The detached signatures a client posts for a version of a local repository (signatures.md R-SIG-031, Q-SIG-37, Q-SIG-102):
// ansible-galaxy has no command to upload one, so a CI job posts it through the management API. The port checks what it can without
// a key, stores the signature for signatures[] of the version document and records it in the signature store, whose job checks it with
// the key of an anchor.

const (
	// MaxVersionSignatures is the most signatures a version takes.
	MaxVersionSignatures = 16
	// maxPostedSignature bounds a posted signature: one armored signature packet is a few hundred bytes.
	maxPostedSignature = 64 << 10
	// manifestLocator is the entry of the archive a signature signs.
	manifestLocator = "entry:" + collection.ManifestName
)

// SignatureRecorder is the signature store as the port records a posted signature through it; *store.Store of
// internal/signatures/store implements it.
type SignatureRecorder interface {
	Record(ctx context.Context, tx meta.Tx, b signatures.Batch) error
	Detach(ctx context.Context, tx meta.Tx, repoID int64, locations []string) (int64, error)
}

func signatureInvalid(detail string) error {
	return errs.E(errs.Unprocessable, errs.CodeSignatureInvalid, detail)
}

// postedFacts reads a posted signature without a key: one armored OpenPGP signature packet that names the fingerprint of its key.
func postedFacts(sig string) (untrusted.OpenPGPSig, error) {
	if len(sig) == 0 || len(sig) > maxPostedSignature || !untrusted.OpenPGPArmored([]byte(sig)) {
		return untrusted.OpenPGPSig{}, signatureInvalid("The signature is not an ASCII-armored OpenPGP signature of at most 64 KiB.")
	}
	facts, err := untrusted.OpenPGPSignatureFacts([]byte(sig))
	if err != nil {
		return untrusted.OpenPGPSig{}, signatureInvalid("The signature is not an OpenPGP signature within the bounds of untrusted content.")
	}
	if len(facts) != 1 {
		return untrusted.OpenPGPSig{}, signatureInvalid("Post one signature at a time: the block holds more than one signature packet.")
	}
	if facts[0].Fingerprint == nil {
		return untrusted.OpenPGPSig{}, signatureInvalid("The signature names no issuer fingerprint (RFC 9580, section 5.2.3.35); make it with a current GnuPG.")
	}
	return facts[0], nil
}

// signatureLocation is the attachment's location of a posted signature: the archive's path and the key's fingerprint.
func signatureLocation(archivePath, fingerprint string) string {
	return archivePath + "#/signatures/" + fingerprint
}

// AddSignature stores a signature a client posted for a version of a local repository (R-SIG-031): checked as far as no key is needed
// (one OpenPGP signature packet, an issuer fingerprint, the prefix of the hash it signs against the version's MANIFEST.json), stored
// for signatures[] of the version document, and recorded in the signature store with the source upload. A second signature of the same
// key replaces the first; a version takes at most [MaxVersionSignatures]. The audit event ansible.signature.added is written in the
// transaction (Q-SIG-107).
func (s localStore) AddSignature(ctx context.Context, in rows.SignatureInput) (rows.PostedSignature, error) {
	facts, err := postedFacts(in.Signature)
	if err != nil {
		return rows.PostedSignature{}, err
	}
	fp := hex.EncodeToString(facts.Fingerprint)
	v, found, err := s.Version(ctx, in.RepoID, in.Name, in.Version)
	if err != nil {
		return rows.PostedSignature{}, err
	}
	if !found {
		return rows.PostedSignature{}, errs.E(errs.NotFound, errs.CodeNotFound, "The collection "+in.Name+" has no version "+in.Version+".")
	}
	manifest, err := s.manifestOf(ctx, v)
	if err != nil {
		return rows.PostedSignature{}, err
	}
	if checked, ok := facts.MatchesHashPrefix(manifest); checked && !ok {
		return rows.PostedSignature{}, signatureInvalid("The signature does not sign the version's MANIFEST.json.")
	}
	out := rows.PostedSignature{Fingerprint: fp, Signature: in.Signature}
	err = s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := txQueries(tx)
		if err != nil {
			return err
		}
		locked, err := qs.AnsibleLockVersion(ctx, q.AnsibleLockVersionParams{RepoID: in.RepoID, Name: in.Name, Version: in.Version})
		if err != nil {
			return err
		}
		if len(locked) == 0 || digestOf(locked[0].Sha256) != v.SHA256 {
			return errs.E(errs.NotFound, errs.CodeNotFound, "The collection "+in.Name+" has no version "+in.Version+".")
		}
		replaced, err := s.withinLimit(ctx, qs, in, fp)
		if err != nil {
			return err
		}
		if _, err := qs.AnsibleInsertVersionSignature(ctx, q.AnsibleInsertVersionSignatureParams{
			RepoID: in.RepoID, Name: in.Name, Version: in.Version, Fingerprint: fp, Signature: in.Signature, PostedBy: actorName(in.Audit),
		}); err != nil {
			return err
		}
		// The version document changes: the package's revision, which its validator is derived from, rises (Q-ANS-31).
		if _, err := qs.AnsibleBumpPackage(ctx, q.AnsibleBumpPackageParams{RepoID: in.RepoID, Name: in.Name}); err != nil {
			return err
		}
		if err := s.record(ctx, tx, in, locked[0], fp); err != nil {
			return err
		}
		return s.signatureEvent(ctx, tx, audit.EventAnsibleSignatureAdded, in.Audit, in.Name, in.Version,
			map[string]any{"name": in.Name, "version": in.Version, "fingerprint": fp, "replaced": replaced})
	})
	return out, err
}

// withinLimit refuses a signature of a new key for a version that holds the most it takes; replaced is true when the version holds
// a signature of the key already.
func (s localStore) withinLimit(ctx context.Context, qs *q.Queries, in rows.SignatureInput, fp string) (replaced bool, err error) {
	have, err := qs.AnsibleVersionSignatures(ctx, q.AnsibleVersionSignaturesParams{RepoID: in.RepoID, Name: in.Name, Version: in.Version})
	if err != nil {
		return false, err
	}
	for _, h := range have {
		if h.Fingerprint == fp {
			return true, nil
		}
	}
	if len(have) >= MaxVersionSignatures {
		return false, errs.E(errs.Conflict, errs.CodeSignatureLimitReached, "The version holds 16 signatures, the most it takes; remove one first.")
	}
	return false, nil
}

// signatureEvent writes the audit event of a posted or removed signature in the transaction of the change (Q-SIG-107).
func (s localStore) signatureEvent(ctx context.Context, tx meta.Tx, typ string, a rows.Audit, name, version string, details map[string]any) error {
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	return s.o.Recorder.RecordTx(ctx, tx, s.event(ctx, typ, a, name, version, raw, nil))
}

// record records a posted signature in the signature store: the armored bytes as the object, the archive as the subject and its
// MANIFEST.json as the signed content, attached at the archive's item so that it goes with the version. A replaced signature of the
// same key loses its attachment first.
func (s localStore) record(ctx context.Context, tx meta.Tx, in rows.SignatureInput, v q.AnsibleLockVersionRow, fp string) error {
	if s.o.Signatures == nil {
		return nil
	}
	loc := signatureLocation(v.Path, fp)
	if _, err := s.o.Signatures.Detach(ctx, tx, in.RepoID, []string{loc}); err != nil {
		return err
	}
	raw := []byte(in.Signature)
	subject := digestOf(v.Sha256)
	return s.o.Signatures.Record(ctx, tx, signatures.Batch{
		RepoID: in.RepoID, Priority: signatures.PriorityUpload,
		Candidates: []signatures.Candidate{{
			Object: signatures.Object{
				SHA256: sha256.Sum256(raw), Scheme: "openpgp", MediaType: "application/pgp-signature", Raw: raw, Size: int64(len(raw)),
			},
			Subject: subject, Role: "publisher", Source: "upload", Location: loc, Name: in.Name, ItemID: v.ItemID,
			ContentBlob: subject, ContentLocator: manifestLocator,
		}},
	})
}

// RemoveSignature removes the signature of a key from a version (R-SIG-031) and writes the audit event ansible.signature.removed in
// the transaction (Q-SIG-107); found is false when the version holds none of it.
func (s localStore) RemoveSignature(ctx context.Context, repoID int64, name, version, fingerprint string, a rows.Audit) (bool, error) {
	fingerprint = strings.ToLower(fingerprint)
	found := false
	err := s.o.DB.InTx(ctx, func(tx meta.Tx) error {
		qs, err := txQueries(tx)
		if err != nil {
			return err
		}
		locked, err := qs.AnsibleLockVersion(ctx, q.AnsibleLockVersionParams{RepoID: repoID, Name: name, Version: version})
		if err != nil || len(locked) == 0 {
			return err
		}
		n, err := qs.AnsibleDeleteVersionSignature(ctx, q.AnsibleDeleteVersionSignatureParams{
			RepoID: repoID, Name: name, Version: version, Fingerprint: fingerprint,
		})
		if err != nil || n == 0 {
			return err
		}
		found = true
		if _, err := qs.AnsibleBumpPackage(ctx, q.AnsibleBumpPackageParams{RepoID: repoID, Name: name}); err != nil {
			return err
		}
		if s.o.Signatures != nil {
			if _, err := s.o.Signatures.Detach(ctx, tx, repoID, []string{signatureLocation(locked[0].Path, fingerprint)}); err != nil {
				return err
			}
		}
		return s.signatureEvent(ctx, tx, audit.EventAnsibleSignatureRemoved, a, name, version,
			map[string]any{"name": name, "version": version, "fingerprint": fingerprint})
	})
	return found, err
}

// manifestOf reads MANIFEST.json from a version's stored archive, under the bounds of R-ANS-031.
func (s localStore) manifestOf(ctx context.Context, v rows.VersionRow) ([]byte, error) {
	if s.o.Blobs == nil {
		return nil, errs.E(errs.Unavailable, errs.CodeUnavailable, "The stored archives cannot be read on this replica.")
	}
	body, err := s.o.Blobs.Open(ctx, v.SHA256, v.Size)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	a, err := collection.ReadArchive(ctx, body, collection.DefaultLimits())
	if err != nil {
		return nil, err
	}
	return a.Manifest, nil
}

// signaturesOf reads the posted signatures of a version.
func signaturesOf(ctx context.Context, qs *q.Queries, repoID int64, name, version string) ([]rows.PostedSignature, error) {
	list, err := qs.AnsibleVersionSignatures(ctx, q.AnsibleVersionSignaturesParams{RepoID: repoID, Name: name, Version: version})
	if err != nil {
		return nil, err
	}
	out := make([]rows.PostedSignature, 0, len(list))
	for _, r := range list {
		out = append(out, rows.PostedSignature{Fingerprint: r.Fingerprint, Signature: r.Signature})
	}
	return out, nil
}

// actorName is who posted, as the audit names an actor.
func actorName(a rows.Audit) string {
	switch {
	case a.Actor.Name != "":
		return a.Actor.Name
	case a.Actor.ID != "":
		return a.Actor.ID
	}
	return "anonymous"
}
