package remotecuration

import (
	"context"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestR_CUR_011_TheVocabularyIsTheMigrations holds the closed lists of this package to the CHECK constraints of the migration, so a
// value one side knows and the other refuses cannot land: the rules' kinds and effects, the queue's states, origins and outcomes, and
// the requests' states (R-CUR-001, R-CUR-011, R-CUR-015).
func TestR_CUR_011_TheVocabularyIsTheMigrations(t *testing.T) {
	raw, err := os.ReadFile("../meta/postgres/migrations/00114_remote_curation.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	// checkList returns the values of "<column> ... CHECK (<column> IN (...))" in the CREATE TABLE of table.
	checkList := func(table, column string) []string {
		t.Helper()
		start := strings.Index(sql, "CREATE TABLE "+table+" (")
		if start < 0 {
			t.Fatalf("the migration creates no table %s", table)
		}
		body := sql[start:]
		body = body[:strings.Index(body, "\n);")]
		m := regexp.MustCompile(`CHECK \(` + column + ` IN \(([^)]*)\)\)`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("%s.%s has no CHECK list", table, column)
		}
		var out []string
		for _, v := range strings.Split(m[1], ",") {
			out = append(out, strings.Trim(strings.TrimSpace(v), "'"))
		}
		return out
	}
	str := func(vs ...any) []string {
		var out []string
		for _, v := range vs {
			switch x := v.(type) {
			case RuleKind:
				out = append(out, string(x))
			case Effect:
				out = append(out, string(x))
			case State:
				out = append(out, string(x))
			case Origin:
				out = append(out, string(x))
			case Outcome:
				out = append(out, string(x))
			case RequestState:
				out = append(out, string(x))
			}
		}
		return out
	}
	for _, c := range []struct {
		table, column string
		want          []string
	}{
		{"remote_delivery_rules", "kind", str(KindSection, KindPackage, KindVersion)},
		{"remote_delivery_rules", "effect", str(Allow, Block)},
		{"remote_download", "state", str(StateWaiting, StateRunning, StateDone, StateFailed, StateCancelled)},
		{"remote_download", "origin", str(OriginPerson, OriginRequest, OriginAllow, OriginBulk, OriginProbe, OriginClient)},
		{"remote_download", "outcome", str(OutcomeCompleted, OutcomeBounded, OutcomeRefused, OutcomeSkipped)},
		{"remote_delivery_requests", "state", str(RequestOpen, RequestAllowed, RequestDeclined, RequestWithdrawn)},
	} {
		if got := checkList(c.table, c.column); !slices.Equal(got, c.want) {
			t.Errorf("%s.%s: the migration allows %v, the package names %v", c.table, c.column, got, c.want)
		}
	}
	// The one active row of a version (remote_download_one_active) is the row in the states Active names.
	if !strings.Contains(sql, "WHERE state IN ('"+string(StateWaiting)+"', '"+string(StateRunning)+"')") {
		t.Fatal("the index of the one active row of a version names other states than Active")
	}
	for _, s := range []State{StateWaiting, StateRunning, StateDone, StateFailed, StateCancelled} {
		if s.Active() != (s == StateWaiting || s == StateRunning) {
			t.Errorf("%s: Active() = %v", s, s.Active())
		}
	}
}

// TestR_CUR_011_AFailureClassIsRetriedOnlyWhenALaterAttemptCanOvercomeIt: the classes of R-CUR-011 "Retries", and no other.
func TestR_CUR_011_AFailureClassIsRetriedOnlyWhenALaterAttemptCanOvercomeIt(t *testing.T) {
	retried := []FailureClass{ClassUpstreamRateLimited, ClassUpstreamError, ClassNetwork, ClassCircuitOpen, ClassStorage, ClassInternal}
	classes := FailureClasses()
	if len(classes) != 15 {
		t.Fatalf("%d classes, R-CUR-011 lists 15", len(classes))
	}
	for _, c := range classes {
		if c.Retryable() != slices.Contains(retried, c) {
			t.Errorf("%s: Retryable() = %v", c, c.Retryable())
		}
	}
}

// TestR_CUR_007_TheStubsAnswerDeliveredNoneQueuedAndNoRequest: until the rules, the queue and the requests are built, a package that
// shows a state reads every version as delivered by the mode, nothing queued and nothing requested.
func TestR_CUR_007_TheStubsAnswerDeliveredNoneQueuedAndNoRequest(t *testing.T) {
	ctx := context.Background()
	var d Decider = DeliverAll{}
	for _, got := range []Decision{d.Decide(ctx, "1", "lodash", "4.17.21"), d.Package(ctx, "1", "lodash")} {
		if !got.Delivered || got.Reason != ReasonNone || got.By.Mode != AllExceptBlocked || got.By.Kind != "" {
			t.Errorf("decision %+v", got)
		}
	}
	var q Queue = NoQueue{}
	if rows, err := q.Active(ctx, "1", "lodash", []string{"4.17.21"}); err != nil || rows == nil || len(rows) != 0 {
		t.Errorf("queue %v, %v", rows, err)
	}
	var r Requests = NoRequests{}
	if open, err := r.Open(ctx, "1", "lodash", []string{"4.17.21"}); err != nil || open == nil || len(open) != 0 {
		t.Errorf("requests %v, %v", open, err)
	}
	if !AllExceptBlocked.Valid() || !OnlyAllowed.Valid() || Mode("blacklist").Valid() {
		t.Error("Mode.Valid")
	}
}
