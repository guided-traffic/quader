package catalogue

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/cataloguebounds"
	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// CodeBaseURLUnknown is why the builder stops for a registration whose reported base URL is no entry of
// server.additional_base_urls, or that reported none (R-BAS-104): the documents would name another host than the Bastion's.
const CodeBaseURLUnknown = "bastion_base_url_unknown"

// revision is the builder's own part of every fingerprint: a change of what the builder makes of an answer (the strip, the paths)
// raises it, and every registration's catalogue is rendered again at its next run.
const revision = "catalogue/1"

// indexPage is the number of packages of one page of the index (index/<n>).
const indexPage = 100

// listPage is the page size the builder reads the management API's lists with.
const listPage = 100

// Registration is one registration as the builder builds it.
type Registration struct {
	ID, Name string
	// GUI and Docs are the registration's switches (R-BAS-102).
	GUI, Docs bool
	// BaseURL is the response base URL the documents are rendered on: the Bastion's reported base URL, an entry of
	// server.additional_base_urls (BaseURL). Empty stops the builder for the registration with CodeBaseURLUnknown.
	BaseURL string
}

// BaseURL answers the response base URL of a registration that reported reported (R-BAS-104, as client-setup with bastion=<name>
// decides it, R-BAS-007): the entry of additional that names it, without a trailing slash; ok is false for none.
func BaseURL(reported string, additional []string) (string, bool) {
	trimmed := strings.TrimSuffix(reported, "/")
	if trimmed == "" {
		return "", false
	}
	for _, listed := range additional {
		if strings.EqualFold(strings.TrimSuffix(listed, "/"), trimmed) {
			return strings.TrimSuffix(listed, "/"), true
		}
	}
	return "", false
}

// Options configure [New].
type Options struct {
	Store    Store
	Renderer Renderer
	// Main are the main installation's own origins (Rewrite.Main).
	Main []string
	// PolicyHash is the README renderer's policy (readme.PolicyHash): a tightened sanitizer renders every document again.
	PolicyHash string
	// Workers render packages in parallel (R-BAS-105); at least one.
	Workers int
	Now     func() time.Time
	Log     *slog.Logger
}

// Builder builds the public catalogues (R-BAS-104 to R-BAS-108).
type Builder struct {
	o Options
}

