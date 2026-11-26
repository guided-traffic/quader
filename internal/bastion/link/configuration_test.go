package link_test

import (
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/settings"
)

// TestR_BAS_093_AChangedConfigurationIsAppliedWithinTheFeedsTime: the link applies the configuration of every handshake whole
// (R-BAS-093): before the first one the built-in configuration holds; after it the main installation's; a change at the main
// installation announced by a settings entry is applied at the next tick, one poll later; a configuration Validate refuses
// fails the handshake, makes the replica unreachable and leaves the one it applied.
func TestR_BAS_093_AChangedConfigurationIsAppliedWithinTheFeedsTime(t *testing.T) {
	r := newRig(t, rigOpts{})
	h := r.l.Configuration()
	if h.Current().Source != settings.SourceBuiltin {
		t.Fatalf("before the first handshake: %s", h.Current().Source)
	}
	r.connect()
	if a := h.Current(); a.Source != settings.SourceMain || a.Flood.Anonymous.RequestsPerSecond != 50 {
		t.Fatalf("after the first handshake: %s, %+v", a.Source, a.Flood.Anonymous)
	}
	r.main.SetConfiguration(func(c *iface.Configuration) {
		c.Revision = 2
		c.Limits.Flood.Anonymous = iface.FloodClass{RequestsPerSecond: 5, Burst: 10, MissesPerMinute: 20}
		c.Limits.Slots.MaxConcurrent = 8
	})
	r.main.AddEntry(iface.FeedEntry{Kind: iface.FeedSettings})
	r.tickAfter(2 * time.Second) // the poll reads the entry
	r.tickAfter(2 * time.Second) // the handshake it asked for
	if a := h.Current(); a.Configuration.Revision != 2 || a.Flood.Anonymous.RequestsPerSecond != 5 || a.Slots.MaxConcurrent != 8 {
		t.Fatalf("one poll after the change: revision %d, %+v, %+v", a.Configuration.Revision, a.Flood.Anonymous, a.Slots)
	}
	r.main.SetConfiguration(func(c *iface.Configuration) {
		c.Revision = 3
		c.Limits.Slots.AnonymousShare = 1.5
	})
	r.main.AddEntry(iface.FeedEntry{Kind: iface.FeedSettings})
	r.tickAfter(2 * time.Second)
	r.tickAfter(2 * time.Second)
	if r.l.Reachable() {
		t.Fatal("a handshake whose configuration is refused left the replica reachable")
	}
	if a := h.Current(); a.Configuration.Revision != 2 {
		t.Fatalf("a refused configuration replaced revision 2 with %d", a.Configuration.Revision)
	}
}
