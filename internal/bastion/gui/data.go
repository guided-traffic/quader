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
)

// projectsBody is the answer of /ui/data/v1/projects: the keys of the projects the catalogue shows, in their order, so that the
// start page leads a visitor into a project, whose list and pages carry it in their addresses as the main installation's do.
type projectsBody struct {
	Items []string `json:"items"`
}

func (h *Handler) projects(w http.ResponseWriter, r *http.Request) {
	pub, err := h.o.Catalogue.PublicSet()
	if err != nil {
		h.readerError(w, r, err)
		return
	}
	files, err := h.o.Catalogue.Files()
	if err != nil {
		h.readerError(w, r, err)
		return
	}
	withFiles := map[string]bool{}
	for _, name := range files {
		project, _, _ := strings.Cut(name, "/")
		withFiles[project] = true
	}
	// A project whose public repositories hold no package and no folder the catalogue shows (the project upstream with an empty
	// public remote) is not offered: its page would say only that nothing is published there (R-UI-229, Q-BAS-399, Q-BAS-400).
	items := []string{}
	for _, p := range pub.Projects {
		if withFiles[p] {
			items = append(items, p)
			continue
		}
		page, err := h.o.Catalogue.Search(catalogue.Query{Project: p, Limit: 1})
		if err != nil {
			h.readerError(w, r, err)
			return
		}
		if len(page.Items) > 0 {
			items = append(items, p)
		}
	}
	h.json(w, r, projectsBody{Items: items})
}

// files answers /ui/data/v1/files: the public repositories whose folders and files the catalogue holds, by their qualified names
// (Q-BAS-400), so that a project's page leads a visitor to a generic repository's files, which no search of packages finds.
func (h *Handler) files(w http.ResponseWriter, r *http.Request) {
	items, err := h.o.Catalogue.Files()
	if err != nil {
		h.readerError(w, r, err)
		return
	}
	h.json(w, r, projectsBody{Items: items})
}

// data answers an address below /ui/data/v1/ (R-BAS-118): the state, the legal pages, the projects, the repositories with files,
// the search, the lookup, a page of a package's versions, and every other document of the catalogue by its path of the grammar (R-BAS-103). Only the legal pages are
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
	case pathLookup:
		h.lookup(w, r)
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
// is old, whether it is, and the switch docs.
type stateBody struct {
	ConfirmedAt       time.Time `json:"confirmedAt"`
	StaleAfterSeconds int64     `json:"staleAfterSeconds"`
	Stale             bool      `json:"stale"`
	Docs              bool      `json:"docs"`
}

func (h *Handler) state(w http.ResponseWriter, r *http.Request) {
	st := h.o.Catalogue.State()
	h.json(w, r, stateBody{
		ConfirmedAt: st.ConfirmedAt.UTC(), StaleAfterSeconds: int64(st.StaleAfter / time.Second), Stale: st.Stale, Docs: st.Docs,
	})
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
