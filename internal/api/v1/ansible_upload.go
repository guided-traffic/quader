package v1

import (
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// GetAnsibleImportTask implements GET /api/v1/projects/{projectKey}/repositories/{repoKey}/ansible/imports/{taskId} (R-ANS-009,
// R-ANS-061): one task of a local repository, of the deployment target for a virtual one.
func (s *Server) GetAnsibleImportTask(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey gen.RepoKey, taskID openapi_types.UUID) {
	store, holder, ok := s.ansibleTarget(w, r, projectKey, repoKey, true)
	if !ok {
		return
	}
	t, found, err := store.Task(r.Context(), holder.NumericID(), taskID.String())
	if err == nil && !found {
		err = errs.E(errs.NotFound, errs.CodeNotFound, "No import task "+taskID.String()+" exists in this repository; a task is kept for a limited time.")
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.getAnsibleImportTask"))
		return
	}
	writeJSON(w, r, http.StatusOK, ansibleTaskRep(t))
}
