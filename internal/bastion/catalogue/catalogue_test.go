package catalogue_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/cache"
	"github.com/guided-traffic/quader/internal/bastion/catalogue"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/maintest"
	"github.com/guided-traffic/quader/internal/bastion/metrics"
	"github.com/guided-traffic/quader/internal/config"
)

// The public catalogue at a replica (R-BAS-109 to R-BAS-113) against the in-process main installation of maintest, which keeps
// the catalogue as the builder's store does (R-BAS-108), with the real client of the main installation and a volume cache in a
// temporary directory. Documents of bytes; the package runs in about a second.

const baseURL = "https://packages.example.com"

// clock is a settable clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }

func (c *clock) Advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// rig is a main installation with a catalogue, and a replica's store and client of it.
type rig struct {
	t      *testing.T
	main   *maintest.Main
	client *mainclient.HTTPClient
	faulty *maintest.Faulty
	dir    string
	key    []byte
	clock  *clock
	m      *metrics.Metrics
	store  *cache.Cache
	cat    *catalogue.Catalogue
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, main: maintest.New(t), faulty: &maintest.Faulty{}, dir: t.TempDir(), clock: &clock{t: time.Now()}}
	r.key = make([]byte, 32)
	if _, err := rand.Read(r.key); err != nil {
		t.Fatal(err)
	}
	r.main.SetConfiguration(func(c *iface.Configuration) { c.Switches.GUI, c.Switches.Docs = true, true })
	r.main.SetPublic(public("acme/npm-local"))
	tok, err := mainclient.ParseToken(r.main.NewToken())
	if err != nil {
		t.Fatal(err)
	}
	if r.m, err = metrics.New(nil); err != nil {
		t.Fatal(err)
	}
	r.client, err = mainclient.New(mainclient.Options{
		URL: r.main.URL(), BaseURL: baseURL, Replica: "replica-1", Release: "1", Tokens: mainclient.NewHolder(tok),
		ConnectTimeout: 2 * time.Second, ResponseHeaderTimeout: 2 * time.Second, CAFile: r.main.CAFile(t.TempDir()), Metrics: r.m,
		WrapTransport: func(rt http.RoundTripper) http.RoundTripper { r.faulty.Next = rt; return r.faulty },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.client.Close)
	r.restart(filepath.Join(r.dir, "volume"))
	return r
}

// restart builds the replica's store and catalogue anew over the volume, as a restarted replica does, with no switches applied
// yet (the built-in configuration).
func (r *rig) restart(volume string) {
	r.t.Helper()
	signer, err := cache.NewSigner(r.key)
	if err != nil {
		r.t.Fatal(err)
	}
	cfg := config.BastionCache{
		Backend: "volume", MaxBytes: 1 << 30, MaxObjectBytes: 1 << 20, DocumentsMaxBytes: 1 << 20, SweepInterval: time.Minute,
		AccessResolution: time.Hour, Volume: config.BastionCacheVolume{Dir: volume},
	}
	if r.store, err = cache.New(context.Background(), cfg, signer, cache.Options{Metrics: r.m, CatalogueMaxBytes: 1 << 20}); err != nil {
		r.t.Fatal(err)
	}
	r.cat = catalogue.New(catalogue.Options{
		Source: r.client, Store: r.store, PollInterval: time.Hour, StaleAfter: time.Hour, MaxBytes: 1 << 20, Metrics: r.m, Now: r.clock.Now,
	})
}

// on applies the switches as a handshake does.
func (r *rig) on(gui, docs bool) { r.cat.SetSwitches(iface.Switches{GUI: gui, Docs: docs}, true) }

func public(repos ...string) iface.CataloguePublic {
	p := iface.CataloguePublic{Projects: []iface.CatalogueProject{}, Repositories: []iface.CatalogueRepository{}}
	seen := map[string]bool{}
	for _, name := range repos {
		project := strings.SplitN(name, "/", 2)[0]
		if !seen[project] {
			p.Projects, seen[project] = append(p.Projects, iface.CatalogueProject{Key: project, DisplayName: project}), true
		}
		p.Repositories = append(p.Repositories, iface.CatalogueRepository{Name: name, Format: "npm", Class: "local", Wiki: true})
	}
	return p
}

