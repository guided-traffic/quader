package v1

import (
	"context"
	"net/http"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/ansible/rows"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

// The management operations of the Ansible Galaxy format (R-ANS-009, R-ANS-012, R-ANS-026). The permission of each is declared in
// api/openapi.yaml and enforced before the handler runs. The handlers are thin: the data port (internal/formatdata/ansible) reads,
// writes and records.

// AnsibleStore is the part of the Ansible Galaxy data port the management operations use.
type AnsibleStore interface {
	Tasks(ctx context.Context, repoID int64, f rows.TaskFilter, after string, limit int) ([]rows.Task, error)
	Tombstones(ctx context.Context, repoID int64, after string, limit int) ([]rows.Tombstone, error)
	ClearTombstone(ctx context.Context, repoID int64, name, version string, a rows.Audit) (bool, error)
	// The reads of the documentation operations (R-ANS-036): rows only, never an upstream.
	DocState(ctx context.Context, repoID int64, name, version string) (rows.DocState, error)
	DocTree(ctx context.Context, repoID int64, name, version string, q *rows.DocQuery) (rows.DocTree, error)
	DocEntry(ctx context.Context, repoID int64, name, version, typ, entry string) (rows.DocEntry, bool, error)
	DocFile(ctx context.Context, repoID int64, name, version, file string) (rows.DocFile, bool, error)
	DocChangelog(ctx context.Context, repoID int64, name, version string) (rows.DocChangelog, bool, error)
	// SearchDocs is one page of the documentation searched across collections (R-ANS-058).
	SearchDocs(ctx context.Context, q rows.DocSearch) ([]rows.DocHit, error)
	// DocCandidates lists the versions of the repositories that hold an entry the query matches, through the search index, at most
	// limit (R-ANS-058, Q-ANS-127).
	DocCandidates(ctx context.Context, repoIDs []int64, query string, limit int) ([]rows.DocVersionKey, error)
	// The signatures a client posts for a version of a local repository (signatures.md R-SIG-031).
	AddSignature(ctx context.Context, in rows.SignatureInput) (rows.PostedSignature, error)
	RemoveSignature(ctx context.Context, repoID int64, name, version, fingerprint string, a rows.Audit) (bool, error)
}

// ansibleTarget resolves the repository of an operation: the data port, then the repository of the path. A repository that is not an
// Ansible Galaxy repository, or whose class the operation does not serve, answers 405 not_applicable, as the reindex does. A virtual
// repository serves what its deployment target holds when virtual is set (the import tasks, R-ANS-024).
func (s *Server) ansibleTarget(w http.ResponseWriter, r *http.Request, project, repo string, virtual bool) (AnsibleStore, repository.Repository, bool) {
	store := s.o.Ansible
	if store == nil || s.o.Repositories.Reader == nil {
		httpserver.Problem(w, r, unavailable("The Ansible Galaxy repository operations"))
		return nil, repository.Repository{}, false
	}
	name := qname.Repo{Project: project, Key: repo}
	v, err := s.o.Repositories.Reader.ByName(r.Context(), name)
	if err == nil && virtual && v.Format == "ansible" && v.Class == repository.Virtual {
		if t, ok := v.DeployTarget(); ok {
			v = t
		} else {
			err = errs.E(errs.MethodNotAllowed, errs.CodeNoDeployTarget, "The virtual repository "+name.String()+" has no deployment target, so it has no import tasks.")
		}
	}
	if err == nil && (v.Format != "ansible" || v.Class != repository.Local) {
		err = errs.E(errs.MethodNotAllowed, errs.CodeNotApplicable,
			"The operation applies to a local Ansible Galaxy repository; "+name.String()+" is a "+string(v.Class)+" "+v.Format+" repository.")
	}
	if err != nil {
		httpserver.Problem(w, r, errs.Wrap(err, "api.ansible"))
		return nil, repository.Repository{}, false
	}
	return store, v, true
}

func ansibleAudit(r *http.Request, repo repository.Repository) rows.Audit {
	a := ActorFrom(httpserver.PrincipalFrom(r.Context()))
	return rows.Audit{
		RepoKey: repo.Name.String(),
		Actor:   rows.Actor{Kind: a.Kind, ID: a.ID, Name: a.Name, TokenID: a.TokenID, AuthMethod: a.AuthMethod},
	}
}
