//go:build integration

package limits

import (
	"encoding/json"
	"testing"
	"time"
)

// TestR_LIM_043_TheRulesOfABastionsPlace: a registration's place holds the five kind rules of the preset Internet (migration 00147's
// two profiles); the assignments API of the place takes a rule with limits:write, answers 403 without it and 404 for a name that is no
// registration, keeps a kind rule (409 assignment_builtin); the usage overview's place filter reads one Bastion's rows or the main
// installation's alone (R-LIM-045, R-UI-242); deleting the registration deletes its rules. One replica, rows only, about 4 s with the
// stack.
func TestR_LIM_043_TheRulesOfABastionsPlace(t *testing.T) {
	e := newEnv(t)
	st, r := e.st, e.r
	if a := st.Do(r, "POST", "/api/v1/bastions", e.admin, map[string]any{"name": "partners"}); a.Status != 201 {
		t.Fatalf("create: %d %s", a.Status, a.Body)
	}
	bid := st.Text(`SELECT id::text FROM principal WHERE kind = 'bastion' AND name = 'partners'`)
	const place = "/api/v1/bastions/partners/limits/assignments"
	type rule struct {
		ID      string            `json:"id"`
		Builtin bool              `json:"builtin"`
		Profile string            `json:"profile"`
		Place   map[string]string `json:"place"`
		Who     map[string]any    `json:"who"`
	}
	list := func() []rule {
		a := st.Do(r, "GET", place, e.admin, nil)
		if a.Status != 200 {
			t.Fatalf("list: %d %s", a.Status, a.Body)
		}
		var doc struct{ Items []rule }
		if err := json.Unmarshal(a.Body, &doc); err != nil {
			t.Fatal(err)
		}
		return doc.Items
	}
	kinds := list()
	if len(kinds) != 5 {
		t.Fatalf("%d rules at a new registration's place, want the five kind rules: %+v", len(kinds), kinds)
	}
	for _, k := range kinds {
		want := "bastion-signed-in"
		if k.Who["callerKind"] == "anonymous" {
			want = "bastion-anonymous"
		}
		if !k.Builtin || k.Profile != want || k.Place["kind"] != "bastion" || k.Place["bastion"] != "partners" {
			t.Fatalf("a kind rule: %+v", k)
		}
	}

	tokenRule := map[string]any{"who": map[string]any{"kind": "technicalToken", "technicalToken": "0192f0d1-1c2a-7c3e-8a51-6f7d2b9c4e42"}, "profile": "technical"}
	if a := st.Do(r, "POST", place, e.reader, tokenRule); a.Status != 403 {
		t.Fatalf("without limits:write: %d %s", a.Status, a.Body)
	}
	if a := st.Do(r, "GET", "/api/v1/bastions/nowhere/limits/assignments", e.admin, nil); a.Status != 404 {
		t.Fatalf("a name that is no registration: %d %s", a.Status, a.Body)
	}
	if a := st.Do(r, "POST", place, e.admin, tokenRule); a.Status != 201 {
		t.Fatalf("with limits:write: %d %s", a.Status, a.Body)
	}
	if a := st.Do(r, "DELETE", place+"/"+kinds[0].ID, e.admin, nil); a.Status != 409 {
		t.Fatalf("deleting a kind rule: %d %s", a.Status, a.Body)
	}
	if n := len(list()); n != 6 {
		t.Fatalf("%d rules after the create, want 6", n)
	}

	// A row a Bastion reported and one the main installation counted (the replica's own download above).
	st.Exec(`INSERT INTO usage_bucket (granularity, bucket_start, subject_kind, subject_ref, place, requests)
	         VALUES ('5m', date_trunc('hour', now()), 'anonymous', '198.51.100.7', $1, 7)`, "bastion:"+bid)
	time.Sleep(1500 * time.Millisecond) // the usage buffer's flush interval of the stack, so the upload above is a row too
	subjects := func(placeFilter string) map[string]int64 {
		a := st.Do(r, "GET", "/api/v1/usage?place="+placeFilter, e.admin, nil)
		if a.Status != 200 {
			t.Fatalf("usage at %s: %d %s", placeFilter, a.Status, a.Body)
		}
		var doc struct {
			Items []struct {
				Subject  map[string]any `json:"subject"`
				Requests int64          `json:"requests"`
			} `json:"items"`
		}
		if err := json.Unmarshal(a.Body, &doc); err != nil {
			t.Fatal(err)
		}
		out := map[string]int64{}
		for _, it := range doc.Items {
			id, _ := it.Subject["id"].(string)
			out[id] = it.Requests
		}
		return out
	}
	if got := subjects("bastion:partners"); len(got) != 1 || got["198.51.100.7"] != 7 {
		t.Fatalf("usage through partners: %v", got)
	}
	if got := subjects("installation"); got["198.51.100.7"] != 0 {
		t.Fatalf("the installation's usage holds the Bastion's row: %v", got)
	}
	if a := st.Do(r, "GET", "/api/v1/usage?place=bastion:nowhere", e.admin, nil); a.Status != 404 {
		t.Fatalf("a place filter of no registration: %d %s", a.Status, a.Body)
	}

	if a := st.Do(r, "DELETE", "/api/v1/bastions/partners", e.admin, nil); a.Status != 204 {
		t.Fatalf("delete: %d %s", a.Status, a.Body)
	}
	if n := st.Number(`SELECT count(*) FROM limit_assignment WHERE bastion_id = $1::uuid`, bid); n != 0 {
		t.Fatalf("%d rules of the deleted registration's place", n)
	}
}
