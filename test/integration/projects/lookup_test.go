//go:build integration

package projects

import (
	"fmt"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/authz"
)

// TestR_AUTHZ_026_PrincipalLookupBounds covers Q-AUTHZ-182 and R-API-077: a prefix of two
// characters at least, 20 results at most, active principals only, ranked together; an item holds
// what a member's row shows and never an e-mail address.
func TestR_AUTHZ_026_PrincipalLookupBounds(t *testing.T) {
	f := newFixture(t)
	f.create("web")
	for i := range 25 {
		f.e.User(fmt.Sprintf("al-%02d", i))
	}
	f.e.User("al-gone")
	f.e.Exec("UPDATE principal SET status = 'disabled' WHERE name = 'al-gone'")
	f.e.Exec("UPDATE app_user SET display_name = 'Swantje Albers', email = 'swantje@example.org' WHERE principal_id = (SELECT id FROM principal WHERE kind = 'user' AND name = 'al-07')")
	f.e.Group("al-ops")
	f.e.Group("zed-ops")
	f.e.Group("al-everyone")
	f.e.Exec("UPDATE user_group SET auto_join = true WHERE principal_id = (SELECT id FROM principal WHERE kind = 'group' AND name = 'al-everyone')")
	f.e.ServiceAccount("al-bot")
	// The project's own service account, by its rows: the replicas of this package do not serve the identity API, which
	// creates one (503 "unavailable").
	f.e.Exec(`WITH p AS (INSERT INTO principal (id, kind, name) VALUES (gen_random_uuid(), 'service_account', 'web/al-deploy') RETURNING id)
		INSERT INTO service_account (principal_id, project_id) SELECT p.id, (SELECT id FROM project WHERE key = 'web') FROM p`)
	lookup := func(query string) []map[string]any {
		t.Helper()
		r := f.do(f.admin, "GET", base+"/web/principals?"+query, nil)
		wantStatus(t, r, 200, "")
		return r.Items(t)
	}
	names := func(items []map[string]any) string {
		var out []string
		for _, it := range items {
			out = append(out, fmt.Sprint(it["principal"]))
		}
		return strings.Join(out, " ")
	}

	t.Run("R-AUTHZ-026 a prefix of two characters answers at most 20 principals, ranked together", func(t *testing.T) {
		got := lookup("q=al")
		if len(got) != 20 {
			t.Fatalf("%d results, want 20", len(got))
		}
		for _, it := range got {
			for _, key := range []string{"email", "status", "origin", "lastLoginAt"} {
				if _, has := it[key]; has {
					t.Fatalf("an item carries %s: %v", key, it)
				}
			}
			if name := it["name"].(string); name == "al-gone" || name == "al-everyone" {
				t.Errorf("a disabled user or an auto-join group was offered: %v", it)
			}
		}
	})
	t.Run("R-API-077 a user is found by a word of its display name and carries it", func(t *testing.T) {
		got := lookup("q=swa&kind=user")
		if len(got) != 1 || got[0]["principal"] != "user:al-07" || got[0]["displayName"] != "Swantje Albers" || got[0]["id"] == nil {
			t.Fatalf("%v", got)
		}
		if _, has := got[0]["email"]; has {
			t.Errorf("the e-mail address left: %v", got[0])
		}
	})
	t.Run("R-AUTHZ-026 a prefix of one character answers 422", func(t *testing.T) {
		wantStatus(t, f.do(f.admin, "GET", base+"/web/principals?q=a", nil), 422, "prefix_too_short")
		wantStatus(t, f.do(f.admin, "GET", base+"/web/principals?q=", nil), 422, "prefix_too_short")
		wantStatus(t, f.do(f.admin, "GET", base+"/web/principals", nil), 400, "")
	})
	t.Run("R-AUTHZ-026 limit and kind narrow the answer, a limit above 20 is refused", func(t *testing.T) {
		if got := lookup("q=al&limit=3"); len(got) != 3 {
			t.Errorf("limit=3: %d", len(got))
		}
		if got := lookup("q=al&kind=group"); names(got) != "group:al-ops" {
			t.Errorf("kind=group: %s", names(got))
		}
		if got := lookup("q=al&kind=user"); len(got) != 20 || got[0]["kind"] != "user" {
			t.Errorf("kind=user: %d", len(got))
		}
		wantStatus(t, f.do(f.admin, "GET", base+"/web/principals?q=al&limit=21", nil), 422, "invalid_limit")
		wantStatus(t, f.do(f.admin, "GET", base+"/web/principals?q=al&limit=0", nil), 422, "invalid_limit")
		wantStatus(t, f.do(f.admin, "GET", base+"/web/principals?q=al&kind=ci_mapping", nil), 422, "invalid_kind")
	})
	t.Run("R-AUTHZ-026 service accounts: the project's own, and the instance's for identities:read", func(t *testing.T) {
		if got := lookup("q=al&kind=service_account"); names(got) != "sa:al-bot sa:web/al-deploy" {
			t.Errorf("the administrator: %s", names(got))
		}
		if got := lookup("q=deploy&kind=service_account"); names(got) != "sa:web/al-deploy" || got[0]["project"] != "web" {
			t.Errorf("by the name within the project: %v", got)
		}
	})
	t.Run("R-AUTHZ-026 the prefix is a prefix of the name or of a word: no wildcard, no infix", func(t *testing.T) {
		if got := lookup("q=%25%25"); len(got) != 0 {
			t.Errorf("q=%%%%: %v", got)
		}
		if got := lookup("q=ps"); len(got) != 0 {
			t.Errorf("an infix matched: %v", got)
		}
		if got := lookup("q=ops&kind=group"); names(got) != "group:al-ops group:zed-ops" {
			t.Errorf("a word of the name: %s", names(got))
		}
	})
	t.Run("R-AUTHZ-026 an unknown project is 404, as is one the caller is no member of; an Owner looks up, a Reader not", func(t *testing.T) {
		wantStatus(t, f.do(f.admin, "GET", base+"/nothing/principals?q=al", nil), 404, "")
		wantStatus(t, f.do(f.alice, "GET", base+"/web/principals?q=al", nil), 404, "")
		wantStatus(t, f.do(f.auditor, "GET", base+"/web/principals?q=al", nil), 200, "") // the auditor holds identities:read
		wantStatus(t, f.member("web", "group:web-leads", "owner"), 201, "")
		r := f.member("web", "user:bob", "reader")
		wantStatus(t, r, 201, "")
		e := fmt.Sprint(r.Epoch())
		wantStatus(t, f.do(f.alice, "GET", base+"/web/principals?q=al", nil, authz.HeaderMinEpoch, e), 200, "")
		got := f.do(f.alice, "GET", base+"/web/principals?q=al&kind=service_account", nil, authz.HeaderMinEpoch, e)
		wantStatus(t, got, 200, "")
		if items := got.Items(t); names(items) != "sa:web/al-deploy" {
			t.Errorf("the Owner found %s, want the project's own account only", names(items))
		}
		wantStatus(t, f.do(f.bob, "GET", base+"/web/principals?q=al", nil, authz.HeaderMinEpoch, e), 403, "")
	})
}
