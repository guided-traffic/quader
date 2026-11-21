package signatures

import "testing"

// TestR_SIG_009_TheBadgeIsTheBestCountingSignature: the rule of R-SIG-009 as a table of signature sets to the badge (Q-SIG-25).
func TestR_SIG_009_TheBadgeIsTheBestCountingSignature(t *testing.T) {
	pub := func(state State, trust string) Counted {
		return Counted{Role: RolePublisher, State: state, Trust: trust}
	}
	cases := []struct {
		name  string
		sigs  []Counted
		want  string
		stale bool
	}{
		{"empty", nil, BadgeNone, false},
		{"one pending", []Counted{pub(StatePending, "")}, BadgeNotChecked, false},
		{"one failed", []Counted{pub(StateFailed, "")}, BadgeNotChecked, false},
		{"one unsupported", []Counted{pub(StateUnsupported, "")}, BadgeUnsupported, false},
		{"one malformed", []Counted{pub(StateMalformed, "")}, BadgeUnsupported, false},
		{"one invalid", []Counted{pub(StateInvalid, "")}, BadgeInvalid, false},
		{"one invalid and one public", []Counted{pub(StateInvalid, ""), pub(StateValid, TrustPublic)}, BadgeVerifiedPublic, false},
		{"one public and one internal", []Counted{pub(StateValid, TrustPublic), pub(StateValid, TrustInternal)}, BadgeVerifiedInternal, false},
		{"one registry only", []Counted{{Role: RoleRegistry, State: StateValid, Trust: TrustPublic}}, BadgeNone, false},
		{"one attestation only", []Counted{{Role: RoleAttestation, State: StateValid, Trust: TrustInternal}}, BadgeNone, false},
		{"one blocked", []Counted{pub(StateValid, TrustBlocked)}, BadgeUntrusted, false},
		{"one no_key", []Counted{pub(StateNoKey, TrustUntrusted)}, BadgeUntrusted, false},
		{"one valid without its trust row", []Counted{pub(StateValid, "")}, BadgeUntrusted, false},
		{"provenance counts", []Counted{{Role: RoleProvenance, State: StateValid, Trust: TrustInternal}}, BadgeVerifiedInternal, false},
		{"invalid beats pending", []Counted{pub(StatePending, ""), pub(StateInvalid, "")}, BadgeInvalid, false},
		{"a stale deciding row", []Counted{{Role: RolePublisher, State: StateValid, Trust: TrustInternal, Stale: true}}, BadgeVerifiedInternal, true},
		{"a stale row that does not decide", []Counted{pub(StateValid, TrustInternal), {Role: RolePublisher, State: StateValid, Trust: TrustPublic, Stale: true}}, BadgeVerifiedInternal, false},
	}
	for _, c := range cases {
		got, stale := Badge(c.sigs)
		if got != c.want || stale != c.stale {
			t.Errorf("%s: Badge = %s, stale %v; want %s, stale %v", c.name, got, stale, c.want, c.stale)
		}
	}
}
