package catalogue

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/cache"
	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// Reader is what the web interface (W-35's P9) and the anonymous answers of W-34 read of the catalogue (R-BAS-113). Every answer
// comes from the replica's memory and its cache backend; none reaches the main installation. While the web interface is off every
// read answers [ErrOff], while the replica holds no state [ErrNotReady].
type Reader interface {
	// State is the state of the web interface: on or off, whether it serves, and its age.
	State() State
	// ConfirmedAt is the time of the replica's last confirmed state; zero while it holds none.
	ConfirmedAt() time.Time
	// PublicSet is the projects and repositories the catalogue shows.
	PublicSet() (iface.CataloguePublic, error)
	// Search lists the public packages by a query, a page at a time, from the narrowest index that answers it (R-BAS-130): a
	// repository's for exactly one repository, a project's for a project, the catalogue's own otherwise.
	Search(q Query) (Page, error)
	// Package is a public package of the index by its catalogue ID; ok is false for one the catalogue does not show.
	Package(id string) (Entry, bool, error)
	// Files is the public repositories whose folders and files the catalogue holds (R-BAS-118, Q-BAS-400): each by its qualified
	// name (<project>/<key>), in the public set's order, while the document of its root folder (files/<project>/<key>/folder) is
	// held. It reads no document: the snapshot's paths say it.
	Files() ([]string, error)
	// Lookup is a public package by its format and its name or its key, as the main installation's lookup finds it (R-API-029):
	// what an address of the web interface names (R-BAS-118); ok is false for one the catalogue does not show.
	Lookup(format, name string) (Entry, bool, error)
	// Versions is a page of a public package's versions, in the main installation's order, as the main installation answered them.
	Versions(ctx context.Context, id, after string, limit int) (VersionPage, error)
	// Document is a document by its path of the grammar (R-BAS-103), verified; ok is false for a path the catalogue does not hold
	// or does not show: an index page, a package that is not public, documentation while docs is off.
	Document(ctx context.Context, path string) (Document, bool, error)
	// Packages is every public package in the order of the index (by name), each with only its public repositories, and a revision
	// that is the same exactly while the index's pages, the pages of the projects' and the repositories' indexes, the public set, the
	// switches docs and hideRepositories and the documentation's trees are: what the sitemap is built from and kept by (R-BAS-122,
	// R-BAS-125, R-BAS-131).
	Packages() ([]Entry, string, error)
	// SearchDocs searches the documentation of every public package's newest version, as the main installation searches across
	// collections (R-BAS-125, formats/ansible.md R-ANS-058), a page at a time.
	SearchDocs(q DocQuery) (DocPage, error)
	// DocEntries lists every entry SearchDocs could find: the newest versions' documentation pages, for the sitemap (W-35's B.11).
	DocEntries() ([]DocHit, error)
}

var _ Reader = (*Catalogue)(nil)

// State is the state of the web interface at the replica (R-BAS-111).
type State struct {
	// Enabled is the switch gui, Docs the switch docs (R-BAS-102). Decided is false while the replica applies the built-in
	// configuration and holds no state: it does not know whether the web interface is on, and its pages answer not ready, not off.
	Enabled bool
	Docs    bool
	Decided bool
	// HideRepositories is the switch hideRepositories (R-BAS-131): the pages name no repository, and the reader shows no
	// repository's index.
	HideRepositories bool
	// Ready is true while the replica holds a state it serves; ConfirmedAt is its time.
	Ready       bool
	ConfirmedAt time.Time
	// StaleAfter is bastion.gui.stale_after; Stale is true when ConfirmedAt is older.
	StaleAfter time.Duration
	Stale      bool
	// Kept is whether the replica's state survives a restart.
	Kept bool
}

// Document is a document of the catalogue: its path, the SHA-256 and size of its raw bytes, its raw bytes and its compressed form.
type Document struct {
	Path   string
	SHA256 string
	Size   int64
	Raw    []byte
	Gzip   []byte
}

