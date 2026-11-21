package formathost

import (
	"context"
	"strconv"

	"github.com/guided-traffic/quader/internal/api/edge"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/files"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/malware/gate"
	"github.com/guided-traffic/quader/internal/repository"
	sgate "github.com/guided-traffic/quader/internal/signatures/gate"
	"github.com/guided-traffic/quader/internal/vuln/enforce"
)

// The gate per repository of the signatures at the serve point of the format host (signatures.md R-SIG-064 to R-SIG-069): every stored
// item a format serves passes itemStore.Serve, a format's routes, a remote repository's hit and cache view and every tier of a virtual
// repository's resolution alike, and there the malware gate, the vulnerability block and the signature gate are decided together before
// one wait. Unlike the malware gate the signature gate is asked whatever the route's kind: the format's Gate names what is an artifact,
// so an OCI manifest by tag is judged. A stored document a format serves without the item store (a remote OCI repository's tag) is
// judged through Host.Signatures().Refuse.

// ProvenanceVerdicts is what the trust step answers a format before it serves a provenance bundle (formats.SignatureVerdicts'
// PublicProvenance, R-SIG-028); the composition root binds the signature store.
type ProvenanceVerdicts interface {
	PublicProvenance(ctx context.Context, repo formats.RepoRef, paths []string) (map[string]string, error)
}

// hostSignatures implements formats.SignatureVerdicts: the verdicts the composition root wired, and the gate of a stored document.
type hostSignatures struct{ h *Host }

var _ formats.SignatureVerdicts = hostSignatures{}

// PublicProvenance implements [formats.SignatureVerdicts]: nothing is vouched for on a replica without the trust step.
func (s hostSignatures) PublicProvenance(ctx context.Context, repo formats.RepoRef, paths []string) (map[string]string, error) {
	if s.h.o.Verdicts == nil {
		return formats.NoSignatureVerdicts.PublicProvenance(ctx, repo, paths)
	}
	return s.h.o.Verdicts.PublicProvenance(ctx, repo, paths)
}

// Refuse implements [formats.SignatureVerdicts] (R-SIG-064): the gates that apply to the request judge the document as an item at the
// path with the blob, and a pending one waits alone (no other check of R-SIG-069 asks about a metadata document). The image scanning's
// pull asks no gate.
func (s hostSignatures) Refuse(ctx context.Context, rq *formats.Request, repo formats.RepoRef, path string, digest [32]byte) error {
	h := s.h
	if systemRead(rq) || !h.o.SignatureGate.Enabled() {
		return nil
	}
	d, ok, err := h.gateDownload(ctx, rq, repo, path, digest, false)
	if err != nil || !ok {
		return err
	}
	return h.o.SignatureGate.Check(ctx, d)
}

// serveChecks applies the three checks of a stored item about to be served (R-SIG-069): after the format's own serve-time check, so that
// a version the caller may not read stays the format's 404, for GET, HEAD, conditional and Range requests alike. A refusal or a pending
// answer ends the request and, in a virtual repository, the resolution (R-MAL-018, R-SIG-065).
func (h *Host) serveChecks(ctx context.Context, rq *formats.Request, repo formats.RepoRef, row items.Item) error {
	srv := sgate.Serve{}
	if h.o.Enforce != nil && row.Kind == items.KindFile {
		srv.Enforce = h.o.Enforce
		srv.VulnOf = enforce.Download{
			Addressed: rq.Addressed().String(), Store: repo.Name.String(), RepoID: strconv.FormatInt(repo.ID, 10), Format: string(repo.Format),
			Path: row.Path, VersionID: row.VersionID, Stored: true, Ecosystem: row.Policy.Ecosystem,
			Block: h.o.Enforce.Decide(row.Policy, row.Block, row.VersionID),
			Actor: edge.ActorFrom(httpserver.PrincipalFrom(ctx)), Source: httpserver.SourceFrom(ctx), Endpoint: rq.Subject.Endpoint,
		}
	}
	if g := h.o.Malware; g.Enabled() && row.Kind == items.KindFile && gatesDownload(rq) {
		strict, err := h.malwareStrict(ctx, rq.Addressed(), repo.Name)
		if err != nil {
			return err
		}
		srv.Malware = g
		srv.MalwareOf = gate.Download{
			Addressed: rq.Addressed(), HolderID: repo.ID, Path: row.Path, Digest: row.Digest, Size: row.Size, Generated: row.Generated,
			System:  rq.Repo.Upstream.System(),
			Verdict: row.Malware, Strict: strict, Arrived: httpserver.InfoFrom(ctx).Received,
			Actor: edge.ActorFrom(httpserver.PrincipalFrom(ctx)), Source: httpserver.SourceFrom(ctx), Endpoint: rq.Subject.Endpoint,
		}
	}
	if h.o.SignatureGate.Enabled() && row.Kind == items.KindFile {
		d, ok, err := h.gateDownload(ctx, rq, repo, row.Path, row.Digest, row.Generated)
		if err != nil {
			return err
		}
		if ok {
			srv.Signatures, srv.SignaturesOf = h.o.SignatureGate, d
		}
	}
	return srv.Run(ctx)
}

