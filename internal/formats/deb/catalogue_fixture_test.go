package deb

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/deb/control"
	"github.com/guided-traffic/quader/internal/formats/deb/rows"
	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// catalogueTwin answers the upstream catalogue's part of the data port from rows in memory: the rows a parse of the fixture's Packages
// index writes (testdata/catalogue/Packages: eight paragraphs of download.docker.com's trixie/stable/binary-amd64 index, recorded on
// 2026-11-24), matched as the statement matches them (every word a case-insensitive substring of the name, the Provides or the short
// description).
type catalogueTwin struct {
	twinPort
	dir, index string
	entries    []rows.Entry
	provides   map[string]string
	desc       map[string]string
}

func newCatalogueTwin() (*catalogueTwin, error) {
	raw, err := os.ReadFile(filepath.Join("testdata", "catalogue", "Packages"))
	if err != nil {
		return nil, err
	}
	t := &catalogueTwin{
		twinPort: twinPort{twinStore: newTwin()}, dir: "dists/trixie", index: "stable/binary-amd64/Packages",
		provides: map[string]string{}, desc: map[string]string{},
	}
	cr := control.NewReader(strings.NewReader(string(raw)), false)
	for {
		p, err := cr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		size, _ := strconv.ParseInt(p.Value("Size"), 10, 64)
		name := p.Value("Package")
		t.entries = append(t.entries, rows.Entry{
			Dir: t.dir, IndexPath: t.index, Package: name, Version: p.Value("Version"), Architecture: p.Value("Architecture"),
			Path: p.Value("Filename"), SHA256: sha256.Sum256([]byte(p.Value("Filename"))), Size: size,
		})
		t.provides[name] = strings.ToLower(p.Value("Provides"))
		short, _, _ := strings.Cut(p.Value("Description"), "\n")
		t.desc[name] = short
	}
	return t, nil
}

func (t *catalogueTwin) CatalogueSuites(context.Context, int64) ([]rows.CatalogueSuite, error) {
	return []rows.CatalogueSuite{{Dir: t.dir, Indexes: []string{t.index}}, {Dir: "dists/bookworm"}}, nil
}

// word undoes CataloguePattern.
func word(pattern string) string {
	p := strings.TrimSuffix(strings.TrimPrefix(pattern, "%"), "%")
	return strings.NewReplacer(`\%`, `%`, `\_`, `_`, `\\`, `\`).Replace(p)
}

func (t *catalogueTwin) CatalogueSearch(_ context.Context, q rows.CatalogueQuery) ([]rows.CatalogueHit, error) {
	byName := map[string]*rows.CatalogueHit{}
	for _, e := range t.entries {
		if e.Package <= q.After || (q.Dir != "" && e.Dir != q.Dir) || (q.IndexPath != "" && e.IndexPath != q.IndexPath) {
			continue
		}
		ok := true
		for _, p := range q.Patterns {
			w := strings.ToLower(word(p))
			in := strings.Contains(e.Package, w) || strings.Contains(t.provides[e.Package], w) ||
				(!q.NamesOnly && strings.Contains(strings.ToLower(t.desc[e.Package]), w))
			ok = ok && in
		}
		if !ok {
			continue
		}
		h, seen := byName[e.Package]
		if !seen {
			h = &rows.CatalogueHit{Name: e.Package, Description: t.desc[e.Package]}
			byName[e.Package] = h
		}
		h.Versions = append(h.Versions, e.Version)
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	out := []rows.CatalogueHit{}
	for _, n := range names {
		if len(out) == q.Limit {
			break
		}
		out = append(out, *byName[n])
	}
	return out, nil
}

func (t *catalogueTwin) CatalogueRows(_ context.Context, _ int64, name string) ([]rows.Entry, error) {
	var out []rows.Entry
	for _, e := range t.entries {
		if e.Package == name {
			out = append(out, e)
		}
	}
	return out, nil
}

// catalogueFixture is check 22's fixture (R-CUR-007, R-DEB-036): the search "docker" over the parsed index (two names, a page of
// one), the level trixie/stable/amd64 and the versions of docker-secrets-engine (three), and the suite trixy, which the upstream does
// not have: neither its InRelease nor its Release is there (Q-CUR-165).
func catalogueFixture() *formattest.CatalogueFixture {
	inRelease, err := os.ReadFile(filepath.Join("testdata", "catalogue", "trixie-InRelease"))
	if err != nil {
		panic(err)
	}
	return &formattest.CatalogueFixture{Cases: []formattest.CatalogueCase{{
		Name: "a remote with a parsed index", Base: "https://download.docker.com/linux/debian/", Limit: 1,
		Search: "docker", Level: []string{"trixie", "stable", "amd64"}, Package: "docker-secrets-engine", Absent: "absent-package",
		Documents:    map[string][]byte{"dists/trixie/InRelease": inRelease},
		UnknownLevel: []string{"trixy"},
		Seed: func(h *formattest.Host) error {
			twin, err := newCatalogueTwin()
			if err != nil {
				return err
			}
			f, ok := h.Format().(*Format)
			if !ok {
				return errors.New("the kit's format is not the Debian format")
			}
			f.Bind(formattest.NewHost(f, formattest.HostOptions{Class: formats.Remote, Data: twin, Resolver: noShadow{}}), nil, nil)
			return nil
		},
	}}}
}
