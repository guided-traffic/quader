//go:build integration

package bastioniface

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastionmain"
	"github.com/guided-traffic/quader/internal/cluster"
)

// recordedSignals keeps what the contact recorder published.
type recordedSignals struct {
	mu  sync.Mutex
	got []string
}

func (r *recordedSignals) Publish(area, payload string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, area+" "+payload)
}

func (r *recordedSignals) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.got
	r.got = nil
	return out
}

// TestR_BAS_105_AContactThatBringsANewBaseURLRaisesTheBuildersSignal (Q-BAS-499): a contact raises bastion_base_url when it brings the
// registration a base URL the builder of its public catalogue does not read yet: the first contact and a move to another address, but
// not a repeat, not a new replica of a rollout that reports the registration's base URL, and not a contact that reports none.
func TestR_BAS_105_AContactThatBringsANewBaseURLRaisesTheBuildersSignal(t *testing.T) {
	e := newEnv(t)
	s := e.newSite(siteOpts{})
	bid := e.registration("partners")
	sig := &recordedSignals{}
	c := bastionmain.NewContacts(bastionmain.ContactOptions{DB: s.db, Audit: s.rec, Signals: sig})
	ctx := context.Background()
	contact := func(replica, base string) []string {
		t.Helper()
		rec := bastionmain.Recognition{Bastion: "partners", BastionID: bid, Interface: iface.InterfaceVersion, Replica: replica}
		rep := bastionmain.Report{ClientAddress: "198.51.100.20", UserAgent: "quader-bastion/0.14.0", ResponseBaseURL: base}
		if err := c.Record(ctx, rec, rep); err != nil {
			t.Fatal(err)
		}
		return sig.take()
	}
	want := []string{cluster.AreaBastionBaseURL + " " + bid}

	if got := contact("bastion-0", "https://packages.example.com"); !slices.Equal(got, want) {
		t.Fatalf("the first contact raised %q, want %q", got, want)
	}
	if got := contact("bastion-1", "https://packages.example.com"); len(got) != 0 {
		t.Errorf("a new replica with the registration's base URL raised %q", got)
	}
	if got := contact("bastion-0", "https://mirror.example.com"); !slices.Equal(got, want) {
		t.Errorf("a move to another base URL raised %q, want %q", got, want)
	}
	if got := contact("bastion-2", ""); len(got) != 0 {
		t.Errorf("a contact without a base URL raised %q", got)
	}
	// Another replica of the main installation hears the same Bastion within the minute: nothing is written, nothing raised.
	c = bastionmain.NewContacts(bastionmain.ContactOptions{DB: s.db, Audit: s.rec, Signals: sig})
	if got := contact("bastion-0", "https://mirror.example.com"); len(got) != 0 {
		t.Errorf("a repeat within the minute raised %q", got)
	}
}
