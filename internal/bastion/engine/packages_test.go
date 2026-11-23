package engine

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/auth/registrytoken"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/bastion/mainclient"
	"github.com/guided-traffic/quader/internal/bastion/shared"
	"github.com/guided-traffic/quader/internal/qname"
)

// The answers per package at the Bastion (bastion.md R-BAS-084, R-BAS-085) over the engine's double of the main
// installation: a registry of one repository, web/docker, whose images are packages, read by registry tokens of the real
// encoder and by Quader tokens narrowed by package. Payloads are bytes.

const pkgRepo = "web/docker"

// pkgReader is what the double knows about a credential: the Quader token behind it (the subject's token ID), its shape on
// the repository, the packages it reads in full (the package shape of R-BAS-080, after a registry token's grant), the
// packages it may read at all, and the expiry the double reports.
type pkgReader struct {
	id   string
	read iface.Read
	full map[string]bool
	may  map[string]bool // nil: every package
	exp  *time.Time
}

// pkgItem is a manifest by tag (a document, Metadata) or a blob (an artifact, Download) of one package.
type pkgItem struct {
	pkg  string
	doc  bool
	body []byte
}

// pkgSite is a double of the main installation for one registry repository: it marks every 200 and 304 with the item's
// package and the caller's shape on it (R-BAS-082), shares a document with a caller full on its package (Q-BAS-244), refuses
// a package a caller may not read with 404 and no package, and answers the check per pair (R-BAS-083).
type pkgSite struct {
	mu      sync.Mutex
	readers map[string]*pkgReader // by token string
	items   map[string]*pkgItem   // by path
	// markAs overrides the package a path's marking names: "" for none (two packages decided), another name for another.
	markAs map[string]string
	// noExpiry makes the double report no expiry for a registry token: a defect at the main installation that the
	// Bastion's own reading of the claims bounds (R-BAS-084).
	noExpiry bool
}

func newPkgSite() *pkgSite {
	return &pkgSite{readers: map[string]*pkgReader{}, items: map[string]*pkgItem{}, markAs: map[string]string{}}
}

func (s *pkgSite) add(tok string, r *pkgReader) { s.mu.Lock(); s.readers[tok] = r; s.mu.Unlock() }

func (s *pkgSite) blob(name, seed string, n int) string {
	b := bytes.Repeat([]byte(seed), n)
	p := "/v2/" + pkgRepo + "/" + name + "/blobs/sha256:" + sha(b)
	s.mu.Lock()
	s.items[p] = &pkgItem{pkg: name, body: b}
	s.mu.Unlock()
	return p
}

func (s *pkgSite) tag(name, tag string) string {
	p := "/v2/" + pkgRepo + "/" + name + "/manifests/" + tag
	s.mu.Lock()
	s.items[p] = &pkgItem{pkg: name, doc: true, body: []byte(`{"schemaVersion":2,"name":"` + name + `","tag":"` + tag + `"}`)}
	s.mu.Unlock()
	return p
}

func (s *pkgSite) subject(tok string) (iface.Subject, *pkgReader, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.readers[tok]
	if !ok {
		return iface.Subject{Kind: iface.SubjectInvalid}, nil, false
	}
	exp := r.exp
	if s.noExpiry && strings.HasPrefix(tok, iface.RegistryTokenPrefix) {
		exp = nil
	}
	return tokenSubject(r.id, exp), r, true
}

func (r *pkgReader) packageRead(pkg string) iface.Read {
	if r.read == iface.ReadFull || r.full[pkg] {
		return iface.ReadFull
	}
	return iface.ReadPartial
}

func (s *pkgSite) checkFn(req iface.CheckRequest) (iface.CheckAnswer, error) {
	tok := ""
	if req.Credential != nil {
		tok = *req.Credential
	}
	sub, r, ok := s.subject(tok)
	if !ok {
		return iface.CheckAnswer{Subject: sub, Repositories: map[string]iface.RepositoryRead{}}, nil
	}
	out := iface.CheckAnswer{Subject: sub, Repositories: map[string]iface.RepositoryRead{}}
	for _, repo := range req.Repositories {
		out.Repositories[repo] = iface.RepositoryRead{Read: r.read}
	}
	for _, p := range req.Packages {
		out.Packages = append(out.Packages, iface.PackageRead{Repository: p.Repository, Package: p.Package, Read: r.packageRead(p.Package)})
	}
	return out, nil
}

