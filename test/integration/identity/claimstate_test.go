//go:build integration

package identity

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/principal"
)

// claimMissingCount reads quader_auth_group_claim_missing_total{name,reason} of a replica.
func claimMissingCount(t *testing.T, reg *prometheus.Registry, provider, reason string) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != "quader_auth_group_claim_missing_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			labels := map[string]string{}
			for _, l := range m.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["name"] == provider && labels["reason"] == reason {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

// TestR_AUTH_008_TheGroupClaimStateIsRecordedWhenItChanges is the owner's W-14 (Q-AUTH-400): the audit log records a missing
// group claim when the identity's stored state changes, as a warning, and its return; the metric counts every sign-in; the
// state is per identity and is written in the transaction of the event.
func TestR_AUTH_008_TheGroupClaimStateIsRecordedWhenItChanges(t *testing.T) {
	e := newEnv(t)
	a := e.newReplica("a", replicaOpts{valkey: vkDead, auditDB: true})
	b := e.newReplica("b", replicaOpts{valkey: vkDead})
	ctx := context.Background()
	absent := func(c *principal.LoginClaims) { c.ClaimState = principal.ClaimAbsent }
	of := func(name string, rec *capture, typ string) []audit.Event {
		var out []audit.Event
		for _, ev := range rec.named(typ) {
			if ev.Subject.Name == name {
				out = append(out, ev)
			}
		}
		return out
	}
	both := func(name, typ string) int { return len(of(name, a.rec, typ)) + len(of(name, b.rec, typ)) }
	signIn := func(t *testing.T, r *replica, c principal.LoginClaims) {
		t.Helper()
		if _, err := r.provisioner.SignIn(ctx, c); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("R-AUTH-008 the first sign-in without the claim, by group sync creating the user, records one warning in the transaction of the state", func(t *testing.T) {
		signIn(t, a, claims("kim", "sub-kim", absent))
		evs := of("kim", a.rec, audit.EventUserGroupsClaimMiss)
		if len(evs) != 1 || evs[0].Outcome != audit.Warning || evs[0].Reason != "absent" {
			t.Fatalf("%+v", evs)
		}
		var d map[string]string
		_ = json.Unmarshal(evs[0].Details, &d)
		if d["provider"] != "corp" || d["onMissingClaim"] != "clear" {
			t.Fatalf("details %v", d)
		}
		if n := e.scalar(`SELECT count(*) FROM audit_event WHERE action = 'user.groups.claim_missing' AND actor_name = 'kim' AND outcome = 'warning'`); n != 1 {
			t.Fatalf("%d stored warnings", n)
		}
		// One transaction has one now(): the state's since is the occurred_at of the event that began it.
		if n := e.scalar(`SELECT count(*) FROM user_identity i JOIN audit_event ev ON ev.actor_name = 'kim' AND ev.action = 'user.groups.claim_missing'
		                   WHERE i.subject = 'sub-kim' AND (i.last_claims -> 'groupClaimMissing' ->> 'since')::timestamptz = ev.occurred_at`); n != 1 {
			t.Fatal("the stored since is not the time of the event")
		}
		u, _ := a.manager.GetUser(ctx, "kim")
		if u.GroupClaimMissing == nil || u.GroupClaimMissing.Reason != "absent" || u.GroupClaimMissing.OnMissingClaim != "clear" || u.GroupClaimMissing.Since.IsZero() {
			t.Fatalf("groupClaimMissing %+v", u.GroupClaimMissing)
		}
	})
	since := func() string {
		u, _ := a.manager.GetUser(ctx, "kim")
		if u.GroupClaimMissing == nil {
			return ""
		}
		return u.GroupClaimMissing.Since.String()
	}
	first := since()

	t.Run("R-AUTH-008 a second sign-in without the claim, on another replica, records nothing, keeps since, and is counted", func(t *testing.T) {
		before := claimMissingCount(t, b.reg, "corp", "absent")
		signIn(t, b, claims("kim", "sub-kim", absent))
		if n := both("kim", audit.EventUserGroupsClaimMiss); n != 1 {
			t.Fatalf("%d claim_missing events", n)
		}
		if got := since(); got != first {
			t.Fatalf("since moved from %s to %s", first, got)
		}
		if got := claimMissingCount(t, b.reg, "corp", "absent"); got != before+1 {
			t.Fatalf("the metric counted %v, want %v", got, before+1)
		}
	})
	t.Run("R-AUTH-008 the claim back records one user.groups.claim_returned and clears the state", func(t *testing.T) {
		signIn(t, a, claims("kim", "sub-kim"))
		back := of("kim", a.rec, audit.EventUserGroupsClaimBack)
		if len(back) != 1 || back[0].Outcome != audit.Success {
			t.Fatalf("%+v", back)
		}
		var d map[string]string
		_ = json.Unmarshal(back[0].Details, &d)
		if d["reason"] != "absent" || d["missingSince"] == "" {
			t.Fatalf("details %v", d)
		}
		if got := since(); got != "" {
			t.Fatalf("the state stayed: %s", got)
		}
		signIn(t, a, claims("kim", "sub-kim"))
		if n := both("kim", audit.EventUserGroupsClaimBack); n != 1 {
			t.Fatalf("%d claim_returned events after a second sign-in with the claim", n)
		}
	})
	t.Run("R-AUTH-008 missing again records it again", func(t *testing.T) {
		signIn(t, a, claims("kim", "sub-kim", absent))
		if n := both("kim", audit.EventUserGroupsClaimMiss); n != 2 {
			t.Fatalf("%d claim_missing events", n)
		}
		if got := since(); got == "" || got == first {
			t.Fatalf("since %q, the first was %q", got, first)
		}
	})
	t.Run("R-AUTH-008 another reason is a change that names the previous one", func(t *testing.T) {
		signIn(t, a, claims("kim", "sub-kim", func(c *principal.LoginClaims) { c.ClaimState = principal.ClaimOverage }))
		evs := of("kim", a.rec, audit.EventUserGroupsClaimMiss)
		if len(evs) != 3 || evs[2].Reason != "overage" {
			t.Fatalf("%+v", evs)
		}
		var d map[string]string
		_ = json.Unmarshal(evs[2].Details, &d)
		if d["previousReason"] != "absent" {
			t.Fatalf("details %v", d)
		}
	})
	t.Run("R-AUTH-008 keep leaves the memberships and records the change once, naming keep", func(t *testing.T) {
		signIn(t, a, claims("ned", "sub-ned", func(c *principal.LoginClaims) { c.Groups = []string{"team-ops"}; c.CreateGroups = true }))
		keep := func(c *principal.LoginClaims) { c.ClaimState = principal.ClaimAbsent; c.KeepOnMissing = true }
		signIn(t, a, claims("ned", "sub-ned", keep))
		signIn(t, b, claims("ned", "sub-ned", keep))
		u, _ := a.manager.GetUser(ctx, "ned")
		if got := groupNames(t, a, u.ID); !slices.Equal(got, []string{"team-ops"}) {
			t.Fatalf("the memberships changed: %v", got)
		}
		evs := append(of("ned", a.rec, audit.EventUserGroupsClaimMiss), of("ned", b.rec, audit.EventUserGroupsClaimMiss)...)
		if len(evs) != 1 {
			t.Fatalf("%d claim_missing events", len(evs))
		}
		var d map[string]string
		_ = json.Unmarshal(evs[0].Details, &d)
		if d["onMissingClaim"] != "keep" || u.GroupClaimMissing == nil || u.GroupClaimMissing.OnMissingClaim != "keep" {
			t.Fatalf("details %v, state %+v", d, u.GroupClaimMissing)
		}
	})
	t.Run("R-AUTH-008 with group sync off a sign-in without the claim stores no state and records nothing", func(t *testing.T) {
		before := claimMissingCount(t, a.reg, "corp", "absent")
		off := func(c *principal.LoginClaims) { c.GroupSync = false; c.ClaimState = principal.ClaimAbsent }
		signIn(t, a, claims("ora", "sub-ora", off))
		signIn(t, a, claims("ora", "sub-ora", off))
		if n := both("ora", audit.EventUserGroupsClaimMiss) + both("ora", audit.EventUserGroupsClaimBack); n != 0 {
			t.Fatalf("%d events", n)
		}
		if n := e.scalar(`SELECT count(*) FROM user_identity WHERE subject = 'sub-ora' AND last_claims ? 'groupClaimMissing'`); n != 0 {
			t.Fatal("a state was stored")
		}
		if got := claimMissingCount(t, a.reg, "corp", "absent"); got != before {
			t.Fatalf("the metric counted %v, want %v", got, before)
		}
	})
	t.Run("R-AUTH-008 the state is per identity: a user with two providers has one for each", func(t *testing.T) {
		if _, err := a.manager.CreateUser(ctx, admin, principal.NewUser{Name: "lou", Identities: []principal.PendingIdentity{
			{Provider: "corp", Issuer: "https://idp.example.com"}, {Provider: "lab", Issuer: "https://lab.example.com"},
		}}); err != nil {
			t.Fatal(err)
		}
		lab := func(c *principal.LoginClaims) { c.Provider, c.Issuer = "lab", "https://lab.example.com" }
		signIn(t, a, claims("lou", "sub-lou", absent))
		signIn(t, a, claims("lou", "sub-lou-lab", lab))
		signIn(t, a, claims("lou", "sub-lou-lab", lab))
		if n := both("lou", audit.EventUserGroupsClaimMiss); n != 1 {
			t.Fatalf("%d claim_missing events", n)
		}
		if n := both("lou", audit.EventUserGroupsClaimBack); n != 0 {
			t.Fatalf("a sign-in through lab cleared corp's state: %d claim_returned", n)
		}
		signIn(t, a, claims("lou", "sub-lou-lab", func(c *principal.LoginClaims) { lab(c); c.ClaimState = principal.ClaimWrongType }))
		if n := both("lou", audit.EventUserGroupsClaimMiss); n != 2 {
			t.Fatalf("%d claim_missing events after lab lost its claim too", n)
		}
		if n := e.scalar(`SELECT count(*) FROM user_identity WHERE last_claims ? 'groupClaimMissing' AND subject IN ('sub-lou', 'sub-lou-lab')`); n != 2 {
			t.Fatalf("%d identities with a state", n)
		}
		u, _ := a.manager.GetUser(ctx, "lou")
		if u.GroupClaimMissing == nil || u.GroupClaimMissing.Provider != "lab" {
			t.Fatalf("the user shows %+v, want the state that began last (lab)", u.GroupClaimMissing)
		}
	})
	t.Run("R-AUTH-008 two sign-ins of one identity at the same moment on two replicas record one change", func(t *testing.T) {
		signIn(t, a, claims("max", "sub-max"))
		var wg sync.WaitGroup
		for _, r := range []*replica{a, b, a, b} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := r.provisioner.SignIn(ctx, claims("max", "sub-max", absent)); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		if n := both("max", audit.EventUserGroupsClaimMiss); n != 1 {
			t.Fatalf("%d claim_missing events", n)
		}
	})
	t.Run("R-AUTH-008 a rule result that is no group name is stored with the identity and recorded once, as a warning, also from a second replica", func(t *testing.T) {
		bad := func(c *principal.LoginClaims) { c.SkippedGroups = []string{"Team A"} }
		signIn(t, a, claims("pia", "sub-pia", bad))
		signIn(t, b, claims("pia", "sub-pia", bad))
		if n := both("pia", audit.EventUserGroupsSkipped); n != 1 {
			t.Fatalf("%d result_skipped events", n)
		}
		if n := e.scalar(`SELECT count(*) FROM audit_event WHERE action = 'user.groups.result_skipped' AND actor_name = 'pia' AND outcome = 'warning'
		                   AND reason = 'invalid_group_name' AND details -> 'skipped' = '["Team A"]'::jsonb`); n != 1 {
			t.Fatalf("%d stored warnings", n)
		}
		if n := e.scalar(`SELECT count(*) FROM user_identity WHERE subject = 'sub-pia' AND last_claims -> 'groupsSkipped' -> 'names' = '["Team A"]'::jsonb`); n != 1 {
			t.Fatal("the skipped results are not stored with the identity")
		}
		signIn(t, a, claims("pia", "sub-pia"))
		if n := e.scalar(`SELECT count(*) FROM user_identity WHERE subject = 'sub-pia' AND last_claims ? 'groupsSkipped'`); n != 0 {
			t.Fatal("the state stayed after a sign-in that skipped nothing")
		}
	})
}
