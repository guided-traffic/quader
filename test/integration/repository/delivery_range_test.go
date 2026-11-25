//go:build integration

package repository

import (
	"net/http"
	"testing"

	"github.com/guided-traffic/quader/test/integration/testenv"
)

// TestR_CUR_026_ARangeIsWrittenThroughTheSwitchAndThePut: the range of a package's allow as the store writes it, against the stack
// (remote-curation.md R-CUR-026): the switch writes it in the format's canonical form whatever the package inherits and journals it, the
// rule and the decision answer it, the replica's snapshot decides by it, a switch without a range keeps it and one with every version
// removes it, and the refusals of a bound the format does not rank, of a range of none and of a range on another rule. The check of the
// bounds is the unit test of the store (TestR_CUR_026_TheStoreChecksARangeAndWritesItCanonical); these cases add the statements, the
// CHECK of 00131 and the answers. Data: rule rows, no upstream.
func TestR_CUR_026_ARangeIsWrittenThroughTheSwitchAndThePut(t *testing.T) {
	e := newRulesEnv(t)
	r := e.st.Start(t, testenv.ReplicaOptions{Name: "web1", Env: []string{"QUADER_API_RATE_LIMITS_ENABLED=false"}})
	maintainer := e.tok["maintainer"]
	sw := func(body map[string]any) testenv.Answer {
		t.Helper()
		return e.st.Do(r, http.MethodPost, rulesPath(rulesRemote)+"/switch", maintainer, body)
	}
	row := func() string {
		return e.st.Text(`SELECT COALESCE(string_agg(effect || ' ' || COALESCE(from_version, '-') || ' ' || COALESCE(to_version, '-') || ' ' || to_inclusive, ','), '')
			FROM remote_delivery_rules d JOIN repositories r ON r.id = d.repo_id WHERE r.key = 'npm-remote' AND d.name = 'lodash'`)
	}
	rangeOf := func(t *testing.T, m map[string]any) map[string]any {
		t.Helper()
		rg, _ := m["range"].(map[string]any)
		return rg
	}

	t.Run("R-CUR-026 a package turned on with a range writes its allow in mode allExceptBlocked and journals the range", func(t *testing.T) {
		before := e.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'repository.narrowed' AND repository = $1`, rulesRemote)
		a := sw(switchBody("lodash", "", true, "range", map[string]any{"fromVersion": "4.0.0", "toVersion": "5.0.0", "toInclusive": false}))
		if a.Status != http.StatusOK {
			t.Fatalf("%d %s", a.Status, a.Body)
		}
		got := a.JSON(t)
		d, _ := got["decision"].(map[string]any)
		by, _ := d["decidedBy"].(map[string]any)
		rule, _ := got["rule"].(map[string]any)
		if d["delivered"] != true || rangeOf(t, by)["fromVersion"] != "4.0.0" || rangeOf(t, rule)["toVersion"] != "5.0.0" || rangeOf(t, rule)["toInclusive"] != false {
			t.Fatalf("the answer: %v", got)
		}
		if have := row(); have != "allow 4.0.0 5.0.0 false" {
			t.Errorf("the row %q", have)
		}
		if n := e.st.Number(`SELECT count(*) FROM revocation_outbox WHERE event = 'repository.narrowed'
			AND entry->'rules'->'rangesNarrowed' @> '[{"name":"lodash","fromVersion":"4.0.0","toVersion":"5.0.0","toInclusive":false}]'::jsonb`); n != 1 || before != 0 {
			t.Errorf("%d repository.narrowed with the range (before %d), want one", n, before)
		}
		if n := e.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'remote.rule.created' AND repository = $1
			AND details -> 'range' ->> 'fromVersion' = '4.0.0'`, rulesRemote); n != 1 {
			t.Errorf("%d remote.rule.created with the range, want one", n)
		}
	})
	t.Run("R-CUR-026 the replica decides by the range: a version above it is outside", func(t *testing.T) {
		e.st.WaitDelivered(t, r, maintainer, rulesRemote, "lodash", "5.0.0", false)
		a := e.st.Do(r, http.MethodPost, rulesPath(rulesRemote)+"/check", maintainer, map[string]any{"name": "lodash", "version": "5.0.0"})
		if got := a.JSON(t); got["reason"] != "outsideRange" {
			t.Errorf("check 5.0.0: %v", got)
		}
		if !e.st.Delivered(r, maintainer, rulesRemote, "lodash", "4.17.21") || !e.st.Delivered(r, maintainer, rulesRemote, "lodash", "") {
			t.Error("a version inside the range and the package itself are delivered")
		}
	})
	t.Run("R-CUR-026 a switch on without a range keeps it; every version removes it", func(t *testing.T) {
		if a := sw(switchBody("lodash", "", true)); a.Status != http.StatusOK || row() != "allow 4.0.0 5.0.0 false" {
			t.Fatalf("without a range: %d %s, row %q", a.Status, a.Body, row())
		}
		a := sw(switchBody("lodash", "", true, "range", map[string]any{}))
		if a.Status != http.StatusOK || row() != "" {
			t.Fatalf("every version: %d %s, row %q; want the allow removed (the mode delivers it)", a.Status, a.Body, row())
		}
	})
	t.Run("R-CUR-026 the refusals: a tag that is no version, a range of none, a range on a block or a version", func(t *testing.T) {
		for _, c := range []struct {
			name, path string
			body       map[string]any
			ptr, code  string
		}{
			{"latest", "/switch", switchBody("lodash", "", true, "range", map[string]any{"fromVersion": "latest"}), "/range/fromVersion", "invalid_version"},
			{"none", "/switch", switchBody("lodash", "", true, "range", map[string]any{"fromVersion": "5.0.0", "toVersion": "4.0.0"}), "/range/toVersion", "out_of_range"},
			{"off", "/switch", switchBody("lodash", "", false, "range", map[string]any{"fromVersion": "4.0.0"}), "/range", "not_applicable"},
			{"version", "", map[string]any{"kind": "version", "name": "lodash", "version": "4.0.0", "effect": "allow", "fromVersion": "4.0.0"}, "/fromVersion", "not_applicable"},
			{"block", "", map[string]any{"kind": "package", "name": "lodash", "effect": "block", "toVersion": "4.0.0"}, "/toVersion", "not_applicable"},
		} {
			a := e.st.Do(r, http.MethodPost, rulesPath(rulesRemote)+c.path, maintainer, c.body)
			if a.Status != http.StatusUnprocessableEntity || !hasFieldError(t, a, c.ptr, c.code) {
				t.Errorf("%s: %d %s, want 422 with %s at %s", c.name, a.Status, a.Body, c.code, c.ptr)
			}
		}
		if have := row(); have != "" {
			t.Errorf("a refused write left the row %q", have)
		}
	})
	t.Run("R-CUR-026 the put writes a range and replaces it; the list answers it", func(t *testing.T) {
		in := map[string]any{"kind": "package", "name": "lodash", "effect": "allow", "fromVersion": "3.0.0"}
		if a := e.st.Do(r, http.MethodPost, rulesPath(rulesRemote), maintainer, in); a.Status != http.StatusCreated {
			t.Fatalf("%d %s", a.Status, a.Body)
		}
		if have := row(); have != "allow 3.0.0 - true" {
			t.Errorf("the row %q", have)
		}
		list := e.st.Do(r, http.MethodGet, rulesPath(rulesRemote)+"?kind=package", maintainer, nil).JSON(t)
		items, _ := list["items"].([]any)
		if len(items) != 1 || rangeOf(t, items[0].(map[string]any))["fromVersion"] != "3.0.0" || rangeOf(t, items[0].(map[string]any))["toVersion"] != nil {
			t.Errorf("the list: %v", list)
		}
		in["effect"] = "block"
		delete(in, "fromVersion")
		if a := e.st.Do(r, http.MethodPost, rulesPath(rulesRemote), maintainer, in); a.Status != http.StatusOK || row() != "block - - true" {
			t.Errorf("the allow turned into a block keeps no range: %d %s, row %q", a.Status, a.Body, row())
		}
	})
}

// hasFieldError reports whether a problem names the field code at the pointer.
func hasFieldError(t *testing.T, a testenv.Answer, ptr, code string) bool {
	t.Helper()
	errs, _ := a.JSON(t)["errors"].([]any)
	for _, x := range errs {
		if m, _ := x.(map[string]any); m["pointer"] == ptr && m["code"] == code {
			return true
		}
	}
	return false
}