func (s *pkgSite) doFn(f mainclient.Forward) (*mainclient.Response, error) {
	path := f.Request.URL.Path
	sub, r, ok := s.subject(tokenOf(f.Request.Header))
	if !ok {
		return resp(http.StatusUnauthorized, marking(iface.ClassPass, "", "", sub), http.Header{"Www-Authenticate": {`Bearer realm="x"`}}, ""), nil
	}
	s.mu.Lock()
	it, found := s.items[path]
	pkg, overridden := s.markAs[path]
	s.mu.Unlock()
	if !found || (r.may != nil && !r.may[it.pkg]) {
		return resp(http.StatusNotFound, marking(iface.ClassPass, r.read, pkgRepo, sub), nil, `{"errors":[{"code":"MANIFEST_UNKNOWN"}]}`), nil
	}
	if !overridden {
		pkg = it.pkg
	}
	m := marking(iface.ClassArtifact, r.read, pkgRepo, sub)
	if pkg != "" {
		m.Package, m.PackageRead = pkg, r.packageRead(pkg)
	}
	etag := `"sha256:` + sha(it.body) + `"`
	if it.doc {
		m.Class = iface.ClassPrivate
		if r.read == iface.ReadFull || m.PackageRead == iface.ReadFull {
			m.Class = iface.ClassShared
		}
	}
	h := http.Header{"Etag": {etag}, "Content-Type": {"application/vnd.oci.image.manifest.v1+json"}}
	if f.IfNoneMatch == etag {
		return resp(http.StatusNotModified, m, h, ""), nil
	}
	return resp(http.StatusOK, m, h, string(it.body)), nil
}

// pkgHarness is a harness in front of a pkgSite with a full reader ($FULL) and the registry token signer.
type pkgHarness struct {
	*harness
	site   *pkgSite
	full   string
	signer *registrytoken.Signer
}

func newPkgHarness(t *testing.T, o options) *pkgHarness {
	t.Helper()
	p := &pkgHarness{site: newPkgSite(), full: newToken(t, "pat"), signer: registrySigner(t)}
	p.site.add(p.full, &pkgReader{id: "full", read: iface.ReadFull})
	if o.main == nil {
		o.main = &fakeMain{}
	}
	o.main.doFn, o.main.checkFn = p.site.doFn, p.site.checkFn
	p.harness = newHarness(t, o)
	return p
}

// registryReader issues a registry token of the Quader token id, granted the entries, and makes the double read it as the
// main installation would: partial on the repository, full on exactly the images its grant pulls, its expiry the token's.
func (p *pkgHarness) registryReader(t *testing.T, id string, grant ...registrytoken.Entry) string {
	t.Helper()
	tok := issueRegistryToken(t, p.signer, id, p.clock.now(), grant...)
	exp := p.clock.now().Truncate(time.Second).Add(5 * time.Minute)
	full := map[string]bool{}
	for _, e := range grant {
		if e.Repo.String() == pkgRepo && e.Actions&registrytoken.Pull != 0 {
			full[e.Name] = true
		}
	}
	p.site.add(tok, &pkgReader{id: id, read: iface.ReadPartial, full: full, exp: &exp})
	return tok
}

// narrowed adds a Quader token whose shape on the repository is partial and that reads the packages in full.
func (p *pkgHarness) narrowed(t *testing.T, id string, packages ...string) string {
	t.Helper()
	tok := newToken(t, "pat")
	full := map[string]bool{}
	for _, n := range packages {
		full[n] = true
	}
	p.site.add(tok, &pkgReader{id: id, read: iface.ReadPartial, full: full, may: full})
	return tok
}

func pull(name string) registrytoken.Entry {
	return registrytoken.Entry{Repo: qname.Repo{Project: "web", Key: "docker"}, Name: name, Actions: registrytoken.Pull}
}

