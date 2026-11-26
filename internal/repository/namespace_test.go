package repository

import "testing"

// TestR_UI_220_HowAClaimCoversANamespace: a claim covers every name of a namespace, some of them, or none, by its literal and its
// wildcard; a "*" does not cross a "/" (R-AUTHZ-005), so it covers only the first level of a nested namespace.
func TestR_UI_220_HowAClaimCoversANamespace(t *testing.T) {
	for _, c := range []struct {
		pattern, prefix string
		deep            bool
		want            string
	}{
		{"acme.*", "acme.", false, CoversAll},
		{"acme*", "acme.", false, CoversAll},
		{"**", "acme.", false, CoversAll},
		{"acme.tools", "acme.", false, CoversSome},
		{"acme.t*", "acme.", false, CoversSome},
		{"acmex.*", "acme.", false, ""},
		{"other.*", "acme.", false, ""},
		{"@acme/*", "@acme/", false, CoversAll},
		{"*", "@acme/", false, ""},
		{"@*", "@acme/", false, ""},
		{"@acme/**", "@acme/", false, CoversAll},
		{"team/*", "team/", true, CoversSome},
		{"team/**", "team/", true, CoversAll},
		{"team/app/image", "team/", true, CoversSome},
		{"t*", "team/app/", true, ""},
	} {
		p, err := ParseClaimPattern(c.pattern)
		if err != nil {
			t.Fatalf("%s: %v", c.pattern, err)
		}
		if got := p.Within(c.prefix, c.deep); got != c.want {
			t.Errorf("%q within %q (deep %v): %q, want %q", c.pattern, c.prefix, c.deep, got, c.want)
		}
	}
}
