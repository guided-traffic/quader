package app

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/guided-traffic/quader/internal/auth"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// rulesStage records the rules it was asked with.
type rulesStage struct{ rules *auth.EndpointRules }

func (s rulesStage) Authenticate(_ *http.Request, ep auth.EndpointRules) (auth.Result, error) {
	*s.rules = ep
	return auth.Result{
		Principal:  principal.Principal{ID: "u-caller", Kind: principal.KindUser, Name: "checked-caller", Status: principal.StatusActive},
		Credential: auth.Credential{Method: auth.MethodBearer, TokenID: "t-caller"},
	}, nil
}

// TestR_BAS_052_TheCheckDecidesItsPathsAsAReplicaThatServesThePackageFamilies: the check presents a registry token below
// /v2/ and every other credential below /api/ (R-BAS-052); the stage it runs them through treats those paths as served,
// so a replica of the role api, which answers the interface but serves neither family, does not read them as unknown
// paths (Q-ARCH-302) and verifies a registry token there. The chain's own rule for every other request is untouched.
func TestR_BAS_052_TheCheckDecidesItsPathsAsAReplicaThatServesThePackageFamilies(t *testing.T) {
	var asked auth.EndpointRules
	a := &App{cfg: &config.Config{}, parts: &wiring{}}
	a.parts.put(reflect.ValueOf(authArea{stage: rulesStage{rules: &asked}}))
	ep, err := httpserver.NewEndpoint(config.ResolvedEndpoint{Name: "default", Default: true, Surface: []string{config.SurfaceAPI}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rt := &endpointRuntime{cfg: config.ResolvedEndpoint{Name: "default", Default: true}, ep: ep}
	if _, err := a.bastionCheckStage(rt)(httptest.NewRequest(http.MethodGet, "/v2/bastion-check", nil)); err != nil {
		t.Fatal(err)
	}
	if asked.Served != nil {
		t.Fatal("the check's stage reads its paths through the process's surface")
	}
	if _, err := a.credentialStage(rt)(httptest.NewRequest(http.MethodGet, "/v2/bastion-check", nil)); err != nil {
		t.Fatal(err)
	}
	if asked.Served == nil || asked.Served(httptest.NewRequest(http.MethodGet, "/v2/bastion-check", nil)) {
		t.Fatal("the chain's stage of a replica that serves no package family serves /v2/")
	}
}
