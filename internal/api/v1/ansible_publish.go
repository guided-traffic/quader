package v1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path"
	"strings"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/qname"
)

// RouteHandoff serves a request of the management API by a route of a format, with every check of the protocol path, and returns a
// refusal unwritten (dispatch.Dispatcher.Handoff, Q-ANS-140).
type RouteHandoff interface {
	Handoff(w http.ResponseWriter, r *http.Request, format string, key qname.Repo, method, rest string) error
}

// ansiblePublishRoute is the method and the path below the repository of the client's publish (R-ANS-008).
const ansiblePublishRoute = "v3/artifacts/collections/"

// maxTaskAnswer bounds what is kept of the answer of the publish route: its 202 body is the address of one task.
const maxTaskAnswer = 4 << 10

// PublishAnsibleCollection implements POST /api/v1/projects/{projectKey}/repositories/{repoKey}/ansible/collections (R-ANS-061): the
// screens' upload of a collection archive. The request is handed to the route of the client's publish, which reads the body as the
// scripted publish's raw archive and runs every check of R-ANS-008 with the caller's principal; the import task it made is read from the
// repository that holds it (the deployment target of a virtual repository) and answered.
func (s *Server) PublishAnsibleCollection(w http.ResponseWriter, r *http.Request, projectKey gen.ProjectKey, repoKey gen.RepoKey, _ gen.PublishAnsibleCollectionParams) {
	if s.o.Routes == nil {
		httpserver.Problem(w, r, unavailable("The publish of a collection from the screen"))
		return
	}
	store, holder, ok := s.ansibleTarget(w, r, projectKey, repoKey, true)
	if !ok {
		return
	}
	ans := &taskAnswer{header: http.Header{}}
	err := s.o.Routes.Handoff(ans, r, "ansible", qname.Repo{Project: projectKey, Key: repoKey}, http.MethodPost, ansiblePublishRoute)
	var id string
	if err == nil {
		id, err = ans.taskID()
	}
	if err == nil {
		t, found, terr := store.Task(r.Context(), holder.NumericID(), id)
		switch {
		case terr != nil:
			err = terr
		case !found:
			err = errs.E(errs.Internal, errs.CodeInternal, "The publish answered the task "+id+", which "+holder.Name.String()+" does not hold.")
		default:
			writeJSON(w, r, http.StatusCreated, ansibleTaskRep(t))
			return
		}
	}
	httpserver.Problem(w, r, errs.Wrap(err, "api.publishAnsibleCollection"))
}

// taskAnswer keeps the answer of the publish route: 202 with {"task": "<address of the import task>"} (R-ANS-008 step 5).
type taskAnswer struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (a *taskAnswer) Header() http.Header { return a.header }

func (a *taskAnswer) WriteHeader(code int) {
	if a.status == 0 {
		a.status = code
	}
}

func (a *taskAnswer) Write(p []byte) (int, error) {
	if a.status == 0 {
		a.status = http.StatusOK
	}
	if room := maxTaskAnswer - a.body.Len(); room > 0 {
		a.body.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// taskID is the id of the task the publish answered: the last segment of its address.
func (a *taskAnswer) taskID() (string, error) {
	var doc struct {
		Task string `json:"task"`
	}
	if a.status != http.StatusAccepted || json.Unmarshal(a.body.Bytes(), &doc) != nil {
		return "", errs.E(errs.Internal, errs.CodeInternal, "The publish of the collection gave no import task.")
	}
	id := path.Base(strings.TrimSuffix(doc.Task, "/"))
	if _, err := uuid.Parse(id); err != nil {
		return "", errs.E(errs.Internal, errs.CodeInternal, "The publish of the collection gave no import task.")
	}
	return id, nil
}