// New returns a builder.
func New(o Options) *Builder {
	if o.Workers < 1 {
		o.Workers = 1
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	return &Builder{o: o}
}

// Report is what one run did.
type Report struct {
	// Built are the registrations built, Dropped those whose catalogue was deleted, Stopped those the builder stopped for.
	Built, Dropped, Stopped []string
	// Rendered counts the units rendered, Documents the documents written, Deleted the paths deleted, Failed the units whose
	// rendering failed (kept as they were, rendered again at the next run).
	Rendered, Documents, Deleted, Failed int
	// Swept counts the documents no row names any more.
	Swept int64
}

// Run builds the catalogue of every registration whose web interface is on and drops the catalogue of every other one that holds
// one. A registration whose build fails is reported in the error and does not stop the others.
func (b *Builder) Run(ctx context.Context, regs []Registration) (Report, error) {
	var rep Report
	held, err := b.o.Store.Held(ctx)
	if err != nil {
		return rep, err
	}
	on := map[string]bool{}
	var errs []error
	for _, reg := range regs {
		if !reg.GUI {
			continue
		}
		on[reg.ID] = true
		if reg.BaseURL == "" {
			if err := b.o.Store.Stop(ctx, reg.ID, CodeBaseURLUnknown, b.o.Now().UnixMicro()); err != nil {
				errs = append(errs, err)
			}
			rep.Stopped = append(rep.Stopped, reg.Name)
			continue
		}
		if err := b.build(ctx, reg, &rep); err != nil {
			errs = append(errs, err)
			continue
		}
		rep.Built = append(rep.Built, reg.Name)
	}
	for _, id := range held {
		if on[id] {
			continue
		}
		// The web interface is off, or the registration is gone: its catalogue goes (R-BAS-102).
		if err := b.o.Store.Drop(ctx, id); err != nil {
			errs = append(errs, err)
			continue
		}
		rep.Dropped = append(rep.Dropped, id)
	}
	n, err := b.o.Store.Sweep(ctx)
	if err != nil {
		errs = append(errs, err)
	}
	rep.Swept = n
	return rep, errors.Join(errs...)
}

// format is what the builder reads of GET /api/v1/formats.
type format struct {
	Name            string                `json:"name"`
	PatternSubject  string                `json:"patternSubject"`
	Wiki            bool                  `json:"wiki"`
	Clients         []struct{ ID string } `json:"clients"`
	UpstreamPresets []struct {
		URL string `json:"url"`
	} `json:"upstreamPresets"`
}

// run is one registration's build.
type run struct {
	b       *Builder
	reg     Registration
	rd      reader
	rw      Rewrite
	formats map[string]format
	salt    []byte
	rep     *Report
	mu      sync.Mutex
	changed bool
}

func (b *Builder) build(ctx context.Context, reg Registration, rep *Report) error {
	st, ok, err := b.o.Store.State(ctx, reg.ID)
	if err != nil {
		return err
	}
	if !ok {
		if err := b.o.Store.Begin(ctx, reg.ID, b.o.Now().UnixMicro()); err != nil {
			return err
		}
	}
	r := &run{b: b, reg: reg, rd: reader{r: b.o.Renderer, base: reg.BaseURL}, rep: rep}
	r.rw = Rewrite{Base: reg.BaseURL, Main: b.o.Main, PublicRegistries: map[string]bool{}}
	var fl struct {
		Formats []format `json:"formats"`
	}
	if _, _, err := r.rd.json(ctx, "/api/v1/formats", &fl); err != nil {
		return err
	}
	r.formats = map[string]format{}
	for _, f := range fl.Formats {
		r.formats[f.Name] = f
		for _, p := range f.UpstreamPresets {
			if u, err := url.Parse(p.URL); err == nil && u.Hostname() != "" {
				r.rw.PublicRegistries[strings.ToLower(u.Hostname())] = true
			}
		}
	}
	public, repos, err := r.publicSet(ctx)
	if err != nil {
		return err
	}
	pub, err := json.Marshal(public)
	if err != nil {
		return err
	}
	formatsDoc, err := json.Marshal(fl)
	if err != nil {
		return err
	}
	r.salt = digest(revision, string(pub), boolText(reg.Docs), reg.BaseURL, b.o.PolicyHash, string(formatsDoc))
	if !bytes.Equal(st.Salt, r.salt) {
		b.o.Log.Info("the public catalogue of a Bastion is rendered whole", "bastion", reg.Name)
	}
	if err := r.units(ctx, repos); err != nil {
		return err
	}
	return b.o.Store.Finish(ctx, reg.ID, r.salt, reg.BaseURL, public, r.changed)
}

// repo is a public repository as the builder reads it.
type repo struct {
	project, key, format, class string
	wiki                        bool
}

func (p repo) name() string { return p.project + "/" + p.key }

// publicSet reads the projects and the repositories the anonymous caller reads (R-BAS-101's public set).
func (r *run) publicSet(ctx context.Context) (iface.CataloguePublic, []repo, error) {
	pub := iface.CataloguePublic{Projects: []string{}, Repositories: []iface.CatalogueRepository{}}
	projects, _, err := r.rd.list(ctx, "/api/v1/projects", listPage)
	if err != nil {
		return pub, nil, err
	}
	var repos []repo
	for _, raw := range projects {
		var p struct {
			Key string `json:"key"`
		}
		if json.Unmarshal(raw, &p) != nil || p.Key == "" {
			continue
		}
		items, ok, err := r.rd.list(ctx, "/api/v1/projects/"+esc(p.Key)+"/repositories", listPage)
		if err != nil {
			return pub, nil, err
		}
		if !ok || len(items) == 0 {
			continue
		}
		pub.Projects = append(pub.Projects, p.Key)
		for _, raw := range items {
			var rr struct {
				Key        string `json:"key"`
				Format     string `json:"format"`
				Class      string `json:"class"`
				Visibility *struct {
					Wiki string `json:"wiki"`
				} `json:"visibility"`
			}
			if json.Unmarshal(raw, &rr) != nil || rr.Key == "" {
				continue
			}
			x := repo{project: p.Key, key: rr.Key, format: rr.Format, class: rr.Class}
			x.wiki = rr.Visibility != nil && rr.Visibility.Wiki == "public"
			repos = append(repos, x)
			pub.Repositories = append(pub.Repositories, iface.CatalogueRepository{Name: x.name(), Format: x.format, Class: x.class, Wiki: x.wiki})
		}
	}
	sort.Strings(pub.Projects)
	sort.Slice(pub.Repositories, func(i, j int) bool { return pub.Repositories[i].Name < pub.Repositories[j].Name })
	sort.Slice(repos, func(i, j int) bool { return repos[i].name() < repos[j].name() })
	return pub, repos, nil
}

// entry is a package of the index.
type entry struct {
	ID     string `json:"id"`
	Format string `json:"format"`
	Name   string `json:"name"`
	raw    []byte // the stripped summary, part of the package's fingerprint
}

// units renders every unit whose fingerprint changed, the documents of the registration as a whole, and deletes the units that
// are gone.
func (r *run) units(ctx context.Context, repos []repo) error {
	store := r.b.o.Store
	id := r.reg.ID
	facts, err := store.PackageFacts(ctx)
	if err != nil {
		return err
	}
	var pathFormats []string
	for name, f := range r.formats {
		if f.PatternSubject == "path" {
			pathFormats = append(pathFormats, name)
		}
	}
	sort.Strings(pathFormats)
	repoFacts, err := store.RepositoryFacts(ctx, pathFormats)
	if err != nil {
		return err
	}
	kept, err := store.Units(ctx, id)
	if err != nil {
		return err
	}
	global, entries, err := r.index(ctx)
	if err != nil {
		return err
	}
	want := map[string]bool{"": true}
	type task struct {
		unit string
		fp   []byte
		do   func(context.Context) ([]rendered, error)
	}
	var tasks []task
	for _, e := range entries {
		want[e.ID] = true
		fp := digest(hex.EncodeToString(r.salt), facts[PackageKey{Format: e.Format, Name: e.Name}], string(e.raw))
		if u, ok := kept[e.ID]; ok && bytes.Equal(u.Fingerprint, fp) {
			continue
		}
		tasks = append(tasks, task{unit: e.ID, fp: fp, do: func(ctx context.Context) ([]rendered, error) { return r.pkg(ctx, e, repos) }})
	}
	for _, rp := range repos {
		if !slices.Contains(pathFormats, rp.format) {
			continue
		}
		unit := "files/" + rp.name()
		want[unit] = true
		fp := digest(hex.EncodeToString(r.salt), repoFacts[rp.name()])
		if u, ok := kept[unit]; ok && bytes.Equal(u.Fingerprint, fp) {
			continue
		}
		tasks = append(tasks, task{unit: unit, fp: fp, do: func(ctx context.Context) ([]rendered, error) { return r.files(ctx, rp) }})
	}

	// The units, in parallel.
	refs := map[string][]string{}
	for u, k := range kept {
		refs[u] = k.Refs
	}
	var (
		wg   sync.WaitGroup
		next = make(chan task)
		errM sync.Mutex
		errs []error
	)
	for range r.b.o.Workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range next {
				docs, err := t.do(ctx)
				if err == nil {
					var ur []string
					ur, err = r.write(ctx, t.unit, docs, t.fp)
					if err == nil {
						r.mu.Lock()
						refs[t.unit] = ur
						r.rep.Rendered++
						r.mu.Unlock()
						continue
					}
				}
				r.mu.Lock()
				r.rep.Failed++
				r.mu.Unlock()
				r.b.o.Log.Warn("a unit of a Bastion's public catalogue was not rendered; it stays as it was until the next run",
					"bastion", r.reg.Name, "unit", t.unit, "error", err)
				if ctx.Err() != nil {
					errM.Lock()
					errs = append(errs, err)
					errM.Unlock()
				}
			}
		}()
	}
	for _, t := range tasks {
		select {
		case next <- t:
		case <-ctx.Done():
		}
	}
	close(next)
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return err
	}

	// The units that are gone: every row deleted.
	live, err := store.LiveUnits(ctx, id)
	if err != nil {
		return err
	}
	for u := range kept {
		if !slices.Contains(live, u) {
			live = append(live, u)
		}
	}
	for _, u := range live {
		if want[u] {
			continue
		}
		if _, err := r.write(ctx, u, nil, nil); err != nil {
			return err
		}
		delete(refs, u)
	}

	// The documents of the registration as a whole: the index, the set-up of every repository, the anchors, the signature objects
	// the units name, and the state.
	more, err := r.wholeRegistration(ctx, repos, refs)
	if err != nil {
		return err
	}
	return r.writeGlobal(ctx, append(global, more...))
}