// gateDownload builds the signature gate's view of a download: the addressed repository and the holder from the replica's snapshot, and
// for a virtual addressed repository its members the caller may read (R-SIG-065). ok is false when no gate applies, which costs no row.
func (h *Host) gateDownload(ctx context.Context, rq *formats.Request, holder formats.RepoRef, path string, blob [32]byte, generated bool) (sgate.Download, bool, error) {
	g := h.o.SignatureGate
	if h.o.Repos == nil {
		return sgate.Download{}, false, nil
	}
	held, err := h.o.Repos.ByName(ctx, holder.Name)
	if err != nil {
		return sgate.Download{}, false, err
	}
	addressed := held
	if a := rq.Addressed(); !a.IsZero() && a != holder.Name {
		if addressed, err = h.o.Repos.ByName(ctx, a); err != nil {
			return sgate.Download{}, false, err
		}
	}
	hr, ar := files.GateRepo(held), files.GateRepo(addressed)
	if !g.Applies(ar, hr) {
		return sgate.Download{}, false, nil
	}
	d := sgate.Download{
		Addressed: ar, Holder: hr, Path: path, Blob: blob, Generated: generated, Arrived: httpserver.InfoFrom(ctx).Received,
		Actor: edge.ActorFrom(httpserver.PrincipalFrom(ctx)), Source: httpserver.SourceFrom(ctx), Endpoint: rq.Subject.Endpoint,
	}
	if addressed.Class == repository.Virtual && ar.Setting.OnlyVerified {
		d.Members = h.readableMembers(ctx, rq, addressed, path)
	}
	return d, true, nil
}

// readableMembers are the members of a virtual repository the request's subject may read the path in (the badge's rule, R-SIG-027):
// the attachments its own gate reads (R-SIG-065).
func (h *Host) readableMembers(ctx context.Context, rq *formats.Request, v repository.Repository, path string) []sgate.Repo {
	var pkg string
	if f, ok := h.o.Formats(v.Format); ok {
		pkg = f.Classify(formats.Path{Path: path}).Package
	}
	coord := coordinate(rq, formats.Target{Package: pkg, Path: path})
	var out []sgate.Repo
	for _, m := range v.Members() {
		if rq.Index != nil {
			if usable, _ := h.o.Decider.MemberUsableOn(ctx, rq.Index, rq.Subject, m.Name, coord); !usable {
				continue
			}
		}
		out = append(out, files.GateRepo(m))
	}
	return out
}

// signatureGateHolds reports whether a signature gate applies to a request of the addressed and the storing repository: the remote
// engine holds a fill for it (R-SIG-067), as where the malware gate is strict.
func (h *Host) signatureGateHolds(ctx context.Context, rq *formats.Request) (bool, error) {
	g := h.o.SignatureGate
	if !g.Enabled() || h.o.Repos == nil || systemRead(rq) {
		return false, nil
	}
	held, err := h.o.Repos.ByName(ctx, rq.Repo.Name)
	if err != nil {
		if errs.KindOf(err) == errs.NotFound {
			return false, nil
		}
		return false, err
	}
	addressed := held
	if a := rq.Addressed(); !a.IsZero() && a != rq.Repo.Name {
		if addressed, err = h.o.Repos.ByName(ctx, a); err != nil {
			if errs.KindOf(err) == errs.NotFound {
				addressed = held
			} else {
				return false, err
			}
		}
	}
	return g.Applies(files.GateRepo(addressed), files.GateRepo(held)), nil
}

// GateClassify is the signature gate's classifier (sgate.Classifier): the item's format names what the gate judges of it
// (formats.SignatureAdapter.Gate, R-SIG-063). A format that is not registered, or an answer of the wrong length, is an artifact (fail
// closed).
func (h *Host) GateClassify(ctx context.Context, repo sgate.Repo, path string, blob [32]byte) (sgate.Subject, error) {
	artifact := sgate.Subject{Kind: sgate.KindArtifact}
	f, ok := h.o.Formats(repo.Format)
	if !ok {
		return artifact, nil
	}
	sa := f.Signatures()
	if sa == nil {
		return artifact, nil
	}
	ref := formats.RepoRef{ID: repo.ID, Name: repo.Name, Class: formats.Class(repo.Class), Format: formats.ID(repo.Format)}
	subs, err := sa.Gate(ctx, h, ref, []formats.GateItem{{Path: path, Blob: blob}})
	if err != nil {
		return sgate.Subject{}, err
	}
	if len(subs) != 1 {
		return artifact, nil
	}
	out := sgate.Subject{Covering: subs[0].Covering}
	switch subs[0].Kind {
	case formats.GateArtifact:
		out.Kind = sgate.KindArtifact
	case formats.GateAttachment:
		out.Kind = sgate.KindAttachment
	case formats.GateSignatureFile:
		out.Kind = sgate.KindSignatureFile
	default:
		out.Kind = sgate.KindNone
	}
	return out, nil
}