// viaCheck fills the path as $FULL and reads it with the token, which costs the token one check: its answers then come
// from the check and not from a marking of that path, so it holds no URL answer for it, and a later hit of the path is the
// work of its package answer alone.
func (p *pkgHarness) viaCheck(t *testing.T, path, tok string) {
	t.Helper()
	p.read(t, path, p.full)
	m := p.mark()
	p.read(t, path, tok)
	if c, n, f := p.cost(m); c > 1 || n+f != 0 {
		t.Fatalf("the first read of a filled path cost %d checks, %d conditional requests and %d forwards, want at most one check", c, n, f)
	}
}

// read requests the path with the token and fails unless the answer is 200 with the item's bytes.
func (p *pkgHarness) read(t *testing.T, path, tok string) {
	t.Helper()
	w := p.get(path, "Authorization", bearer(tok))
	p.site.mu.Lock()
	want := p.site.items[path].body
	p.site.mu.Unlock()
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), want) {
		t.Fatalf("%s: %d, %d bytes: %s", path, w.Code, w.Body.Len(), w.Body.String())
	}
}

// cost is what the requests since mark cost the main installation, by kind.
func (p *pkgHarness) cost(mark int) (checks, conditionals, forwards int) {
	for _, r := range p.main.requests()[mark:] {
		switch r.Kind {
		case mainclient.KindCheck:
			checks++
		case mainclient.KindConditional:
			conditionals++
		case mainclient.KindForward:
			forwards++
		}
	}
	return checks, conditionals, forwards
}

func (p *pkgHarness) mark() int { return len(p.main.requests()) }

func (p *pkgHarness) wantCost(t *testing.T, what string, mark, checks, conditionals, forwards int) {
	t.Helper()
	c, n, f := p.cost(mark)
	if c != checks || n != conditionals || f != forwards {
		for _, r := range p.main.requests()[mark:] {
			t.Logf("%s %s %s", r.Kind, r.Method, r.Target)
		}
		t.Fatalf("%s cost %d checks, %d conditional requests and %d forwards, want %d, %d and %d", what, c, n, f, checks, conditionals, forwards)
	}
}