// rendered is one document before it is kept.
type rendered struct {
	path string
	body []byte
	refs []string
}

// index reads the index of public packages in pages of indexPage (index/<n>) and answers them with the packages they list.
func (r *run) index(ctx context.Context) ([]rendered, []entry, error) {
	items, _, err := r.rd.list(ctx, "/api/v1/packages?include=vulnerabilities", indexPage)
	if err != nil {
		return nil, nil, err
	}
	var out []rendered
	var entries []entry
	for i := 0; i == 0 || i*indexPage < len(items); i++ {
		end := min((i+1)*indexPage, len(items))
		pageItems := items[i*indexPage : end]
		doc, err := json.Marshal(page{Items: pageItems})
		if err != nil {
			return nil, nil, err
		}
		p, err := Prepare(doc, "application/json", r.rw)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, rendered{path: iface.CataloguePath{Kind: iface.CatalogueIndex, Index: i}.String(), body: p.Body})
		for _, raw := range pageItems {
			var e entry
			if json.Unmarshal(raw, &e) != nil || e.ID == "" {
				continue
			}
			sp, err := Prepare(raw, "application/json", r.rw)
			if err != nil {
				return nil, nil, err
			}
			e.raw = sp.Body
			entries = append(entries, e)
		}
	}
	return out, entries, nil
}

