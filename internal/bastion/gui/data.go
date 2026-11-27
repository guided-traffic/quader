package gui

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/catalogue"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/errs"
)

// dataRoot is the address the application reads its data from (R-BAS-118, W-35's B.13): the main installation's shapes below a
// path of the Bastion's own. The catalogue's builder rewrote the main installation's /api/v1/ links in the documents to it.
const dataRoot = "/ui/data/v1/"

// The paths of the data that are no document of the catalogue.
const (
	pathState    = "state"
	pathLegal    = "legal"
	pathPackages = "packages"
	pathLookup   = "packages/lookup"
	pathProjects = "projects"
	pathFiles    = "files"
	// pathRepositories is the public repositories with their format and class (R-BAS-129).
	pathRepositories = "repositories"
	// pathDocs is the documentation searched across packages (R-BAS-125): the main installation's GET /api/v1/search/documentation.
	pathDocs = "search/documentation"
	// pathMalwareSignatures is today's signatures of the malware scan (malware-scanning.md R-MAL-042): the main installation's GET
	// /api/v1/malware/signatures as the catalogue's state carries it.
	pathMalwareSignatures = "malware/signatures"
	// pathVulnerabilityDatabase is today's vulnerability database of the image scanning (vulnerability-scanning.md R-VULN-052): the
	// main installation's GET /api/v1/vulnerabilities/database as the catalogue's state carries it.
	pathVulnerabilityDatabase = "vulnerabilities/database"
)

// projectsBody is the answer of /ui/data/v1/projects: the projects the catalogue shows, in the order of their keys, each with its key,
// display name and description (R-BAS-128), so that the start page names a project as the main installation does and leads a visitor
// into it, whose list and pages carry its key in their addresses as the main installation's do.
type projectsBody struct {
	Items []iface.CatalogueProject `json:"items"`
}

// namesBody is a list of qualified names: the answer of /ui/data/v1/files.
type namesBody struct {
	Items []string `json:"items"`
}

// repositoriesBody is the answer of /ui/data/v1/repositories: the public repositories of the catalogue with their format and class
// (R-BAS-129), what the page Set up client of a repository reads to know its clients.
type repositoriesBody struct {
	Items []repositoryItem `json:"items"`
}

type repositoryItem struct {
	Name   string `json:"name"`
	Format string `json:"format"`
	Class  string `json:"class"`
}

func (h *Handler) projects(w http.ResponseWriter, r *http.Request) {
	items, err := h.offeredProjects()
	if err != nil {
		h.readerError(w, r, err)
		return
	}
	h.json(w, r, projectsBody{Items: items})
}

// offeredProjects are the projects the start page offers. A project whose public repositories hold no package and no folder the
// catalogue shows (the project upstream with an empty public remote) is not offered: its page would say only that nothing is
// published there (R-UI-229, Q-BAS-399, Q-BAS-400).
func (h *Handler) offeredProjects() ([]iface.CatalogueProject, error) {
	pub, err := h.o.Catalogue.PublicSet()
	if err != nil {
		return nil, err
	}
	files, err := h.o.Catalogue.Files()
	if err != nil {
		return nil, err
	}
	withFiles := map[string]bool{}
	for _, name := range files {
		project, _, _ := strings.Cut(name, "/")
		withFiles[project] = true
	}
	items := []iface.CatalogueProject{}
	for _, p := range pub.Projects {
		if withFiles[p.Key] {
			items = append(items, p)
			continue
		}
		page, err := h.o.Catalogue.Search(catalogue.Query{Project: p.Key, Limit: 1})
		if err != nil {
			return nil, err
		}
		if len(page.Items) > 0 {
			items = append(items, p)
		}
	}
	return items, nil
}