// Query is a search of the public packages (the main installation's list, R-API-029, as W-35's B.8 has it): Q a substring of the
// name, the description or a keyword without regard to case; Formats, Project and Repositories filters; Deprecated only the
// deprecated ones; Sort one of name (the default), createdAt, updatedAt and relevance, a leading - descending; After the Next of
// the page before; Limit 50 by default, at most 100.
type Query struct {
	Q            string
	Formats      []string
	Project      string
	Repositories []string
	Deprecated   bool
	Sort         string
	After        string
	Limit        int
}

// The page sizes of a search and of the versions (B.8).
const (
	DefaultLimit = 50
	MaxLimit     = 100
)

// Page is one page of a search: Next is the cursor of the following page, "" for the last.
type Page struct {
	Items []Entry
	Next  string
}

// VersionPage is one page of a package's versions: each item as the versions list of the main installation holds it.
type VersionPage struct {
	Items []json.RawMessage
	Next  string
}

// State implements [Reader].
func (c *Catalogue) State() State {
	gui, docs := c.effective()
	s := c.cur.Load()
	st := State{
		Enabled: gui, Docs: docs, HideRepositories: c.hidden(), Decided: c.decided() || s != nil, StaleAfter: c.o.StaleAfter, Kept: c.o.Kept,
	}
	if gui && s != nil {
		st.Ready, st.ConfirmedAt = true, s.confirmedAt
		st.Stale = c.o.StaleAfter > 0 && c.o.Now().Sub(s.confirmedAt) > c.o.StaleAfter
	}
	return st
}

// ConfirmedAt implements [Reader].
func (c *Catalogue) ConfirmedAt() time.Time { return c.State().ConfirmedAt }

// served is the snapshot a read answers from, or why there is none: off only when the switches were decided (a configuration of
// the main installation, stored or of a handshake); a replica that knows neither its switches nor a state is not ready, never off,
// so that a page is not answered as gone because a replica started without its volume while the main installation is unreachable
// (R-BAS-111).
func (c *Catalogue) served() (*snapshot, error) {
	if gui, _ := c.effective(); !gui && c.decided() {
		return nil, ErrOff
	}
	s := c.cur.Load()
	if s == nil {
		return nil, ErrNotReady
	}
	if gui, docs := c.effective(); s.docs != docs || s.gui != gui || s.hide != c.hidden() {
		// The switches changed since the round: docs off hides the documentation at once (R-BAS-102), hideRepositories the
		// repositories' indexes (R-BAS-131).
		n := *s
		n.gui, n.docs, n.hide = gui, docs, c.hidden()
		s = &n
	}
	return s, nil
}

// PublicSet implements [Reader].
func (c *Catalogue) PublicSet() (iface.CataloguePublic, error) {
	s, err := c.served()
	if err != nil {
		return iface.CataloguePublic{}, err
	}
	return iface.CataloguePublic{
		Projects: append([]iface.CatalogueProject{}, s.public.Projects...), Repositories: append([]iface.CatalogueRepository{}, s.public.Repositories...),
	}, nil
}

// Files implements [Reader].
func (c *Catalogue) Files() ([]string, error) {
	s, err := c.served()
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, r := range s.public.Repositories {
		project, key, ok := strings.Cut(r.Name, "/")
		if !ok {
			continue
		}
		p := iface.CataloguePath{Kind: iface.CatalogueFolder, Project: project, Repository: key}
		if _, held := s.lookup(p.String()); held && s.shows(p) {
			out = append(out, r.Name)
		}
	}
	return out, nil
}

// Package implements [Reader].
func (c *Catalogue) Package(id string) (Entry, bool, error) {
	s, err := c.served()
	if err != nil {
		return Entry{}, false, err
	}
	e := s.entry(id)
	if e == nil {
		return Entry{}, false, nil
	}
	return *e, true, nil
}

// Lookup implements [Reader]: the name first, then the key (an Ansible role's address holds its key, Q-UI-1317).
func (c *Catalogue) Lookup(format, name string) (Entry, bool, error) {
	s, err := c.served()
	if err != nil {
		return Entry{}, false, err
	}
	if s.index == nil {
		return Entry{}, false, nil
	}
	for _, by := range []map[string][]*Entry{s.index.byName, s.index.byKey} {
		for _, e := range by[format+"\x00"+name] {
			if s.visible(e) {
				return *e, true, nil
			}
		}
	}
	return Entry{}, false, nil
}

