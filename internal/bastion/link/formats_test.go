package link_test

import (
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/maintest"
)

// TestR_BAS_057_TheHandshakeCarriesTheFormatTable: the link keeps the handshake's format table and bound repository until
// the next handshake, which the request flow reads (R-BAS-057, Q-BAS-170); a format the table does not name gets the
// defaults; a table the Bastion cannot rely on is a broken answer: the handshake fails and the replica is unreachable.
func TestR_BAS_057_TheHandshakeCarriesTheFormatTable(t *testing.T) {
	r := newRig(t, rigOpts{})
	r.main.SetBoundRepository("web/docker-hub")
	r.tickAfter(time.Second)
	st := r.l.State()
	if !st.Reachable || !reflect.DeepEqual(st.Formats, maintest.Formats()) || st.BoundRepository != "web/docker-hub" {
		t.Fatalf("the state after the handshake: reachable %v, formats %+v, bound %q", st.Reachable, st.Formats, st.BoundRepository)
	}
	if root, ok := st.Formats.RegistryRoot(); !ok || root.ID != "oci" || len(root.ReplayHeaders) != 4 {
		t.Fatalf("the registry root: %+v %v", root, ok)
	}
	if st.Formats.Lookup("clamav").RateLimitedStatus != http.StatusServiceUnavailable || st.Formats.Lookup("rubygems").RateLimitedStatus != http.StatusTooManyRequests {
		t.Fatal("the rate-limit statuses of the table and of a format it does not name")
	}

	// A binding removed at the main installation reaches the Bastion at its next handshake (H-1248).
	r.main.SetBoundRepository("")
	r.l.RequestHandshake()
	r.tickAfter(time.Second)
	if st := r.l.State(); st.BoundRepository != "" {
		t.Fatalf("the binding after the next handshake: %q", st.BoundRepository)
	}

	// Two formats with the registry root: the table is refused, and the replica is unreachable.
	bad := maintest.Formats()
	bad[0].RegistryRoot = true
	r.main.SetFormats(bad)
	r.l.RequestHandshake()
	r.tickAfter(time.Second)
	if st := r.l.State(); st.Reachable {
		t.Fatal("a table with two registry roots was accepted")
	}

	// A main installation that sends no table: an empty table, every format gets the defaults.
	r.main.SetFormats(nil)
	r.l.RequestHandshake()
	r.tickAfter(time.Second)
	st = r.l.State()
	if !st.Reachable || len(st.Formats) != 0 {
		t.Fatalf("no table: reachable %v, formats %+v", st.Reachable, st.Formats)
	}
	if _, ok := st.Formats.RegistryRoot(); ok || st.Formats.Lookup("oci").RateLimitedStatus != http.StatusTooManyRequests {
		t.Fatal("the defaults of an empty table")
	}
}
