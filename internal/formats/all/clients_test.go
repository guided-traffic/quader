package all

import (
	"slices"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
)

// TestR_API_053_EveryFormatNamesItsClients pins the clients each registered format names in its descriptor, the clients of
// GET /api/v1/formats and of its guide, a system-only format's too (formats/clamav.md R-CLAMAV-001). A new format, and a
// client a format adds or drops, fails here until the table says so. The check stood in the integration area packages until
// 2026-11-23, where only a builder who ran that area saw it (Q-ARCH-701); that area now holds that the API lists every
// descriptor's clients. Milliseconds.
func TestR_API_053_EveryFormatNamesItsClients(t *testing.T) {
	want := map[formats.ID][]string{
		"npm":     {"npm", "pnpm", "yarn", "yarn-classic"},
		"nuget":   {"dotnet", "nuget", "visual-studio"},
		"generic": {"curl"},
		"oci":     {"docker", "podman"},
		"deb":     {"apt"},
		"ansible": {"ansible-galaxy"},
		"clamav":  {"freshclam"},
	}
	seen := map[formats.ID]bool{}
	for _, d := range formats.Descriptors() {
		seen[d.ID] = true
		var ids []string
		for _, c := range d.Clients {
			ids = append(ids, c.ID)
		}
		w, ok := want[d.ID]
		switch {
		case !ok:
			t.Errorf("%s is registered and not in this list: name its clients here", d.ID)
		case !slices.Equal(ids, w):
			t.Errorf("%s names the clients %v, want %v", d.ID, ids, w)
		}
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("%s is listed and not registered", id)
		}
	}
}
