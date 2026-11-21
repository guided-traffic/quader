package gateset

import (
	"slices"
	"testing"
)

func on(refs ...string) Setting { return Setting{OnlyVerified: true, Anchors: refs} }

// TestR_SIG_061_TheOrderOfAChangeOfTheGate: the table of R-SIG-061 as a function of the stored and the new member, a swap of one reference
// for another a weakening, a create compared with the defaults, and anchors changed while the gate is off doing nothing. Unit, milliseconds.
func TestR_SIG_061_TheOrderOfAChangeOfTheGate(t *testing.T) {
	const a, b, c = "installation/release-key", "configuration/gitlab-ci-signing", "shipped/sigstore-public"
	for _, tc := range []struct {
		name      string
		cur, next Setting
		want      Change
	}{
		{"onlyVerified from false to true narrows", Setting{}, on(), Narrows},
		{"onlyVerified from true to false weakens", on(), Setting{}, Weakens},
		{"a list replaced by any anchor weakens", on(a), on(), Weakens},
		{"any anchor replaced by a list narrows", on(), on(a), Narrows},
		{"a reference added weakens", on(a), on(a, b), Weakens},
		{"a reference removed from a list that stays non-empty narrows", on(a, b), on(a), Narrows},
		{"one reference swapped for another weakens", on(a, b), on(a, c), Weakens},
		{"the same list in another order is the same", on(a, b), on(b, a), Same},
		{"a create with the gate on narrows", Setting{}, on(a), Narrows},
		{"a create with the defaults is the same", Setting{}, Setting{}, Same},
		{"anchors changed while the gate is off do nothing", Setting{Anchors: []string{a}}, Setting{Anchors: []string{a, b}}, Same},
		{"switching off with a list weakens", on(a), Setting{Anchors: []string{a}}, Weakens},
	} {
		if got := Order(tc.cur, tc.next); got != tc.want {
			t.Errorf("%s: Order = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestR_SIG_061_TheReplayTakesTheNarrower: the replay of a journaled narrowing sets the gate on with a list before any anchor and of two
// lists the references in both; two lists without a common reference disable the repository; anchors a restored member stored while
// its gate was off count as any anchor. Unit, milliseconds.
func TestR_SIG_061_TheReplayTakesTheNarrower(t *testing.T) {
	const a, b, c = "installation/a", "installation/b", "installation/c"
	for _, tc := range []struct {
		name                string
		restored, journaled Setting
		want                Setting
		disable             bool
	}{
		{"off restored, any journaled: on with any anchor", Setting{}, on(), on(), false},
		{"any restored, a list journaled: the list", on(), on(a), on(a), false},
		{"a list restored, any journaled: the restored list", on(a, b), on(), on(a, b), false},
		{"two lists: the references in both", on(a, b), on(b, c), on(b), false},
		{"two lists without a common reference: disabled", on(a), on(c), Setting{}, true},
		{"anchors stored while off count as any", Setting{Anchors: []string{a}}, on(c), on(c), false},
	} {
		got, disable := Narrower(tc.restored, tc.journaled)
		if disable != tc.disable || got.OnlyVerified != tc.want.OnlyVerified || !slices.Equal(got.Anchors, tc.want.Anchors) {
			t.Errorf("%s: Narrower = %+v %v, want %+v %v", tc.name, got, disable, tc.want, tc.disable)
		}
	}
}

// TestR_SIG_061_AReferenceNamesAScopeAndAKey: the four scopes, the key rule of an anchor; everything else is refused. Unit, milliseconds.
func TestR_SIG_061_AReferenceNamesAScopeAndAKey(t *testing.T) {
	for _, ok := range []string{"shipped/sigstore-public", "configuration/gitlab-ci-signing", "installation/release-key", "project/team-key"} {
		if _, _, valid := ParseRef(ok); !valid {
			t.Errorf("%q is refused", ok)
		}
	}
	for _, bad := range []string{"", "release-key", "global/a", "installation/", "installation/Release", "installation/a/b", "project/-a"} {
		if _, _, valid := ParseRef(bad); valid {
			t.Errorf("%q is accepted", bad)
		}
	}
}
