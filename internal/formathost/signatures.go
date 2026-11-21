package formathost

import (
	"context"
	"crypto/sha256"
	"log/slog"
	"strings"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/proxy"
	"github.com/guided-traffic/quader/internal/signatures"
)

// SignatureRecorder is what the storage hooks record through (signatures.md R-SIG-010, R-SIG-011): what a commit or a remote's
// stored document carries, the subject that arrived, and the origin of an upload. internal/signatures/store implements it; nil records
// nothing (a replica or a test that has not wired it).
type SignatureRecorder interface {
	Resolve(ctx context.Context, tx meta.Tx, repoID int64, paths []string) (map[string]signatures.Stored, error)
	Record(ctx context.Context, tx meta.Tx, b signatures.Batch) error
	SubjectArrived(ctx context.Context, tx meta.Tx, subject [32]byte) (int64, error)
	RecordOrigin(ctx context.Context, tx meta.Tx, o signatures.Origin) error
}

// recordSignatures asks the format's detector what ev carries and records it in tx: the candidates, each path they name resolved in
// the event's repository and nowhere else, as one batch that replaces what the carrier and the named locations had of the sources a
// detector names (R-SIG-004, Q-SIG-217). A
// detector's error never fails the commit: it is logged, and the content is stored without candidates (Q-SIG-47). A store error
// does: the rows and the content commit together.
func (d Deps) recordSignatures(ctx context.Context, tx meta.Tx, f formats.Format, ev formats.SignatureEvent, fresh bool) error {
	priority := signatures.PriorityFill
	if ev.Origin == formats.OriginUpload {
		priority = signatures.PriorityUpload
	}
	return d.recordAt(ctx, tx, f, ev, fresh, priority)
}

// detectorSources are the attachment sources a format's detector names (formats.SignatureSourceValid): a detector's batch replaces
// attachments of these and of no other source, so a signature a client posted through the management API ("upload") or one Quader
// issued is never removed by the batch of the item it is attached to (R-SIG-004, Q-SIG-217).
func detectorSources() []string {
	return []string{
		formats.SignatureSourceReferrer, formats.SignatureSourceReferrersTag, formats.SignatureSourceCosignTag, formats.SignatureSourceIndexEntry,
		formats.SignatureSourceDocument, formats.SignatureSourceEmbedded, formats.SignatureSourceSibling, formats.SignatureSourceRelease,
	}
}

// recordAt is recordSignatures with the priority the batch's signatures are due at: a commit's, or discovery's (R-SIG-011). An older
// cosign signature tag it records (the source cosign_tag) takes Quader's referrer of its subject in the same transaction (signatures.md
// R-SIG-054, Q-SIG-66).
func (d Deps) recordAt(ctx context.Context, tx meta.Tx, f formats.Format, ev formats.SignatureEvent, fresh bool, priority int) error {
	if d.Signatures == nil {
		return nil
	}
	sa := f.Signatures()
	if sa == nil || ev.Content == nil {
		return nil
	}
	got, err := sa.Detect(ctx, ev)
	if err != nil {
		d.log().Warn("signatures: the detector could not read the stored content; it is stored without signatures",
			"format", f.Descriptor().ID, "repo", ev.Repo.Name.String(), "path", ev.Path.Path, "err", err)
		got = nil
	}
	b := signatures.Batch{RepoID: ev.Repo.ID, CarrierItem: ev.Item.ID, Fresh: fresh && ev.Item.ID != 0, Priority: priority, Sources: detectorSources()}
	if ev.Item.ID == 0 {
		b.Document = ev.Path.Path
	}
	stored, err := d.resolveNamed(ctx, tx, ev, got)
	if err != nil {
		return err
	}
	var tagged [][32]byte
	for _, c := range got {
		if cand, ok := candidateOf(f, ev, c, stored); ok {
			b.Candidates = append(b.Candidates, cand)
			if cand.Source == formats.SignatureSourceCosignTag {
				tagged = append(tagged, cand.Subject)
			}
		}
	}
	if err := d.Signatures.Record(ctx, tx, b); err != nil {
		return err
	}
	if len(tagged) == 0 || d.Attestations == nil {
		return nil
	}
	displaced, err := d.Attestations.SignatureTagArrived(ctx, tx, ev.Repo.ID, tagged)
	if err != nil {
		return err
	}
	return d.removeDisplaced(ctx, tx, ev.Repo, displaced)
}