// entry is a public package of the snapshot's index.
func (s *snapshot) entry(id string) *Entry {
	if s.index == nil {
		return nil
	}
	e := s.index.byID[id]
	if e == nil || !s.visible(e) {
		return nil
	}
	return e
}

// shows reports whether the snapshot shows the document of a path (R-BAS-112): the catalogue's own index pages are the reader's own;
// a project's index while the project is public, a repository's while the repository is and the repositories are shown (R-BAS-130,
// R-BAS-131); a package's documents while the package is public (an image's list by content and its set-up without a version among
// them), its documentation while its wiki is too and docs is on; a repository's set-up and files while the repository is public; the
// anchors, the signature objects and the catalogue's state always.
func (s *snapshot) shows(p iface.CataloguePath) bool {
	switch p.Kind {
	case iface.CatalogueIndex:
		return false
	case iface.CatalogueProjectIndex:
		return s.project(p.Project)
	case iface.CatalogueRepositoryIndex:
		_, ok := s.repos[p.Project+"/"+p.Repository]
		return ok && !s.hide
	case iface.CatalogueState, iface.CatalogueSignatureObject, iface.CatalogueTrustedRoot, iface.CatalogueChain:
		return true
	case iface.CatalogueSetup:
		if _, ok := s.repos[p.Project+"/"+p.Repository]; !ok {
			return false
		}
		return p.SetupPackage == "" || s.entry(p.SetupPackage) != nil
	case iface.CatalogueFolder, iface.CatalogueFile, iface.CatalogueFileSetup:
		_, ok := s.repos[p.Project+"/"+p.Repository]
		return ok
	}
	e := s.entry(p.Package)
	if e == nil {
		return false
	}
	return !p.Documentation() || s.wiki(e)
}

// project reports whether the public set holds the project.
func (s *snapshot) project(key string) bool {
	for _, p := range s.public.Projects {
		if p.Key == key {
			return true
		}
	}
	return false
}

// Document implements [Reader]. The compressed bytes are read from the store and verified against the SHA-256 of their raw bytes
// on every read (R-BAS-113, D6): a document the store lost or that fails is forgotten, counted and fetched again by the next round,
// and answers not found until then.
func (c *Catalogue) Document(ctx context.Context, path string) (Document, bool, error) {
	s, err := c.served()
	if err != nil {
		return Document{}, false, err
	}
	p, perr := iface.ParseCataloguePath(path)
	if perr != nil || !s.shows(p) {
		return Document{}, false, nil //nolint:nilerr // a path outside the grammar is a document the catalogue does not hold
	}
	r, ok := s.lookup(path)
	if !ok {
		return Document{}, false, nil
	}
	gz, err := c.o.Store.GetCatalogueDocument(ctx, r.sha, maxDocGzip)
	switch {
	case errors.Is(err, cache.ErrNotFound) || errors.Is(err, cache.ErrCorrupt):
		c.log.Warn("a document of the public catalogue is not in the store; it is fetched again", "path", path, "error", err)
		c.lost(r.name)
		return Document{}, false, nil
	case err != nil:
		return Document{}, false, err
	}
	raw, err := open(gz, r.sha, r.size)
	if err != nil {
		c.o.Metrics.CacheCorrupt.Inc()
		c.log.Error("a document of the public catalogue failed its verification; it is deleted and fetched again", "path", path, "error", err)
		_ = c.o.Store.DeleteCatalogueObject(context.WithoutCancel(ctx), r.name)
		c.lost(r.name)
		return Document{}, false, nil
	}
	return Document{Path: path, SHA256: r.sha, Size: r.size, Raw: raw, Gzip: gz}, true, nil
}

