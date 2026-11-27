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

// TestR_BAS_135_APairOfDeploymentsRaisesTheSignalOnceEach: a second deployment under the same registration raises bastion_base_url
// at its first contact, which may move the catalogue's stable base URL; the two deployments' contacts taking turns afterwards raise
// nothing, so the builder does not run again at each of them.
func TestR_BAS_135_APairOfDeploymentsRaisesTheSignalOnceEach(t *testing.T) {
	e := newEnv(t)
	s := e.newSite(siteOpts{})
	bid := e.registration("partners")
	sig := &recordedSignals{}
	ctx := context.Background()
	contact := func(replica, base string) []string {
		t.Helper()
		// A recorder per contact: each is a first write of its replica, as a contact a minute after the last one is.
		c := bastionmain.NewContacts(bastionmain.ContactOptions{DB: s.db, Audit: s.rec, Signals: sig})
		rec := bastionmain.Recognition{Bastion: "partners", BastionID: bid, Interface: iface.InterfaceVersion, Replica: replica}
		if err := c.Record(ctx, rec, bastionmain.Report{ClientAddress: "198.51.100.20", UserAgent: "quader-bastion/0.14.0", ResponseBaseURL: base}); err != nil {
			t.Fatal(err)
		}
		return sig.take()
	}
	want := []string{cluster.AreaBastionBaseURL + " " + bid}
	if got := contact("eu-0", "https://pkgs-eu.example.com"); !slices.Equal(got, want) {
		t.Fatalf("the first deployment raised %q", got)
	}
	if got := contact("us-0", "https://pkgs-us.example.com"); !slices.Equal(got, want) {
		t.Fatalf("the second deployment raised %q", got)
	}
	for i := range 2 {
		e.exec("UPDATE bastion_contact SET contact_at = now() - interval '2 minutes' WHERE bastion_id = $1::uuid", bid)
		for _, c := range [][2]string{{"eu-0", "https://pkgs-eu.example.com"}, {"us-0", "https://pkgs-us.example.com"}, {"us-1", "https://pkgs-us.example.com"}} {
			if got := contact(c[0], c[1]); len(got) != 0 {
				t.Errorf("turn %d: %s raised %q", i, c[0], got)
			}
		}
	}
}
