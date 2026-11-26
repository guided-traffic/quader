package bastionmain

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/errs"
)

// TestR_BAS_017_TheNetworksABastionConnectsFromAreValidated (Q-BAS-431): an address is its own network, an IPv4 address in IPv6 form
// the IPv4 one; /0, a network with bits beyond its prefix, a zone, a duplicate, a word and more than 32 entries are field errors at
// the entry.
func TestR_BAS_017_TheNetworksABastionConnectsFromAreValidated(t *testing.T) {
	got, err := ParseEgress("/egress", []string{"198.51.100.20", "203.0.113.0/28", "2001:db8:4::/64", "::ffff:192.0.2.7", "2001:db8::1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"198.51.100.20/32", "203.0.113.0/28", "2001:db8:4::/64", "192.0.2.7/32", "2001:db8::1/128"}
	if strings.Join(EgressText(got), " ") != strings.Join(want, " ") {
		t.Fatalf("canonical form %v, want %v", EgressText(got), want)
	}
	if got, err := ParseEgress("/egress", nil); err != nil || len(got) != 0 {
		t.Fatalf("an empty list: %v %v", got, err)
	}
	refused := map[string][]string{
		"/egress/0": {"0.0.0.0/0"},
		"/egress/1": {"10.0.0.0/8", "::/0"},
		"/egress/2": {"10.0.0.0/8", "192.0.2.0/24", "10.1.2.3/8"},
		"/egress/3": {"10.0.0.0/8", "192.0.2.0/24", "198.51.100.1", "198.51.100.1/32"},
		"/egress/4": {"10.0.0.0/8", "192.0.2.0/24", "198.51.100.1", "2001:db8::/32", "fe80::1%eth0"},
		"/egress/5": {"10.0.0.0/8", "192.0.2.0/24", "198.51.100.1", "2001:db8::/32", "10.9.0.0/16", "bastion.example.com"},
	}
	for at, in := range refused {
		_, err := ParseEgress("/egress", in)
		e, ok := errs.As(err)
		if !ok || e.Kind != errs.Invalid || len(e.Fields) != 1 || e.Fields[0].Pointer != at {
			t.Errorf("%v: %v, want a field error at %s", in, err, at)
		}
	}
	if _, err := ParseEgress("/egress", []string{"10.1.2.3/8"}); err == nil || !strings.Contains(err.Error()+fieldDetails(err), "10.0.0.0/8") {
		t.Errorf("the error does not name the network meant: %v", fieldDetails(err))
	}
	many := make([]string, MaxEgress+1)
	for i := range many {
		many[i] = netip.AddrFrom4([4]byte{10, 0, byte(i), 1}).String()
	}
	if _, err := ParseEgress("/egress", many); err == nil {
		t.Errorf("%d networks were accepted", len(many))
	}
	if _, err := ParseEgress("/egress", many[:MaxEgress]); err != nil {
		t.Errorf("%d networks: %v", MaxEgress, err)
	}
}

func fieldDetails(err error) string {
	e, ok := errs.As(err)
	if !ok || len(e.Fields) == 0 {
		return ""
	}
	return e.Fields[0].Detail
}

// twoBastions knows two Bastion tokens: tokValid of partners and tokOther of edge.
type twoBastions struct{}

const tokOther = "qdr_bst_otherotherotherotherotherother"

func (twoBastions) Authenticate(_ context.Context, raw string, _ AuthRequest) (BastionToken, error) {
	switch raw {
	case tokValid:
		return BastionToken{TokenID: "t-a", BastionID: "b-partners", Name: "partners", ExpiresAt: time.Now().Add(time.Hour)}, nil
	case tokOther:
		return BastionToken{TokenID: "t-b", BastionID: "b-edge", Name: "edge", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	return BastionToken{}, ErrTokenInvalid
}

// TestR_BAS_017_ARequestCountsAsComingFromItsBastionOnlyFromItsOwnRegistrationsNetworks (Q-BAS-334, Q-BAS-430): partners connects
// from 198.51.100.16/28, edge from 203.0.113.0/28, a third registration lists none. A request with edge's token from partners' network
// is still edge's request, recognized and served, but not from its networks, so nothing it forwards counts; the same token from edge's
// own network is. The address compared is the one the chain resolved before the Bastion's headers (Source), never a forwarded one.
func TestR_BAS_017_ARequestCountsAsComingFromItsBastionOnlyFromItsOwnRegistrationsNetworks(t *testing.T) {
	nets := map[string][]netip.Prefix{
		"b-partners": {netip.MustParsePrefix("198.51.100.16/28")},
		"b-edge":     {netip.MustParsePrefix("203.0.113.0/28")},
	}
	o := RecognizeOptions{
		Tokens: twoBastions{},
		Egress: func(id string) []netip.Prefix { return nets[id] },
		Source: func(r *http.Request) (string, string) { return r.Header.Get("X-Test-Source"), "" },
	}
	cases := []struct {
		name, token, source string
		bastion             string
		from                bool
	}{
		{"edge's token from edge's network", tokOther, "203.0.113.5", "edge", true},
		{"edge's token from partners' network", tokOther, "198.51.100.20", "edge", false},
		{"partners' token from partners' network", tokValid, "198.51.100.20", "partners", true},
		{"partners' token from elsewhere", tokValid, "192.0.2.9", "partners", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := forwarded("/api/npm/web/npm-all/lodash", iface.HeaderAuthorization, "Bearer "+c.token, iface.HeaderInterface, version(),
				"X-Test-Source", c.source, "X-Forwarded-For", "198.51.100.20")
			w, seen := run(t, o, r)
			if w.Code != http.StatusOK || seen == nil {
				t.Fatalf("status %d", w.Code)
			}
			rec, ok := RecognitionFrom(seen.Context())
			if !ok || rec.Bastion != c.bastion || rec.FromEgress != c.from {
				t.Fatalf("recognition %+v, want %s from its networks %v", rec, c.bastion, c.from)
			}
		})
	}
	t.Run("a registration without networks, and no snapshot", func(t *testing.T) {
		delete(nets, "b-edge")
		r := forwarded("/api/npm/web/npm-all/lodash", iface.HeaderAuthorization, "Bearer "+tokOther, iface.HeaderInterface, version(),
			"X-Test-Source", "203.0.113.5")
		if _, seen := run(t, o, r); seen == nil {
			t.Fatal("not served")
		} else if rec, _ := RecognitionFrom(seen.Context()); rec.Bastion != "edge" || rec.FromEgress {
			t.Fatalf("recognition %+v", rec)
		}
		o.Egress = nil
		r = forwarded("/api/npm/web/npm-all/lodash", iface.HeaderAuthorization, "Bearer "+tokValid, iface.HeaderInterface, version(),
			"X-Test-Source", "198.51.100.20")
		if _, seen := run(t, o, r); seen == nil {
			t.Fatal("not served")
		} else if rec, _ := RecognitionFrom(seen.Context()); rec.FromEgress {
			t.Fatalf("recognition %+v", rec)
		}
	})
}

// TestR_BAS_017_TheSnapshotHoldsEachRegistrationsNetworks: the snapshot answers each registration's networks from memory, replaces
// them at a load, and logs a network that server.trusted_proxies covers too, at the call that sets those and at the load that brings
// one, once.
func TestR_BAS_017_TheSnapshotHoldsEachRegistrationsNetworks(t *testing.T) {
	rows := []SnapshotRow{{ID: "b1", Egress: []netip.Prefix{netip.MustParsePrefix("10.1.0.0/16")}}, {ID: "b2"}}
	var logs bytes.Buffer
	s := newRegistrationSnapshot(func(context.Context) ([]SnapshotRow, error) { return rows, nil }, nil, slog.New(slog.NewTextHandler(&logs, nil)))
	if s.Egress("b1") != nil {
		t.Fatal("networks before the first load")
	}
	s.refresh(context.Background())
	if len(s.Egress("b1")) != 1 || s.Egress("b2") != nil || s.On("b1") {
		t.Fatalf("first load: %v %v", s.Egress("b1"), s.Egress("b2"))
	}
	s.WarnOverlap([]netip.Prefix{netip.MustParsePrefix("10.1.2.0/24")})
	if strings.Count(logs.String(), "also in server.trusted_proxies") != 1 {
		t.Fatalf("no warning for the network present at start: %s", logs.String())
	}
	rows = []SnapshotRow{{ID: "b1", Egress: []netip.Prefix{netip.MustParsePrefix("10.1.0.0/16")}}, {ID: "b2", Egress: []netip.Prefix{netip.MustParsePrefix("10.1.2.7/32")}}}
	s.refresh(context.Background())
	if got := s.Egress("b2"); len(got) != 1 || got[0].String() != "10.1.2.7/32" {
		t.Fatalf("b2 after the load: %v", got)
	}
	if strings.Count(logs.String(), "also in server.trusted_proxies") != 2 {
		t.Fatalf("the new network was not logged once, nor the old one again: %s", logs.String())
	}
	var none *RegistrationSnapshot
	if none.Egress("b1") != nil {
		t.Fatal("a nil snapshot names a network")
	}
}