// doc reads one document of a package's version, or none for a 404; it records the times taken out of it in times under kind.
func (r *run) doc(ctx context.Context, path iface.CataloguePath, target string, times map[string]any, omitted *[]string) (*rendered, error) {
	a, ok, err := r.rd.get(ctx, target)
	if err != nil || !ok {
		return nil, err
	}
	p, err := Prepare(a.Body, a.ContentType, r.rw)
	if err != nil {
		return nil, errRender{target: target, status: a.Status, detail: err.Error()}
	}
	if len(p.Body) > cataloguebounds.DocumentMaxBytes {
		// A document above the bound is left out and named in its version's state (R-BAS-103's bound, the measurement's proposal 6).
		*omitted = append(*omitted, string(path.Kind))
		return nil, nil
	}
	if times != nil && len(p.Times) > 0 {
		times[string(path.Kind)] = p.Times
	}
	return &rendered{path: path.String(), body: p.Body, refs: p.Refs}, nil
}

// pkg renders the documents of one package: the package, its versions, and per version the version, its README (docs on), its open
// findings, its signatures, its contents, its documentation tree with its entries and files (docs on), its set-up per client and its
// state (R-BAS-104, the plan's section C).
func (r *run) pkg(ctx context.Context, e entry, repos []repo) ([]rendered, error) {
	base := "/api/v1/packages/" + esc(e.ID)
	var out []rendered
	var none []string
	pkgDoc, err := r.doc(ctx, iface.CataloguePath{Kind: iface.CataloguePackage, Package: e.ID}, base, nil, &none)
	if err != nil || pkgDoc == nil {
		return nil, err
	}
	out = append(out, *pkgDoc)
	items, ok, err := r.rd.list(ctx, base+"/versions?include=digest&include=kind&include=state", listPage)
	if err != nil || !ok {
		return out, err
	}
	vl, err := json.Marshal(page{Items: items})
	if err != nil {
		return nil, err
	}
	vp, err := Prepare(vl, "application/json", r.rw)
	if err != nil {
		return nil, err
	}
	out = append(out, rendered{path: iface.CataloguePath{Kind: iface.CatalogueVersions, Package: e.ID}.String(), body: vp.Body})
	f := r.formats[e.Format]
	for _, raw := range items {
		var v struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(raw, &v) != nil || v.Version == "" {
			continue
		}
		docs, err := r.version(ctx, e, f, v.Version, repos)
		if err != nil {
			return nil, err
		}
		out = append(out, docs...)
	}
	return out, nil
}

