package tarball

import (
	"archive/tar"
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/untrusted"
)

// TestR_NPM_051_TheREADMEIsReadFromTheTarballByItsInstallPath: the README of a stored tarball is the file at its install path (the
// entry name without the package folder), cut at the bound without a broken character; a file the tarball lacks, a nested one of the same
// name and a link are no README. Unit, a tarball of a few hundred bytes, microseconds.
func TestR_NPM_051_TheREADMEIsReadFromTheTarballByItsInstallPath(t *testing.T) {
	data := build(t,
		ent{name: "package/package.json", body: pkgJSON},
		ent{name: "package/docs/README.md", body: "nested"},
		ent{name: "package/LINK.md", typ: tar.TypeSymlink, link: "README.md"},
		ent{name: "package/README.md", body: "# Acme UI\n\nTäglich.\n"},
	)
	read := func(name string, max int64) (string, bool) {
		t.Helper()
		b, found, err := ReadFile(context.Background(), bytes.NewReader(data), untrusted.DefaultLimits(), name, max)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return string(b), found
	}
	if got, found := read("README.md", 64<<10); !found || got != "# Acme UI\n\nTäglich.\n" {
		t.Errorf("README.md: %q %v", got, found)
	}
	// "Tä" is cut inside the ä: the partial character goes.
	if got, _ := read("README.md", int64(strings.Index("# Acme UI\n\nTäglich.\n", "ä")+1)); got != "# Acme UI\n\nT" {
		t.Errorf("cut README: %q", got)
	}
	for _, name := range []string{"CHANGELOG.md", "LINK.md", "README"} {
		if _, found := read(name, 1024); found {
			t.Errorf("%s was found", name)
		}
	}
	if _, _, err := ReadFile(context.Background(), bytes.NewReader([]byte("not a gzip stream")), untrusted.DefaultLimits(), "README.md", 1024); err == nil {
		t.Error("a broken stream read without an error")
	}
}
