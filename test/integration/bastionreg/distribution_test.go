//go:build integration

package bastionreg

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/project"
	"github.com/guided-traffic/quader/internal/qname"
	"github.com/guided-traffic/quader/internal/repository"
)

// TestR_BAS_134_TheDistributionIsStoredAndNarrowsInOneTransaction: what a Bastion may distribute against PostgreSQL (bastion.md
// R-BAS-134). A new Bastion allows no project; PUT allows one with endpoints:write (an auditor with endpoints:read reads the list and is
// refused the write), a repeat records nothing; the repository's member bastions names a Bastion only while it allows the project
// (422 bastion_not_allowed at the entry before it), and a choice written around the API is refused by the database (AI011); taking
// the project off the list deletes its choices in the same transaction and journals {bastionId, projectId}, and allowing it again
// brings none back; a move to another project deletes the choices with repository.bastion.removed (cause move); the replay of a removal
// deletes the restored rows again. A handful of rows, about 3 s.
func TestR_BAS_134_TheDistributionIsStoredAndNarrowsInOneTransaction(t *testing.T) {
	e := newEnv(t)
	r, c := e.newCast("web-1", replicaOpts{})
	ctx := context.Background()
	want(t, r.call("POST", bastions, c.admin, map[string]any{"name": "partners"}), http.StatusCreated, "")
	bid := e.text(`SELECT id::text FROM principal WHERE kind = 'bastion' AND name = 'partners'`)
	e.exec(`INSERT INTO project (id, key, display_name) VALUES (gen_random_uuid(), 'platform', 'Platform engineering'),
	                                                          (gen_random_uuid(), 'other', 'Other')`)
	pid := e.text(`SELECT id::text FROM project WHERE key = 'platform'`)
	repos, err := repository.NewService(repository.Options{Runner: r.db, Notifier: r.changes, Recorder: r.writer, Log: quiet})
	if err != nil {
		t.Fatal(err)
	}
	owner := repository.Caller{Actor: audit.Actor{Kind: "admin-cli", Name: "test"}}
	for _, key := range []string{"web", "tools"} {
		if _, err := repos.Create(ctx, owner, "platform", []byte(`{"key":"`+key+`","class":"local","format":"generic"}`)); err != nil {
			t.Fatalf("create %s: %v", key, err)
		}
	}
	web := qname.Repo{Project: "platform", Key: "web"}
	webID := e.text(`SELECT r.id::text FROM repositories r JOIN project p ON p.id = r.project_id WHERE p.key = 'platform' AND r.key = 'web'`)
	choose := func(names string) (repository.Stored, error) {
		return repos.Patch(ctx, owner, web, repository.MatchAny(), func(cur []byte) ([]byte, error) {
			var doc map[string]json.RawMessage // the merge of {"bastions": names}: every other member as stored
			if err := json.Unmarshal(cur, &doc); err != nil {
				return nil, err
			}
			doc["bastions"] = json.RawMessage(names)
			return json.Marshal(doc)
		})
	}
	choices := func() int64 {
		return e.number(`SELECT count(*) FROM repository_bastion WHERE bastion_id = $1::uuid`, bid)
	}

	t.Run("a new Bastion allows nothing, and a choice of it is refused", func(t *testing.T) {
		list := r.call("GET", bastions+"/partners/projects", c.auditor, nil).json(t)
		if items, _ := list["items"].([]any); len(items) != 0 {
			t.Fatalf("%v", list)
		}
		_, err := choose(`["partners"]`)
		var ee *errs.Error
		if !errors.As(err, &ee) || len(ee.Fields) != 1 || ee.Fields[0].Code != errs.CodeBastionNotAllowed || ee.Fields[0].Pointer != "/bastions/0" {
			t.Fatalf("%v", err)
		}
	})

	t.Run("an administrator allows the project; an auditor reads it and is refused the write", func(t *testing.T) {
		want(t, r.call("PUT", bastions+"/partners/projects/platform", c.auditor, nil), http.StatusForbidden, "")
		want(t, r.call("PUT", bastions+"/partners/projects/platform", c.admin, nil), http.StatusNoContent, "")
		want(t, r.call("PUT", bastions+"/partners/projects/platform", c.admin, nil), http.StatusNoContent, "")
		want(t, r.call("PUT", bastions+"/partners/projects/nowhere", c.admin, nil), http.StatusNotFound, "")
		if got := e.events(audit.EventBastionProjectAllowed); len(got) != 1 || got[0].ResourceID != bid || got[0].Details["project"] != "platform" {
			t.Fatalf("bastion.project.allowed = %+v", got)
		}
	})

	t.Run("the Owner chooses the Bastion; the database refuses a choice around the API", func(t *testing.T) {
		st, err := choose(`["partners"]`)
		if err != nil || len(st.Document.Bastions) != 1 || st.Document.Bastions[0].Name != "partners" {
			t.Fatalf("%+v %v", st.Document.Bastions, err)
		}
		if got := e.events(audit.EventRepositoryBastionAdded); len(got) != 1 || got[0].ResourceID != webID {
			t.Fatalf("repository.bastion.added = %+v", got)
		}
		err = e.execErr(`INSERT INTO repository_bastion (repository_id, bastion_id)
		                 SELECT r.id, $1::uuid FROM repositories r JOIN project p ON p.id = r.project_id WHERE p.key = 'platform' AND r.key = 'tools'`, bid)
		if err != nil {
			t.Fatalf("a choice of an allowed project around the API: %v", err)
		}
		e.exec(`INSERT INTO repositories (key, format, class, project_id) SELECT 'stray', 'generic', 'local', id FROM project WHERE key = 'other'`)
		var pg *pgconn.PgError
		err = e.execErr(`INSERT INTO repository_bastion (repository_id, bastion_id)
		                 SELECT r.id, $1::uuid FROM repositories r JOIN project p ON p.id = r.project_id WHERE p.key = 'other'`, bid)
		if !errors.As(err, &pg) || pg.Code != "AI011" {
			t.Fatalf("a choice of a project the Bastion does not allow: %v", err)
		}
		list := r.call("GET", bastions+"/partners/projects", c.auditor, nil).json(t)
		items, _ := list["items"].([]any)
		first, _ := items[0].(map[string]any)
		totals, _ := list["totals"].(map[string]any)
		if len(items) != 1 || first["project"] != "platform" || first["allowedBy"] != "root" || totals["repositories"] != float64(2) ||
			!slices.Equal(anyStrings(first["repositories"]), []string{"tools", "web"}) {
			t.Fatalf("%v", list)
		}
		offered := r.call("GET", "/api/v1/projects/platform/bastions", c.admin, nil).json(t)
		if o, _ := offered["items"].([]any); len(o) != 1 || o[0].(map[string]any)["name"] != "partners" {
			t.Fatalf("%v", offered)
		}
	})

	t.Run("taking the project off deletes its choices and journals it; allowing it again brings none back", func(t *testing.T) {
		want(t, r.call("DELETE", bastions+"/partners/projects/platform", c.admin, nil), http.StatusNoContent, "")
		want(t, r.call("DELETE", bastions+"/partners/projects/platform", c.admin, nil), http.StatusNotFound, "")
		if n := choices(); n != 0 {
			t.Fatalf("%d choices outlived the removal", n)
		}
		got := e.events(audit.EventBastionProjectRemoved)
		if len(got) != 1 || !slices.Equal(anyStrings(got[0].Details["repositories"]), []string{"tools", "web"}) {
			t.Fatalf("bastion.project.removed = %+v", got)
		}
		if n := e.number(`SELECT count(*) FROM revocation_outbox WHERE event = 'bastion.project.removed' AND entry ->> 'bastionId' = $1
		                   AND entry ->> 'projectId' = $2`, bid, pid); n != 1 {
			t.Fatalf("journal entries of the removal = %d, want 1", n)
		}
		want(t, r.call("PUT", bastions+"/partners/projects/platform", c.admin, nil), http.StatusNoContent, "")
		if n := choices(); n != 0 {
			t.Fatalf("allowing again brought back %d choices", n)
		}

		e.exec(`INSERT INTO repository_bastion (repository_id, bastion_id) VALUES ($1::bigint, $2::uuid)`, webID, bid) // a restored state
		replay(t, r, audit.EventBastionProjectRemoved, `{"bastionId":"`+bid+`","projectId":"`+pid+`","name":"partners","project":"platform"}`,
			journal.Applied)
		if n := choices() + e.number(`SELECT count(*) FROM bastion_project WHERE bastion_id = $1::uuid`, bid); n != 0 {
			t.Fatalf("the replay left %d rows", n)
		}
	})

	t.Run("a move deletes the choices with repository.bastion.removed; its replay deletes a restored one", func(t *testing.T) {
		want(t, r.call("PUT", bastions+"/partners/projects/platform", c.admin, nil), http.StatusNoContent, "")
		if _, err := choose(`["partners"]`); err != nil {
			t.Fatal(err)
		}
		policies := authz.NewService(authz.ServiceOptions{DB: r.db, Changes: r.changes, Principals: r.principals, Repositories: noRepositories{}, Recorder: r.writer})
		moves := project.New(project.Options{DB: r.db, Changes: r.changes, Principals: r.principals, Repositories: noRepositories{}, Policies: policies, Recorder: r.writer})
		if _, _, err := moves.MoveRepository(ctx, authz.Subject{Name: "test"}, web, "other", false); err != nil {
			t.Fatal(err)
		}
		if n := choices(); n != 0 {
			t.Fatalf("%d choices outlived the move", n)
		}
		removed := e.events(audit.EventRepositoryBastionRemoved)
		if len(removed) != 1 || removed[0].Details["cause"] != "move" || removed[0].ResourceID != webID {
			t.Fatalf("repository.bastion.removed = %+v", removed)
		}
		id, _ := strconv.ParseInt(webID, 10, 64)
		e.exec(`UPDATE repositories SET project_id = $1::uuid WHERE id = $2`, pid, id) // the restored state: back in platform
		e.exec(`INSERT INTO repository_bastion (repository_id, bastion_id) VALUES ($1, $2::uuid)`, id, bid)
		replay(t, r, audit.EventRepositoryBastionRemoved, `{"repositoryId":"`+webID+`","bastionId":"`+bid+`","name":"partners"}`, journal.Applied)
		replay(t, r, audit.EventRepositoryBastionRemoved, `{"repositoryId":"`+webID+`","bastionId":"`+bid+`","name":"partners"}`, journal.AlreadyInEffect)
		if n := choices(); n != 0 {
			t.Fatalf("the replay left %d choices", n)
		}
	})
}

// replay applies one journal entry in a transaction of its own and checks the outcome.
func replay(t *testing.T, r *replica, event, entry string, outcome journal.Outcome) {
	t.Helper()
	h, ok := journal.HandlerFor(event)
	if !ok {
		t.Fatalf("no replay handler of %s", event)
	}
	var res journal.Result
	if err := r.db.InTx(context.Background(), func(tx meta.Tx) error {
		var err error
		res, err = h.Apply(context.Background(), tx, journal.Env{Notifier: r.changes}, journal.Object{Event: event, Entry: []byte(entry)})
		return err
	}); err != nil {
		t.Fatalf("replay of %s: %v", event, err)
	}
	if res.Outcome != outcome {
		t.Fatalf("replay of %s: %+v, want %s", event, res, outcome)
	}
}

func anyStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, s := range list {
		str, _ := s.(string)
		out = append(out, str)
	}
	return out
}