// version renders the documents of one version.
func (r *run) version(ctx context.Context, e entry, f format, version string, repos []repo) ([]rendered, error) {
	at := func(k iface.CatalogueKind) iface.CataloguePath {
		return iface.CataloguePath{Kind: k, Package: e.ID, Version: version}
	}
	vt := "/api/v1/packages/" + esc(e.ID) + "/versions/" + esc(version)
	times := map[string]any{}
	var omitted []string
	var out []rendered
	add := func(p iface.CataloguePath, target string) (*rendered, error) {
		d, err := r.doc(ctx, p, target, times, &omitted)
		if d != nil {
			out = append(out, *d)
		}
		return d, err
	}
	vd, err := add(at(iface.CatalogueVersion), vt)
	if err != nil || vd == nil {
		return nil, err
	}
	if r.reg.Docs {
		if _, err := add(at(iface.CatalogueReadme), vt+"/readme"); err != nil {
			return nil, err
		}
	}
	if _, err := add(at(iface.CatalogueVulnerabilities), vt+"/vulnerabilities"); err != nil {
		return nil, err
	}
	if _, err := add(at(iface.CatalogueSignatures), vt+"/signatures"); err != nil {
		return nil, err
	}
	if f.Name == "deb" {
		if err := r.contents(ctx, at(iface.CatalogueContents), vt+"/contents", times, &omitted, &out); err != nil {
			return nil, err
		}
	}
	if r.reg.Docs && f.Wiki {
		if err := r.docs(ctx, e, version, vt, times, &omitted, &out); err != nil {
			return nil, err
		}
	}
	if err := r.versionSetup(ctx, e, f, version, vd.body, repos, &out); err != nil {
		return nil, err
	}
	state := map[string]any{"times": times}
	if len(omitted) > 0 {
		state["omitted"] = omitted
	}
	sb, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	out = append(out, rendered{path: at(iface.CatalogueVersionState).String(), body: sb})
	return out, nil
}

// contents renders a Debian package's contents: the file list always, the changelog and the copyright only while the documentation
// is on (the plan's section C).
func (r *run) contents(ctx context.Context, p iface.CataloguePath, target string, times map[string]any, omitted *[]string, out *[]rendered) error {
	d, err := r.doc(ctx, p, target, times, omitted)
	if err != nil || d == nil {
		return err
	}
	if !r.reg.Docs {
		var m map[string]any
		dec := json.NewDecoder(bytes.NewReader(d.body))
		dec.UseNumber()
		if dec.Decode(&m) == nil {
			for _, k := range []string{"changelog", "changelogFile", "changelogTruncated", "copyright", "copyrightTruncated"} {
				delete(m, k)
			}
			if b, err := json.Marshal(m); err == nil {
				d.body = b
			}
		}
	}
	*out = append(*out, *d)
	return nil
}

