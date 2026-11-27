package bastionmain

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/journal"
)

// The disable of a Bastion on the main installation's side (R-BAS-133): its state, the refusal of its work, the cut of its forwards
// in flight and the journal. Unit tests, milliseconds, no data.

func modeOf(m iface.DisableMode) *iface.DisableMode { return &m }

func statusAt(replica string, at time.Time, mode *iface.DisableMode, running int64) ReplicaState {
	return ReplicaState{Replica: replica, ReportedAt: at, Status: iface.ReplicaStatus{Replica: replica, Operation: iface.Operation{Disabled: mode, Running: running}}}
}

// TestR_BAS_133_StateOfARegistration: the six states and the running requests, from the registration's disable, its contacts and the
// statuses of its replicas.
func TestR_BAS_133_StateOfARegistration(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	soft, hard := iface.DisableSoft, iface.DisableHard
	disabled := func(m iface.DisableMode, since time.Duration) *Disable { return &Disable{Mode: m, At: ago(since)} }
	for _, c := range []struct {
		name    string
		reg     Registration
		state   State
		running int64
	}{
		{"no replica was ever heard", Registration{}, StateNoContact, 0},
		{"a contact within five minutes", Registration{Contacts: []Contact{{Replica: "a", ContactAt: ago(4 * time.Minute)}}}, StateConnected, 0},
		{"an old contact but a fresh status", Registration{
			Contacts: []Contact{{Replica: "a", ContactAt: ago(time.Hour)}}, Status: []ReplicaState{statusAt("a", ago(time.Second), nil, 0)},
		}, StateConnected, 0},
		{"no replica heard for six minutes", Registration{Contacts: []Contact{{Replica: "a", ContactAt: ago(6 * time.Minute)}}}, StateContactLost, 0},
		{"disabled soft, a replica still runs three downloads", Registration{
			Disabled: disabled(soft, 5*time.Second), Status: []ReplicaState{statusAt("a", ago(time.Second), &soft, 3), statusAt("b", ago(time.Second), &soft, 0)},
		}, StateDisabling, 3},
		{"disabled soft, every replica confirms and runs nothing", Registration{
			Disabled: disabled(soft, 5*time.Second), Status: []ReplicaState{statusAt("a", ago(time.Second), &soft, 0), statusAt("b", ago(time.Second), &soft, 0)},
		}, StateDisabled, 0},
		{"disabled hard, confirmed", Registration{
			Disabled: disabled(hard, 5*time.Second), Status: []ReplicaState{statusAt("a", ago(time.Second), &hard, 0)},
		}, StateDisabled, 0},
		{"soft confirmed, then hard not yet", Registration{
			Disabled: disabled(hard, 2*time.Second), Status: []ReplicaState{statusAt("a", ago(time.Second), &soft, 2)},
		}, StateDisabling, 0},
		{"a replica heard since does not confirm within 15 seconds", Registration{
			Disabled: disabled(soft, 20*time.Second), Status: []ReplicaState{statusAt("a", ago(time.Second), &soft, 0), statusAt("b", ago(time.Second), nil, 0)},
		}, StateDisableUnconfirmed, 0},
		{"none heard since, waiting within 15 seconds", Registration{
			Disabled: disabled(soft, 3*time.Second), Status: []ReplicaState{statusAt("a", ago(4*time.Second), nil, 0)},
		}, StateDisabling, 0},
		{"none heard since for 15 seconds", Registration{
			Disabled: disabled(soft, 16*time.Second), Status: []ReplicaState{statusAt("a", ago(17*time.Second), nil, 0)},
		}, StateDisableUnconfirmed, 0},
		{"no replica heard in the five minutes before: at once", Registration{
			Disabled: disabled(hard, time.Second), Contacts: []Contact{{Replica: "a", ContactAt: ago(10 * time.Minute)}},
		}, StateDisableUnconfirmed, 0},
		{"never heard: at once", Registration{Disabled: disabled(hard, time.Second)}, StateDisableUnconfirmed, 0},
		{"a status from before the disable confirms nothing", Registration{
			Disabled: disabled(soft, 20*time.Second), Status: []ReplicaState{statusAt("a", ago(25*time.Second), &soft, 0)},
		}, StateDisableUnconfirmed, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got, running := StateOf(c.reg, now); got != c.state || running != c.running {
				t.Errorf("StateOf = %s, %d; want %s, %d", got, running, c.state, c.running)
			}
		})
	}
}