// TestR_BAS_084_PackageAnswers: a package answer is recorded from a marking of the request's repository and from the check
// while the caller's shape on the repository is partial; a grants entry, and a token entry naming the Quader token behind a
// registry token, make it unusable, also for an entry another caller revalidated since; it ends at the registry token's
// expiry, also in the grace period, whatever expiry the main installation reported; the shared store carries it from
// replica A to replica B; a registry token is recorded full only on a pair its unverified grant pulls.
func TestR_BAS_084_PackageAnswers(t *testing.T) {
	ctx := context.Background()
	t.Run("recorded from a marking of the repository while the shape is partial, never for a full shape", func(t *testing.T) {
		p := newPkgHarness(t, options{})
		blob := p.site.blob("app", "a", 64)
		part := p.narrowed(t, "part", "app")
		p.read(t, blob, part)
		if a := p.e.answers.pkg(ctx, credentialKey(part), pkgRepo, "app"); a == nil || a.Read != iface.ReadFull || a.TokenID != "part" {
			t.Fatalf("the package answer of a narrowed reader: %+v", a)
		}
		p.read(t, blob, p.full)
		if a := p.e.answers.pkg(ctx, credentialKey(p.full), pkgRepo, "app"); a != nil {
			t.Fatalf("a full reader of the repository got a package answer: %+v", a)
		}
		if ent, ok := p.e.loadEntry(ctx, blob); !ok || ent.Package != "app" {
			t.Fatalf("the entry of the fill carries the package %q, want app", ent.Package)
		}
		if got := readCounter(p.m.Answers.WithLabelValues("package")); got != 1 {
			t.Fatalf("quader_bastion_answers{kind=\"package\"} = %v, want 1", got)
		}
	})

	t.Run("an answer obtained before a grants entry is not used after it, also for an entry revalidated since", func(t *testing.T) {
		p := newPkgHarness(t, options{})
		blob := p.site.blob("app", "g", 64)
		part := p.narrowed(t, "part", "app")
		p.read(t, blob, part)
		p.clock.advance(time.Second)
		p.feed.Record(iface.FeedEntry{ID: "g1", Kind: iface.FeedGrants}, p.clock.now())
		if a := p.e.answers.pkg(ctx, credentialKey(part), pkgRepo, "app"); a != nil {
			t.Fatalf("a package answer from before the grants entry is usable: %+v", a)
		}
		// $FULL revalidates the entry more than the skew margin after the entry, so it is fresh again; the narrowed reader's
		// old answer must not serve it.
		p.clock.advance(16 * time.Second)
		p.read(t, blob, p.full)
		m := p.mark()
		p.read(t, blob, part)
		if c, n, f := p.cost(m); c+n+f == 0 {
			t.Fatal("a package answer obtained before a grants entry served an entry after it")
		}
	})

	t.Run("a token entry naming the Quader token behind a registry token drops its package answers", func(t *testing.T) {
		p := newPkgHarness(t, options{})
		blob := p.site.blob("app", "t", 64)
		reg := p.registryReader(t, "qt-1", pull("app"))
		p.viaCheck(t, blob, reg)
		m := p.mark()
		p.read(t, blob, reg)
		p.wantCost(t, "a warm read on the package answer", m, 0, 0, 0)
		p.clock.advance(time.Second)
		p.feed.Record(iface.FeedEntry{ID: "t1", Kind: iface.FeedToken, Tokens: []string{"qt-1"}}, p.clock.now())
		p.site.mu.Lock()
		delete(p.site.readers, reg) // the main installation now calls the token behind it revoked
		p.site.mu.Unlock()
		m = p.mark()
		if w := p.get(blob, "Authorization", bearer(reg)); w.Code != http.StatusUnauthorized {
			t.Fatalf("a registry token whose Quader token was revoked read the entry: %d", w.Code)
		}
		if c, _, _ := p.cost(m); c != 1 {
			t.Fatal("the revoked token's package answer was used")
		}
	})

	t.Run("the answer is unusable at the registry token's expiry, also in the grace period", func(t *testing.T) {
		for _, noExpiry := range []bool{false, true} {
			name := "the expiry the main installation reported"
			if noExpiry {
				name = "no expiry reported: the claims' expiry ends it"
			}
			t.Run(name, func(t *testing.T) {
				p := newPkgHarness(t, options{})
				p.site.noExpiry = noExpiry
				blob := p.site.blob("app", "x", 64)
				reg := p.registryReader(t, "qt-2", pull("app"))
				p.read(t, blob, reg)
				p.clock.advance(5*time.Minute - time.Second)
				p.setUnreachable(true)
				if a := p.e.answers.pkg(ctx, credentialKey(reg), pkgRepo, "app"); a == nil {
					t.Fatal("the package answer ended before the token")
				}
				p.clock.advance(time.Second)
				if a := p.e.answers.pkg(ctx, credentialKey(reg), pkgRepo, "app"); a != nil {
					t.Fatalf("the package answer outlived the registry token in the grace period: %+v", a)
				}
			})
		}
	})

	t.Run("with the local shared store an answer recorded on replica A serves on B", func(t *testing.T) {
		st := shared.NewLocal(1, nil)
		st.Shared = func() bool { return true }
		a := newPkgHarness(t, options{signer: true, state: st})
		blob := a.site.blob("app", "s", 64)
		reg := a.registryReader(t, "qt-3", pull("app"))
		a.viaCheck(t, blob, reg)
		b := newHarness(t, options{signer: true, state: st, cache: a.cache, main: a.main, clock: a.clock})
		m := a.mark()
		if w := b.get(blob, "Authorization", bearer(reg)); w.Code != http.StatusOK || w.Body.Len() != 64 {
			t.Fatalf("replica B: %d", w.Code)
		}
		a.wantCost(t, "replica B's read on replica A's package answer", m, 0, 0, 0)
	})

	t.Run("a registry token is recorded full only on a pair its unverified grant pulls", func(t *testing.T) {
		p := newPkgHarness(t, options{})
		blob := p.site.blob("app", "r", 64)
		cases := []struct {
			name  string
			grant []registrytoken.Entry
			want  iface.Read
		}{
			{"granted the image", []registrytoken.Entry{pull("app")}, iface.ReadFull},
			{"granted another image", []registrytoken.Entry{pull("other")}, iface.ReadPartial},
			{"granted the image with push only", []registrytoken.Entry{{Repo: qname.Repo{Project: "web", Key: "docker"}, Name: "app", Actions: registrytoken.Push}}, iface.ReadPartial},
			{"granted the image of another repository", []registrytoken.Entry{{Repo: qname.Repo{Project: "web", Key: "other"}, Name: "app", Actions: registrytoken.Pull}}, iface.ReadPartial},
		}
		for i, c := range cases {
			reg := issueRegistryToken(t, p.signer, fmt.Sprintf("qt-g%d", i), p.clock.now(), c.grant...)
			// A main installation with a defect: it marks the token full on the image whatever the grant says.
			exp := p.clock.now().Truncate(time.Second).Add(5 * time.Minute)
			p.site.add(reg, &pkgReader{id: fmt.Sprintf("qt-g%d", i), read: iface.ReadPartial, full: map[string]bool{"app": true}, exp: &exp})
			p.read(t, blob, reg)
			if a := p.e.answers.pkg(ctx, credentialKey(reg), pkgRepo, "app"); a == nil || a.Read != c.want {
				t.Errorf("%s: the package answer is %+v, want %s", c.name, a, c.want)
			}
		}
	})
}