// docs renders a version's documentation tree, every entry that has documentation and every file of it (the README and the
// Markdown files of docs/, and the changelog).
func (r *run) docs(ctx context.Context, e entry, version, vt string, times map[string]any, omitted *[]string, out *[]rendered) error {
	tp := iface.CataloguePath{Kind: iface.CatalogueDocs, Package: e.ID, Version: version}
	d, err := r.doc(ctx, tp, vt+"/docs", times, omitted)
	if err != nil || d == nil {
		return err
	}
	*out = append(*out, *d)
	var tree struct {
		Groups []struct {
			Types []struct {
				Entries []struct {
					Type    string `json:"type"`
					Name    string `json:"name"`
					HasDocs bool   `json:"hasDocs"`
				} `json:"entries"`
			} `json:"types"`
		} `json:"groups"`
		Files []struct {
			Name string `json:"name"`
		} `json:"files"`
		Changelog json.RawMessage `json:"changelog"`
	}
	if json.Unmarshal(d.body, &tree) != nil {
		return nil //nolint:nilerr // a tree of another shape names no entry and no file: the tree alone is kept
	}
	for _, g := range tree.Groups {
		for _, t := range g.Types {
			for _, en := range t.Entries {
				if !en.HasDocs {
					continue
				}
				p := iface.CataloguePath{Kind: iface.CatalogueDocEntry, Package: e.ID, Version: version, EntryType: en.Type, EntryName: en.Name}
				if _, err := iface.ParseCataloguePath(p.String()); err != nil {
					continue // a name the grammar does not admit has no path
				}
				x, err := r.doc(ctx, p, vt+"/docs/entries/"+esc(en.Type)+"/"+esc(en.Name), times, omitted)
				if err != nil {
					return err
				}
				if x != nil {
					*out = append(*out, *x)
				}
			}
		}
	}
	files := make([]string, 0, len(tree.Files)+1)
	for _, f := range tree.Files {
		files = append(files, f.Name)
	}
	if len(tree.Changelog) > 0 && string(tree.Changelog) != "null" {
		files = append(files, "changelog")
	}
	for _, name := range files {
		p := iface.CataloguePath{Kind: iface.CatalogueDocFile, Package: e.ID, Version: version, File: name}
		if _, err := iface.ParseCataloguePath(p.String()); err != nil {
			continue
		}
		x, err := r.doc(ctx, p, vt+"/docs/files/"+esc(name), times, omitted)
		if err != nil {
			return err
		}
		if x != nil {
			*out = append(*out, *x)
		}
	}
	return nil
}

// versionSetup renders the set-up of every client of the format for the version, in the version's first repository the public set
// holds (the plan's P6: "per version and client of the version's first client repository").
func (r *run) versionSetup(ctx context.Context, e entry, f format, version string, versionDoc []byte, repos []repo, out *[]rendered) error {
	var vd struct {
		Repositories []struct {
			Key string `json:"key"`
		} `json:"repositories"`
	}
	if json.Unmarshal(versionDoc, &vd) != nil {
		return nil //nolint:nilerr // a version of another shape names no repository: it has no set-up
	}
	var holder *repo
	for _, h := range vd.Repositories {
		for i := range repos {
			if repos[i].name() == h.Key || repos[i].key == h.Key {
				holder = &repos[i]
				break
			}
		}
		if holder != nil {
			break
		}
	}
	if holder == nil {
		return nil
	}
	for _, c := range f.Clients {
		p := iface.CataloguePath{Kind: iface.CatalogueSetup, Project: holder.project, Repository: holder.key, Client: c.ID, SetupPackage: e.ID, SetupVersion: version}
		target := "/api/v1/projects/" + esc(holder.project) + "/repositories/" + esc(holder.key) + "/client-setup?credentials=none&bastion=" +
			url.QueryEscape(r.reg.Name) + "&client=" + url.QueryEscape(c.ID) + "&package=" + url.QueryEscape(e.Name) + "&version=" + url.QueryEscape(version)
		var none []string
		d, err := r.doc(ctx, p, target, nil, &none)
		if err != nil {
			return err
		}
		if d != nil {
			*out = append(*out, *d)
		}
	}
	return nil
}

