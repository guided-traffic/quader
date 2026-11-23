package mirror

import (
	"fmt"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/repository"
)

type lowerNames struct{}

func (lowerNames) Normalize(name string) (string, bool) { return strings.ToLower(name), name != "" }

// presetCatalog registers the rig's format npm with the rig's upstream as its preset, so the rig's mirror reads a well-known
// upstream for the bound of R-REPO-052.
func presetCatalog() *catalog.Registry {
	r := catalog.NewRegistry()
	r.Register(catalog.Entry{Facts: catalog.Facts{
		ID: "npm", Classes: []catalog.Class{catalog.Local, catalog.Remote, catalog.Virtual}, Layout: "npm", ProtocolBasePath: "/api/npm/{project}/{key}/",
		PatternSubject: repository.SubjectName, PackageNames: true,
		UpstreamPresets: []catalog.UpstreamPreset{{ID: "npm-public", DisplayName: "Public", URL: "https://registry.example.org/", Default: true}},
	}, Names: lowerNames{}, Mirror: true})
	return r
}

// R-REPO-052, Q-CUR-43: a mirror of a well-known upstream without a bound is not dispatched and a pass of it does not start,
// whatever started its job; it keeps its pending names, and once it has a bound it is dispatched and runs. The document is built,
// not saved, as one stored under an earlier rule is.
func TestR_REPO_052_AMirrorWithoutABoundDoesNotRun(t *testing.T) {
	g := newRig(t, nil, "")
	g.names("a")
	g.plan("a", "1.0.0")
	eng, err := New(Options{
		Share: testShare, Store: g.store, Repos: g.repos, Host: g.host, Queue: g.queue, Audit: g.rec, Metrics: g.met, Holder: "holder-a",
		Clock: g.clock, Jitter: fixedJitter(0.5), Catalog: presetCatalog(),
		NormalizeName: func(_, n string) (string, bool) { return strings.ToLower(n), true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if out, err := eng.Dispatch(t.Context()); err != nil || out.Enqueued != 0 {
		t.Fatalf("the dispatch of an unbounded mirror: %+v, %v", out, err)
	}
	res, err := g.runAs(eng)
	if err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "skipped", fmt.Sprint(res.get("skipped")), "unbounded")
	if stored := g.host.stored(); len(stored) != 0 {
		t.Fatalf("an unbounded pass stored %v", stored)
	}
	// A scope with a literal prefix bounds it.
	g.update([]string{"a*"}, `{"concurrency":1}`)
	if out, err := eng.Dispatch(t.Context()); err != nil || out.Enqueued != 1 {
		t.Fatalf("the dispatch of a bounded mirror: %+v, %v", out, err)
	}
	if res, err = g.runAs(eng); err != nil {
		t.Fatal(err)
	}
	mustEqual(t, "outcome", fmt.Sprint(res.get("outcome")), "complete")
	mustContain(t, "stored", g.host.stored(), g.paths("a")...)
}
