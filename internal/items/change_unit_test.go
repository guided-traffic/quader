package items

import (
	"strings"
	"testing"
)

// R-OPS-056: the event of a change names the unit the audit log joins the act's events by: a push the unit of the format (an image, a
// version), a delete the package, a file of a format whose unit is a file its folder or the folder the person deleted, and a file of no
// package of a package format none. Unit tier, microseconds.
func TestR_OPS_056_AChangeNamesItsUnit(t *testing.T) {
	s := &Store{units: func(format, path string) DownloadUnit {
		switch {
		case format == "generic":
			return DownloadUnit{Files: true}
		case strings.HasSuffix(path, ".tgz"):
			return DownloadUnit{Package: "left-pad", Version: "1.3.0", Key: "left-pad@1.3.0"}
		}
		return DownloadUnit{}
	}}
	for _, c := range []struct {
		name, format, path, root string
		deleted                  bool
		want                     map[string]any
	}{
		{
			"a pushed tarball", "npm", "left-pad/-/left-pad-1.3.0.tgz", "", false,
			map[string]any{"package": "left-pad", "version": "1.3.0", "unit": "left-pad@1.3.0"},
		},
		{
			"a deleted tarball", "npm", "left-pad/-/left-pad-1.3.0.tgz", "left-pad", true,
			map[string]any{"package": "left-pad", "version": "1.3.0", "unit": "left-pad"},
		},
		{"a file uploaded into a folder", "generic", "releases/1.0/app.zip", "", false, map[string]any{"unit": "releases/1.0/"}},
		{"a file uploaded into the root", "generic", "app.zip", "", false, map[string]any{"unit": "/"}},
		{"a file of a folder deleted", "generic", "releases/1.0/bin/app", "releases", true, map[string]any{"unit": "releases/"}},
		{"a file deleted by itself", "generic", "releases/1.0/app.zip", "releases/1.0/app.zip", true, map[string]any{"unit": "releases/1.0/"}},
		{"a file of no package", "npm", "-/v1/search", "", false, map[string]any{}},
	} {
		got := map[string]any{}
		s.nameChangeUnit(got, c.format, c.path, c.root, c.deleted)
		if len(got) != len(c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
			continue
		}
		for k, v := range c.want {
			if got[k] != v {
				t.Errorf("%s: %v, want %v", c.name, got, c.want)
			}
		}
	}
	none := map[string]any{}
	(&Store{}).nameChangeUnit(none, "npm", "left-pad/-/left-pad-1.3.0.tgz", "", false)
	if len(none) != 0 {
		t.Errorf("a store without units names none: %v", none)
	}
}