// files renders a generic repository: every folder's listing, every file's description and its set-up per client. A file whose path
// the grammar does not admit (a control character, invalid UTF-8) has no path and is left out (R-BAS-103).
func (r *run) files(ctx context.Context, rp repo) ([]rendered, error) {
	at := "/api/v1/projects/" + esc(rp.project) + "/repositories/" + esc(rp.key)
	f := r.formats[rp.format]
	var out []rendered
	folders := []string{""}
	for len(folders) > 0 {
		folder := folders[0]
		folders = folders[1:]
		fp := iface.CataloguePath{Kind: iface.CatalogueFolder, Project: rp.project, Repository: rp.key, File: folder}
		if _, err := iface.ParseCataloguePath(fp.String()); err != nil {
			continue
		}
		items, ok, err := r.rd.list(ctx, withQuery(at+"/children", "path", folder), listPage)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		lb, err := json.Marshal(page{Items: items})
		if err != nil {
			return nil, err
		}
		lp, err := Prepare(lb, "application/json", r.rw)
		if err != nil {
			return nil, err
		}
		out = append(out, rendered{path: fp.String(), body: lp.Body})
		for _, raw := range items {
			var it struct {
				Path string `json:"path"`
				Kind string `json:"kind"`
			}
			if json.Unmarshal(raw, &it) != nil || it.Path == "" {
				continue
			}
			path := strings.Trim(it.Path, "/")
			if folder != "" && !strings.HasPrefix(path, folder+"/") {
				path = folder + "/" + path
			}
			if it.Kind == "folder" {
				folders = append(folders, path)
				continue
			}
			docs, err := r.file(ctx, rp, f, at, path)
			if err != nil {
				return nil, err
			}
			out = append(out, docs...)
		}
	}
	return out, nil
}

// file renders one generic file's description and its set-up per client.
func (r *run) file(ctx context.Context, rp repo, f format, at, path string) ([]rendered, error) {
	p := iface.CataloguePath{Kind: iface.CatalogueFile, Project: rp.project, Repository: rp.key, File: path}
	if _, err := iface.ParseCataloguePath(p.String()); err != nil {
		return nil, nil //nolint:nilerr // a path the grammar does not admit has no document (R-BAS-103)
	}
	var none []string
	d, err := r.doc(ctx, p, withQuery(at+"/items", "path", path), nil, &none)
	if err != nil || d == nil {
		return nil, err
	}
	out := []rendered{*d}
	var item struct {
		Digests struct {
			SHA256 string `json:"sha256"`
		} `json:"digests"`
	}
	_ = json.Unmarshal(d.body, &item)
	for _, c := range f.Clients {
		sp := iface.CataloguePath{Kind: iface.CatalogueFileSetup, Project: rp.project, Repository: rp.key, Client: c.ID, File: path}
		target := at + "/client-setup?credentials=none&bastion=" + url.QueryEscape(r.reg.Name) + "&client=" + url.QueryEscape(c.ID) +
			"&path=" + url.QueryEscape(path)
		if item.Digests.SHA256 != "" {
			target += "&sha256=" + url.QueryEscape(item.Digests.SHA256)
		}
		x, err := r.doc(ctx, sp, target, nil, &none)
		if err != nil {
			return nil, err
		}
		if x != nil {
			out = append(out, *x)
		}
	}
	return out, nil
}

// wholeRegistration renders the documents that belong to no package: the set-up of every public repository per client, the
// anchors to verify signatures offline, and the signature objects the units name.
func (r *run) wholeRegistration(ctx context.Context, repos []repo, refs map[string][]string) ([]rendered, error) {
	var out []rendered
	var none []string
	for _, rp := range repos {
		f := r.formats[rp.format]
		for _, c := range f.Clients {
			p := iface.CataloguePath{Kind: iface.CatalogueSetup, Project: rp.project, Repository: rp.key, Client: c.ID}
			target := "/api/v1/projects/" + esc(rp.project) + "/repositories/" + esc(rp.key) + "/client-setup?credentials=none&bastion=" +
				url.QueryEscape(r.reg.Name) + "&client=" + url.QueryEscape(c.ID)
			d, err := r.doc(ctx, p, target, nil, &none)
			if err != nil {
				return nil, err
			}
			if d != nil {
				out = append(out, *d)
			}
		}
	}
	for _, s := range []struct {
		kind   iface.CatalogueKind
		target string
	}{{iface.CatalogueTrustedRoot, "/api/v1/signing/trusted-root.json"}, {iface.CatalogueChain, "/api/v1/signing/chain.pem"}} {
		d, err := r.doc(ctx, iface.CataloguePath{Kind: s.kind}, s.target, nil, &none)
		if err != nil {
			return nil, err
		}
		if d != nil {
			out = append(out, *d)
		}
	}
	objects := map[string]bool{}
	for _, list := range refs {
		for _, o := range list {
			objects[o] = true
		}
	}
	names := make([]string, 0, len(objects))
	for o := range objects {
		names = append(names, o)
	}
	sort.Strings(names)
	for _, o := range names {
		d, err := r.doc(ctx, iface.CataloguePath{Kind: iface.CatalogueSignatureObject, SHA256: o}, "/api/v1/signatures/objects/"+o, nil, &none)
		if err != nil {
			return nil, err
		}
		if d != nil {
			out = append(out, *d)
		}
	}
	return out, nil
}

