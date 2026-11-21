package gate

import (
	"testing"

	"github.com/guided-traffic/quader/internal/signatures"
)

var (
	blobA = [32]byte{0xa}
	blobB = [32]byte{0xb}
)

// snapshotOf is a snapshot the tests build: anchor 1 shipped, 2 of the configuration, 3 the administrator's, 4 a project's; epoch 10.
func snapshotOf() *Snapshot {
	return &Snapshot{
		Anchors: map[int64]Anchor{
			1: {ID: 1, Scope: ScopeShipped, Key: "sigstore-public", Enabled: true},
			2: {ID: 2, Scope: "configuration", Key: "gitlab-ci-signing", Enabled: true},
			3: {ID: 3, Scope: "installation", Key: "release-key", Enabled: true},
			4: {ID: 4, Scope: ScopeProject, ProjectID: "p1", Key: "team-key", Enabled: true},
		},
		ProjectAnchors: true, Epoch: 10,
	}
}

func valid(subject [32]byte, state string, anchors ...int64) Row {
	return Row{
		Subject: subject, State: signatures.StateValid, HasTrust: true, TrustState: state, TrustAnchors: anchors, TrustEpoch: 10,
		KeyIDs: []string{"ABCDEF0123456789"},
	}
}

// issued marks a row as a signature Quader made itself (stage 2's identity_id).
func issued(r Row) Row {
	r.Issued = true
	return r
}

