package bastionmain

import (
	"fmt"
	"net/netip"
	"slices"
	"strconv"
)

// The addresses a Bastion connects from (R-BAS-017, R-BAS-093, the owner's Q-BAS-334; api-and-ui.md R-UI-243): a member of each
// registration, empty for a new one. A recognized request of a Bastion whose address - as stage 5 resolved it with
// server.trusted_proxies - lies in one of its own registration's networks may forward its caller's address and host, and its check
// names the caller's address; from any other address, and with an empty list, nothing the Bastion reports about its callers counts
// (Q-BAS-430).

// MaxEgress is the most networks one registration lists (Q-BAS-431): a Bastion's replicas leave through a NAT, a node range or a few
// nodes, and the snapshot of every replica holds every registration's list.
const MaxEgress = 32

// ParseEgress validates a registration's list of networks and returns it in its canonical form (Q-BAS-431): an address alone is the
// network of that one address (/32, /128), an IPv4 address in IPv6 form is the IPv4 one, a network keeps its prefix length. Refused are
// what is neither, a zone, a network with bits set beyond its prefix (the error names the network it may have meant), /0 (every
// address could speak for the Bastion), a network listed twice and more than [MaxEgress]. The error is a field error of pointer
// (/egress) and the index of the entry.
func ParseEgress(pointer string, in []string) ([]netip.Prefix, error) {
	if len(in) > MaxEgress {
		return nil, invalid(pointer, "too_many", fmt.Sprintf("A Bastion lists at most %d networks.", MaxEgress))
	}
	out := make([]netip.Prefix, 0, len(in))
	for i, s := range in {
		at := pointer + "/" + strconv.Itoa(i)
		p, err := parseEgressEntry(s)
		if err != nil {
			return nil, invalid(at, "invalid_network", err.Error())
		}
		if slices.Contains(out, p) {
			return nil, invalid(at, "duplicate", fmt.Sprintf("%s is listed twice.", p))
		}
		out = append(out, p)
	}
	return out, nil
}

// parseEgressEntry is one entry of [ParseEgress].
func parseEgressEntry(s string) (netip.Prefix, error) {
	if p, err := netip.ParsePrefix(s); err == nil {
		if p.Addr().Is4In6() && p.Bits() >= 96 {
			p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
		}
		switch {
		case p.Bits() == 0:
			return netip.Prefix{}, fmt.Errorf("%s covers every address: anyone holding this Bastion's token could choose the caller's address it forwards; list the addresses the Bastion connects from", s)
		case p.Masked() != p:
			return netip.Prefix{}, fmt.Errorf("%s has bits set beyond its prefix; the network is %s", s, p.Masked())
		}
		return p, nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil || a.Zone() != "" {
		return netip.Prefix{}, fmt.Errorf("%q is neither an IP address nor a network in CIDR form (198.51.100.20, 198.51.100.0/28, 2001:db8::/64)", s)
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}

// EgressText is the list in the text form the API and the database use: the network with its prefix length, as PostgreSQL's cidr
// prints it.
func EgressText(ps []netip.Prefix) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.String()
	}
	return out
}

// egressOf parses the text form the database returns; an entry it cannot read is left out (the column's type admits none).
func egressOf(texts []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(texts))
	for _, s := range texts {
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// InEgress reports whether a lies in one of the networks.
func InEgress(a netip.Addr, networks []netip.Prefix) bool {
	if !a.IsValid() {
		return false
	}
	a = a.Unmap().WithZone("")
	for _, p := range networks {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
