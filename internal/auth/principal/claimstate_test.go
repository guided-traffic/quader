package principal

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
)

// The table of R-AUTH-008 (Q-AUTH-400, Q-AUTH-401, Q-AUTH-402): which sign-in records what, and which state it leaves.
func TestR_AUTH_008_TheClaimStateRecordsItsChangesOnly(t *testing.T) {
	earlier := time.Date(2026, 11, 4, 8, 30, 0, 0, time.UTC)
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.FixedZone("CET", 3600))
	login := func(state GroupClaimState, keep bool) LoginClaims {
		return LoginClaims{Provider: "corp", ClaimState: state, KeepOnMissing: keep}
	}
	stored := func(reason, policy string) *ClaimMissing {
		return &ClaimMissing{Provider: "corp", Reason: reason, Since: earlier, OnMissingClaim: policy}
	}
	detailsOf := func(t *testing.T, ev *audit.Event) map[string]string {
		t.Helper()
		var d map[string]string
		if err := json.Unmarshal(ev.Details, &d); err != nil {
			t.Fatal(err)
		}
		return d
	}

	t.Run("R-AUTH-008 the first sign-in without the claim records it as a warning and starts the state", func(t *testing.T) {
		next, ev := claimChange(nil, login(ClaimAbsent, false), now)
		if ev == nil || ev.Type != audit.EventUserGroupsClaimMiss || ev.Outcome != audit.Warning || ev.Reason != "absent" {
			t.Fatalf("event %+v", ev)
		}
		if d := detailsOf(t, ev); d["provider"] != "corp" || d["reason"] != "absent" || d["onMissingClaim"] != "clear" || d["previousReason"] != "" {
			t.Fatalf("details %v", d)
		}
		if next == nil || next.Reason != "absent" || !next.Since.Equal(now) || next.Since.Location() != time.UTC || next.OnMissingClaim != "clear" {
			t.Fatalf("state %+v", next)
		}
	})
	t.Run("R-AUTH-008 a second sign-in without the claim records nothing and keeps since", func(t *testing.T) {
		next, ev := claimChange(stored("absent", "clear"), login(ClaimAbsent, false), now)
		if ev != nil {
			t.Fatalf("recorded %+v", ev)
		}
		if next == nil || !next.Since.Equal(earlier) || next.Reason != "absent" {
			t.Fatalf("state %+v", next)
		}
	})
	t.Run("R-AUTH-008 the claim back records claim_returned with the reason and since when it was missing, and drops the state", func(t *testing.T) {
		next, ev := claimChange(stored("overage", "keep"), login(ClaimPresent, false), now)
		if next != nil {
			t.Fatalf("state %+v", next)
		}
		if ev == nil || ev.Type != audit.EventUserGroupsClaimBack || ev.Outcome != audit.Success || ev.Reason != "" {
			t.Fatalf("event %+v", ev)
		}
		d := detailsOf(t, ev)
		if d["reason"] != "overage" || d["missingSince"] != "2026-11-04T08:30:00Z" || d["onMissingClaim"] != "keep" || d["provider"] != "corp" {
			t.Fatalf("details %v", d)
		}
	})
	t.Run("R-AUTH-008 missing again after it was back is a change again", func(t *testing.T) {
		if _, ev := claimChange(nil, login(ClaimWrongType, false), now); ev == nil || ev.Type != audit.EventUserGroupsClaimMiss {
			t.Fatalf("event %+v", ev)
		}
	})
	t.Run("R-AUTH-008 another reason is a change that names the previous one and starts since anew", func(t *testing.T) {
		next, ev := claimChange(stored("overage", "clear"), login(ClaimAbsent, false), now)
		if ev == nil || ev.Type != audit.EventUserGroupsClaimMiss || ev.Reason != "absent" {
			t.Fatalf("event %+v", ev)
		}
		if d := detailsOf(t, ev); d["previousReason"] != "overage" {
			t.Fatalf("details %v", d)
		}
		if next == nil || !next.Since.Equal(now) || next.Reason != "absent" {
			t.Fatalf("state %+v", next)
		}
	})
	t.Run("R-AUTH-008 keep records the same changes and names its policy; a policy change alone records nothing", func(t *testing.T) {
		_, ev := claimChange(nil, login(ClaimAbsent, true), now)
		if ev == nil || detailsOf(t, ev)["onMissingClaim"] != "keep" {
			t.Fatalf("event %+v", ev)
		}
		next, ev := claimChange(stored("absent", "clear"), login(ClaimAbsent, true), now)
		if ev != nil || next.OnMissingClaim != "keep" || !next.Since.Equal(earlier) {
			t.Fatalf("event %+v, state %+v", ev, next)
		}
	})
	t.Run("R-AUTH-008 a usable claim without a stored state records nothing and stores none", func(t *testing.T) {
		if next, ev := claimChange(nil, login(ClaimPresent, false), now); next != nil || ev != nil {
			t.Fatalf("state %+v, event %+v", next, ev)
		}
	})
}

func TestR_AUTH_008_AStoredStateNeedsAReasonAndATime(t *testing.T) {
	for _, raw := range []string{"", "null", `{"provider":"corp","reason":"absent"}`, `{"provider":"corp","since":"2026-11-04T08:30:00Z"}`, `{`} {
		if got := parseClaimMissing(raw); got != nil {
			t.Errorf("%q gave %+v", raw, got)
		}
	}
	got := parseClaimMissing(`{"provider":"corp","reason":"absent","since":"2026-11-04T08:30:00Z","onMissingClaim":"keep"}`)
	if got == nil || got.Reason != "absent" || got.OnMissingClaim != "keep" || got.Since.Year() != 2026 {
		t.Fatalf("%+v", got)
	}
}
