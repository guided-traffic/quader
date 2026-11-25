//go:build integration

package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/qname"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules/store"
	"github.com/guided-traffic/quader/internal/repository"
)

// R-CUR-006, R-CUR-021, R-REPO-061: the narrowings of a remote's delivery rules and of its delivery mode, written after t through the rules'
// store and the repository service the API uses, shipped with the journal's shipper and replayed into the copy made at t. Data: rule rows
// and one document, no artifact.

// ruleCaller is the administrator of repoCaller as the rules' store records a write.
var ruleCaller = store.Caller{Actor: repoCaller.Actor}

// rulesStore is the rules' store on a side, as the composition root builds it (internal/app/wire_delivery_rules.go).
func rulesStore(t *testing.T, s *twinSide) *store.Store {
	t.Helper()
	st, err := store.NewStore(store.Options{Runner: s.db, Recorder: s.rec, Notifier: s.changes, Compile: repository.CompileSection})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// deliveryMode sets remote.delivery.mode of a stored document, every other member kept.
func deliveryMode(mode string) repository.Patcher {
	return func(cur []byte) ([]byte, error) {
		var doc map[string]any
		if err := json.Unmarshal(cur, &doc); err != nil {
			return nil, err
		}
		remote, _ := doc["remote"].(map[string]any)
		delivery, _ := remote["delivery"].(map[string]any)
		if delivery == nil {
			delivery = map[string]any{}
		}
		delivery["mode"] = mode
		remote["delivery"] = delivery
		return json.Marshal(doc)
	}
}

// rulesOf lists a repository's rules as "<subject>:<effect>" in the order of their subjects.
func (s *twinSide) rulesOf(key string) string {
	s.t.Helper()
	return s.text(`SELECT COALESCE(string_agg(COALESCE(NULLIF(name, ''), pattern) || ':' || effect, ',' ORDER BY COALESCE(NULLIF(name, ''), pattern)), '')
		FROM remote_delivery_rules WHERE repo_id = (SELECT id FROM repositories WHERE key = $1)`, key)
}

// TestR_CUR_006_TheRulesAndTheModeNarrowedAfterARestorePointAreReplayed: after t a block is added, an allow is turned into a block, an
// allow is removed, the package's switch with Reset removes a version's allow (R-CUR-024), and the mode is set to onlyAllowed; each is a
// repository.narrowed entry (parts rules and deliveryMode). The replay applies them to the copy made at t (the allows of left-pad and
// of left-pad@1.0.0 are gone, evil and lodash are blocked, the mode is onlyAllowed), raises the
// rule set's revision, writes no audit event, and a second replay applies nothing again. A restored rule set at its bound of sections takes no further
// section block: the repository is disabled and the entry suspended (fail closed). Data: about 100 rule rows, no artifact; about 1 s.
func TestR_CUR_006_TheRulesAndTheModeNarrowedAfterARestorePointAreReplayed(t *testing.T) {
	w := newNarrowWorld(t)
	ctx := context.Background()
	w.create("remote", "npm-remote", "npm", "")
	w.create("remote", "npm-full", "npm", "")
	target := func(key string) store.Target {
		return store.Target{ID: w.src.repoID(key), Name: qname.Repo{Project: "web", Key: key}, Format: "npm"}
	}
	put := func(rs *store.Store, key string, kind rc.RuleKind, subject string, effect rc.Effect) string {
		t.Helper()
		in := store.Input{Kind: kind, Effect: effect, Name: subject}
		if kind == rc.KindSection {
			in = store.Input{Kind: kind, Effect: effect, Pattern: subject}
		}
		wr, err := rs.Put(ctx, target(key), ruleCaller, in)
		if err != nil {
			t.Fatalf("rule %s %s on %s: %v", subject, effect, key, err)
		}
		return wr.ID
	}
	rs := rulesStore(t, w.src)
	leftPad := put(rs, "npm-remote", rc.KindPackage, "left-pad", rc.Allow)
	put(rs, "npm-remote", rc.KindPackage, "lodash", rc.Allow)
	if _, err := rs.Put(ctx, target("npm-remote"), ruleCaller, store.Input{Kind: rc.KindVersion, Name: "left-pad", Version: "1.0.0", Effect: rc.Allow}); err != nil {
		t.Fatal(err)
	}
	w.snapshot()

	rs = rulesStore(t, w.src)
	put(rs, "npm-remote", rc.KindPackage, "evil", rc.Block)
	put(rs, "npm-remote", rc.KindPackage, "lodash", rc.Block)
	if err := rs.Delete(ctx, target("npm-remote"), ruleCaller, leftPad); err != nil {
		t.Fatal(err)
	}
	// The package's switch with Reset (R-CUR-024) removes the version's allow: one entry with the allows it removed.
	if err := w.src.db.InTx(ctx, func(tx meta.Tx) error {
		_, err := rs.SwitchTx(ctx, tx, target("npm-remote"), ruleCaller, store.SwitchInput{Name: "left-pad", Delivered: true, ResetVersions: true})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Patch(ctx, repoCaller, qname.Repo{Project: "web", Key: "npm-remote"}, repository.Precondition{Any: true}, deliveryMode(string(rc.OnlyAllowed))); err != nil {
		t.Fatal(err)
	}
	put(rs, "npm-full", rc.KindSection, "@evil/*", rc.Block)
	js := w.ship(6)
	w.src.close()

	rst := w.tw.open(w.tw.restored)
	if got := rst.rulesOf("npm-remote"); got != "left-pad:allow,left-pad:allow,lodash:allow" {
		t.Fatalf("the rules at t: %s", got)
	}
	// The copy of npm-full holds the bound of a repository's sections (rules.MaxSections): the journaled section block cannot be added.
	rst.exec(`INSERT INTO remote_delivery_rules (id, repo_id, kind, pattern, effect)
		SELECT gen_random_uuid(), r.id, 'section', '@s' || i || '/*', 'allow' FROM repositories r, generate_series(1, 100) i WHERE r.key = 'npm-full'`)
	revision := rst.number("SELECT delivery_rules_revision FROM repositories WHERE key = 'npm-remote'")
	sum, _ := rst.replay(js, w.tw.at, rst.changes)

	if o := outcomes(sum)["repository.narrowed web/npm-remote"]; len(o) != 5 || slices.ContainsFunc(o, func(x journal.Outcome) bool { return x != journal.Applied }) {
		t.Errorf("repository.narrowed web/npm-remote: %v, want five applied", o)
	}
	if got := rst.rulesOf("npm-remote"); got != "evil:block,lodash:block" {
		t.Errorf("the restored rules: %s, want evil and lodash blocked and the allows of left-pad and of left-pad@1.0.0 gone", got)
	}
	if got := rst.text("SELECT COALESCE(config->'remote'->'delivery'->>'mode', '') FROM repositories WHERE key = 'npm-remote'"); got != string(rc.OnlyAllowed) {
		t.Errorf("remote.delivery.mode = %q", got)
	}
	if got := rst.number("SELECT delivery_rules_revision FROM repositories WHERE key = 'npm-remote'"); got <= revision {
		t.Errorf("delivery_rules_revision %d -> %d: a rendition under the restored rules would be answered as current", revision, got)
	}
	if rst.disabledRepo("npm-remote") {
		t.Error("npm-remote was disabled")
	}
	if rst.number("SELECT count(*) FROM audit_event WHERE action = 'repository.narrowed'") != 0 {
		t.Error("the replay wrote an audit event")
	}
	t.Run("R-CUR-006 R-HA-054 a rule set at its bound takes no further block: the repository is disabled and the entry suspended", func(t *testing.T) {
		if o := outcomes(sum)["repository.narrowed web/npm-full"]; !slices.Equal(o, []journal.Outcome{journal.Suspended}) {
			t.Errorf("repository.narrowed web/npm-full: %v, want suspended", o)
		}
		if !rst.disabledRepo("npm-full") {
			t.Error("npm-full is not disabled")
		}
		if n := rst.number(fmt.Sprintf("SELECT count(*) FROM remote_delivery_rules WHERE repo_id = %d", rst.repoID("npm-full"))); n != 100 {
			t.Errorf("%d rules on npm-full, want the 100 restored", n)
		}
	})
	// The disabled npm-full is in effect too: a disabled repository delivers nothing.
	secondPass(t, rst, js, w.tw.at, 6)
}