// TestR_SIG_062_WhatPassesAndWhatIsRefused: the table of R-SIG-062 as signature sets to a decision and a reason, with any anchor and with
// chosen anchors; one of Quader's own signatures never passes. Unit, milliseconds, rows built by the test.
func TestR_SIG_062_WhatPassesAndWhatIsRefused(t *testing.T) {
	snap := snapshotOf()
	chosenRelease := snap.Chosen([]string{"installation/release-key"}, "")
	for _, tc := range []struct {
		name   string
		rows   []Row
		chosen map[int64]bool
		rel    bool
		want   Judgement
	}{
		{"no signature: missing", nil, nil, false, Judgement{Refused, ReasonMissing}},
		{"one pending: pending", []Row{{Subject: blobA, State: signatures.StatePending}}, nil, false, Judgement{Outcome: Pending}},
		{"one valid without a trust row: pending", []Row{{Subject: blobA, State: signatures.StateValid}}, nil, false, Judgement{Outcome: Pending}},
		{"one internal: passes", []Row{valid(blobA, TrustInternal, 3)}, nil, false, Judgement{Outcome: Pass}},
		{"one public with any anchor: passes", []Row{valid(blobA, TrustPublic, 1)}, nil, false, Judgement{Outcome: Pass}},
		{"one public whose anchor is not chosen", []Row{valid(blobA, TrustPublic, 1)}, chosenRelease, false, Judgement{Refused, ReasonNotChosen}},
		{"one internal whose anchor is chosen", []Row{valid(blobA, TrustInternal, 3)}, chosenRelease, false, Judgement{Outcome: Pass}},
		{
			"one untrusted and one invalid: invalid",
			[]Row{valid(blobA, TrustUntrusted), {Subject: blobA, State: signatures.StateInvalid}},
			nil, false,
			Judgement{Refused, ReasonInvalid},
		},
		{"one blocked beside one internal: passes", []Row{valid(blobA, TrustBlocked), valid(blobA, TrustInternal, 2)}, nil, false, Judgement{Outcome: Pass}},
		{"one blocked: blocked", []Row{valid(blobA, TrustBlocked)}, nil, false, Judgement{Refused, ReasonBlocked}},
		{"no key: untrusted", []Row{{Subject: blobA, State: signatures.StateNoKey}}, nil, false, Judgement{Refused, ReasonUntrusted}},
		{"unsupported: not checked", []Row{{Subject: blobA, State: signatures.StateUnsupported}}, nil, false, Judgement{Refused, ReasonNotChecked}},
		{"one of Quader's own whose chain ends in an anchor: missing", []Row{issued(valid(blobA, TrustInternal, 3))}, nil, false, Judgement{Refused, ReasonMissing}},
		{"one of Quader's own beside a pending one: pending", []Row{issued(valid(blobA, TrustInternal, 3)), {Subject: blobA, State: signatures.StatePending}}, nil, false, Judgement{Outcome: Pending}},
		{"one of Quader's own beside a publisher's internal one: passes", []Row{issued(valid(blobA, TrustInternal, 3)), valid(blobB, TrustInternal, 3)}, nil, false, Judgement{Outcome: Pass}},
		{"a release passes whatever", nil, nil, true, Judgement{Outcome: Pass}},
		{"a covering digest's signature passes", []Row{valid(blobB, TrustInternal, 3)}, nil, false, Judgement{Outcome: Pass}},
	} {
		got := Judge(snap, tc.chosen, [][32]byte{blobA, blobB}, tc.rows, nil, tc.rel)
		if got != tc.want {
			t.Errorf("%s: Judge = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// TestR_SIG_062_FetchStateOfARemote: a fetch due or running is pending, a failed one reads not_checked, a done one decides on the rows.
// Unit, milliseconds.
func TestR_SIG_062_FetchStateOfARemote(t *testing.T) {
	snap := snapshotOf()
	for _, tc := range []struct {
		name string
		f    Fetch
		want Judgement
	}{
		{"queued", Fetch{Subject: blobA, Queued: true}, Judgement{Outcome: Pending}},
		{"failed", Fetch{Subject: blobA, Failed: true}, Judgement{Refused, ReasonNotChecked}},
		{"fetched, nothing found", Fetch{Subject: blobA, Fetched: true}, Judgement{Refused, ReasonMissing}},
		{"fetched and queued again by a refresh", Fetch{Subject: blobA, Fetched: true, Queued: true}, Judgement{Refused, ReasonMissing}},
	} {
		if got := Judge(snap, nil, [][32]byte{blobA}, nil, []Fetch{tc.f}, false); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// TestR_SIG_066_TheGateSubtractsANarrowing: a row whose only anchor was removed reads untrusted; with the removed anchor and another it
// reads by the other; a block created after the row makes it blocked, one created before changes nothing; an anchor whose material was
// replaced after the row counts for nothing; an anchor added after the row is not in it; a project anchor while the switch is off counts
// for nothing. Unit, milliseconds, a snapshot built by the test.
func TestR_SIG_066_TheGateSubtractsANarrowing(t *testing.T) {
	removed := snapshotOf()
	delete(removed.Anchors, 3)
	if s, _ := removed.Subtract(valid(blobA, TrustInternal, 3)); s != TrustUntrusted {
		t.Errorf("the only anchor removed: %s", s)
	}
	if s, a := removed.Subtract(valid(blobA, TrustInternal, 1, 3)); s != TrustPublic || len(a) != 1 || a[0] != 1 {
		t.Errorf("the removed anchor and a shipped one: %s %v", s, a)
	}
	blockedLater := snapshotOf()
	blockedLater.Blocks = []Block{{ID: 1, Kind: signatures.BlockFingerprint, Value: "abcdef0123456789", Epoch: 11}}
	if s, _ := blockedLater.Subtract(valid(blobA, TrustInternal, 3)); s != TrustBlocked {
		t.Errorf("a block after the row: %s", s)
	}
	blockedBefore := snapshotOf()
	blockedBefore.Blocks = []Block{{ID: 1, Kind: signatures.BlockFingerprint, Value: "abcdef0123456789", Epoch: 9}}
	if s, _ := blockedBefore.Subtract(valid(blobA, TrustInternal, 3)); s != TrustInternal {
		t.Errorf("a block before the row (the row holds it): %s", s)
	}
	identity := snapshotOf()
	identity.Blocks = []Block{{ID: 2, Kind: signatures.BlockIdentity, Value: `https://gitlab\.example/.*`, Issuer: "https://gitlab.example", Epoch: 11}}
	r := valid(blobA, TrustInternal, 2)
	r.OIDCIssuer, r.Identity = "https://gitlab.example", "https://gitlab.example/web/app//.gitlab-ci.yml@refs/heads/main"
	if s, _ := identity.Subtract(r); s != TrustBlocked {
		t.Errorf("an identity block after the row: %s", s)
	}
	narrowed := snapshotOf()
	a3 := narrowed.Anchors[3]
	a3.NarrowedEpoch = 11
	narrowed.Anchors[3] = a3
	if s, _ := narrowed.Subtract(valid(blobA, TrustInternal, 3)); s != TrustUntrusted {
		t.Errorf("an anchor whose material was replaced after the row: %s", s)
	}
	added := snapshotOf()
	added.Anchors[5] = Anchor{ID: 5, Scope: "installation", Key: "new", Enabled: true}
	if s, a := added.Subtract(valid(blobA, TrustUntrusted)); s != TrustUntrusted || len(a) != 0 {
		t.Errorf("an anchor added after the row widened it: %s %v", s, a)
	}
	switchedOff := snapshotOf()
	switchedOff.ProjectAnchors = false
	if s, _ := switchedOff.Subtract(valid(blobA, TrustInternal, 4)); s != TrustUntrusted {
		t.Errorf("a project anchor while the switch is off: %s", s)
	}
	disabled := snapshotOf()
	a2 := disabled.Anchors[2]
	a2.Enabled = false
	disabled.Anchors[2] = a2
	if got := Judge(disabled, nil, [][32]byte{blobA}, []Row{valid(blobA, TrustInternal, 2)}, nil, false); got != (Judgement{Refused, ReasonUntrusted}) {
		t.Errorf("an anchor switched off: %+v", got)
	}
}

// TestR_SIG_061_ChosenAnchorsResolveInTheScope: references resolve to the anchors in force of the repository's scope; a removed one
// resolves to nothing, a project anchor only for its own project. Unit, milliseconds.
func TestR_SIG_061_ChosenAnchorsResolveInTheScope(t *testing.T) {
	snap := snapshotOf()
	got := snap.Chosen([]string{"installation/release-key", "project/team-key", "installation/removed"}, "p1")
	if len(got) != 2 || !got[3] || !got[4] {
		t.Errorf("in p1: %v", got)
	}
	if got := snap.Chosen([]string{"project/team-key"}, "p2"); len(got) != 0 {
		t.Errorf("another project's anchor resolved: %v", got)
	}
}
