package registry

import (
	"net/http"
	"slices"

	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/bastionmain"
)

// tellRoute tells the marking stage of a recognized Bastion request the kind of a core route below /v2/ before the route
// authenticates or reads a body (R-BAS-014, R-BAS-051): the token service and the catalog are Volatile reads, the token
// service's POST a read route that takes a body. The dispatcher tells the marking the repository routes below /v2/ itself.
// methods are the methods the route answers; for any other the route answers its own 405 and the marking is told nothing,
// so the probe of a method no core route serves is not taken for a read route that takes a body (Q-BAS-189). handled
// reports that the marking answered the request (the 204 of a probe); a non-nil error is the route's to render. Outside a
// Bastion request it does nothing.
func tellRoute(w http.ResponseWriter, r *http.Request, format, name string, methods ...string) (handled bool, err error) {
	obs := bastionmain.ObserverFrom(r.Context())
	if obs == nil || !slices.Contains(methods, r.Method) {
		return false, nil
	}
	return obs.Matched(w, r, bastionmain.RouteMatch{
		Format: format, Name: name, Method: r.Method, Kind: bastionmain.KindVolatile, Action: authz.Read,
	})
}
