package v1

import (
	"net/http"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/ansible/rows"
	"github.com/guided-traffic/quader/internal/formats/ansible/version"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// maxSignatureBody bounds the body of a posted signature: the armored signature of at most 64 KiB and its JSON.
const maxSignatureBody = 80 << 10

type ansibleSignature struct {
	Fingerprint string `json:"fingerprint"`
	Signature   string `json:"signature"`
}

// AddAnsibleVersionSignature implements POST
// /api/v1/projects/{projectKey}/repositories/{repoKey}/ansible/collections/{collection}/versions/{version}/signatures (signatures.md
// R-SIG-031): a detached signature of the version's MANIFEST.json, checked as far as no key is needed and stored for signatures[] of
// the version document and for the signature store's check.
func (s *Server) AddAnsibleVersionSignature(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey gen.RepoKey, collection, ver string) {
	store, repo, ok := s.ansibleTarget(w, r, projectKey, repoKey, false)
	if !ok {
		return
	}
	if !version.Valid(ver) {
		httpserver.Problem(w, r, errs.E(errs.Invalid, errs.CodeInvalid, "The version is not a semantic version."))
		return
	}
	var in gen.AnsibleSignatureInput
	err := DecodeJSON(w, r, &in, maxSignatureBody)
	if err == nil && in.Signature == "" {
		err = errs.E(errs.Invalid, errs.CodeInvalid, `The body is {"signature": "<armored OpenPGP signature of MANIFEST.json>"}.`)
	}
	var got rows.PostedSignature
	if err == nil {
		got, err = store.AddSignature(r.Context(), rows.SignatureInput{
			RepoID: repo.NumericID(), Name: collection, Version: ver, Signature: in.Signature, Audit: ansibleAudit(r, repo),
		})
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.addAnsibleVersionSignature"))
		return
	}
	writeJSON(w, r, http.StatusCreated, ansibleSignature{Fingerprint: got.Fingerprint, Signature: got.Signature})
}

// RemoveAnsibleVersionSignature implements DELETE
// /api/v1/projects/{projectKey}/repositories/{repoKey}/ansible/collections/{collection}/versions/{version}/signatures/{fingerprint}
// (R-SIG-031).
func (s *Server) RemoveAnsibleVersionSignature(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey gen.RepoKey, collection, ver, fingerprint string) {
	store, repo, ok := s.ansibleTarget(w, r, projectKey, repoKey, false)
	if !ok {
		return
	}
	if !version.Valid(ver) {
		httpserver.Problem(w, r, errs.E(errs.Invalid, errs.CodeInvalid, "The version is not a semantic version."))
		return
	}
	found, err := store.RemoveSignature(r.Context(), repo.NumericID(), collection, ver, fingerprint, ansibleAudit(r, repo))
	if err == nil && !found {
		err = errs.E(errs.NotFound, errs.CodeNotFound, "The version "+version.Number(ver)+" of "+collection+" holds no signature of the key "+fingerprint+".")
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.removeAnsibleVersionSignature"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
