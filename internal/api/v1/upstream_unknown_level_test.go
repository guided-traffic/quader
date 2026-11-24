package v1

import (
	"testing"

	_ "github.com/guided-traffic/quader/internal/formats/all" // the formats' presets
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// TestR_CUR_009_AnUnknownLevelNamesTheUpstream: a level page's `unknown` names the upstream by its preset's display name, else by its
// host, and carries the rule, the forbidden characters and the suggestion, null where the format sets none (Q-CUR-165). Microseconds.
func TestR_CUR_009_AnUnknownLevelNamesTheUpstream(t *testing.T) {
	for _, c := range []struct {
		format, upstream, want string
	}{
		{"oci", "https://registry-1.docker.io/", "Docker Hub"},
		{"npm", "https://registry.npmjs.org/", "npm public registry"},
		{"npm", "https://npm.example.org/", "npm.example.org"},
		{"deb", "https://download.docker.com/linux/debian/", "download.docker.com"},
	} {
		if got := upstreamName(c.format, c.upstream); got != c.want {
			t.Errorf("R-CUR-009 upstreamName(%s, %s) = %q, want %q", c.format, c.upstream, got, c.want)
		}
	}
	call := catalogueCall{}
	call.repo.Format, call.t.Upstream = "oci", "https://registry-1.docker.io/"
	if got := unknownLevel(call, nil); got != nil {
		t.Fatalf("R-CUR-009 a known level's unknown is %v, want nil", got)
	}
	got, ok := unknownLevel(call, &discover.UnknownLevel{Rule: discover.NameRuleLowercaseDigits, Forbidden: "-", Suggestion: "guidedtraffic"}).(map[string]any)
	if !ok || got["upstream"] != "Docker Hub" || got["rule"] != "lowercaseDigits" || got["forbidden"] != "-" || got["suggestion"] != "guidedtraffic" {
		t.Fatalf("R-CUR-009 unknown = %v", got)
	}
	bare, ok := unknownLevel(call, &discover.UnknownLevel{}).(map[string]any)
	if !ok || bare["rule"] != nil || bare["forbidden"] != nil || bare["suggestion"] != nil {
		t.Fatalf("R-CUR-009 a bare unknown = %v", bare)
	}
}