// disabledSource is the snapshot's answer of the tests.
func disabledSource(m map[string]iface.DisableMode) func(string) iface.DisableMode {
	return func(id string) iface.DisableMode { return m[id] }
}

// TestR_BAS_133_ADisabledRegistrationsWorkIsRefused: a forward, the check and the catalogue routes of a disabled registration answer
// 503 bastion_disabled before any handler runs; the handshake, the feed and the download intake are answered, and the token is not
// refused. Enabled again, the forward passes.
func TestR_BAS_133_ADisabledRegistrationsWorkIsRefused(t *testing.T) {
	const id = "0192f1c4-0000-7000-8000-0000000000b1" // fakeTokens' registration
	modes := map[string]iface.DisableMode{id: iface.DisableSoft}
	o := RecognizeOptions{Tokens: &fakeTokens{}, Disabled: disabledSource(modes), InFlight: NewInFlight()}
	route := func(method, path string) *http.Request {
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Authorization", "Bearer "+tokValid)
		r.Header.Set(iface.HeaderInterface, version())
		return r
	}
	fwd := func() *http.Request {
		return forwarded("/api/npm/web/npm-all/lodash", iface.HeaderAuthorization, "Bearer "+tokValid, iface.HeaderInterface, version())
	}
	for name, r := range map[string]*http.Request{
		"a forward":                fwd(),
		"the check":                route(http.MethodPost, iface.PathCheck),
		"the catalogue's changes":  route(http.MethodGet, iface.PathCatalogue),
		"a catalogue document":     route(http.MethodGet, iface.PathCatalogueDocument),
		"the catalogue's batch":    route(http.MethodPost, iface.PathCatalogueDocuments),
		"the catalogue public set": route(http.MethodGet, iface.PathCataloguePublic),
	} {
		t.Run("refused: "+name, func(t *testing.T) {
			w, seen := run(t, o, r)
			if w.Code != http.StatusServiceUnavailable || seen != nil || problem(t, w)["code"] != errs.CodeBastionDisabled {
				t.Fatalf("status %d, handler reached %v, body %s", w.Code, seen != nil, w.Body)
			}
		})
	}
	for name, r := range map[string]*http.Request{
		"the handshake":         route(http.MethodGet, iface.PathHandshake),
		"the feed":              route(http.MethodGet, iface.PathFeed),
		"the download intake":   route(http.MethodPost, iface.PathDownloads),
		"an unknown path below": route(http.MethodGet, iface.PathPrefix+"other"),
	} {
		t.Run("answered: "+name, func(t *testing.T) {
			if w, seen := run(t, o, r); w.Code != http.StatusOK || seen == nil {
				t.Fatalf("status %d, handler reached %v, body %s", w.Code, seen != nil, w.Body)
			}
		})
	}
	delete(modes, id)
	if w, seen := run(t, o, fwd()); w.Code != http.StatusOK || seen == nil {
		t.Fatalf("enabled again: status %d, handler reached %v", w.Code, seen != nil)
	}
}

