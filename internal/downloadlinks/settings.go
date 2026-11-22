// Package downloadlinks is the delivery by signed links of the object store (storage.md R-STOR-014, R-STOR-070 to R-STOR-074;
// usage-and-limits.md R-LIM-040): the installation's switch and its details (one row, its store and the replica's snapshot), the
// rule that keeps a link's host off Quader's hosts, whether each connection can serve links on this replica (its address, its
// encryption and its link check), the choice of the connection and the signing of one link, and the metrics. The serve point
// (internal/formathost, internal/api/edge) asks it for a link after its own conditions held; nothing here reads a request.
//
// The handover per blob to a Bastion (R-STOR-095, R-STOR-096) is decided in this package as well, in a file of its own: it shares
// the public addresses, the host rule, the signing and the first part of the link check.
package downloadlinks

import (
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"
)

// The ranges and defaults of the settings (R-STOR-014, Q-STOR-290).
const (
	DefaultMinSize         int64 = 1 << 20 // 1 MiB
	MaxMinSize             int64 = 1 << 40 // 1 TiB
	DefaultLinkLifeSeconds       = 60
	MinLinkLifeSeconds           = 10
	MaxLinkLifeSeconds           = 3600
)

// Settings is the row download_link_settings: the switch, the details and the row's revision.
type Settings struct {
	Enabled         bool
	MinSize         int64
	LinkLifeSeconds int
	// PublicAddresses maps a connection's id to the https origin its links name (Q-STOR-281); an absent connection links to its
	// own endpoint.
	PublicAddresses map[string]string
	Version         int64
	UpdatedAt       time.Time
	// UpdatedBy is the principal's id, "" for none; UpdatedByName is "<kind>:<name>" of it.
	UpdatedBy, UpdatedByName string
}

// Defaults are the settings of a new installation: off, 1 MiB, 60 s, no public address.
func Defaults() Settings {
	return Settings{MinSize: DefaultMinSize, LinkLifeSeconds: DefaultLinkLifeSeconds, PublicAddresses: map[string]string{}}
}

// LinkLife is the life of a link as the settings give it, before the signing credential bounds it.
func (s Settings) LinkLife() time.Duration { return time.Duration(s.LinkLifeSeconds) * time.Second }

// state is what an audit event's before and after hold of the settings.
func (s Settings) state() map[string]any {
	addrs := s.PublicAddresses
	if addrs == nil {
		addrs = map[string]string{}
	}
	return map[string]any{"enabled": s.Enabled, "minSize": s.MinSize, "linkLifeSeconds": s.LinkLifeSeconds, "publicAddresses": addrs}
}

// equal reports whether two settings hold the same values, revisions aside.
func (s Settings) equal(o Settings) bool {
	return s.Enabled == o.Enabled && s.MinSize == o.MinSize && s.LinkLifeSeconds == o.LinkLifeSeconds && maps.Equal(s.PublicAddresses, o.PublicAddresses)
}

// Patch is a merge patch of the settings (R-STOR-014): a nil member keeps the stored value; a public address of nil returns that
// connection to its endpoint.
type Patch struct {
	Enabled         *bool
	MinSize         *int64
	LinkLifeSeconds *int
	PublicAddresses map[string]*string
}

// Apply is the settings with the patch applied.
func (s Settings) Apply(p Patch) Settings {
	out := s
	out.PublicAddresses = maps.Clone(s.PublicAddresses)
	if out.PublicAddresses == nil {
		out.PublicAddresses = map[string]string{}
	}
	if p.Enabled != nil {
		out.Enabled = *p.Enabled
	}
	if p.MinSize != nil {
		out.MinSize = *p.MinSize
	}
	if p.LinkLifeSeconds != nil {
		out.LinkLifeSeconds = *p.LinkLifeSeconds
	}
	for conn, addr := range p.PublicAddresses {
		if addr == nil {
			delete(out.PublicAddresses, conn)
		} else {
			out.PublicAddresses[conn] = *addr
		}
	}
	return out
}

// Violation is a value a write may not store: the JSON pointer of the member, a code (invalid_value) and why.
type Violation struct {
	Pointer, Code, Detail string
}

// The codes of a violation and of a connection's reasons.
const (
	CodeInvalidValue = "invalid_value"
)