// files answers /ui/data/v1/files: the public repositories whose folders and files the catalogue holds, by their qualified names
// (Q-BAS-400), so that a project's page leads a visitor to a generic repository's files, which no search of packages finds.
func (h *Handler) files(w http.ResponseWriter, r *http.Request) {
	items, err := h.o.Catalogue.Files()
	if err != nil {
		h.readerError(w, r, err)
		return
	}
	h.json(w, r, namesBody{Items: items})
}

// repositories answers /ui/data/v1/repositories: the public set's repositories with their format and class (R-BAS-129), in its
// order. Whether a repository's wiki is public is the catalogue's own fact and stays out.
func (h *Handler) repositories(w http.ResponseWriter, r *http.Request) {
	pub, err := h.o.Catalogue.PublicSet()
	if err != nil {
		h.readerError(w, r, err)
		return
	}
	items := make([]repositoryItem, 0, len(pub.Repositories))
	for _, x := range pub.Repositories {
		items = append(items, repositoryItem{Name: x.Name, Format: x.Format, Class: x.Class})
	}
	h.json(w, r, repositoriesBody{Items: items})
}

// data answers an address below /ui/data/v1/ (R-BAS-118): the state, the legal pages, the projects, the repositories with files,
// the search, the lookup, today's malware signatures (R-MAL-042) and vulnerability database (R-VULN-052), a page of a package's versions, and every other document of the catalogue by its path of the grammar (R-BAS-103). Only the legal pages are
// served while the replica is not ready.
func (h *Handler) data(w http.ResponseWriter, r *http.Request, ready bool) {
	rest := strings.TrimPrefix(r.URL.EscapedPath(), dataRoot)
	if rest == pathLegal {
		h.legal(w, r)
		return
	}
	if !ready {
		h.problem(w, r, errNotReady())
		return
	}
	switch rest {
	case pathState:
		h.state(w, r)
		return
	case pathPackages:
		h.search(w, r)
		return
	case pathProjects:
		h.projects(w, r)
		return
	case pathFiles:
		h.files(w, r)
		return
	case pathRepositories:
		h.repositories(w, r)
		return
	case pathLookup:
		h.lookup(w, r)
		return
	case pathDocs:
		h.searchDocs(w, r)
		return
	case pathMalwareSignatures:
		h.malwareSignatures(w, r)
		return
	case pathVulnerabilityDatabase:
		h.vulnerabilityDatabase(w, r)
		return
	}
	path, ok := canonical(rest)
	if !ok {
		h.problem(w, r, errNotFound())
		return
	}
	p, err := iface.ParseCataloguePath(path)
	if err != nil {
		h.problem(w, r, errNotFound())
		return
	}
	switch p.Kind {
	case iface.CatalogueIndex, iface.CatalogueState:
		// The index is the reader's own, the catalogue's times are the state's (R-BAS-112).
		h.problem(w, r, errNotFound())
	case iface.CatalogueVersions:
		h.versions(w, r, p.Package)
	default:
		h.document(w, r, path, p.Kind)
	}
}

// canonical writes an escaped path in the one spelling of the grammar (R-BAS-103): each segment decoded, then every byte outside
// the unreserved characters as %XX with upper-case hex. A browser escapes less than the grammar does (encodeURIComponent keeps
// `!'()*`); a decoded "/" inside a segment stays encoded and the grammar refuses it.
func canonical(escaped string) (string, bool) {
	if escaped == "" || len(escaped) > iface.MaxCataloguePathBytes {
		return "", false
	}
	segs := strings.Split(escaped, "/")
	var b strings.Builder
	for i, s := range segs {
		v, err := url.PathUnescape(s)
		if err != nil {
			return "", false
		}
		if i > 0 {
			b.WriteByte('/')
		}
		for j := range len(v) {
			c := v[j]
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~' {
				b.WriteByte(c)
				continue
			}
			b.WriteString("%" + strings.ToUpper(hex.EncodeToString([]byte{c})))
		}
	}
	return b.String(), true
}