// TestR_BAS_084_TheEntryCarriesThePackageOfItsLastMarking: an entry takes the package of the 200 that filled it; a 304 for
// the key replaces it, and one whose marking names no package clears it, after which a reader full on the old package is
// asked about the entry again.
func TestR_BAS_084_TheEntryCarriesThePackageOfItsLastMarking(t *testing.T) {
	ctx := context.Background()
	p := newPkgHarness(t, options{cfg: []func(*configT){func(c *configT) { c.Bastion.Cache.ArtifactRevalidateAfter = 30 * time.Second }}})
	for i, c := range []struct{ name, marked string }{{"a 304 naming another package", "other"}, {"a 304 naming none", ""}} {
		blob := p.site.blob("app", fmt.Sprint(i), 64)
		part := p.narrowed(t, fmt.Sprintf("part-%d", i), "app")
		p.viaCheck(t, blob, part)
		if ent, ok := p.e.loadEntry(ctx, blob); !ok || ent.Package != "app" {
			t.Fatalf("the entry of the fill carries the package %q, want app", ent.Package)
		}
		m := p.mark()
		p.read(t, blob, part)
		p.wantCost(t, "a warm read on the package answer", m, 0, 0, 0)

		p.site.mu.Lock()
		p.site.markAs[blob] = c.marked
		p.site.mu.Unlock()
		p.clock.advance(31 * time.Second)
		p.read(t, blob, p.full) // the conditional request of a full reader renews the entry
		if ent, ok := p.e.loadEntry(ctx, blob); !ok || ent.Package != c.marked {
			t.Fatalf("%s: the entry carries the package %q", c.name, ent.Package)
		}
		m = p.mark()
		p.read(t, blob, part)
		p.wantCost(t, c.name+", the narrowed reader's next read", m, 0, 1, 0)
	}
}

// TestR_BAS_085_AnEntryIsServedOnAFullAnswerForItsPackageOnly: an entry of P is served without a request to a credential
// holding a full answer on P, and not to one holding full on Q.
func TestR_BAS_085_AnEntryIsServedOnAFullAnswerForItsPackageOnly(t *testing.T) {
	p := newPkgHarness(t, options{})
	appBlob, otherBlob := p.site.blob("app", "p", 64), p.site.blob("other", "q", 64)
	onApp := p.registryReader(t, "qt-4", pull("app"), pull("other"))
	p.site.mu.Lock()
	p.site.readers[onApp].full = map[string]bool{"app": true} // full on app only, whatever it may pull
	p.site.mu.Unlock()
	p.read(t, otherBlob, p.full)
	p.viaCheck(t, appBlob, onApp)
	m := p.mark()
	p.read(t, appBlob, onApp)
	p.wantCost(t, "the entry of the package the answer is full on", m, 0, 0, 0)
	m = p.mark()
	p.read(t, otherBlob, onApp)
	p.wantCost(t, "an entry of another package", m, 0, 1, 0)
}

