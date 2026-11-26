package catalog

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestR_BAS_058_RegisterHoldsTheBastionFactsToTheirRules: a format's Bastion facts are canonical header names that are neither
// the core's nor Quader's, each once, and a rate-limit status of 429 or 503 (Q-BAS-181, Q-BAS-182); Register refuses anything
// else, and the facts of OCI and the ClamAV definitions pass.
func TestR_BAS_058_RegisterHoldsTheBastionFactsToTheirRules(t *testing.T) {
	with := func(id string, b BastionFacts) Entry {
		e := valid(id)
		e.Bastion = b
		return e
	}
	bad := map[string]struct {
		e    Entry
		says string
	}{
		"a status that is no rate limit":       {with("st418", BastionFacts{RateLimitedStatus: http.StatusTeapot}), "429 or 503"},
		"a negative longest wait":              {with("neg", BastionFacts{RetryAfterMax: -time.Second}), "whole seconds"},
		"a longest wait of a part of a second": {with("frac", BastionFacts{RetryAfterMax: 1500 * time.Millisecond}), "whole seconds"},
		"a header that is not canonical":       {with("canon", BastionFacts{ReplayHeaders: []string{"Docker-Distribution-API-Version"}}), "Docker-Distribution-Api-Version"},
		"a name that is no header name":        {with("token", BastionFacts{ReplayHeaders: []string{"Bad Header"}}), "no header name"},
		"an empty name":                        {with("empty", BastionFacts{ReplayHeaders: []string{""}}), "no header name"},
		"a header twice":                       {with("twice", BastionFacts{ReplayHeaders: []string{"Link", "Link"}}), "twice"},
		"a header of Quader's":                 {with("quader", BastionFacts{ReplayHeaders: []string{"Quader-Annotations-Cut"}}), "Quader's own"},
		"a representation header of the core":  {with("repr", BastionFacts{ReplayHeaders: []string{"Etag"}}), "the core's"},
		"a content header of R-ARCH-007":       {with("csp", BastionFacts{ReplayHeaders: []string{"Content-Security-Policy"}}), "the core's"},
		"a per-request header of the core":     {with("perreq", BastionFacts{ReplayHeaders: []string{"X-Request-Id"}}), "the core's"},
		"the member that answered":             {with("member", BastionFacts{ReplayHeaders: []string{"Quader-Resolved-From"}}), "Quader's own"},
		"a hop-by-hop header":                  {with("hop", BastionFacts{ReplayHeaders: []string{"Transfer-Encoding"}}), "the core's"},
		"a cookie, which names one caller":     {with("cookie", BastionFacts{ReplayHeaders: []string{"Set-Cookie"}}), "the core's"},
		"the framing of one answer":            {with("framing", BastionFacts{ReplayHeaders: []string{"Content-Length"}}), "the core's"},
		"the cache state of the remote engine": {with("cachest", BastionFacts{ReplayHeaders: []string{"Cache-Status"}}), "the core's"},
	}
	for name, c := range bad {
		t.Run("Register panics on "+name, func(t *testing.T) {
			defer func() {
				p := recover()
				if p == nil {
					t.Fatal("no panic")
				}
				if msg, _ := p.(string); !strings.Contains(msg, c.says) {
					t.Fatalf("the panic %q does not say %q", p, c.says)
				}
			}()
			NewRegistry().Register(c.e)
		})
	}
	t.Run("the facts of OCI and of the ClamAV definitions are accepted and kept as registered", func(t *testing.T) {
		r := NewRegistry()
		headers := []string{"Docker-Content-Digest", "Docker-Distribution-Api-Version", "Oci-Filters-Applied", "Link"}
		r.Register(with("oci", BastionFacts{ReplayHeaders: headers}))
		r.Register(with("clamav", BastionFacts{RateLimitedStatus: http.StatusServiceUnavailable}))
		r.Register(valid("npm"))
		headers[0] = "Changed-After-Register"
		oci, _ := r.Lookup("oci")
		clamav, _ := r.Lookup("clamav")
		npm, _ := r.Lookup("npm")
		if got := oci.Bastion.ReplayHeaders; !slices.Equal(got, []string{"Docker-Content-Digest", "Docker-Distribution-Api-Version", "Oci-Filters-Applied", "Link"}) {
			t.Errorf("OCI's replay headers = %q: the registry keeps a copy", got)
		}
		for _, c := range []struct {
			e    Entry
			want int
		}{{oci, http.StatusTooManyRequests}, {clamav, http.StatusServiceUnavailable}, {npm, http.StatusTooManyRequests}} {
			if got := c.e.Bastion.RateLimited(); got != c.want {
				t.Errorf("%s: RateLimited() = %d, want %d", c.e.ID, got, c.want)
			}
		}
	})
	t.Run("a cap of 30 s is accepted and tells at most 30 s; no cap tells the true wait", func(t *testing.T) {
		r := NewRegistry()
		r.Register(with("nuget", BastionFacts{RetryAfterMax: 30 * time.Second}))
		nuget, _ := r.Lookup("nuget")
		if nuget.Bastion.CapRetryAfter(time.Hour) != 30*time.Second || nuget.Bastion.CapRetryAfter(5*time.Second) != 5*time.Second {
			t.Errorf("CapRetryAfter with a cap of 30 s: %s, %s", nuget.Bastion.CapRetryAfter(time.Hour), nuget.Bastion.CapRetryAfter(5*time.Second))
		}
		if got := (BastionFacts{}).CapRetryAfter(24 * time.Hour); got != 24*time.Hour {
			t.Errorf("CapRetryAfter without a cap: %s", got)
		}
	})
}

// TestR_BAS_058_TheCoresHeaderListsAreCanonicalAndApart: the two lists of the core are in canonical form, hold no header twice
// and share none, so a header is either replayed or dropped by a cache, never both (Q-BAS-181).
func TestR_BAS_058_TheCoresHeaderListsAreCanonicalAndApart(t *testing.T) {
	repr, per := RepresentationHeaders(), PerRequestHeaders()
	seen := map[string]string{}
	for list, hs := range map[string][]string{"representation": repr, "per-request": per} {
		for _, h := range hs {
			if http.CanonicalHeaderKey(h) != h || !validHeaderName(h) {
				t.Errorf("%s header %q is not in canonical form", list, h)
			}
			if other, dup := seen[h]; dup {
				t.Errorf("%q is a %s header and a %s header", h, other, list)
			}
			seen[h] = list
		}
	}
	for _, h := range []string{"Etag", "Content-Type", "X-Content-Type-Options", "Content-Security-Policy", "Cross-Origin-Resource-Policy"} {
		if !slices.Contains(repr, h) {
			t.Errorf("the representation headers lack %s", h)
		}
	}
}

// TestR_EP_006_TheRegistryRootHasTheBoundPrefixV2: the format that declares the registry root has the bound-host prefix /v2/
// and every other format none (Q-BAS-183); the handshake's format table carries it (R-BAS-057).
func TestR_EP_006_TheRegistryRootHasTheBoundPrefixV2(t *testing.T) {
	root := Facts{ID: "oci", RegistryRoot: true, ProtocolBasePath: RegistryRootBasePath}
	if got := root.BoundPrefixes(); !slices.Equal(got, []string{"/v2/"}) {
		t.Errorf("BoundPrefixes of the registry root = %q", got)
	}
	if got := (Facts{ID: "npm"}).BoundPrefixes(); len(got) != 0 {
		t.Errorf("BoundPrefixes of npm = %q, want none", got)
	}
}