// readerError answers an error of the catalogue's reader: off is 404, not ready 503, a query the reader refuses 400.
func (h *Handler) readerError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, catalogue.ErrOff):
		h.problem(w, r, errNotFound())
	case errors.Is(err, catalogue.ErrNotReady):
		h.problem(w, r, errNotReady())
	case errors.Is(err, catalogue.ErrQuery):
		h.problem(w, r, errs.E(errs.Invalid, errs.CodeInvalid, "The query is not one this address answers: "+
			strings.TrimPrefix(err.Error(), catalogue.ErrQuery.Error()+": ")+"."))
	default:
		h.log.Error("a read of the public catalogue failed", "path", r.URL.Path, "error", err)
		h.problem(w, r, errs.E(errs.Internal, errs.CodeInternal, textUnhandled))
	}
}

// stateBody is the answer of /ui/data/v1/state (W-35's B.2b): the time of the state served, the age after which the pages say it
// is old, whether it is, the switch docs, and the installation's default form of a time the application writes every time in
// (R-BAS-128).
type stateBody struct {
	ConfirmedAt       time.Time `json:"confirmedAt"`
	StaleAfterSeconds int64     `json:"staleAfterSeconds"`
	Stale             bool      `json:"stale"`
	Docs              bool      `json:"docs"`
	TimeFormat        string    `json:"timeFormat"`
}

func (h *Handler) state(w http.ResponseWriter, r *http.Request) {
	st := h.o.Catalogue.State()
	format := iface.FactoryTimeFormat
	if h.o.TimeFormat != nil {
		if f := h.o.TimeFormat(); f != "" {
			format = f
		}
	}
	h.json(w, r, stateBody{
		ConfirmedAt: st.ConfirmedAt.UTC(), StaleAfterSeconds: int64(st.StaleAfter / time.Second), Stale: st.Stale, Docs: st.Docs,
		TimeFormat: format,
	})
}

// signaturesBody is the main installation's shape of GET /api/v1/malware/signatures (R-MAL-040) as a Bastion answers it (R-MAL-042,
// Q-BAS-492): the set, its daily, its build time, the unit and the bounds from the catalogue's state; the time of the import and the
// engine are the main installation's and null here, and no set is called stale, since a Bastion knows no stale_after (W-36's B.15).
type signaturesBody struct {
	Version          *string `json:"version"`
	Daily            *int64  `json:"daily"`
	BuiltAt          *string `json:"builtAt"`
	ImportedAt       *string `json:"importedAt"`
	Stale            bool    `json:"stale"`
	Scanner          string  `json:"scanner"`
	Engine           *string `json:"engine"`
	CycleDays        float64 `json:"cycleDays"`
	OverdueAfterDays float64 `json:"overdueAfterDays"`
}

// malwareSignatures answers today's signatures from the catalogue's state document (R-MAL-042): 404 while the catalogue holds none
// (the main installation's scanning is off), as the main installation's 503 tells its own pages that it is off.
func (h *Handler) malwareSignatures(w http.ResponseWriter, r *http.Request) {
	st, ok := h.catalogueState(w, r)
	if !ok {
		return
	}
	if st.Malware == nil {
		h.problem(w, r, errNotFound())
		return
	}
	m := st.Malware
	h.json(w, r, signaturesBody{
		Version: m.Version, Daily: m.Daily, BuiltAt: m.BuiltAt, Scanner: m.Scanner, CycleDays: m.CycleDays, OverdueAfterDays: m.OverdueAfterDays,
	})
}

// databaseBody is the main installation's shape of GET /api/v1/vulnerabilities/database (R-VULN-052) as a Bastion answers it
// (Q-BAS-492): the unit, the digest and the build time from the catalogue's state; when the main installation last confirmed it is its
// own and null here, and no database is called stale.
type databaseBody struct {
	Scanner  string  `json:"scanner"`
	Digest   *string `json:"digest"`
	BuiltAt  *string `json:"builtAt"`
	SyncedAt *string `json:"syncedAt"`
	Stale    bool    `json:"stale"`
}