// Validate checks the values a write would store (R-STOR-014): the ranges of minSize and linkLifeSeconds, and each public
// address against the rules of R-STOR-074, for a connection this replica configures. The answer is the violations in member order;
// none is a value that may be stored.
func Validate(s Settings, connections []string, rule HostRule) []Violation {
	var out []Violation
	if s.MinSize < 0 || s.MinSize > MaxMinSize {
		out = append(out, Violation{"/minSize", CodeInvalidValue, fmt.Sprintf("must be 0 to %d bytes", MaxMinSize)})
	}
	if s.LinkLifeSeconds < MinLinkLifeSeconds || s.LinkLifeSeconds > MaxLinkLifeSeconds {
		out = append(out, Violation{"/linkLifeSeconds", CodeInvalidValue, fmt.Sprintf("must be %d to %d seconds", MinLinkLifeSeconds, MaxLinkLifeSeconds)})
	}
	for _, conn := range slices.Sorted(maps.Keys(s.PublicAddresses)) {
		ptr := "/publicAddresses/" + escapePointer(conn)
		if !slices.Contains(connections, conn) {
			out = append(out, Violation{ptr, CodeInvalidValue, "the installation has no connection " + conn})
			continue
		}
		if err := CheckAddress(s.PublicAddresses[conn], rule); err != nil {
			out = append(out, Violation{ptr, CodeInvalidValue, err.Error()})
		}
	}
	return out
}

// escapePointer escapes a member name for a JSON pointer (RFC 6901).
func escapePointer(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

// ParseAddress reads a public address: an https origin, https://host[:port], without user information, path, query or fragment
// (R-STOR-074). It returns the origin as written and its host in lower case without the port.
func ParseAddress(s string) (origin, host string, err error) {
	u, err := url.Parse(s)
	switch {
	case err != nil || u.Host == "":
		return "", "", fmt.Errorf("%q is not an https origin (https://host[:port])", s)
	case u.Scheme != "https":
		return "", "", fmt.Errorf("%q is not https: a link is a bearer and goes to an https origin only (R-STOR-072)", s)
	case u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "":
		return "", "", fmt.Errorf("%q is not an origin: it has a path, a query, a fragment or user information", s)
	}
	h := strings.ToLower(u.Hostname())
	if h == "" {
		return "", "", fmt.Errorf("%q names no host", s)
	}
	return "https://" + u.Host, h, nil
}

// CheckAddress checks a public address as a write and every start check it (R-STOR-074): an https origin whose host is no host of
// Quader nor below one (R-STOR-072, Q-STOR-288). The host compared is the address's; the bucket's name that virtual-hosted addressing
// puts before it is checked where the connection is known (LinkHost).
func CheckAddress(addr string, rule HostRule) error {
	_, host, err := ParseAddress(addr)
	if err != nil {
		return err
	}
	if q, refused := rule.Refuses(host); refused {
		return fmt.Errorf("the host %s is a host of Quader or lies below %s: a client that keeps its credential for that host would send it to the store, which refuses such a link (R-STOR-072)", host, q)
	}
	return nil
}

// LinkHost is the host of a link signed against an address, as the store sees it (R-STOR-072): with virtual-hosted addressing the
// bucket's name before the address's host, with path-style addressing the address's host. The address is an origin or an endpoint;
// a host without a scheme is read as https.
func LinkHost(address, bucket string, pathStyle bool) string {
	if !strings.Contains(address, "://") {
		address = "https://" + address
	}
	u, err := url.Parse(address)
	if err != nil {
		return ""
	}
	h := strings.ToLower(u.Hostname())
	if pathStyle || bucket == "" || net.ParseIP(h) != nil {
		return h
	}
	return strings.ToLower(bucket) + "." + h
}

// encodeAddresses is the column public_addresses of the settings.
func encodeAddresses(m map[string]string) string {
	if m == nil {
		m = map[string]string{}
	}
	raw, _ := json.Marshal(m) // a map of strings always marshals
	return string(raw)
}

// decodeAddresses reads the column public_addresses.
func decodeAddresses(raw string) (map[string]string, error) {
	out := map[string]string{}
	if raw == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("download_link_settings.public_addresses: %w", err)
	}
	return out, nil
}
