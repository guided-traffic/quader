package edge

import (
	"context"
	"reflect"
	"testing"

	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// TestR_BAS_008_AForwardAndACheckAreDecidedOnTheCallersSubject: a request through a Bastion is decided as any other (R-AUTHZ-030,
// R-BAS-008, R-BAS-011). For every kind of caller, the subject of a recognized forward - and of the caller of a check, which the
// check decides on the caller's principal alone - is the subject of the same caller at the main installation's own door, so it is
// internal exactly when the direct request is; the authorization table of R-AUTHZ-030 (TestR_AUTHZ_030_WhoReadsByEachLevel) is
// its answer through a Bastion as well.
func TestR_BAS_008_AForwardAndACheckAreDecidedOnTheCallersSubject(t *testing.T) {
	session := httpserver.Credential{Method: "session", SessionID: "s1"}
	bearer := func(id string) httpserver.Credential { return httpserver.Credential{Method: "bearer", TokenID: id} }
	callers := map[string]httpserver.Principal{
		"a user through a session":                       {Kind: httpserver.KindUser, ID: "u1", Name: "dana", Credential: session},
		"a user through a personal token":                {Kind: httpserver.KindUser, ID: "u1", Name: "dana", Credential: bearer("t1")},
		"an external user through a personal token":      {Kind: httpserver.KindUser, ID: "u2", Name: "erin", External: true, Credential: bearer("t2")},
		"a technical token of a project service account": {Kind: httpserver.KindServiceAccount, ID: "sa1", Name: "web/partner", Credential: bearer("t3")},
		"a CI token":            {Kind: httpserver.KindCI, Name: "ci", Mappings: []string{"ci:gitlab/acme"}, Credential: bearer("t4")},
		"the anonymous subject": {Anonymous: true, Kind: httpserver.KindAnonymous},
	}
	for name, p := range callers {
		t.Run("R-BAS-008 "+name, func(t *testing.T) {
			direct := SubjectOf(httpserver.WithPrincipal(context.Background(), p))
			forward := SubjectOf(bastionmain.WithRecognition(httpserver.WithPrincipal(context.Background(), p), bastionmain.Recognition{Bastion: "partners"}))
			if !reflect.DeepEqual(direct, forward) {
				t.Errorf("the forward's subject %+v differs from the direct one %+v", forward, direct)
			}
			if direct.Internal() != forward.Internal() {
				t.Errorf("internal: direct %v, through a Bastion %v", direct.Internal(), forward.Internal())
			}
			if want := !p.Anonymous && !p.External; direct.Internal() != want {
				t.Errorf("internal = %v, want %v", direct.Internal(), want)
			}
		})
	}
}