// vulnerabilityDatabase answers today's vulnerability database from the catalogue's state document: 404 while it holds none (the
// main installation's image scanning is off).
func (h *Handler) vulnerabilityDatabase(w http.ResponseWriter, r *http.Request) {
	st, ok := h.catalogueState(w, r)
	if !ok {
		return
	}
	if st.Vulnerabilities == nil {
		h.problem(w, r, errNotFound())
		return
	}
	v := st.Vulnerabilities
	h.json(w, r, databaseBody{Scanner: v.Scanner, Digest: v.Digest, BuiltAt: v.BuiltAt})
}

// catalogueState reads the catalogue's state document for an address that takes no parameter; false when it answered already (a
// parameter, no state, a failure of the reader).
func (h *Handler) catalogueState(w http.ResponseWriter, r *http.Request) (iface.CatalogueStateDocument, bool) {
	var st iface.CatalogueStateDocument
	if len(r.URL.Query()) != 0 {
		h.problem(w, r, errs.E(errs.Invalid, errs.CodeInvalid, "This address takes no parameter."))
		return st, false
	}
	d, ok, err := h.o.Catalogue.Document(r.Context(), iface.CataloguePath{Kind: iface.CatalogueState}.String())
	if err != nil {
		h.readerError(w, r, err)
		return st, false
	}
	if !ok || json.Unmarshal(d.Raw, &st) != nil {
		h.problem(w, r, errNotFound())
		return st, false
	}
	return st, true
}

// legalPage and legalBody are the main installation's shape of GET /api/v1/legal (R-UI-225).
type legalPage struct {
	HTML string `json:"html"`
	Lang string `json:"lang"`
}

type legalBody struct {
	Imprint *legalPage `json:"imprint"`
	Privacy *legalPage `json:"privacy"`
}

// legal answers the Bastion's own imprint and privacy notice (R-BAS-100) as the locale shows them.
func (h *Handler) legal(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	locale := q.Get("locale")
	if (locale != "en" && locale != "de") || len(q) != 1 {
		h.problem(w, r, errs.E(errs.Invalid, errs.CodeInvalid, "The locale is en or de."))
		return
	}
	set := h.o.Legal.Pages(locale)
	var body legalBody
	if p := set.Imprint; p != nil {
		body.Imprint = &legalPage{HTML: p.HTML, Lang: p.Lang}
	}
	if p := set.Privacy; p != nil {
		body.Privacy = &legalPage{HTML: p.HTML, Lang: p.Lang}
	}
	h.json(w, r, body)
}

// page is the main installation's envelope of a list (R-API-010).
type page struct {
	Items      []json.RawMessage `json:"items"`
	NextCursor *string           `json:"nextCursor"`
}

func next(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// searchParams are the parameters of the search (W-35's B.8); include is accepted and has no effect, since the index's summaries
// carry the counts of open findings already.
func searchParams() map[string]bool {
	return map[string]bool{
		"q": true, "format": true, "project": true, "repository": true, "deprecated": true, "sort": true, "cursor": true, "limit": true,
		"include": true,
	}
}

// search lists the public packages (W-35's B.8) in the main installation's shape of GET /api/v1/packages.
func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	for k := range qs {
		if !searchParams()[k] {
			h.problem(w, r, errs.E(errs.Invalid, errs.CodeInvalid, "The parameter "+strconv.Quote(k)+" is not one this address answers."))
			return
		}
	}
	limit, ok := limitOf(qs.Get("limit"))
	if !ok {
		h.problem(w, r, errs.E(errs.Invalid, errs.CodeInvalid, "The limit is a number from 1 to 100."))
		return
	}
	q := catalogue.Query{
		Q: qs.Get("q"), Formats: qs["format"], Project: qs.Get("project"), Repositories: qs["repository"],
		Deprecated: qs.Get("deprecated") == "true", Sort: qs.Get("sort"), After: qs.Get("cursor"), Limit: limit,
	}
	res, err := h.o.Catalogue.Search(q)
	if err != nil {
		h.readerError(w, r, err)
		return
	}
	out := page{Items: make([]json.RawMessage, 0, len(res.Items)), NextCursor: next(res.Next)}
	for _, e := range res.Items {
		out.Items = append(out.Items, e.Summary)
	}
	h.json(w, r, out)
}