// TestR_BAS_133_AHardDisableCancelsTheForwardsInFlight: a forward blocked on its answer is cancelled when the snapshot takes the
// registration's hard disable, the other registration's forward runs on, and the stage forgets both when they end.
func TestR_BAS_133_AHardDisableCancelsTheForwardsInFlight(t *testing.T) {
	const id = "0192f1c4-0000-7000-8000-0000000000b1"
	inflight := NewInFlight()
	snap := newRegistrationSnapshot(nil, nil, nil)
	snap.OnHardDisabled(func(b string) { inflight.Cancel(b) })
	snap.set([]SnapshotRow{{ID: id}})
	started, ended := make(chan struct{}), make(chan error, 1)
	h := Recognize(RecognizeOptions{Tokens: &fakeTokens{}, Disabled: snap.Disabled, InFlight: inflight})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		ended <- context.Cause(r.Context())
	}))
	go h.ServeHTTP(httptest.NewRecorder(), forwarded("/files/web/raw/big.bin", iface.HeaderAuthorization, "Bearer "+tokValid, iface.HeaderInterface, version()))
	<-started
	if n := inflight.Len(id); n != 1 {
		t.Fatalf("in flight = %d, want 1", n)
	}
	other, done := inflight.Track(context.Background(), "another")
	defer done()
	snap.set([]SnapshotRow{{ID: id, Disabled: iface.DisableSoft}})
	select {
	case err := <-ended:
		t.Fatalf("a soft disable cut the forward: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	snap.set([]SnapshotRow{{ID: id, Disabled: iface.DisableHard}})
	select {
	case err := <-ended:
		if !errors.Is(err, ErrHardDisabled) {
			t.Fatalf("cause = %v, want ErrHardDisabled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the hard disable did not cancel the forward")
	}
	if other.Err() != nil {
		t.Error("another registration's forward was cancelled")
	}
	deadline := time.Now().Add(time.Second)
	for inflight.Len(id) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if n := inflight.Len(id); n != 0 {
		t.Errorf("in flight after the end = %d", n)
	}
	if snap.Disabled(id) != iface.DisableHard {
		t.Errorf("snapshot's way = %q", snap.Disabled(id))
	}
}

// TestR_BAS_133_TheDisableIsJournaledAndReplayed: bastion.disabled carries the entry {principalId, name, mode} and a deletion's the
// way hard; the bastion domain replays both, and an entry without an ID or with a way that is neither soft nor hard is skipped before
// any statement runs. The replay's statement runs in test/integration/bastionreg.
func TestR_BAS_133_TheDisableIsJournaledAndReplayed(t *testing.T) {
	var e map[string]string
	if err := json.Unmarshal(registrationEntry("0192f1c4-0000-7000-8000-0000000000b1", "partners", iface.DisableSoft), &e); err != nil ||
		e["principalId"] != "0192f1c4-0000-7000-8000-0000000000b1" || e["name"] != "partners" || e["mode"] != "soft" {
		t.Fatalf("entry = %v, %v", e, err)
	}
	for _, ev := range []string{audit.EventBastionDisabled, audit.EventBastionDeleted} {
		if h, ok := journal.HandlerFor(ev); !ok || h.Domain != "bastion" {
			t.Errorf("%s has no bastion handler", ev)
		}
		for _, bad := range []string{`{"name":"partners"}`, `{"principalId":"partners"}`, `[]`} {
			o := journal.Object{Event: ev, Entry: json.RawMessage(bad)}
			if r, err := replayDisable(context.Background(), nil, journal.Env{}, o); err != nil || r.Outcome != journal.Skipped {
				t.Errorf("%s %s: %+v, %v; want skipped as malformed", ev, bad, r, err)
			}
		}
	}
	o := journal.Object{Event: audit.EventBastionDisabled, Entry: json.RawMessage(`{"principalId":"0192f1c4-0000-7000-8000-0000000000b1","mode":"off"}`)}
	if r, err := replayDisable(context.Background(), nil, journal.Env{}, o); err != nil || r.Outcome != journal.Skipped {
		t.Errorf("an unknown way: %+v, %v; want skipped as malformed", r, err)
	}
}

// TestR_BAS_133_TheConfigurationCarriesTheDisable: the snapshot's configuration of a disabled registration names its way, and the
// interface refuses another one.
func TestR_BAS_133_TheConfigurationCarriesTheDisable(t *testing.T) {
	c, err := Configuration(1, iface.Switches{}, DefaultLimits(), presetRules(iface.PresetInternet))
	if err != nil {
		t.Fatal(err)
	}
	if c.Disabled != nil {
		t.Fatalf("a new configuration is disabled: %v", *c.Disabled)
	}
	b, _ := json.Marshal(c)
	var m map[string]json.RawMessage
	_ = json.Unmarshal(b, &m)
	if string(m["disabled"]) != "null" {
		t.Errorf("disabled on the wire = %s, want null", m["disabled"])
	}
	c.Disabled = modeOf(iface.DisableHard)
	if err := c.Validate(); err != nil {
		t.Errorf("hard: %v", err)
	}
	c.Disabled = modeOf("off")
	if err := c.Validate(); err == nil {
		t.Error("the way off was accepted")
	}
	st := iface.ReplicaStatus{Operation: iface.Operation{Disabled: modeOf("off")}}
	if err := st.Validate(); err == nil {
		t.Error("a status with the way off was accepted")
	}
	st = iface.ReplicaStatus{Operation: iface.Operation{Running: -1}}
	if err := st.Validate(); err == nil {
		t.Error("a status with negative running was accepted")
	}
}
