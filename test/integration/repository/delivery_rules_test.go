//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// The delivery rules of a remote repository as the repository model and the management API hold them (remote-curation.md R-CUR-002,
// R-CUR-005, R-CUR-006). The decision of a name and a version is the unit table of internal/remotecuration/rules, the store's folding
// its unit tests; these cases add what needs the stack: the document's validation, the decision point of every caller, the
// transactions with their journal entry, and the propagation to a second replica. Data: rule rows, no artifact.

// TestR_CUR_002_ARemoteHasNoIncludeOrExcludePatterns: a write that sets excludePatterns or includePatterns on a remote answers 422
// not_applicable at its pointer and a remote's document reads back without them; on a local repository the list is stored.
func TestR_CUR_002_ARemoteHasNoIncludeOrExcludePatterns(t *testing.T) {
	e := newEnv(t)
	a := e.newReplica("cur-002", replicaOpts{})
	ctx := context.Background()
	a.mustCreate("npm-remote", "remote", "npm", "")
	a.mustCreate("npm-local", "local", "npm", "")
	for _, member := range []string{"excludePatterns", "includePatterns"} {
		_, err := a.svc.Patch(ctx, caller, qr("npm-remote"), repository.MatchAny(), mergePatch(`{"`+member+`":["@acme/*"]}`))
		wantCode(t, err, 422, errs.CodeValidation)
		if got := fieldPointers(t, err)["/"+member]; got != errs.CodeNotApplicable {
			t.Errorf("%s on a remote: %v, want not_applicable at /%s", member, err, member)
		}
	}
	st, err := a.svc.Get(ctx, qr("npm-remote"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(st.Document)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "includePatterns") || strings.Contains(string(raw), "excludePatterns") {
		t.Errorf("the remote's document reads back with patterns: %s", raw)
	}
	st, err = a.svc.Patch(ctx, caller, qr("npm-local"), repository.MatchAny(), mergePatch(`{"excludePatterns":["@acme/*"]}`))
	if err != nil || len(st.Document.ExcludePatterns) != 1 || st.Document.ExcludePatterns[0] != "@acme/*" {
		t.Fatalf("the local repository's list: %v, %v", st.Document.ExcludePatterns, err)
	}
}

// rulesEnv is one installation with the remote web/npm-remote, the local web/npm-local and a caller of every row of R-CUR-005:
//
//	reader, developer, maintainer, owner   the roles of the project web
//	admin                                  the system role admin
//	auditor                                repositories:read-config (the system role auditor)
//	repoadmin                              repositories:read-config and repositories:write-config (the system role repository-admin)
//	""                                     an anonymous caller
type rulesEnv struct {
	st  *testenv.Stack
	tok map[string]string
}

const (
	rulesRemote = "web/npm-remote"
	rulesLocal  = "web/npm-local"
)

func newRulesEnv(t *testing.T) *rulesEnv {
	t.Helper()
	st := testenv.NewStack(t, testenv.StackOptions{Name: "rules"})
	st.Repository(rulesRemote, "remote", "npm", `{"remote":{"url":"https://registry.example.org/"}}`)
	st.Repository(rulesLocal, "local", "npm", `{}`)
	e := &rulesEnv{st: st, tok: map[string]string{"": ""}}
	for _, role := range []string{"reader", "developer", "maintainer", "owner"} {
		p := st.User(role)
		st.Member("web", p, role)
		sc := scope.Default()
		if role == "owner" {
			// An Owner administers its project through a session or a token with admin true (R-AUTHZ-026).
			sc = scope.Scope{Inherit: true, Admin: true}
		}
		e.tok[role] = st.Token(p, sc)
	}
	admin, auditor, repoadmin := st.User("rita"), st.User("auditor"), st.User("repoadmin")
	st.Bind("admin", admin)
	st.Bind("auditor", auditor)
	st.Bind("repository-admin", repoadmin)
	e.tok["admin"] = st.Token(admin, scope.Scope{Inherit: true, Admin: true})
	e.tok["auditor"] = st.Token(auditor, scope.Scope{Inherit: true, System: []string{"repositories:read-config"}})
	e.tok["repoadmin"] = st.Token(repoadmin, scope.Scope{Inherit: true, System: []string{"repositories:read-config", "repositories:write-config"}})
	return e
}

func rulesPath(key string) string {
	project, k, _ := strings.Cut(key, "/")
	return testenv.ManagementURL(project, k) + "/delivery-rules"
}

// TestR_CUR_005_WhoReadsWritesAndChecksTheRules holds the rows of R-CUR-005 for the rules' operations: the rules with their notes are
// read with manage, repositories:read-config or the role admin; a rule is written and removed with manage (Maintainer, Owner) or admin,
// not with repositories:write-config; the check is a reader's; an anonymous caller gets 401 at every operation.
func TestR_CUR_005_WhoReadsWritesAndChecksTheRules(t *testing.T) {
	e := newRulesEnv(t)
	r := e.st.Start(t, testenv.ReplicaOptions{Name: "web1", Env: []string{"QUADER_API_RATE_LIMITS_ENABLED=false"}})
	do := func(who, method, path string, body any) testenv.Answer {
		t.Helper()
		return e.st.Do(r, method, path, e.tok[who], body)
	}
	// The rule every refused writer would remove.
	target := e.st.MustPutDeliveryRule(r, e.tok["admin"], rulesRemote, testenv.PackageRule("target", "block"))
	type row struct{ list, put, del, check int }
	for who, want := range map[string]row{
		"reader":     {http.StatusForbidden, http.StatusForbidden, http.StatusForbidden, http.StatusOK},
		"developer":  {http.StatusForbidden, http.StatusForbidden, http.StatusForbidden, http.StatusOK},
		"maintainer": {http.StatusOK, http.StatusCreated, http.StatusNoContent, http.StatusOK},
		"owner":      {http.StatusOK, http.StatusCreated, http.StatusNoContent, http.StatusOK},
		"admin":      {http.StatusOK, http.StatusCreated, http.StatusNoContent, http.StatusOK},
		// repositories:read-config reads the rules; without read on the repository its content is hidden, so a write, a removal
		// and a check answer the 404 of a repository that does not exist (R-AUTHZ-016).
		"auditor":   {http.StatusOK, http.StatusNotFound, http.StatusNotFound, http.StatusNotFound},
		"repoadmin": {http.StatusOK, http.StatusNotFound, http.StatusNotFound, http.StatusNotFound},
		"":          {http.StatusUnauthorized, http.StatusUnauthorized, http.StatusUnauthorized, http.StatusUnauthorized},
	} {
		t.Run("R-CUR-005 "+map[bool]string{true: "anonymous", false: who}[who == ""], func(t *testing.T) {
			if a := do(who, http.MethodGet, rulesPath(rulesRemote), nil); a.Status != want.list {
				t.Errorf("list: %d %s, want %d", a.Status, a.Body, want.list)
			}
			put := do(who, http.MethodPost, rulesPath(rulesRemote), testenv.PackageRule("by-"+who, "allow"))
			if put.Status != want.put {
				t.Errorf("write: %d %s, want %d", put.Status, put.Body, want.put)
			}
			id := target
			if put.Status == http.StatusCreated {
				id, _ = put.JSON(t)["id"].(string)
			}
			if a := do(who, http.MethodDelete, rulesPath(rulesRemote)+"/"+id, nil); a.Status != want.del {
				t.Errorf("remove: %d %s, want %d", a.Status, a.Body, want.del)
			}
			if a := do(who, http.MethodPost, rulesPath(rulesRemote)+"/check", map[string]any{"name": "left-pad", "version": "1.3.0"}); a.Status != want.check {
				t.Errorf("check: %d %s, want %d", a.Status, a.Body, want.check)
			}
		})
	}
	if n := e.st.Number(`SELECT count(*) FROM remote_delivery_rules`); n != 1 {
		t.Errorf("%d rules remain, want the target alone", n)
	}
}

// TestR_CUR_006_TheRulesAreWrittenCheckedAndPropagated: create 201, replace 200 (one rule per subject), remove 204; 422 for a name npm
// refuses, a range as a version and a 101st section rule; 400 not_applicable on a local repository; a block records repository.narrowed
// with the part rules, an allow does not; the check names the rule that decides, or the mode, on a second replica once its snapshot holds
// the write (R-CUR-006 "Propagation").
func TestR_CUR_006_TheRulesAreWrittenCheckedAndPropagated(t *testing.T) {
	e := newRulesEnv(t)
	env := []string{"QUADER_API_RATE_LIMITS_ENABLED=false", "QUADER_CLUSTER_EPOCH_POLL_INTERVAL=200ms"}
	a := e.st.Start(t, testenv.ReplicaOptions{Name: "web-a", Env: env})
	b := e.st.Start(t, testenv.ReplicaOptions{Name: "web-b", Env: env})
	admin := e.tok["admin"]
	post := func(body any) testenv.Answer {
		t.Helper()
		return e.st.Do(a, http.MethodPost, rulesPath(rulesRemote), admin, body)
	}
	check := func() map[string]any {
		t.Helper()
		ans := e.st.Do(b, http.MethodPost, rulesPath(rulesRemote)+"/check", admin, map[string]any{"name": "left-pad", "version": "1.3.0"})
		if ans.Status != http.StatusOK {
			t.Fatalf("check: %d %s", ans.Status, ans.Body)
		}
		return ans.JSON(t)
	}
	narrowed := func() int64 {
		return e.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'repository.narrowed' AND repository = $1
			AND details->'parts' = '["rules"]'::jsonb`, rulesRemote)
	}
	var id string

	t.Run("R-CUR-006 a block is created, journaled and refuses on the second replica", func(t *testing.T) {
		ans := post(map[string]any{"kind": "package", "name": "left-pad", "effect": "block", "note": "under review"})
		if ans.Status != http.StatusCreated {
			t.Fatalf("%d %s", ans.Status, ans.Body)
		}
		w := ans.JSON(t)
		id, _ = w["id"].(string)
		if w["subject"] != "left-pad" || w["effect"] != "block" || w["note"] != "under review" || w["revision"] == nil {
			t.Errorf("the written rule %v", w)
		}
		if n := narrowed(); n != 1 {
			t.Errorf("%d repository.narrowed with the part rules, want 1", n)
		}
		e.st.WaitDelivered(t, b, admin, rulesRemote, "left-pad", "1.3.0", false)
		d := check()
		by, _ := d["decidedBy"].(map[string]any)
		if d["reason"] != "blocked" || by["kind"] != "package" || by["subject"] != "left-pad" || by["effect"] != "block" {
			t.Errorf("check %v", d)
		}
	})
	t.Run("R-CUR-006 the subject's second write replaces its effect: 200, the same rule, no narrowing", func(t *testing.T) {
		ans := post(map[string]any{"kind": "package", "name": "left-pad", "effect": "allow"})
		if ans.Status != http.StatusOK || ans.JSON(t)["id"] != id {
			t.Fatalf("%d %s, want 200 with the rule %s", ans.Status, ans.Body, id)
		}
		if n := narrowed(); n != 1 {
			t.Errorf("%d repository.narrowed after an allow, want still 1", n)
		}
		e.st.WaitDelivered(t, b, admin, rulesRemote, "left-pad", "1.3.0", true)
		list := e.st.Do(a, http.MethodGet, rulesPath(rulesRemote), admin, nil)
		if items, _ := list.JSON(t)["items"].([]any); list.Status != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["effect"] != "allow" {
			t.Errorf("list: %d %s", list.Status, list.Body)
		}
	})
	t.Run("R-CUR-006 the removal answers 204 and the mode decides again", func(t *testing.T) {
		if ans := e.st.Do(a, http.MethodDelete, rulesPath(rulesRemote)+"/"+id, admin, nil); ans.Status != http.StatusNoContent {
			t.Fatalf("%d %s", ans.Status, ans.Body)
		}
		testenv.WaitFor(t, "the mode to decide on the second replica", 15*time.Second, func() bool {
			by, _ := check()["decidedBy"].(map[string]any)
			return by["mode"] == "allExceptBlocked"
		})
		// Removing an allow narrows (R-CUR-006 "Journal").
		if n := narrowed(); n != 2 {
			t.Errorf("%d repository.narrowed after the allow's removal, want 2", n)
		}
	})
	t.Run("R-CUR-006 422 for a name npm refuses, a range and a 101st section; 400 on a local repository", func(t *testing.T) {
		for body, code := range map[string]string{
			`{"kind":"package","name":"@acme","effect":"block"}`:                       errs.CodeInvalidName, // a scope without a name
			`{"kind":"version","name":"left-pad","version":"^1.0.0","effect":"block"}`: errs.CodeInvalidVersion,
		} {
			if ans := post(json.RawMessage(body)); ans.Status != http.StatusUnprocessableEntity || ans.Code(t) != code {
				t.Errorf("%s: %d %s, want 422 %s", body, ans.Status, ans.Body, code)
			}
		}
		e.st.Exec(`INSERT INTO remote_delivery_rules (id, repo_id, kind, pattern, effect)
			SELECT gen_random_uuid(), r.id, 'section', '@s' || i || '/*', 'block' FROM repositories r, generate_series(1, 100) i WHERE r.key = 'npm-remote'`)
		if ans := post(testenv.SectionRule("@s101/*", "block")); ans.Status != http.StatusUnprocessableEntity || ans.Code(t) != errs.CodeTooManyRules {
			t.Errorf("a 101st section: %d %s", ans.Status, ans.Body)
		}
		ans := e.st.Do(a, http.MethodPost, rulesPath(rulesLocal), admin, testenv.PackageRule("left-pad", "block"))
		if ans.Status != http.StatusBadRequest || ans.Code(t) != errs.CodeNotApplicable {
			t.Errorf("a local repository: %d %s", ans.Status, ans.Body)
		}
	})
}
