package formathost

import (
	"bytes"
	"context"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/repository"
)

// SigningActor is the actor of Quader's own writes of the job signing (signatures.md R-SIG-057, system:signing): the commits of an
// attestation's referrer. The storage hooks record no origin, detect no signature and attest nothing of such a commit.
var SigningActor = audit.Actor{Kind: "system", Name: "signing"}

// AttestationWork is what the storage hooks tell the job signing (signatures.md R-SIG-053, R-SIG-054); internal/signing/attest
// implements it.
type AttestationWork interface {
	// Committed is a file a client or a fill stored in a format that attests something, in the commit's transaction.
	Committed(ctx context.Context, tx meta.Tx, repoID, itemID int64, fill bool) error
	// SignatureTagArrived is an older cosign signature tag of the repository recorded for the subjects, in the commit's transaction: it
	// answers the referrers of Quader's attestations of those subjects, which the hook removes in the same transaction (R-SIG-054).
	SignatureTagArrived(ctx context.Context, tx meta.Tx, repoID int64, subjects [][32]byte) ([]DisplacedReferrer, error)
}

// DisplacedReferrer is an attestation's referrer an older cosign signature tag displaced: the attested item and the digests of the
// referrer manifest and of the bundle.
type DisplacedReferrer struct {
	Path     string
	Blob     [32]byte
	Manifest [32]byte
	Bundle   [32]byte
}

// removeDisplaced removes the referrers an older cosign signature tag displaced, in the tag's commit transaction, as the system: cosign
// 3 reads an older tag only while no bundle stands among the referrers (R-SIG-054, Q-SIG-66).
func (d Deps) removeDisplaced(ctx context.Context, tx meta.Tx, repo formats.RepoRef, displaced []DisplacedReferrer) error {
	ar := formats.CapabilitiesFor(repo.Format).AttestationReferrer
	if ar == nil || len(displaced) == 0 {
		return nil
	}
	var rm ItemRemover
	if d.Remover != nil {
		rm = d.Remover()
	}
	if rm == nil {
		return errs.E(errs.Internal, errs.CodeInternal, "This host cannot remove an attestation's referrer.")
	}
	var paths []string
	for _, x := range displaced {
		paths = append(paths, ar.ReferrerPaths(formats.AttestationItem{Path: x.Path, Blob: x.Blob}, x.Manifest, x.Bundle)...)
	}
	return rm.RemoveIn(ctx, tx, items.RemoveSpec{
		Repo: items.RepoRef{ID: repo.ID, Name: repo.Name, Format: string(repo.Format), Class: string(repo.Class)}, Paths: paths, Actor: SigningActor,
	})
}

// AttestationSubjects asks the format of a repository which of its stored items Quader attests (signatures.md R-SIG-044), through this
// host: the job signing's question when it signs and when discovery runs. A format that is not registered attests nothing.
func (h *Host) AttestationSubjects(ctx context.Context, repo formats.RepoRef, its []formats.AttestationItem) ([]formats.AttestationSubject, error) {
	f, ok := h.o.lookup(string(repo.Format))
	if !ok {
		return formats.AttestNoneFor(its), nil
	}
	sa := f.Signatures()
	if sa == nil {
		return formats.AttestNoneFor(its), nil
	}
	out, err := sa.AttestationSubjects(ctx, h, repo, its)
	if err != nil {
		return nil, err
	}
	if len(out) != len(its) {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "A format answered "+strconv.Itoa(len(out))+" attestation subjects for "+strconv.Itoa(len(its))+" items.")
	}
	return out, nil
}

// ReferrerPlacement asks the format of a repository whether an attestation of an item may stand among its referrers now (R-SIG-054); a
// format without referrers serves every attestation through the API, with no reason to name.
func (h *Host) ReferrerPlacement(ctx context.Context, repo formats.RepoRef, it formats.AttestationItem) (bool, string, error) {
	ar := formats.CapabilitiesFor(repo.Format).AttestationReferrer
	if ar == nil {
		return false, "", nil
	}
	return ar.ReferrerPlacement(ctx, h, repo, it)
}

// PutReferrer stores an attestation's referrer of an item (R-SIG-054): the format builds its files, and each is committed through the
// item store as the system's write in a transaction of its own, the manifest last; the answer is the manifest's digest. An identical file
// at a path (the empty config another artifact stored) changes nothing.
func (h *Host) PutReferrer(ctx context.Context, ref formats.RepoRef, sub formats.AttestationSubject, it formats.AttestationItem,
	bundle []byte, predicateType string, created time.Time,
) ([32]byte, error) {
	ar := formats.CapabilitiesFor(ref.Format).AttestationReferrer
	if ar == nil || h.o.Repos == nil {
		return [32]byte{}, errs.E(errs.Internal, errs.CodeInternal, "The format of "+ref.Name.String()+" serves no attestation as a referrer.")
	}
	repo, err := h.o.Repos.ByName(ctx, ref.Name)
	if err != nil {
		return [32]byte{}, err
	}
	files, digest, err := ar.AttestationReferrer(sub, it, bundle, predicateType, created)
	if err != nil {
		return [32]byte{}, err
	}
	for _, f := range files {
		p, err := repository.CleanPath(f.Path)
		if err != nil {
			return [32]byte{}, err
		}
		sp, err := h.o.Items.Spool(ctx, bytes.NewReader(f.Body), items.IngestOptions{ContentLength: int64(len(f.Body)), MaxSize: int64(len(f.Body))})
		if err != nil {
			return [32]byte{}, err
		}
		_, err = h.o.Items.Commit(ctx, items.CommitSpec{
			Repo: repo, Path: p, Spooled: sp, MediaType: f.MediaType, Origin: items.OriginUpload, Actor: SigningActor, Payload: f.Payload,
		})
		_ = sp.Close()
		if err != nil {
			return [32]byte{}, err
		}
	}
	return digest, nil
}

// RemoveReferrer removes an attestation's referrer of an item in the caller's transaction (R-SIG-053, R-SIG-054): its manifest and its
// bundle's layer, as the system.
func (h *Host) RemoveReferrer(ctx context.Context, tx meta.Tx, repo formats.RepoRef, it formats.AttestationItem, manifest, bundle [32]byte) error {
	ar := formats.CapabilitiesFor(repo.Format).AttestationReferrer
	if ar == nil || h.o.Remover == nil {
		return nil
	}
	rm := h.o.Remover()
	if rm == nil {
		return errs.E(errs.Internal, errs.CodeInternal, "This host cannot remove items.")
	}
	return rm.RemoveIn(ctx, tx, items.RemoveSpec{
		Repo:  items.RepoRef{ID: repo.ID, Name: repo.Name, Format: string(repo.Format), Class: string(repo.Class)},
		Paths: ar.ReferrerPaths(it, manifest, bundle), Actor: SigningActor,
	})
}