// Packages implements [Reader].
func (c *Catalogue) Packages() ([]Entry, string, error) {
	s, err := c.served()
	if err != nil {
		return nil, "", err
	}
	h := sha256.New()
	if s.index != nil {
		for _, p := range s.index.pages {
			_, _ = fmt.Fprintln(h, p)
		}
	}
	repos := make([]string, 0, len(s.repos))
	for name, r := range s.repos {
		repos = append(repos, fmt.Sprintf("%s %t", name, r.Wiki))
	}
	sort.Strings(repos)
	_, _ = fmt.Fprintln(h, strings.Join(repos, ","), s.docs, s.hide)
	// The scopes' pages (R-BAS-130): a repository's page in the sitemap carries the newest time of its index.
	for _, m := range []map[string]*index{s.scoped.projects, s.scoped.repositories} {
		scopes := make([]string, 0, len(m))
		for name, idx := range m {
			scopes = append(scopes, name+" "+strings.Join(idx.pages, " "))
		}
		sort.Strings(scopes)
		_, _ = fmt.Fprintln(h, strings.Join(scopes, ","))
	}
	if s.docIndex != nil {
		// The documentation's trees (R-BAS-125): a tree that changed changes the sitemap's documentation entries.
		trees := make([]string, 0, len(s.docIndex.trees))
		for id, sha := range s.docIndex.trees {
			trees = append(trees, id+" "+sha)
		}
		sort.Strings(trees)
		_, _ = fmt.Fprintln(h, strings.Join(trees, ","))
	}
	out := []Entry{}
	if s.index != nil {
		for _, e := range s.index.entries {
			var public []string
			for _, r := range e.Repositories {
				if _, ok := s.repos[r]; ok {
					public = append(public, r)
				}
			}
			if len(public) == 0 {
				continue
			}
			v := *e
			v.Repositories = public
			out = append(out, v)
		}
	}
	return out, hex.EncodeToString(h.Sum(nil)), nil
}

// Versions implements [Reader]: the versions list of the package (packages/<id>/versions), a page after the version named by
// after.
func (c *Catalogue) Versions(ctx context.Context, id, after string, limit int) (VersionPage, error) {
	limit, err := pageLimit(limit)
	if err != nil {
		return VersionPage{}, err
	}
	d, ok, err := c.Document(ctx, iface.CataloguePath{Kind: iface.CatalogueVersions, Package: id}.String())
	if err != nil || !ok {
		return VersionPage{}, err
	}
	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(d.Raw, &list); err != nil {
		return VersionPage{}, fmt.Errorf("catalogue: the versions of %s do not decode: %w", id, err)
	}
	start := 0
	if after != "" {
		start = -1
		for i, it := range list.Items {
			if versionOf(it) == after {
				start = i + 1
				break
			}
		}
		if start < 0 {
			return VersionPage{}, fmt.Errorf("%w: no version %q", ErrQuery, after)
		}
	}
	end := min(start+limit, len(list.Items))
	page := VersionPage{Items: list.Items[start:end]}
	if end < len(list.Items) && end > 0 {
		page.Next = versionOf(list.Items[end-1])
	}
	return page, nil
}

func versionOf(item json.RawMessage) string {
	var v struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(item, &v)
	return v.Version
}

func pageLimit(limit int) (int, error) {
	switch {
	case limit == 0:
		return DefaultLimit, nil
	case limit < 0 || limit > MaxLimit:
		return 0, fmt.Errorf("%w: a limit of %d, 1 to %d", ErrQuery, limit, MaxLimit)
	}
	return limit, nil
}

// sortKey is an entry's place in the order of a sort: compared field by field, the ID last, so the order is total and a cursor
// finds its place also after its entry left the index.
type sortKey struct {
	A string `json:"a"`
	B string `json:"b"`
	I string `json:"i"`
}

func keyOf(e *Entry, field, q string) sortKey {
	switch field {
	case "createdAt":
		return sortKey{A: fmt.Sprintf("%020d", e.CreatedAt.UnixNano()), B: e.Name, I: e.ID}
	case "updatedAt":
		return sortKey{A: fmt.Sprintf("%020d", e.UpdatedAt.UnixNano()), B: e.Name, I: e.ID}
	case "relevance":
		return sortKey{A: fmt.Sprint(relevance(e, q)), B: e.Name, I: e.ID}
	}
	return sortKey{A: e.Name, I: e.ID}
}