// TestR_BAS_085_AWarmPullWithANewRegistryTokenCostsOneCheck: a tag document and eleven blobs a first registry token filled
// cost a second registry token of the same grant exactly one check, which names the image, and no conditional request.
func TestR_BAS_085_AWarmPullWithANewRegistryTokenCostsOneCheck(t *testing.T) {
	p := newPkgHarness(t, options{})
	paths := []string{p.site.tag("team/app", "1")}
	for i := range 11 {
		paths = append(paths, p.site.blob("team/app", string(rune('a'+i)), 32))
	}
	first := p.registryReader(t, "qt-5", pull("team/app"))
	for _, path := range paths {
		p.read(t, path, first)
	}
	p.clock.advance(time.Second)
	second := p.registryReader(t, "qt-5", pull("team/app"))
	if second == first {
		t.Fatal("the second registry token is the first")
	}
	m := p.mark()
	for _, path := range paths {
		p.read(t, path, second)
	}
	p.wantCost(t, "a warm pull with a new registry token", m, 1, 0, 0)
	chk := p.main.requests()[m].Check
	if chk == nil || len(chk.Packages) != 1 || chk.Packages[0] != (iface.CheckPackage{Repository: pkgRepo, Package: "team/app"}) || chk.Repositories[0] != pkgRepo {
		t.Fatalf("the check asked %+v, want the repository and the image", chk)
	}
}

// TestR_BAS_085_NoRoundTripTellsACachedPackageFromAnUncachedOne: with a known partial shape on the repository, a cached and
// an uncached path of a package the caller may not read each cost exactly one request, and no check is sent.
func TestR_BAS_085_NoRoundTripTellsACachedPackageFromAnUncachedOne(t *testing.T) {
	p := newPkgHarness(t, options{})
	mine, cached, uncached := p.site.blob("app", "m", 64), p.site.blob("hidden", "h", 64), p.site.blob("hidden", "u", 64)
	part := p.narrowed(t, "part", "app")
	p.read(t, mine, part) // the narrowed reader's shape on the repository is known: partial
	p.read(t, cached, p.full)
	for _, c := range []struct{ name, path string }{{"the cached path", cached}, {"the uncached path", uncached}} {
		m := p.mark()
		if w := p.get(c.path, "Authorization", bearer(part)); w.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", c.name, w.Code)
		}
		if checks, n, f := p.cost(m); checks != 0 || n+f != 1 {
			t.Fatalf("%s cost %d checks and %d requests, want no check and one request", c.name, checks, n+f)
		}
	}
}

// TestR_BAS_085_AContentEntryMakesTheNextReadOfAPackageReaderAConditionalRequest: a content entry of the repository leaves a
// package answer usable and makes the entry stale, so the next read of a caller full on the package is a conditional
// request, not a check, and the read after its 304 is a hit again.
func TestR_BAS_085_AContentEntryMakesTheNextReadOfAPackageReaderAConditionalRequest(t *testing.T) {
	ctx := context.Background()
	p := newPkgHarness(t, options{})
	blob := p.site.blob("app", "c", 64)
	part := p.narrowed(t, "part", "app")
	p.viaCheck(t, blob, part)
	p.clock.advance(time.Second)
	p.feed.Record(iface.FeedEntry{ID: "c1", Kind: iface.FeedContent, Repositories: []string{pkgRepo}}, p.clock.now())
	if a := p.e.answers.pkg(ctx, credentialKey(part), pkgRepo, "app"); a == nil {
		t.Fatal("a content entry dropped the package answer")
	}
	p.clock.advance(16 * time.Second)
	m := p.mark()
	p.read(t, blob, part)
	p.wantCost(t, "the read after a content entry", m, 0, 1, 0)
	m = p.mark()
	p.read(t, blob, part)
	p.wantCost(t, "the read after the revalidation", m, 0, 0, 0)
}