// pkg is a package of the fixture: its catalogue ID, name, repository and versions.
type pkg struct {
	id, name, repo string
	versions       []string
}

func vpath(id, v string, kind iface.CatalogueKind) string {
	return iface.CataloguePath{Kind: kind, Package: id, Version: v}.String()
}

// publish keeps the documents of the packages as a run of the builder does: the index, and per package its summary, its versions
// and per version the version, its README and its state.
func (r *rig) publish(pkgs ...pkg) {
	r.t.Helper()
	var items []map[string]any
	for _, p := range pkgs {
		summary := map[string]any{
			"id": p.id, "format": "npm", "name": p.name, "key": p.name, "description": "the " + p.name + " package",
			"latestVersion": p.versions[len(p.versions)-1], "keywords": []string{"kw-" + p.name}, "repositories": []string{p.repo},
			"createdAt": "2026-11-01T00:00:00Z", "updatedAt": "2026-11-0" + fmt.Sprint(len(items)+1) + "T00:00:00Z", "versionCount": len(p.versions),
		}
		items = append(items, summary)
		r.main.PutDocument(iface.CataloguePath{Kind: iface.CataloguePackage, Package: p.id}.String(), mustJSON(r.t, summary))
		var vs []map[string]any
		for _, v := range p.versions {
			vs = append(vs, map[string]any{"version": v})
			r.main.PutDocument(vpath(p.id, v, iface.CatalogueVersion), mustJSON(r.t, map[string]any{"name": p.name, "version": v}))
			r.main.PutDocument(vpath(p.id, v, iface.CatalogueReadme), []byte(`{"html":"<p>`+p.name+` `+v+`</p>"}`))
		}
		r.main.PutDocument(iface.CataloguePath{Kind: iface.CatalogueVersions, Package: p.id}.String(), mustJSON(r.t, map[string]any{"items": vs}))
	}
	r.main.PutDocument("index/0", mustJSON(r.t, map[string]any{"items": items}))
	r.main.PutDocument("state", []byte(`{"builtAt":"2026-11-26T10:00:00Z"}`))
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// doc reads a document through the reader; ok is false for one it does not serve.
func (r *rig) doc(path string) (string, bool) {
	r.t.Helper()
	d, ok, err := r.cat.Document(context.Background(), path)
	if err != nil {
		r.t.Fatalf("%s: %v", path, err)
	}
	return string(d.Raw), ok
}

func (r *rig) tick() { r.cat.Tick(context.Background()) }

var (
	leftPad = pkg{id: "pkg_A1", name: "left-pad", repo: "acme/npm-local", versions: []string{"1.0.0"}}
	rightPd = pkg{id: "pkg_B2", name: "right-pad", repo: "acme/npm-other", versions: []string{"1.0.0", "2.0.0"}}
)

// TestR_BAS_109_ARoundMirrorsTheCatalogueAndReadsOnlyWhatChanged: a round reads the public set, the change list and the documents
// in a batch, verified; the reader serves them and searches the index; a round with nothing new reads no document; a new version
// and a deleted document arrive at the next round.
func TestR_BAS_109_ARoundMirrorsTheCatalogueAndReadsOnlyWhatChanged(t *testing.T) {
	r := newRig(t)
	r.main.SetPublic(public("acme/npm-local", "acme/npm-other"))
	r.publish(leftPad, rightPd)
	r.on(true, true)
	r.tick()
	if st := r.cat.State(); !st.Ready || !st.Enabled || st.ConfirmedAt.IsZero() {
		t.Fatalf("after a round: %+v", st)
	}
	if got, ok := r.doc(vpath("pkg_B2", "2.0.0", iface.CatalogueVersion)); !ok || !strings.Contains(got, `"2.0.0"`) {
		t.Fatalf("the version: %q %v", got, ok)
	}
	if _, ok := r.doc("index/0"); ok {
		t.Error("an index page is served: it is the reader's own")
	}
	page, err := r.cat.Search(catalogue.Query{Q: "PAD", Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].Name != "left-pad" || page.Next == "" {
		t.Fatalf("the first page: %+v %v", page, err)
	}
	if page, err = r.cat.Search(catalogue.Query{Q: "pad", Limit: 1, After: page.Next}); err != nil || len(page.Items) != 1 || page.Items[0].Name != "right-pad" || page.Next != "" {
		t.Fatalf("the second page: %+v %v", page, err)
	}
	if page, _ := r.cat.Search(catalogue.Query{Sort: "-updatedAt"}); len(page.Items) != 2 || page.Items[0].ID != "pkg_B2" {
		t.Errorf("sorted by updatedAt descending: %+v", page.Items)
	}
	if page, _ := r.cat.Search(catalogue.Query{Q: "kw-left"}); len(page.Items) != 1 {
		t.Errorf("a keyword does not match: %+v", page.Items)
	}
	if _, err := r.cat.Search(catalogue.Query{Sort: "downloads"}); !errors.Is(err, catalogue.ErrQuery) {
		t.Errorf("an unknown sort: %v", err)
	}
	if vp, err := r.cat.Versions(context.Background(), "pkg_B2", "", 1); err != nil || len(vp.Items) != 1 || vp.Next != "1.0.0" {
		t.Errorf("the versions' first page: %+v %v", vp, err)
	}

	batches := len(r.main.CatalogueBatches())
	r.tick()
	if n := len(r.main.CatalogueBatches()); n != batches {
		t.Errorf("a round with nothing new read %d batches", n-batches)
	}
	r.main.PutDocument(vpath("pkg_A1", "1.1.0", iface.CatalogueVersion), []byte(`{"version":"1.1.0"}`))
	r.main.DeleteDocument(vpath("pkg_A1", "1.0.0", iface.CatalogueReadme))
	r.tick()
	if _, ok := r.doc(vpath("pkg_A1", "1.1.0", iface.CatalogueVersion)); !ok {
		t.Error("a new document is not served after the round")
	}
	if _, ok := r.doc(vpath("pkg_A1", "1.0.0", iface.CatalogueReadme)); ok {
		t.Error("a deleted document is still served")
	}
	if last := r.main.CatalogueBatches(); len(last[len(last)-1]) != 1 {
		t.Errorf("the round read %v, not the one new document", last[len(last)-1])
	}
}

// TestR_BAS_109_AResetKeepsTheOldStateServedUntilTheNewOneIsComplete: a catalogue built again answers the replica's cursor with
// reset; while the new list cannot be read whole (a document fails its verification), the old state is served with its time; the
// next round swaps the new one in, and what the new catalogue no longer holds is gone.
func TestR_BAS_109_AResetKeepsTheOldStateServedUntilTheNewOneIsComplete(t *testing.T) {
	r := newRig(t)
	r.publish(leftPad)
	r.on(true, true)
	r.tick()
	first := r.cat.ConfirmedAt()

	r.main.RebuildCatalogue()
	other := pkg{id: "pkg_C3", name: "other", repo: "acme/npm-local", versions: []string{"3.0.0"}}
	r.publish(other)
	r.main.CorruptNext(vpath("pkg_C3", "3.0.0", iface.CatalogueVersion))
	r.clock.Advance(time.Minute)
	r.tick()
	if _, ok := r.doc(vpath("pkg_A1", "1.0.0", iface.CatalogueVersion)); !ok {
		t.Fatal("the old state is not served while the reset is read")
	}
	if got := r.cat.ConfirmedAt(); !got.Equal(first) {
		t.Fatalf("a round that failed moved confirmedAt from %s to %s", first, got)
	}
	r.tick()
	if _, ok := r.doc(vpath("pkg_C3", "3.0.0", iface.CatalogueVersion)); !ok {
		t.Fatal("the new catalogue is not served after its round")
	}
	if _, ok := r.doc(vpath("pkg_A1", "1.0.0", iface.CatalogueVersion)); ok {
		t.Error("a document the rebuilt catalogue does not hold is served")
	}
}

// TestR_BAS_112_TheFeedHooksHideAndResyncWithoutWaitingForThePoll: with a poll of an hour, a grants entry makes the replica read
// the public set at once, which hides the packages of a repository that fell out of it; a content entry makes it read the change
// list at once.
func TestR_BAS_112_TheFeedHooksHideAndResyncWithoutWaitingForThePoll(t *testing.T) {
	r := newRig(t)
	r.main.SetPublic(public("acme/npm-local", "acme/npm-other"))
	r.publish(leftPad, rightPd)
	r.on(true, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.cat.Run(ctx)
	waitFor(t, "the first round", func() bool { return r.cat.State().Ready })

	r.main.SetPublic(public("acme/npm-local"))
	r.cat.Feed([]iface.FeedEntry{{Kind: iface.FeedGrants}})
	waitFor(t, "right-pad hidden", func() bool {
		_, ok := r.doc(vpath("pkg_B2", "1.0.0", iface.CatalogueVersion))
		return !ok
	})
	if page, _ := r.cat.Search(catalogue.Query{}); len(page.Items) != 1 || page.Items[0].ID != "pkg_A1" {
		t.Errorf("the search lists a hidden package: %+v", page.Items)
	}
	if _, ok, _ := r.cat.Package("pkg_B2"); ok {
		t.Error("a hidden package is answered")
	}

	r.main.DeleteDocument(vpath("pkg_A1", "1.0.0", iface.CatalogueReadme))
	r.cat.Feed([]iface.FeedEntry{{Kind: iface.FeedContent, Repositories: []string{"acme/npm-local"}}})
	waitFor(t, "the withdrawn README gone", func() bool {
		_, ok := r.doc(vpath("pkg_A1", "1.0.0", iface.CatalogueReadme))
		return !ok
	})
}

// TestR_BAS_113_AnIntegrityFailureIsFetchedAgain: a document whose bytes the main installation sends do not verify is not kept and
// fails the round, the next round fetches it; a stored document that fails its verification on a read is deleted, answers not
// found, and the next round fetches it again.
// TestR_BAS_113_TheLookupFindsAPublicPackageByItsFormatAndName: the address of a package's page names its format and its name
// (R-BAS-118); the lookup finds it there, not under another format, and not once its repository left the public set.
func TestR_BAS_113_TheLookupFindsAPublicPackageByItsFormatAndName(t *testing.T) {
	r := newRig(t)
	r.main.SetPublic(public("acme/npm-local", "acme/npm-other"))
	r.publish(leftPad, rightPd)
	r.on(true, true)
	r.tick()
	if e, ok, err := r.cat.Lookup("npm", "right-pad"); err != nil || !ok || e.ID != "pkg_B2" {
		t.Fatalf("lookup = %+v %v %v", e, ok, err)
	}
	if _, ok, _ := r.cat.Lookup("nuget", "right-pad"); ok {
		t.Error("found under another format")
	}
	r.main.SetPublic(public("acme/npm-local"))
	r.tick()
	if _, ok, _ := r.cat.Lookup("npm", "right-pad"); ok {
		t.Error("found after its repository left the public set")
	}
	r.on(false, false)
	if _, _, err := r.cat.Lookup("npm", "left-pad"); !errors.Is(err, catalogue.ErrOff) {
		t.Errorf("off: %v", err)
	}
}

// TestR_BAS_122_PackagesListsThePublicPackagesUnderARevision: the sitemap's source lists every public package with its public
// repositories only, and its revision stays while nothing it lists changed and moves when the public set or docs does (R-BAS-122).
func TestR_BAS_122_PackagesListsThePublicPackagesUnderARevision(t *testing.T) {
	r := newRig(t)
	r.main.SetPublic(public("acme/npm-local", "acme/npm-other"))
	r.publish(leftPad, rightPd)
	r.on(true, true)
	r.tick()
	list, rev, err := r.cat.Packages()
	if err != nil || len(list) != 2 || list[0].Name != "left-pad" || list[1].Name != "right-pad" ||
		strings.Join(list[1].Repositories, ",") != "acme/npm-other" {
		t.Fatalf("packages = %+v %v", list, err)
	}
	r.tick()
	if _, again, _ := r.cat.Packages(); again != rev {
		t.Error("a round that changed nothing moved the revision")
	}
	r.main.SetPublic(public("acme/npm-local"))
	r.tick()
	list, moved, _ := r.cat.Packages()
	if len(list) != 1 || list[0].Name != "left-pad" || moved == rev {
		t.Errorf("after the public set lost a repository: %+v, revision moved %t", list, moved != rev)
	}
	r.on(true, false)
	if _, docs, _ := r.cat.Packages(); docs == moved {
		t.Error("docs off did not move the revision")
	}
	r.on(false, false)
	if _, _, err := r.cat.Packages(); !errors.Is(err, catalogue.ErrOff) {
		t.Errorf("off: %v", err)
	}
}

func TestR_BAS_113_AnIntegrityFailureIsFetchedAgain(t *testing.T) {
	r := newRig(t)
	r.publish(leftPad)
	target := vpath("pkg_A1", "1.0.0", iface.CatalogueVersion)
	r.main.CorruptNext(target)
	r.on(true, true)
	r.tick()
	if r.cat.State().Ready {
		t.Fatal("a round whose document did not verify confirmed the state")
	}
	r.tick()
	if _, ok := r.doc(target); !ok {
		t.Fatal("the document is not served after the next round")
	}

	// The stored bytes changed under the replica: the read finds out, and the round fetches them again.
	sum := docSHA(t, r, target)
	stored := filepath.Join(r.dir, "volume", "catalogue", "documents", sum[0:2], sum[2:4], sum)
	if err := os.WriteFile(stored, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.doc(target); ok {
		t.Fatal("a stored document that does not verify is served")
	}
	r.tick()
	if _, ok := r.doc(target); !ok {
		t.Fatal("the document is not fetched again")
	}
}

func docSHA(t *testing.T, r *rig, path string) string {
	t.Helper()
	d, ok, err := r.cat.Document(context.Background(), path)
	if err != nil || !ok {
		t.Fatalf("%s: %v %v", path, ok, err)
	}
	return d.SHA256
}

// TestR_BAS_111_AStoredStateIsServedAfterARestartWithItsAge: a replica restarted while the main installation is unreachable serves
// the stored state with the stored confirmedAt and switches, says it is stale once stale_after has passed, and serves it on; a
// replica that finds no state answers not ready; the switch gui off makes everything unreadable at once and deletes the tree.
func TestR_BAS_111_AStoredStateIsServedAfterARestartWithItsAge(t *testing.T) {
	r := newRig(t)
	r.publish(leftPad)
	r.on(true, true)
	r.tick()
	confirmed := r.cat.ConfirmedAt()

	r.faulty.SetDown(true)
	r.restart(filepath.Join(r.dir, "volume"))
	if !r.cat.Load(context.Background()) {
		t.Fatal("the stored state is not loaded")
	}
	st := r.cat.State()
	if !st.Ready || !st.Enabled || !st.ConfirmedAt.Equal(confirmed.Truncate(time.Nanosecond)) || st.Stale {
		t.Fatalf("after the restart: %+v, confirmed %s", st, confirmed)
	}
	r.clock.Advance(2 * time.Hour)
	r.tick()
	if st := r.cat.State(); !st.Ready || !st.Stale || !st.ConfirmedAt.Equal(confirmed) {
		t.Fatalf("two hours into the outage: %+v", st)
	}
	if _, ok := r.doc(vpath("pkg_A1", "1.0.0", iface.CatalogueReadme)); !ok {
		t.Fatal("the stale state is not served")
	}

	// Without its volume a replica knows neither its switches nor a state: not ready, never off.
	r.restart(filepath.Join(r.dir, "other-volume"))
	if r.cat.Load(context.Background()) {
		t.Fatal("a state is loaded from an empty volume")
	}
	if _, _, err := r.cat.Document(context.Background(), "state"); !errors.Is(err, catalogue.ErrNotReady) {
		t.Fatalf("without a state or switches: %v", err)
	}
	r.on(true, true)
	if _, _, err := r.cat.Document(context.Background(), "state"); !errors.Is(err, catalogue.ErrNotReady) || r.cat.State().Ready {
		t.Fatalf("without a state: %v", err)
	}

	r.faulty.SetDown(false)
	r.restart(filepath.Join(r.dir, "volume"))
	r.cat.Load(context.Background())
	r.on(false, false)
	if _, _, err := r.cat.Document(context.Background(), "state"); !errors.Is(err, catalogue.ErrOff) {
		t.Fatalf("gui off: %v", err)
	}
	r.tick()
	if _, err := r.store.LoadCatalogueState(context.Background()); !errors.Is(err, cache.ErrNotFound) {
		t.Errorf("the state record survives gui off: %v", err)
	}
	n := 0
	_ = r.store.ListCatalogue(context.Background(), func(cache.CatalogueObject) error { n++; return nil })
	if n != 0 {
		t.Errorf("%d documents survive gui off", n)
	}
}

// TestR_BAS_118_FilesNamesThePublicRepositoriesWithARootFolder: the repositories whose files a page leads to (Q-BAS-400) are those of
// the public set whose root folder the catalogue holds; one that leaves the public set leaves the list, and the switch off answers
// off.
func TestR_BAS_118_FilesNamesThePublicRepositoriesWithARootFolder(t *testing.T) {
	r := newRig(t)
	r.main.SetPublic(public("acme/npm-local", "acme/downloads"))
	r.publish(leftPad)
	folder := func(repo string) string {
		return iface.CataloguePath{Kind: iface.CatalogueFolder, Project: "acme", Repository: repo}.String()
	}
	r.main.PutDocument(folder("downloads"), []byte(`{"items":[{"path":"a.txt","kind":"file"}]}`))
	r.main.PutDocument(folder("private"), []byte(`{"items":[]}`))
	r.on(true, true)
	r.tick()
	if got, err := r.cat.Files(); err != nil || strings.Join(got, ",") != "acme/downloads" {
		t.Fatalf("files = %v %v", got, err)
	}
	r.main.SetPublic(public("acme/npm-local"))
	r.tick()
	if got, err := r.cat.Files(); err != nil || len(got) != 0 {
		t.Errorf("files after the repository left the public set = %v %v", got, err)
	}
	r.on(false, false)
	if _, err := r.cat.Files(); !errors.Is(err, catalogue.ErrOff) {
		t.Errorf("off: %v", err)
	}
}

// TestR_BAS_112_DocsOffHidesTheDocumentationAtOnce: the switch docs off hides the README before any round, the rest stays.
func TestR_BAS_112_DocsOffHidesTheDocumentationAtOnce(t *testing.T) {
	r := newRig(t)
	r.publish(leftPad)
	r.on(true, true)
	r.tick()
	r.on(true, false)
	if _, ok := r.doc(vpath("pkg_A1", "1.0.0", iface.CatalogueReadme)); ok {
		t.Error("the README is served with docs off")
	}
	if _, ok := r.doc(vpath("pkg_A1", "1.0.0", iface.CatalogueVersion)); !ok {
		t.Error("the version is not served with docs off")
	}
}

// TestR_BAS_109_AReplicaOfASharedStoreTakesTheStateAnotherWrote: a replica that does not hold the lease of a shared store takes the
// state the syncing one wrote, without a request of its own to the main installation.
func TestR_BAS_109_AReplicaOfASharedStoreTakesTheStateAnotherWrote(t *testing.T) {
	r := newRig(t)
	r.publish(leftPad)
	r.on(true, true)
	r.tick()
	follower := catalogue.New(catalogue.Options{
		Source: nil, Store: r.store, SharedStore: true, Lease: func() catalogue.Lease { return taken{} },
		PollInterval: time.Hour, Metrics: r.m, Now: r.clock.Now,
	})
	follower.SetSwitches(iface.Switches{GUI: true, Docs: true}, true)
	follower.Tick(context.Background())
	if d, ok, err := follower.Document(context.Background(), vpath("pkg_A1", "1.0.0", iface.CatalogueVersion)); err != nil || !ok || d.Size == 0 {
		t.Fatalf("the follower: %v %v", ok, err)
	}
}

// taken is a lease another replica holds.
type taken struct{}

func (taken) TryAcquire(context.Context, time.Duration) (bool, func(), error) {
	return false, func() {}, nil
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