func (k sortKey) less(o sortKey, desc bool) bool {
	if k.A != o.A {
		return (k.A < o.A) != desc
	}
	if k.B != o.B {
		return (k.B < o.B) != desc
	}
	return k.I < o.I
}

// Search implements [Reader].
func (c *Catalogue) Search(q Query) (Page, error) {
	s, err := c.served()
	if err != nil {
		return Page{}, err
	}
	limit, err := pageLimit(q.Limit)
	if err != nil {
		return Page{}, err
	}
	field, desc := strings.TrimPrefix(q.Sort, "-"), strings.HasPrefix(q.Sort, "-")
	switch field {
	case "":
		field = "name"
	case "name", "createdAt", "updatedAt", "relevance":
	default:
		return Page{}, fmt.Errorf("%w: the sort %q", ErrQuery, q.Sort)
	}
	var from *sortKey
	if q.After != "" {
		raw, err := base64.RawURLEncoding.DecodeString(q.After)
		var k sortKey
		if err != nil || json.Unmarshal(raw, &k) != nil {
			return Page{}, fmt.Errorf("%w: the cursor", ErrQuery)
		}
		from = &k
	}
	needle := strings.ToLower(strings.TrimSpace(q.Q))
	type hit struct {
		e *Entry
		k sortKey
	}
	var hits []hit
	idx, f, scoped := s.searchIndex(q)
	if idx != nil {
		for _, e := range idx.entries {
			if !s.shown(e, scoped) || !matches(e, needle) || !filtered(e, f) {
				continue
			}
			k := keyOf(e, field, needle)
			if from != nil && !from.less(k, desc) {
				continue
			}
			hits = append(hits, hit{e, k})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].k.less(hits[j].k, desc) })
	page := Page{Items: []Entry{}}
	for i, h := range hits {
		if i == limit {
			b, _ := json.Marshal(hits[i-1].k)
			page.Next = base64.RawURLEncoding.EncodeToString(b)
			break
		}
		page.Items = append(page.Items, *h.e)
	}
	return page, nil
}

// searchIndex is the index a search reads (R-BAS-130, Q-BAS-507) and the query it filters that index by: a repository's own for
// exactly one repository while the repositories are shown (the index is the repository, so no filter of the repositories applies);
// a project's for a project, and for one repository whose index is not shown (hidden, or not built yet), filtered by the
// repositories; the catalogue's own otherwise. scoped is false for the catalogue's own index.
func (s *snapshot) searchIndex(q Query) (idx *index, f Query, scoped bool) {
	if len(q.Repositories) == 1 {
		name := q.Repositories[0]
		project, _, _ := strings.Cut(name, "/")
		if idx := s.scoped.repositories[name]; idx != nil && !s.hide && (q.Project == "" || q.Project == project) {
			if _, ok := s.repos[name]; ok {
				f = q
				f.Project, f.Repositories = "", nil
				return idx, f, true
			}
		}
		if q.Project == "" {
			q.Project = project
		}
	}
	if idx := s.scoped.projects[q.Project]; idx != nil && s.project(q.Project) {
		// The index is the project's: its entries name the repositories that hold them in it.
		f = q
		f.Project = ""
		return idx, f, true
	}
	return s.index, q, false
}

// shown reports whether an entry of an index is a public package: one of the catalogue's own index whose repositories are in the
// public set, and for an entry of a scope's index the same package in the catalogue's own (R-BAS-112), so that a scope lists no
// package the pages would not show.
func (s *snapshot) shown(e *Entry, scoped bool) bool {
	if scoped {
		return s.entry(e.ID) != nil
	}
	return s.visible(e)
}

// filtered applies the filters of a query to an entry.
func filtered(e *Entry, q Query) bool {
	if q.Deprecated && !e.Deprecated {
		return false
	}
	if len(q.Formats) > 0 && !contains(q.Formats, e.Format) {
		return false
	}
	if q.Project != "" || len(q.Repositories) > 0 {
		in := false
		for _, r := range e.Repositories {
			if (q.Project == "" || strings.HasPrefix(r, q.Project+"/")) && (len(q.Repositories) == 0 || contains(q.Repositories, r)) {
				in = true
				break
			}
		}
		if !in {
			return false
		}
	}
	return true
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
