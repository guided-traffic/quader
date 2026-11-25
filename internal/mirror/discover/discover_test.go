package discover

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

type throttled struct{ wait time.Duration }

func (t throttled) Error() string                     { return "throttled" }
func (t throttled) RetryAfter() (time.Duration, bool) { return t.wait, t.wait > 0 }
func (t throttled) RateLimitReason() string           { return "too_many_requests" }

// Contract test of R-REPO-051, R-REPO-052, R-REPO-054 (the mirror mode, not built yet): the IDs stay out of the test names, so the
// coverage evidence does not count a requirement whose engine is missing (Q-ROAD-5).
func TestDiscoverContract(t *testing.T) {
	t.Run("a wrapped rate-limit error is found with its wait", func(t *testing.T) {
		err := fmt.Errorf("listing page 3: %w", throttled{wait: 7 * time.Second})
		rl, ok := AsRateLimited(err)
		if !ok {
			t.Fatal("AsRateLimited did not find the wrapped error")
		}
		if d, hinted := rl.RetryAfter(); !hinted || d != 7*time.Second {
			t.Fatalf("RetryAfter = %v, %v", d, hinted)
		}
		if _, ok := AsRateLimited(errors.New("connection refused")); ok {
			t.Fatal("a transport error counts as a rate limit")
		}
	})
	t.Run("auto tries feed, listing and names in that order", func(t *testing.T) {
		got := Order()
		if len(got) != 3 || got[0] != MethodFeed || got[1] != MethodListing || got[2] != MethodNames {
			t.Fatalf("Order() = %v", got)
		}
		for _, m := range got {
			if !m.Valid() {
				t.Fatalf("%q is not valid", m)
			}
		}
		if Method("crawl").Valid() {
			t.Fatal("an unknown method is valid")
		}
	})
	t.Run("the literal prefix and the bounding rule of include patterns", func(t *testing.T) {
		for _, tc := range []struct {
			pattern  string
			prefix   string
			wildcard bool
			bounding bool
		}{
			{"lodash", "lodash", false, true},
			{"@types/*", "@types/", true, true},
			{"Microsoft.Extensions.*", "Microsoft.Extensions.", true, true},
			{"*", "", true, false},
			{"**", "", true, false},
			{"@*/*", "@", true, false},
			{"?x", "", true, false},
			{`a\*b*`, "a*b", true, true},
			{"@acme/", "@acme/", false, true},
		} {
			prefix, wildcard := LiteralPrefix(tc.pattern)
			if prefix != tc.prefix || wildcard != tc.wildcard {
				t.Errorf("LiteralPrefix(%q) = %q, %v; want %q, %v", tc.pattern, prefix, wildcard, tc.prefix, tc.wildcard)
			}
			if got := Bounding(tc.pattern); got != tc.bounding {
				t.Errorf("Bounding(%q) = %v, want %v", tc.pattern, got, tc.bounding)
			}
		}
	})
}

// TestR_CUR_009_TheVersionFilterFindsAVersionByTheNameOfItsTag: the filter by a tag's name keeps the version it points at whatever its
// place in the list, so a tagged version past the first page is found (Q-CUR-182); a version's own text still matches.
func TestR_CUR_009_TheVersionFilterFindsAVersionByTheNameOfItsTag(t *testing.T) {
	all := make([]VersionFacts, 0, 150)
	for i := 150; i > 0; i-- {
		all = append(all, VersionFacts{Version: fmt.Sprintf("1.%d.0", i)})
	}
	tags := map[string]string{"next": "1.150.0", "latest": "1.150.0", "lts-legacy": "1.2.0", "gone": "0.9.0"}
	page, total, _, done := PageTaggedVersions(all, tags, VersionsRequest{Filter: "LTS", Limit: 100})
	if total != 1 || !done || len(page) != 1 || page[0].Version != "1.2.0" {
		t.Fatalf("filter LTS = %v (total %d), want 1.2.0 alone, the 149th of the list", page, total)
	}
	if page, total, _, _ = PageTaggedVersions(all, tags, VersionsRequest{Filter: "1.15"}); total != 2 || page[0].Version != "1.150.0" || page[1].Version != "1.15.0" {
		t.Fatalf("filter 1.15 = %v (total %d), want the versions that hold it", page, total)
	}
	if _, total, _, _ = PageVersions(all, VersionsRequest{Filter: "lts"}); total != 0 {
		t.Fatalf("a format without tags found %d versions by a tag's name", total)
	}
}
