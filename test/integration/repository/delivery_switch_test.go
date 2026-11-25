//go:build integration

package repository

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/wellknown"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// The switches of a package and a version and the change of the delivery mode (remote-curation.md R-CUR-024, R-CUR-025). What a
// subject inherits is the unit table of internal/remotecuration/rules (TestR_CUR_024_WhatASubjectInherits); these cases add what needs
// the stack: the write's transaction that decides against the stored rules, its events and its journal entry, the decision point, and
// allow-stored against the packages a repository stores. Data: rule rows and five package rows, no artifact.

func switchBody(name, version string, delivered bool, extra ...any) map[string]any {
	b := map[string]any{"name": name, "delivered": delivered}
	if version != "" {
		b["version"] = version
	}
	for i := 0; i+1 < len(extra); i += 2 {
		b[extra[i].(string)] = extra[i+1]
	}
	return b
}

// TestR_CUR_024_ASwitchWritesOnlyWhatDepartsFromWhatIsInherited: in mode allExceptBlocked a version switched off writes the version's
// block and records repository.narrowed; switched on again it removes the block; with the package blocked a version switched on writes
// the version's allow; the package switched on with resetVersions removes the package's block and every version rule in one revision,
// and journals the allows it removed; a Reader is refused 403; a system repository of the project upstream answers 409.
func TestR_CUR_024_ASwitchWritesOnlyWhatDepartsFromWhatIsInherited(t *testing.T) {
	e := newRulesEnv(t)
	rulesWellKnown(e.st, wellknown.ScannerTrivyKey, string(wellknown.KindSystem), `{"remote":{"url":"https://registry.example.org/"}}`, "private")
	r := e.st.Start(t, testenv.ReplicaOptions{Name: "web1", Env: []string{"QUADER_API_RATE_LIMITS_ENABLED=false"}})
	maintainer := e.tok["maintainer"]
	sw := func(token string, body map[string]any) testenv.Answer {
		t.Helper()
		return e.st.Do(r, http.MethodPost, rulesPath(rulesRemote)+"/switch", token, body)
	}
	ok := func(body map[string]any) map[string]any {
		t.Helper()
		a := sw(maintainer, body)
		if a.Status != http.StatusOK {
			t.Fatalf("switch %v: %d %s", body, a.Status, a.Body)
		}
		return a.JSON(t)
	}
	rules := func() []string {
		t.Helper()
		all := e.st.Text(`SELECT COALESCE(string_agg(kind || ' ' || COALESCE(name, pattern) || COALESCE('@' || version, '') || ' ' || effect, ',' ORDER BY 1), '')
			FROM remote_delivery_rules d JOIN repositories r ON r.id = d.repo_id WHERE r.key = 'npm-remote'`)
		if all == "" {
			return nil
		}
		return strings.Split(all, ",")
	}
	narrowed := func() int64 {
		return e.st.Number(`SELECT count(*) FROM audit_event WHERE action = 'repository.narrowed' AND repository = $1`, rulesRemote)
	}
	events := func(action string) int64 {
		return e.st.Number(`SELECT count(*) FROM audit_event WHERE action = $1 AND repository = $2`, action, rulesRemote)
	}

	t.Run("R-CUR-024 a version switched off writes its block and narrows; switched on again the block goes", func(t *testing.T) {
		got := ok(switchBody("left-pad", "1.3.0", false))
		d, _ := got["decision"].(map[string]any)
		rule, _ := got["rule"].(map[string]any)
		if d["delivered"] != false || rule["subject"] != "left-pad@1.3.0" || rule["effect"] != "block" {
			t.Fatalf("off: %v", got)
		}
		if n := narrowed(); n != 1 {
			t.Errorf("%d repository.narrowed, want 1", n)
		}
		got = ok(switchBody("left-pad", "1.3.0", true))
		removed, _ := got["removed"].([]any)
		if got["rule"] != nil || len(removed) != 1 || got["decision"].(map[string]any)["delivered"] != true {
			t.Fatalf("on: %v", got)
		}
		if have := rules(); len(have) != 0 {
			t.Errorf("rules after the switch back: %v, want none", have)
		}
		// Switched on where it is delivered already: nothing to write, nothing recorded.
		before := events("remote.rule.created") + events("remote.rule.deleted")
		ok(switchBody("left-pad", "1.3.0", true))
		if after := events("remote.rule.created") + events("remote.rule.deleted"); after != before {
			t.Errorf("a switch to the inherited state without a rule recorded %d events", after-before)
		}
	})
	t.Run("R-CUR-024 with the package blocked a version switched on writes its allow", func(t *testing.T) {
		ok(switchBody("left-pad", "", false))
		ok(switchBody("left-pad", "1.3.0", true))
		ok(switchBody("left-pad", "1.2.0", false)) // equals the package's block: no rule
		if have := rules(); !slices.Equal(have, []string{"package left-pad block", "version left-pad@1.3.0 allow"}) {
			t.Fatalf("rules %v", have)
		}
		e.st.Exec(`INSERT INTO remote_delivery_rules (id, repo_id, kind, name, version, effect)
			SELECT gen_random_uuid(), id, 'version', 'left-pad', '1.0.0', 'block' FROM repositories WHERE key = 'npm-remote'`)
	})
	t.Run("R-CUR-024 the package switched on with resetVersions removes its rule and every version rule in one revision", func(t *testing.T) {
		rev := e.st.Number(`SELECT delivery_rules_revision FROM repositories WHERE key = 'npm-remote'`)
		before := narrowed()
		got := ok(switchBody("left-pad", "", true, "resetVersions", true))
		removed, _ := got["removed"].([]any)
		if got["rule"] != nil || len(removed) != 3 || got["decision"].(map[string]any)["delivered"] != true {
			t.Fatalf("reset: %v", got)
		}
		if have := rules(); len(have) != 0 {
			t.Errorf("rules after the reset: %v", have)
		}
		if after := e.st.Number(`SELECT delivery_rules_revision FROM repositories WHERE key = 'npm-remote'`); after != rev+1 {
			t.Errorf("the revision went from %d to %d, want one raise", rev, after)
		}
		// The version's allow it removed narrows: one entry with that subject.
		if n := narrowed(); n != before+1 {
			t.Errorf("%d repository.narrowed after the reset, want %d", n, before+1)
		}
		allows := e.st.Number(`SELECT count(*) FROM revocation_outbox WHERE event = 'repository.narrowed'
			AND entry->'rules'->'allowsRemoved' @> '[{"name":"left-pad","version":"1.3.0"}]'::jsonb`)
		if allows != 1 {
			t.Errorf("%d journal entries with the removed allow of left-pad@1.3.0, want 1", allows)
		}
		if a := sw(maintainer, switchBody("left-pad", "1.3.0", true, "resetVersions", true)); a.Status != http.StatusUnprocessableEntity {
			t.Errorf("resetVersions on a version: %d %s, want 422", a.Status, a.Body)
		}
	})
	t.Run("R-CUR-005 R-CUR-024 a Reader is refused; a system repository keeps the product's rules", func(t *testing.T) {
		if a := sw(e.tok["reader"], switchBody("left-pad", "", false)); a.Status != http.StatusForbidden {
			t.Errorf("reader: %d %s, want 403", a.Status, a.Body)
		}
		a := e.st.Do(r, http.MethodPost, rulesPath("upstream/"+wellknown.ScannerTrivyKey)+"/switch", e.tok["admin"], switchBody("aquasec/trivy-db", "", false))
		if a.Status != http.StatusConflict || a.Code(t) != errs.CodeManagedByProduct {
			t.Errorf("system repository: %d %s, want 409 managed_by_product", a.Status, a.Body)
		}
	})
}