// write keeps a unit's documents: the ones whose bytes changed are written, the paths it no longer has are deleted, and the unit's
// fingerprint is set (nil fp deletes the unit). It answers the signature objects the unit names.
func (r *run) write(ctx context.Context, unit string, docs []rendered, fp []byte, keep ...string) ([]string, error) {
	store := r.b.o.Store
	old, err := store.UnitRows(ctx, r.reg.ID, unit)
	if err != nil {
		return nil, err
	}
	var put []Doc
	seen := map[string]bool{}
	for _, k := range keep {
		seen[k] = true // kept as it is, never deleted by this write
	}
	refSet := map[string]bool{}
	written := map[string]bool{}
	for _, d := range docs {
		if written[d.path] {
			continue
		}
		written[d.path], seen[d.path] = true, true
		for _, x := range d.refs {
			refSet[x] = true
		}
		sum := sha256.Sum256(d.body)
		if old[d.path] == hex.EncodeToString(sum[:]) {
			continue
		}
		doc, err := NewDoc(d.path, d.body)
		if err != nil {
			return nil, err
		}
		put = append(put, doc)
	}
	var del []string
	for p, sha := range old {
		if sha != "" && !seen[p] {
			del = append(del, p)
		}
	}
	sort.Strings(del)
	refs := make([]string, 0, len(refSet))
	for x := range refSet {
		refs = append(refs, x)
	}
	sort.Strings(refs)
	var u *Unit
	if fp != nil {
		u = &Unit{Fingerprint: fp, Refs: refs}
	}
	if err := store.Write(ctx, r.reg.ID, unit, put, del, u); err != nil {
		return nil, err
	}
	if len(put)+len(del) > 0 {
		r.mu.Lock()
		r.changed = true
		r.rep.Documents += len(put)
		r.rep.Deleted += len(del)
		r.mu.Unlock()
	}
	return refs, nil
}

// writeGlobal keeps the documents of the registration as a whole, and the catalogue's state document with the time of the last run
// that changed a document (R-BAS-103: the documents carry no time; the state does). A run that changed nothing keeps the state.
func (r *run) writeGlobal(ctx context.Context, docs []rendered) error {
	statePath := iface.CataloguePath{Kind: iface.CatalogueState}.String()
	old, err := r.b.o.Store.UnitRows(ctx, r.reg.ID, "")
	if err != nil {
		return err
	}
	r.mu.Lock()
	changed := r.changed
	r.mu.Unlock()
	seen := map[string]bool{statePath: true}
	for _, d := range docs {
		seen[d.path] = true
		sum := sha256.Sum256(d.body)
		changed = changed || old[d.path] != hex.EncodeToString(sum[:])
	}
	for p, sha := range old {
		changed = changed || (sha != "" && !seen[p])
	}
	if !changed && old[statePath] != "" {
		_, err := r.write(ctx, "", docs, []byte{}, statePath)
		return err
	}
	sb, err := json.Marshal(map[string]any{"builtAt": r.b.o.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		return err
	}
	_, err = r.write(ctx, "", append(docs, rendered{path: statePath, body: sb}), []byte{})
	return err
}

// digest is the SHA-256 of the parts, each prefixed with its length.
func digest(parts ...string) []byte {
	h := sha256.New()
	for _, p := range parts {
		var n [8]byte
		binary.LittleEndian.PutUint64(n[:], uint64(len(p)))
		h.Write(n[:])
		h.Write([]byte(p))
	}
	return h.Sum(nil)
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
