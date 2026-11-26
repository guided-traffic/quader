//go:build integration

package bastionreg

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/httpserver"
)

// TestR_BAS_017_EachRegistrationNamesTheNetworksItsBastionConnectsFrom (Q-BAS-334, Q-BAS-430, Q-BAS-431): partners is created with
// its network, edge without one and is given its own by PATCH, which a caller with endpoints:read is refused, /0 is refused at the
// entry, and the change raises the revision and records bastion.updated with the lists before and after. The replica's snapshot
// follows the change area; then a forward with edge's token from partners' network is edge's request, served, but the caller's
// address it forwards does not count: the request is the Bastion's own, from its address. From edge's own network it counts.
func TestR_BAS_017_EachRegistrationNamesTheNetworksItsBastionConnectsFrom(t *testing.T) {
	e := newEnv(t)
	r, c := e.newCast("web-1", replicaOpts{})
	p := r.call("POST", bastions, c.admin, map[string]any{"name": "partners", "egress": []string{"198.51.100.16/28"}})
	want(t, p, http.StatusCreated, "")
	if got := fmt.Sprint(p.json(t)["egress"]); got != "[198.51.100.16/28]" {
		t.Fatalf("partners' networks: %s", got)
	}
	created := r.call("POST", bastions, c.admin, map[string]any{"name": "edge"})
	want(t, created, http.StatusCreated, "")
	if got := fmt.Sprint(created.json(t)["egress"]); got != "[]" {
		t.Fatalf("a registration created without networks: %s", got)
	}
	edgeID := e.text(`SELECT id::text FROM principal WHERE kind = 'bastion' AND name = 'edge'`)
	snap := bastionmain.NewRegistrationSnapshot(r.db, r.caches, quiet)
	if err := snap.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = snap.Stop(context.Background()) })

	want(t, r.call("PATCH", bastions+"/edge", c.auditor, map[string]any{"egress": []string{"203.0.113.0/28"}}, "If-Match", "*"), http.StatusForbidden, "")
	wide := r.call("PATCH", bastions+"/edge", c.admin, map[string]any{"egress": []string{"203.0.113.0/28", "0.0.0.0/0"}}, "If-Match", "*")
	want(t, wide, http.StatusUnprocessableEntity, "")
	if !strings.Contains(string(wide.Body), `"/egress/1"`) {
		t.Fatalf("the refusal of /0 does not point at the entry: %s", wide.Body)
	}
	set := r.call("PATCH", bastions+"/edge", c.admin, map[string]any{"egress": []string{"203.0.113.0/28", "2001:db8:4::1"}}, "If-Match", created.Header.Get("ETag"))
	want(t, set, http.StatusOK, "")
	if got := fmt.Sprint(set.json(t)["egress"]); got != "[203.0.113.0/28 2001:db8:4::1/128]" || set.Header.Get("ETag") == created.Header.Get("ETag") {
		t.Fatalf("after the PATCH: %s, ETag %q", got, set.Header.Get("ETag"))
	}
	upd := e.events(audit.EventBastionUpdated)
	if len(upd) != 1 || upd[0].ResourceID != edgeID || fmt.Sprint(upd[0].Details["fields"]) != "[egress]" ||
		fmt.Sprint(upd[0].Details["egressBefore"]) != "[]" || fmt.Sprint(upd[0].Details["egressAfter"]) != "[203.0.113.0/28 2001:db8:4::1/128]" {
		t.Fatalf("bastion.updated = %+v", upd)
	}
	waitFor(t, "the snapshot of edge's networks", 5*time.Second, func() bool { return len(snap.Egress(edgeID)) == 2 })

	tok, err := r.regs.CreateToken(context.Background(), systemCaller, "edge", bastionmain.TokenRequest{Via: "admin-cli"})
	if err != nil {
		t.Fatal(err)
	}
	h := forwardChain(t, r, snap)
	if got := forward(t, h, "198.51.100.20", tok.Secret); got != "198.51.100.20" {
		t.Errorf("edge's token from partners' network: the client is %s, want the address the request came from", got)
	}
	if got := forward(t, h, "203.0.113.5", tok.Secret); got != "192.0.2.1" {
		t.Errorf("edge's token from its own network: the client is %s, want the forwarded caller", got)
	}
}

// forwardChain is the default endpoint's chain as the composition root builds it for a Bastion's forwards (app.bastionStages): the
// recognition over the replica's real authenticator and the snapshot, and the re-derivation of app.bastionForwarded with the
// networks of the request's own registration. The handler answers the client address the chain resolved.
func forwardChain(t *testing.T, r *replica, snap *bastionmain.RegistrationSnapshot) http.Handler {
	t.Helper()
	resolver, err := httpserver.NewResolver(nil, baseURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	router := httpserver.NewClientRouter()
	router.HandleFunc("GET /api/npm/{projectKey}/{repoKey}/{rest...}", func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte(httpserver.InfoFrom(req.Context()).Client.String()))
	})
	return httpserver.Chain{
		Router: router, Resolver: resolver, Log: quiet,
		Bastion: bastionmain.Recognize(bastionmain.RecognizeOptions{
			Tokens: bastionmain.NewAuthenticator(r.tokens), Egress: snap.Egress,
			Source: func(req *http.Request) (string, string) {
				src := httpserver.SourceFrom(req.Context())
				return src.ClientAddr, src.UserAgent
			},
			Forwarded: func(req *http.Request) context.Context {
				if rec, _ := bastionmain.RecognitionFrom(req.Context()); rec.FromEgress {
					return httpserver.WithResolved(req.Context(), resolver.AlsoTrusting(snap.Egress(rec.BastionID)).Resolve(req))
				}
				return req.Context()
			},
			Problem: func(w http.ResponseWriter, req *http.Request, err error) { errs.Write(w, req, err, "", nil) },
		}),
	}.Handler()
}

// forward sends a Bastion's forward of a caller at 192.0.2.1 from peer and returns the client address the handler saw.
func forward(t *testing.T, h http.Handler, peer, bst string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/npm/it/npm-all/lodash", nil)
	req.RemoteAddr = peer + ":40000"
	req.Header.Set("X-Forwarded-For", "192.0.2.1")
	req.Header.Set(iface.HeaderAuthorization, "Bearer "+bst)
	req.Header.Set(iface.HeaderInterface, strconv.Itoa(iface.InterfaceVersion))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("the forward from %s: %d %s", peer, w.Code, w.Body)
	}
	return w.Body.String()
}