// TestR_BAS_085_AFollowerFullOnTheFillsPackageFollowsItWithoutARequest: a caller whose package answer is full on the package
// the leader's marking named follows a running fill of the key from its spool without a request of its own.
func TestR_BAS_085_AFollowerFullOnTheFillsPackageFollowsItWithoutARequest(t *testing.T) {
	p := newPkgHarness(t, options{})
	small, big := p.site.blob("app", "s", 64), p.site.blob("app", "b", 128<<10)
	part := p.narrowed(t, "part", "app")
	p.read(t, small, part) // the follower's package answer on app
	p.site.mu.Lock()
	data := p.site.items[big].body
	p.site.mu.Unlock()
	body := newSteppedBody(data, 16<<10)
	viaSite := p.main.doFn
	p.main.doFn = func(f mainclient.Forward) (*mainclient.Response, error) {
		r, err := viaSite(f)
		if err == nil && r.Status == http.StatusOK && f.Request.URL.Path == big {
			r.Body = body
		}
		return r, err
	}
	m, forwards := p.mark(), p.main.count(mainclient.KindForward)
	_, leaderDone := serveAsync(p.harness, big, "Authorization", bearer(p.full))
	body.release(1)
	eventuallyTrue(t, 5*time.Second, "the leader's fill", func() bool { return p.main.count(mainclient.KindForward) > forwards })
	fw, followerDone := serveAsync(p.harness, big, "Authorization", bearer(part))
	// The follower streams from the spool while the fill still runs: 16 KiB are written, the rest waits for the test.
	eventuallyTrue(t, 5*time.Second, "the follower to stream from the running fill", func() bool { _, b := fw.got(); return len(b) > 0 })
	body.release(7)
	if waitDone(t, leaderDone) || waitDone(t, followerDone) {
		t.Fatal("a transfer was aborted")
	}
	if st, got := fw.got(); st != http.StatusOK || !bytes.Equal(got, data) {
		t.Fatalf("the follower: %d, %d bytes", st, len(got))
	}
	p.wantCost(t, "the leader's fill and the follower", m, 0, 0, 1)
}

// TestR_BAS_085_ACallerReachingTheCheckAfterAnEarlierCheckEndedUsesItsPackageAnswer: storedCheck rebuilds the package answer
// an earlier check of the same credential, repository and package stored, without extending it, and no second check is sent.
func TestR_BAS_085_ACallerReachingTheCheckAfterAnEarlierCheckEndedUsesItsPackageAnswer(t *testing.T) {
	ctx := context.Background()
	p := newPkgHarness(t, options{})
	tag := p.site.tag("app", "1")
	p.read(t, tag, p.full)
	reg := p.registryReader(t, "qt-6", pull("app"))
	p.read(t, tag, reg) // one check, which stores the shape and the package answer
	stored := p.e.answers.pkg(ctx, credentialKey(reg), pkgRepo, "app")
	if stored == nil || stored.Read != iface.ReadFull {
		t.Fatalf("the check stored the package answer %+v", stored)
	}
	p.clock.advance(time.Minute)
	m := p.mark()
	q := &rq{e: p.e, r: httptest.NewRequest(http.MethodGet, tag, nil), cred: credential{token: reg, key: credentialKey(reg), registry: true}, repo: pkgRepo, addr: "203.0.113.7", clientText: "203.0.113.7"}
	shape, pkg, done := p.e.checkShape(q, pkgRepo, "app")
	if done || shape == nil || shape.Read != iface.ReadPartial || pkg == nil || pkg.Read != iface.ReadFull {
		t.Fatalf("the stored answers did not answer the late caller: done=%v shape=%+v package=%+v", done, shape, pkg)
	}
	p.wantCost(t, "the late caller", m, 0, 0, 0)
	if again := p.e.answers.pkg(ctx, credentialKey(reg), pkgRepo, "app"); again == nil || !again.ObtainedAt.Equal(stored.ObtainedAt) {
		t.Fatalf("the rebuilt package answer was obtained at %v, want the stored %v", again.ObtainedAt, stored.ObtainedAt)
	}
}