func limitOf(s string) (int, bool) {
	if s == "" {
		return 0, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > catalogue.MaxLimit {
		return 0, false
	}
	return n, true
}

// searchDocs lists the entries of the documentation of the public packages' newest versions (R-BAS-125) in the main installation's
// shape of GET /api/v1/search/documentation: a list of R-API-010 by q, project, cursor and limit. While docs is off it finds
// nothing, as a reader who may read no wiki finds nothing there.
func (h *Handler) searchDocs(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	for k := range qs {
		if k != "q" && k != "project" && k != "cursor" && k != "limit" {
			h.problem(w, r, errs.E(errs.Invalid, errs.CodeInvalid, "The parameter "+strconv.Quote(k)+" is not one this address answers."))
			return
		}
	}
	limit, ok := limitOf(qs.Get("limit"))
	if !ok {
		h.problem(w, r, errs.E(errs.Invalid, errs.CodeInvalid, "The limit is a number from 1 to 100."))
		return
	}
	res, err := h.o.Catalogue.SearchDocs(catalogue.DocQuery{Q: qs.Get("q"), Project: qs.Get("project"), After: qs.Get("cursor"), Limit: limit})
	if err != nil {
		h.readerError(w, r, err)
		return
	}
	type docsPage struct {
		Items      []catalogue.DocHit `json:"items"`
		NextCursor *string            `json:"nextCursor"`
	}
	h.json(w, r, docsPage{Items: res.Items, NextCursor: next(res.Next)})
}

// lookup finds a public package by its format and its name or key (R-API-029's lookup).
func (h *Handler) lookup(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	format, name := qs.Get("format"), qs.Get("name")
	if format == "" || name == "" {
		h.problem(w, r, errs.E(errs.Invalid, errs.CodeInvalid, "The lookup takes a format and a name."))
		return
	}
	e, ok, err := h.o.Catalogue.Lookup(format, name)
	switch {
	case err != nil:
		h.readerError(w, r, err)
	case !ok:
		h.problem(w, r, errNotFound())
	default:
		h.raw(w, r, e.Summary)
	}
}

// versions answers a page of a public package's versions, in the main installation's order (R-BAS-113). The application sorts,
// searches and groups them in the browser, as the main installation's view does; only cursor and limit are read.
func (h *Handler) versions(w http.ResponseWriter, r *http.Request, id string) {
	qs := r.URL.Query()
	limit, ok := limitOf(qs.Get("limit"))
	if !ok {
		h.problem(w, r, errs.E(errs.Invalid, errs.CodeInvalid, "The limit is a number from 1 to 100."))
		return
	}
	if _, ok, err := h.o.Catalogue.Package(id); err != nil || !ok {
		if err != nil {
			h.readerError(w, r, err)
			return
		}
		h.problem(w, r, errNotFound())
		return
	}
	res, err := h.o.Catalogue.Versions(r.Context(), id, qs.Get("cursor"), limit)
	if err != nil {
		h.readerError(w, r, err)
		return
	}
	items := res.Items
	if items == nil {
		items = []json.RawMessage{}
	}
	h.json(w, r, page{Items: items, NextCursor: next(res.Next)})
}

// document answers a document of the catalogue (R-BAS-113): its bytes as the builder stored them, compressed for a client that
// takes gzip, with the SHA-256 of its raw bytes as its ETag.
func (h *Handler) document(w http.ResponseWriter, r *http.Request, path string, kind iface.CatalogueKind) {
	d, ok, err := h.o.Catalogue.Document(r.Context(), path)
	switch {
	case err != nil:
		h.readerError(w, r, err)
		return
	case !ok:
		h.problem(w, r, errNotFound())
		return
	}
	if kind == iface.CatalogueContents && !h.o.Catalogue.State().Docs {
		// A Debian package's changelog and copyright are documentation (R-BAS-126): with docs off they leave the contents at once,
		// before the builder's next run takes them out of the document.
		if body, ok := withoutDocumentation(d.Raw); ok {
			h.raw(w, r, body)
			return
		}
	}
	hd := w.Header()
	hd.Set("Content-Type", contentType(kind))
	hd.Set("Cache-Control", "no-cache")
	hd.Add("Vary", "Accept-Encoding")
	switch kind {
	case iface.CatalogueSignatureObject, iface.CatalogueChain, iface.CatalogueTrustedRoot:
		// What a visitor saves to verify offline (W-35's B.9).
		hd.Set("Content-Disposition", "attachment")
	}
	body, tag := d.Raw, `"`+d.SHA256+`"`
	if acceptsGzip(r) && len(d.Gzip) > 0 {
		body, tag = d.Gzip, `"`+d.SHA256+`-gzip"`
		hd.Set("Content-Encoding", "gzip")
	}
	h.write(w, r, tag, body)
}

// documentationMembers are the members of a Debian package's contents that are its documentation (R-BAS-124): the changelog and the
// copyright with their marks.
func documentationMembers() []string {
	return []string{"changelog", "changelogFile", "changelogTruncated", "copyright", "copyrightTruncated"}
}

// withoutDocumentation is a contents document without its documentation's members; ok is false when it holds none (or is no object),
// and the stored document is answered as it is.
func withoutDocumentation(raw []byte) ([]byte, bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil, false
	}
	found := false
	for _, k := range documentationMembers() {
		if _, ok := m[k]; ok {
			delete(m, k)
			found = true
		}
	}
	if !found {
		return nil, false
	}
	b, err := json.Marshal(m)
	return b, err == nil
}

