package downloadlinks

import (
	"net"
	"net/url"
	"slices"
	"strings"

	"github.com/guided-traffic/quader/internal/config"
)

// HostRule is the set of hosts Quader answers on, as its configuration names them (R-STOR-072, Q-STOR-288): a link's host must be
// none of them and lie below none, because the clients written in Go keep their Authorization on a redirect to the same host or a
// subdomain of it, and an object store refuses a signed link that arrives with a credential. A host Quader is reached by that its
// configuration does not list is not seen (H-1282).
type HostRule struct {
	// exact are the host names: the hosts of server.base_url, server.additional_base_urls and server.content_base_url, and of every
	// endpoint its base_url, its hosts and the host names of its host_roots.
	exact []string
	// suffixes are the wildcard hosts of the endpoints without their "*": ".packages.example.com".
	suffixes []string
}

// NewHostRule reads the hosts of a server section.
func NewHostRule(s config.Server) HostRule {
	var r HostRule
	add := func(h string) {
		h = strings.TrimSuffix(strings.ToLower(h), ".")
		if h != "" && !slices.Contains(r.exact, h) {
			r.exact = append(r.exact, h)
		}
	}
	for _, u := range append([]string{s.BaseURL, s.ContentBaseURL}, s.AdditionalBaseURLs...) {
		add(originHost(u))
	}
	for _, ep := range s.ResolveEndpoints() {
		add(originHost(ep.BaseURL))
		for _, h := range ep.Hosts {
			add(h)
		}
		for h := range ep.HostRoots {
			add(h)
		}
		if w := strings.ToLower(ep.WildcardHost); strings.HasPrefix(w, "*.") {
			if suf := strings.TrimPrefix(w, "*"); !slices.Contains(r.suffixes, suf) {
				r.suffixes = append(r.suffixes, suf)
			}
		}
	}
	slices.Sort(r.exact)
	slices.Sort(r.suffixes)
	return r
}

// HostRuleOf is a rule of given hosts and wildcard hosts ("*.packages.example.com"), for tests and tools.
func HostRuleOf(hosts []string, wildcards ...string) HostRule {
	var s config.Server
	s.Endpoints = map[string]config.Endpoint{config.DefaultEndpoint: {Hosts: hosts}}
	r := NewHostRule(s)
	for _, w := range wildcards {
		if w = strings.ToLower(w); strings.HasPrefix(w, "*.") {
			r.suffixes = append(r.suffixes, strings.TrimPrefix(w, "*"))
		}
	}
	return r
}

// Refuses reports whether host (lower case, without a port) is a host of Quader, lies below one, or matches or lies below a
// wildcard host, and names the host of Quader it collides with. An IP address collides only with the same address.
func (r HostRule) Refuses(host string) (string, bool) {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" {
		return "", false
	}
	ip := net.ParseIP(host) != nil
	for _, q := range r.exact {
		if host == q || (!ip && net.ParseIP(q) == nil && strings.HasSuffix(host, "."+q)) {
			return q, true
		}
	}
	if !ip {
		for _, suf := range r.suffixes {
			if strings.HasSuffix(host, suf) {
				return "*" + suf, true
			}
		}
	}
	return "", false
}

// originHost is the host of an origin or URL in lower case without the port, "" for none.
func originHost(s string) string {
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}