// TestR_CUR_025_AChangeToOnlyAllowedKeepsTheStoredPackagesOnRequest: five stored packages, one with a package rule and one under a
// section's allow; the summary counts the three the mode decides and names them; allow-stored writes three rules in one revision with
// three remote.rule.created; the mode's change then refuses none of the five; and a bound of 100 rules that the offer would exceed
// answers 422 too_many_rules and writes nothing. A repository-admin reads the summary and may not allow.
func TestR_CUR_025_AChangeToOnlyAllowedKeepsTheStoredPackagesOnRequest(t *testing.T) {
	e := newRulesEnv(t)
	r := e.st.Start(t, testenv.ReplicaOptions{Name: "web1", Env: []string{"QUADER_API_RATE_LIMITS_ENABLED=false", "QUADER_PROXY_DELIVERY_MAX_RULES=100"}})
	admin := e.tok["admin"]
	for _, name := range []string{"@acme/ui", "express", "left-pad", "lodash", "react"} {
		e.st.Exec(`INSERT INTO packages (repo_id, name, display_name) SELECT id, $1, $1 FROM repositories WHERE key = 'npm-remote'`, name)
	}
	e.st.MustPutDeliveryRule(r, admin, rulesRemote, testenv.PackageRule("react", "block"))
	e.st.MustPutDeliveryRule(r, admin, rulesRemote, testenv.SectionRule("@acme/*", "allow"))
	e.st.WaitDelivered(t, r, admin, rulesRemote, "react", "", false)
	summary := func(token string) testenv.Answer {
		t.Helper()
		return e.st.Do(r, http.MethodGet, rulesPath(rulesRemote)+"/summary", token, nil)
	}

	t.Run("R-CUR-025 the summary names the stored packages the mode decides", func(t *testing.T) {
		a := summary(e.tok["repoadmin"])
		if a.Status != http.StatusOK {
			t.Fatalf("%d %s", a.Status, a.Body)
		}
		s := a.JSON(t)
		unruled, _ := s["unruled"].(map[string]any)
		names, _ := unruled["names"].([]any)
		counts, _ := s["rules"].(map[string]any)
		if unruled["count"] != float64(3) || unruled["counted"] != true || len(names) != 3 || names[0] != "express" || names[2] != "lodash" {
			t.Errorf("unruled %v", unruled)
		}
		if s["mode"] != "allExceptBlocked" || s["maxRules"] != float64(100) || counts["block"] != float64(1) || counts["sections"] != float64(1) {
			t.Errorf("summary %v", s)
		}
		if a := summary(e.tok["reader"]); a.Status != http.StatusForbidden {
			t.Errorf("a reader's summary: %d, want 403", a.Status)
		}
		if a := e.st.Do(r, http.MethodPost, rulesPath(rulesRemote)+"/allow-stored", e.tok["repoadmin"], nil); a.Status != http.StatusNotFound {
			t.Errorf("a repository-admin's allow-stored: %d %s, want the 404 of a hidden repository", a.Status, a.Body)
		}
	})
	t.Run("R-CUR-025 a bound the offer would exceed answers 422 too_many_rules and writes nothing", func(t *testing.T) {
		e.st.Exec(`INSERT INTO remote_delivery_rules (id, repo_id, kind, name, effect)
			SELECT gen_random_uuid(), r.id, 'package', 'filler-' || i, 'block' FROM repositories r, generate_series(1, 97) i WHERE r.key = 'npm-remote'`)
		before := e.st.Number(`SELECT count(*) FROM remote_delivery_rules`)
		a := e.st.Do(r, http.MethodPost, rulesPath(rulesRemote)+"/allow-stored", admin, nil)
		if a.Status != http.StatusUnprocessableEntity || a.Code(t) != errs.CodeTooManyRules {
			t.Errorf("%d %s, want 422 too_many_rules", a.Status, a.Body)
		}
		if after := e.st.Number(`SELECT count(*) FROM remote_delivery_rules`); after != before {
			t.Errorf("%d rules written", after-before)
		}
		e.st.Exec(`DELETE FROM remote_delivery_rules WHERE name LIKE 'filler-%'`)
	})
	t.Run("R-CUR-025 allow-stored writes three rules in one revision, and the mode then refuses none of the five", func(t *testing.T) {
		rev := e.st.Number(`SELECT delivery_rules_revision FROM repositories WHERE key = 'npm-remote'`)
		created := events(e, "remote.rule.created")
		a := e.st.Do(r, http.MethodPost, rulesPath(rulesRemote)+"/allow-stored", admin, nil)
		if a.Status != http.StatusOK || a.JSON(t)["created"] != float64(3) {
			t.Fatalf("%d %s", a.Status, a.Body)
		}
		if after := e.st.Number(`SELECT delivery_rules_revision FROM repositories WHERE key = 'npm-remote'`); after != rev+1 {
			t.Errorf("the revision went from %d to %d", rev, after)
		}
		if n := events(e, "remote.rule.created") - created; n != 3 {
			t.Errorf("%d remote.rule.created, want 3", n)
		}
		// The mode as its PATCH stores it (the PATCH and its journal are R-REPO-061's case); the seeded document holds only the URL,
		// so the member is set in place and a rule's write makes every replica read the repository again.
		e.st.Exec(`UPDATE repositories SET config = jsonb_set(config, '{remote,delivery}', '{"mode":"onlyAllowed"}') WHERE key = 'npm-remote'`)
		e.st.MustPutDeliveryRule(r, admin, rulesRemote, testenv.PackageRule("other", "block"))
		e.st.WaitDelivered(t, r, admin, rulesRemote, "never-stored", "", false)
		for name, want := range map[string]bool{"@acme/ui": true, "express": true, "left-pad": true, "lodash": true, "react": false} {
			if got := e.st.Delivered(r, admin, rulesRemote, name, "1.0.0"); got != want {
				t.Errorf("%s delivered %v, want %v (react keeps its block)", name, got, want)
			}
		}
		if a := e.st.Do(r, http.MethodPost, rulesPath(rulesRemote)+"/allow-stored", admin, nil); a.Status != http.StatusConflict {
			t.Errorf("allow-stored in mode onlyAllowed: %d %s, want 409", a.Status, a.Body)
		}
	})
}

func events(e *rulesEnv, action string) int64 {
	return e.st.Number(`SELECT count(*) FROM audit_event WHERE action = $1 AND repository = $2`, action, rulesRemote)
}