// contentType is the media type of a document by its kind: the main installation's JSON answers, the signing material as it is.
func contentType(kind iface.CatalogueKind) string {
	switch kind {
	case iface.CatalogueSignatureObject:
		return "application/octet-stream"
	case iface.CatalogueChain:
		return "application/pem-certificate-chain"
	}
	return "application/json"
}

// acceptsGzip reports whether the request takes a gzip body: the coding listed without q=0.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(coding), "gzip") {
			continue
		}
		q := strings.ReplaceAll(strings.TrimSpace(params), " ", "")
		return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
	}
	return false
}

// json writes a computed answer with the SHA-256 of its bytes as its ETag.
func (h *Handler) json(w http.ResponseWriter, r *http.Request, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		h.readerError(w, r, err)
		return
	}
	h.raw(w, r, body)
}

// raw writes JSON bytes as they are, with the SHA-256 of them as their ETag.
func (h *Handler) raw(w http.ResponseWriter, r *http.Request, body []byte) {
	sum := sha256.Sum256(body)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	h.write(w, r, `"`+hex.EncodeToString(sum[:])+`"`, body)
}

// write answers a body with its ETag, or 304 when the request names the tag (RFC 9110 If-None-Match, weak comparison).
func (h *Handler) write(w http.ResponseWriter, r *http.Request, tag string, body []byte) {
	hd := w.Header()
	hd.Set("ETag", tag)
	if matches(r.Header.Get("If-None-Match"), tag) {
		hd.Del("Content-Encoding")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	hd.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = bytes.NewReader(body).WriteTo(w)
	}
}

// matches reports whether an If-None-Match header names tag or is "*".
func matches(header, tag string) bool {
	if header == "" {
		return false
	}
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimPrefix(strings.TrimSpace(t), "W/")
		if t == "*" || t == tag {
			return true
		}
	}
	return false
}
