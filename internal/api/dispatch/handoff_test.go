package dispatch

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
)

// handoff hands a request of the management API to the probe's route on a repository and returns the recorder and the error.
func (r *rig) handoff(key, method, rest string) (*httptest.ResponseRecorder, error) {
	r.t.Helper()
	rec := httptest.NewRecorder()
	err := r.d.Handoff(rec, httptest.NewRequest("POST", "/api/v1/projects/it/repositories/"+key+"/probe", nil), "probe", qr(key), method, rest)
	return rec, err
}

// TestR_ANS_061_TheHandoffRunsTheRouteWithEveryCheckAndReturnsItsRefusals: a request of the management API that a format's route serves
// passes the coarse decision of the route's action and the repository's checks, a write to a virtual repository goes to its deployment
// target, the route's answer reaches the writer, and every refusal is returned unwritten for the management API's own shape (Q-ANS-140).
func TestR_ANS_061_TheHandoffRunsTheRouteWithEveryCheckAndReturnsItsRefusals(t *testing.T) {
	t.Run("a served write reaches the handler and its answer the writer; the request keeps its label", func(t *testing.T) {
		r := newRig(t)
		rec, err := r.handoff("local", "PUT", "left-pad/-/x.tgz")
		if err != nil || rec.Code != 204 {
			t.Fatalf("answer %d, error %v", rec.Code, err)
		}
		if strings.Join(r.reached, ",") != "probe.file.put it/local left-pad" || len(r.labels) != 0 {
			t.Fatalf("reached %v, labels %v", r.reached, r.labels)
		}
		if got := r.decider.asked[0]; got.Repository != qr("local") || len(got.Actions) != 1 || got.Actions[0] != authz.Deploy {
			t.Fatalf("the coarse coordinate: %+v", got)
		}
	})
	t.Run("a virtual repository's write is its deployment target's", func(t *testing.T) {
		r := newRig(t)
		if _, err := r.handoff("virtual-with-target", "PUT", "left-pad/-/x.tgz"); err != nil || strings.Join(r.reached, ",") != "probe.file.put it/local left-pad" {
			t.Fatalf("reached %v, error %v", r.reached, err)
		}
		if _, err := r.handoff("virtual", "PUT", "left-pad/-/x.tgz"); codeOf(err) != errs.CodeNoDeployTarget {
			t.Fatalf("without a target: %v", err)
		}
	})
	t.Run("a refusal is returned and nothing is written", func(t *testing.T) {
		r := newRig(t)
		r.decider.decision = authz.Decision{Outcome: authz.DenyVisible}
		for _, c := range []struct{ key, method, rest, want string }{
			{"local", "PUT", "left-pad/-/x.tgz", errs.CodeForbidden},
			{"local", "PUT", "a/../b", errs.CodeInvalidPath},
			{"local", "PATCH", "left-pad/-/x.tgz", errs.CodeMethodNotAllowed},
		} {
			rec, err := r.handoff(c.key, c.method, c.rest)
			if codeOf(err) != c.want || rec.Body.Len() != 0 || len(rec.Header()) != 0 {
				t.Errorf("%s %s %s: error %v, written %d %v %q", c.key, c.method, c.rest, err, rec.Code, rec.Header(), rec.Body)
			}
		}
		r.decider.decision = authz.Decision{Outcome: authz.Allow}
		for key, want := range map[string]string{"off": errs.CodeRepositoryDisabled, "other": errs.CodeRepositoryNotFound, "dying": errs.CodeRepositoryNotFound} {
			if _, err := r.handoff(key, "PUT", "left-pad/-/x.tgz"); codeOf(err) != want {
				t.Errorf("%s: %v, want %s", key, err, want)
			}
		}
		if len(r.reached) != 0 {
			t.Fatalf("a refused request reached the handler: %v", r.reached)
		}
		if err := r.d.Handoff(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), "bare", qr("other"), "PUT", "x"); codeOf(err) != errs.CodeNotApplicable {
			t.Fatalf("a format without routes: %v", err)
		}
	})
}

func codeOf(err error) string {
	if e, ok := errs.As(err); ok {
		return e.Code
	}
	return ""
}