// resolveNamed resolves every path the candidates name, in one statement.
func (d Deps) resolveNamed(ctx context.Context, tx meta.Tx, ev formats.SignatureEvent, got []formats.SignatureCandidate) (map[string]signatures.Stored, error) {
	seen := map[string]bool{}
	var paths []string
	add := func(p string) {
		if p != "" && p != ev.Path.Path && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	for _, c := range got {
		add(c.Object.Path)
		add(c.SubjectPath)
		add(c.SignedContent.Path)
	}
	stored, err := d.Signatures.Resolve(ctx, tx, ev.Repo.ID, paths)
	if err != nil {
		return nil, err
	}
	var self [32]byte
	copy(self[:], ev.Digests.SHA256)
	stored[ev.Path.Path] = signatures.Stored{Blob: self, Size: ev.Content.Size(), ItemID: ev.Item.ID}
	return stored, nil
}

// candidateOf turns a format's candidate into the store's, its paths resolved; ok is false for a candidate that names a path that holds
// nothing (it is named again when that path is stored) or that names no object or no subject.
func candidateOf(f formats.Format, ev formats.SignatureEvent, c formats.SignatureCandidate, stored map[string]signatures.Stored) (signatures.Candidate, bool) {
	out := signatures.Candidate{
		Part: c.Part, Subject: c.Subject, SubjectName: c.SubjectName, Role: c.Role, Source: c.Source, Location: c.Location,
		ItemID: ev.Item.ID,
	}
	if !formats.SignatureRoleValid(c.Role) || !formats.SignatureSourceValid(c.Source) || c.Location == "" || c.Part < 0 {
		return out, false
	}
	obj, carrier, ok := objectOf(c.Object, stored)
	if !ok {
		return out, false
	}
	obj.Scheme = c.SchemeHint
	if !formats.SignatureSchemeValid(obj.Scheme) {
		obj.Scheme = formats.SignatureSchemeUnknown
	}
	out.Object = obj
	if carrier != nil {
		// The original is a stored file of its own (a signature file beside its file): the attachment follows that file.
		out.ItemID = carrier.ItemID
	}
	if c.SubjectPath != "" {
		s, ok := stored[c.SubjectPath]
		if !ok {
			return out, false
		}
		out.Subject = s.Blob
	}
	if out.Subject == [32]byte{} {
		return out, false
	}
	switch sc := c.SignedContent; {
	case sc.Raw != nil:
		// A message the signature commits to (npm's registry signature): its digest is what a key checks (Q-SIG-23).
		sum := sha256.Sum256(sc.Raw)
		out.SignedDigest, out.DigestAlg = sum[:], "sha256"
	case sc.Path != "":
		s, ok := stored[sc.Path]
		if !ok {
			return out, false
		}
		out.ContentBlob, out.ContentLocator = s.Blob, sc.Locator
	case sc.Blob != [32]byte{} || sc.Locator != "":
		out.ContentBlob, out.ContentLocator = sc.Blob, sc.Locator
	}
	loc, _, _ := strings.Cut(c.Location, "#")
	out.Name = f.Classify(formats.Path{Path: loc}).Package
	return out, true
}

// objectOf resolves the original: its bytes, a blob it is part of, or a stored file. carrier is the stored file when the locator named
// one by its path.
func objectOf(l formats.SignatureLocator, stored map[string]signatures.Stored) (signatures.Object, *signatures.Stored, bool) {
	o := signatures.Object{MediaType: l.MediaType, Locator: l.Locator}
	switch {
	case l.Raw != nil:
		o.Raw, o.SHA256, o.Size = l.Raw, sha256.Sum256(l.Raw), int64(len(l.Raw))
		return o, nil, len(l.Raw) > 0
	case l.Path != "":
		s, ok := stored[l.Path]
		if !ok {
			return o, nil, false
		}
		o.ItemBlob, o.SHA256, o.Size = s.Blob, s.Blob, s.Size
		if l.SHA256 != [32]byte{} {
			o.SHA256, o.Size = l.SHA256, l.Size
		}
		var carrier *signatures.Stored
		if s.ItemID != 0 {
			carrier = &s
		}
		return o, carrier, true
	case l.Blob != [32]byte{}:
		o.ItemBlob, o.SHA256, o.Size = l.Blob, l.Blob, l.Size
		if l.SHA256 != [32]byte{} {
			o.SHA256 = l.SHA256
		}
		if o.Size == 0 {
			if s, ok := storedBlob(stored, l.Blob); ok {
				o.Size = s.Size
			}
		}
		return o, nil, o.Size > 0
	}
	return o, nil, false
}

// storedBlob finds a resolved path that holds the blob (the committed content itself).
func storedBlob(stored map[string]signatures.Stored, blob [32]byte) (signatures.Stored, bool) {
	for _, s := range stored {
		if s.Blob == blob {
			return s, true
		}
	}
	return signatures.Stored{}, false
}

// recordOrigin records who uploaded a committed item (R-SIG-010): every item a client upload commits, the lead and its companions; a
// remote's fill and a file Quader derived record none.
func (d Deps) recordOrigin(ctx context.Context, tx meta.Tx, itemID int64, kind, name, tokenID string) error {
	if d.Signatures == nil || itemID == 0 {
		return nil
	}
	return d.Signatures.RecordOrigin(ctx, tx, signatures.Origin{ItemID: itemID, PrincipalKind: kind, PrincipalName: name, TokenID: tokenID})
}

var _ proxy.DocumentHook = (*adapter)(nil)

// DocumentStored implements [proxy.DocumentHook]: the signatures a remote's stored document carries are recorded with it, in the fill's
// transaction (R-SIG-011), and a document is a subject that may have arrived after its signature.
func (a *adapter) DocumentStored(ctx context.Context, tx meta.Tx, d proxy.StoredDocument) error {
	if a.deps.Signatures == nil {
		return nil
	}
	ev := formats.SignatureEvent{
		Origin: formats.OriginFill, Repo: a.ref, Path: formats.Path{Path: d.Path}, Variant: d.Variant, MediaType: d.MediaType,
		Digests: formats.Digests{SHA256: d.Digest[:]}, Content: d.Content,
	}
	if err := a.deps.recordSignatures(ctx, tx, a.f, ev, false); err != nil {
		return err
	}
	_, err := a.deps.Signatures.SubjectArrived(ctx, tx, d.Digest)
	return err
}

func (d Deps) log() *slog.Logger {
	if d.Log != nil {
		return d.Log
	}
	return slog.Default()
}
