package formathost

import (
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	rc "github.com/guided-traffic/quader/internal/remotecuration"
	"github.com/guided-traffic/quader/internal/remotecuration/rules"
	"github.com/guided-traffic/quader/internal/repository"
)

// nameFormat classifies "<name>/<version>.bin" as the version of a package, and a path of one segment as a file of no version.
type nameFormat struct{ portFormat }

func (nameFormat) Classify(p formats.Path) formats.PathInfo {
	name, file, ok := strings.Cut(p.Path, "/")
	if !ok {
		return formats.PathInfo{Kind: formats.Content, Package: p.Path}
	}
	return formats.PathInfo{Kind: formats.Content, Package: name, Version: strings.TrimSuffix(file, ".bin")}
}

func (nameFormat) Descriptor() formats.Descriptor {
	d := portFormat{id: "names"}.Descriptor()
	d.PatternSubject = catalog.SubjectName
	return d
}

// R-CUR-017 (remote-curation.md): a pass of the mirror asks only for a name its scope (remote.mirror.include) covers and of which the
// delivery rules deliver a version, and fills only a version the rules deliver; an empty scope is everything the rules deliver.
// Milliseconds, no data.
func TestR_CUR_017_TheMirrorAdmitsWhatItsScopeAndTheRulesAdmit(t *testing.T) {
	doc := `{"remote":{"url":"https://upstream.example.org/","mode":"mirror","mirror":{"include":["acme-*"]}}}`
	repo, err := repository.Build(7, 1, qr("names-remote"), repository.Remote, "names", []byte(doc), nil)
	if err != nil {
		t.Fatal(err)
	}
	repo, err = repository.WithRules(repo, rc.OnlyAllowed, []rules.Rule{
		{Kind: rc.KindPackage, Name: "acme-a", Effect: rc.Allow},
		{Kind: rc.KindVersion, Name: "acme-a", Version: "2", Effect: rc.Block},
		{Kind: rc.KindPackage, Name: "other", Effect: rc.Allow},
	}, rules.Canon{})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := mirrorScope(repo)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"acme-a": true, "acme-b": false, "other": false} {
		if got := scope(name) && repo.Delivery().Package(name).Delivered; got != want {
			t.Errorf("name %s: admitted %v, want %v", name, got, want)
		}
	}
	m := &MirrorHost{h: &Host{o: Options{Formats: func(string) (formats.Format, bool) { return nameFormat{}, true }}}}
	for path, want := range map[string]bool{"acme-a/1.bin": true, "acme-a/2.bin": false, "acme-b/1.bin": false, "acme-a": true} {
		if got := m.admitted(repo, formats.Path{Path: path}, true); got != want {
			t.Errorf("fill %s: admitted %v, want %v", path, got, want)
		}
	}
	// R-CUR-014, Q-CUR-147: a row of the download queue stores what the rules do not deliver, for examination; the patterns still hold.
	for _, path := range []string{"acme-a/2.bin", "acme-b/1.bin"} {
		if !m.admitted(repo, formats.Path{Path: path}, false) {
			t.Errorf("a download ahead of %s is not admitted", path)
		}
	}
}
