package v1

import (
	"net/http"
	"path"
	"strconv"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/files"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/items"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/signing/bundle"
)

// GetAttestation implements GET /api/v1/projects/{projectKey}/repositories/{repoKey}/attestation (signatures.md R-SIG-054, R-SIG-055):
// the bundle of Quader's receipt attestation of a stored file, as a download <file>.quader.sigstore.json. The file is decided as the
// item (R-STOR-016): a path the caller may not read answers 404, as a folder and a file without an attestation do.
func (s *Server) GetAttestation(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKeyText gen.RepoKey, params gen.GetAttestationParams) {
	ctx := r.Context()
	repoKey := qname.Repo{Project: projectKey, Key: repoKeyText}
	sv := s.o.Signing
	var err error
	if sv.Attestations == nil {
		err = unavailable("The receipt attestations")
	}
	var svc *files.Service
	if err == nil {
		svc, err = s.filesService()
	}
	var p string
	if err == nil {
		p, err = CheckItemPath(params.Path, PathOptions{})
	}
	var it items.Item
	if err == nil {
		var c files.Caller
		if c, err = s.filesCaller(r, repoKey); err == nil {
			it, err = svc.ItemInfo(ctx, c, repoKey, p)
		}
	}
	if err == nil && it.Kind != items.KindFile {
		err = errs.E(errs.NotFound, errs.CodeNotFound, "A folder has no attestation.")
	}
	var raw []byte
	if err == nil {
		var found bool
		raw, found, err = sv.Attestations.Attestation(ctx, it.RepoID, it.Path)
		if err == nil && !found {
			err = errs.E(errs.NotFound, errs.CodeNotFound, "Quader has not attested this file (yet).")
		}
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", bundle.MediaType)
	h.Set("Content-Disposition", `attachment; filename=`+strconv.Quote(path.Base(it.Path)+bundle.FileSuffix))
	h.Set("Cache-Control", "private, no-cache")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}
