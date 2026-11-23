package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/cache"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/errs"
)

// grants is an Access that holds the named permissions and refuses every other one.
type grants map[string]bool

func (g grants) Require(_ context.Context, permission string) error {
	if g[permission] {
		return nil
	}
	return errs.E(errs.Forbidden, errs.CodeForbidden, "not held: "+permission)
}

const (
	factCaller = "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a10" // alice, the caller of authed
	factAdmin  = "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a20" // bob, an administrator
	factOwn    = "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a31" // a token of alice
	factOther  = "0192f0c4-7a7e-7b51-9b1e-2f0d6c9e1a32" // a token of bob
)

// tokenRow is one row of IdentityListTokens and IdentityTokenRow, which share their columns.
func tokenRow(id, ownerID, ownerName string) []any {
	at := pgtype.Timestamptz{Time: time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC), Valid: true}
	none := pgtype.Timestamptz{}
	return []any{
		id, "pat", "qdr_pat_ab", ownerID, "user", ownerName, "ci", json.RawMessage(`{"inherit":true,"admin":true,"repositories":[],"system":[]}`),
		true, at, ownerName, "api", none, none, "", "", "", "", none, "", false, json.RawMessage(`null`), "", "",
	}
}

func adminFactSetup(t *testing.T, held grants) setup {
	t.Helper()
	store := &overviewStore{db: overviewDB{rows: map[string][][]any{
		"IdentityListTokens":     {tokenRow(factOther, factAdmin, "bob"), tokenRow(factOwn, factCaller, "alice")},
		"IdentityTokenRow":       {tokenRow(factOther, factAdmin, "bob")},
		"IdentityAdminHolderIDs": {{factAdmin}},
	}}}
	return newSetup(t, func(o *Options) {
		o.Access = held
		o.API.Store = store
		o.API.Config = apiConfigForTest()
		o.Auth.Manager = principal.NewManager(principal.ManagerOptions{DB: store})
		reg, err := cache.NewRegistry(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		o.Auth.Tokens = token.NewService(token.ServiceOptions{DB: store, Cache: reg, Tokens: config.AuthTokens{CacheTTL: time.Minute}})
	}, authed)
}

func httptestGet(h http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

// ownerIsAdmin reads the member of one token of an answer: present with its value, or absent.
func ownerIsAdmin(t *testing.T, raw json.RawMessage) (value, present bool) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	v, ok := m["ownerIsAdmin"]
	if !ok {
		return false, false
	}
	return string(v) == "true", true
}

// TestR_AUTH_028_TheOwnersRoleIsShownOnlyWithIdentitiesRead: a holder of tokens:read alone lists and reads other people's tokens
// without learning whether their owners are administrators; with identities:read, which shows the role on the user list
// anyway, it does (R-AUTH-028, Q-AUTH-428). A caller always learns it for its own tokens: its own role is no secret to it.
func TestR_AUTH_028_TheOwnersRoleIsShownOnlyWithIdentitiesRead(t *testing.T) {
	for _, tc := range []struct {
		name      string
		held      grants
		otherSeen bool
	}{
		{"tokens:read alone", grants{"tokens:read": true}, false},
		{"tokens:read and identities:read", grants{"tokens:read": true, "identities:read": true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := adminFactSetup(t, tc.held)
			val := newValidator(t)
			h := val.Handler(t, s.handler)

			w := httptestGet(h, "/api/v1/tokens")
			if w.Code != http.StatusOK {
				t.Fatalf("list: %d %s", w.Code, w.Body.String())
			}
			var page struct{ Items []json.RawMessage }
			if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 2 {
				t.Fatalf("list: %v %s", err, w.Body.String())
			}
			if v, ok := ownerIsAdmin(t, page.Items[0]); ok != tc.otherSeen || (ok && !v) {
				t.Errorf("list, bob's token: ownerIsAdmin %v present %v, want present %v (true)", v, ok, tc.otherSeen)
			}
			if v, ok := ownerIsAdmin(t, page.Items[1]); !ok || v {
				t.Errorf("list, the caller's own token: ownerIsAdmin %v present %v, want false and present", v, ok)
			}

			w = httptestGet(h, "/api/v1/tokens/"+factOther)
			if w.Code != http.StatusOK {
				t.Fatalf("read: %d %s", w.Code, w.Body.String())
			}
			if v, ok := ownerIsAdmin(t, w.Body.Bytes()); ok != tc.otherSeen || (ok && !v) {
				t.Errorf("read, bob's token: ownerIsAdmin %v present %v, want present %v (true)", v, ok, tc.otherSeen)
			}
		})
	}
}

// TestR_AUTH_020_AServiceAccountStatesItsRoleOnlyWithIdentitiesRead: every answer that renders a service account decides isAdmin
// by the same question as a token's ownerIsAdmin (Q-AUTH-428). An Owner of a project reads its accounts without
// identities:read and gets no isAdmin.
func TestR_AUTH_020_AServiceAccountStatesItsRoleOnlyWithIdentitiesRead(t *testing.T) {
	a := principal.ServiceAccount{ID: factAdmin, Name: "deploy-bot", Status: "active", IsAdmin: true}
	for _, tc := range []struct {
		name  string
		held  grants
		shown bool
	}{
		{"an Owner of the project, no identities:read", grants{"tokens:read": true}, false},
		{"identities:read", grants{"identities:read": true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(Options{Access: tc.held})
			raw, err := json.Marshal(toServiceAccount(a, s.seesAdminRole(context.Background())))
			if err != nil {
				t.Fatal(err)
			}
			var m map[string]json.RawMessage
			_ = json.Unmarshal(raw, &m)
			if v, ok := m["isAdmin"]; ok != tc.shown || (ok && string(v) != "true") {
				t.Fatalf("isAdmin %s present %v, want present %v: %s", v, ok, tc.shown, raw)
			}
		})
	}
}
