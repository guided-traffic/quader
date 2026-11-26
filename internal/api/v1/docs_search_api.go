package v1

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/ansible/rows"
	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/packages/view"
)

// The documentation searched across collections (formats/ansible.md R-ANS-058, Q-ANS-125 to Q-ANS-128): the modules, plugins and roles of
// the newest version of every Ansible Galaxy collection the caller may read, per repository, from the top bar's search and its results
// page. It starts at the search index: the versions with a matching entry (AnsibleDocsCandidates, through ansible_doc_entries_search),
// then the newest version of each of their collections in its repository (view.Service.NewestOf, the version a package's page opens on,
// R-UI-218), kept
// where it is a candidate and the caller may read its wiki (R-ANS-037), and only then the entries of those versions (AnsibleDocsSearch).
// So an entry the caller may not read is neither found nor counted, and a collection without a match costs nothing. A fixed number of
// statements whatever the number of entries: the scope's, the candidates, the withheld versions, the newest versions, the entries.

// The bounds of the text: those of the search field's suggestions (R-API-060) and the terms of the documentation search (R-ANS-036).
const (
	docSearchMinText = 2
	docSearchMaxText = 100
	// docSearchMaxVersions bounds the versions with a match one request decides (Q-ANS-127): a text that matches the documentation of
	// more collections is too broad.
	docSearchMaxVersions = 20_000
)

// errDocsTooBroad is a text that matches the documentation of more than docSearchMaxVersions versions; the list answers it as
// docsTooBroad, the suggestions as none with more.
var errDocsTooBroad = errors.New("v1: the documentation search text is too broad")

func docsTooBroad() error {
	return errs.E(errs.Invalid, errs.CodeInvalid,
		"The search text matches the documentation of more collections than one request decides; type more of it.")
}

// docSearchText reads the text: trimmed, control characters removed, 2 to 100 characters, lower-cased, at most 8 terms of at most 64
// characters each (R-ANS-036's rule for a term), joined by one space.
func docSearchText(raw string) (text string, ok bool) {
	text = strings.TrimSpace(strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, raw))
	if n := utf8.RuneCountInString(text); n < docSearchMinText || n > docSearchMaxText {
		return "", false
	}
	terms := strings.Fields(strings.ToLower(text))
	if len(terms) > maxQueryTerms {
		terms = terms[:maxQueryTerms]
	}
	for i, t := range terms {
		if r := []rune(t); len(r) > maxQueryTermRune {
			terms[i] = string(r[:maxQueryTermRune])
		}
	}
	return strings.Join(terms, " "), true
}

// docSearch is one page of hits for the caller: limit+1 rows when there are more.
func (s *Server) docSearch(r *http.Request, text, project string, after *rows.DocHit, limit int) ([]gen.DocumentationHit, []rows.DocHit, error) {
	ps, err := s.packageServices()
	if err != nil {
		return nil, nil, err
	}
	st := s.o.Ansible
	query := rows.ViewQuery(text)
	if st == nil || query == "" {
		return nil, nil, nil // no Ansible Galaxy data port, or a text without a letter or a digit: nothing to find
	}
	ctx, sc, err := s.packageScope(r, ps, view.ScopeRequest{Formats: []string{"ansible"}})
	if err != nil {
		return nil, nil, err
	}
	keys, err := readableDocs(ctx, ps.View, st, sc, project, query)
	if err != nil || len(keys) == 0 {
		return nil, nil, err
	}
	hits, err := st.SearchDocs(ctx, rows.DocSearch{Versions: keys, Text: text, Query: query, After: after, Limit: limit + 1})
	if err != nil {
		return nil, nil, err
	}
	out := make([]gen.DocumentationHit, 0, len(hits))
	topo := sc.Topology()
	for _, h := range hits {
		repo := topo.Name(h.RepoID)
		hit := gen.DocumentationHit{
			Format: "ansible", PackageId: view.ID("ansible", h.Name), Package: h.Name, Version: h.Version,
			Project: repo.Project, Repository: repo.Key, Type: h.Type, Name: h.Entry, ShortDescription: h.ShortDescription,
		}
		if h.Deprecated {
			// The collection's deprecation in the hit's repository (R-ANS-060): every version of it shares it.
			hit.Deprecation = &gen.PackageDeprecation{Scope: gen.PackageDeprecationScopePackage}
			if h.DeprecationMessage != "" {
				msg := h.DeprecationMessage
				hit.Deprecation.Message = &msg
			}
		}
		out = append(out, hit)
	}
	return out, hits, nil
}

// readableDocs are the versions whose entries the search reads: of the versions with a matching entry in the scope's repositories (in
// the project when one is given), those that are the newest version of their collection in their repository and whose wiki the caller
// may read (R-ANS-037).
func readableDocs(ctx context.Context, v *view.Service, st AnsibleStore, sc view.Scope, project, query string) ([]rows.DocVersionKey, error) {
	topo := sc.Topology()
	var repos []int64
	for _, id := range sc.Repos() {
		if project == "" || topo.Name(id).Project == project {
			repos = append(repos, id)
		}
	}
	cands, err := st.DocCandidates(ctx, repos, query, docSearchMaxVersions+1)
	if err != nil || len(cands) == 0 {
		return nil, err
	}
	if len(cands) > docSearchMaxVersions {
		return nil, errDocsTooBroad
	}
	matched := make(map[rows.DocVersionKey]bool, len(cands))
	var pkgs []view.PackageKey
	seen := map[view.PackageKey]bool{}
	for _, c := range cands {
		matched[c] = true
		if k := (view.PackageKey{RepoID: c.RepoID, Name: c.Name}); !seen[k] {
			seen[k] = true
			pkgs = append(pkgs, k)
		}
	}
	newest, err := v.NewestOf(ctx, sc, "ansible", pkgs)
	if err != nil {
		return nil, err
	}
	keys := make([]rows.DocVersionKey, 0, len(newest))
	for _, n := range newest {
		k := rows.DocVersionKey{RepoID: n.RepoID, Name: n.Name, Version: n.Version}
		if matched[k] && sc.MayReadWiki(n.Repo, n.Name, n.Version) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// docHitKey is the cursor of a hit: its tier, its entry, its collection, its repository and its type.
func docHitKey(h rows.DocHit) []string {
	return []string{strconv.Itoa(h.Tier), h.Entry, h.Name, strconv.FormatInt(h.RepoID, 10), h.Type}
}

func docHitAfter(key []string) (*rows.DocHit, bool) {
	if key == nil {
		return nil, true
	}
	if len(key) != 5 {
		return nil, false
	}
	tier, err1 := strconv.Atoi(key[0])
	repo, err2 := strconv.ParseInt(key[3], 10, 64)
	if err1 != nil || err2 != nil || tier < 0 || tier > 3 {
		return nil, false
	}
	return &rows.DocHit{DocVersionKey: rows.DocVersionKey{RepoID: repo, Name: key[2]}, Type: key[4], Entry: key[1], Tier: tier}, true
}

// SearchDocumentation implements GET /api/v1/search/documentation (R-ANS-058).
func (s *Server) SearchDocumentation(w http.ResponseWriter, r *http.Request, _ gen.SearchDocumentationParams) {
	if err := s.anonymousGate(r.Context()); err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	lq, err := ParseList(r, ListSpec{Filters: []string{"q", "project"}}, s.o.API.Config)
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	text, ok := docSearchText(lq.First("q"))
	if !ok {
		httpserver.Problem(w, r, badParam("q", "must hold 2 to 100 characters after trimming"))
		return
	}
	after, ok := docHitAfter(lq.After)
	if !ok {
		httpserver.Problem(w, r, invalidCursor())
		return
	}
	items, hits, err := s.docSearch(r, text, lq.First("project"), after, lq.Limit)
	if errors.Is(err, errDocsTooBroad) {
		err = docsTooBroad()
	}
	if err != nil {
		httpserver.Problem(w, r, err)
		return
	}
	page := Page[gen.DocumentationHit]{Items: items}
	if len(items) > lq.Limit {
		page.Items = items[:lq.Limit]
		c := lq.EncodeCursor(docHitKey(hits[lq.Limit-1]))
		page.NextCursor = &c
	}
	if page.Items == nil {
		page.Items = []gen.DocumentationHit{}
	}
	WritePage(w, r, page)
}

// suggestDocumentation is the kind documentation of the suggestions (R-ANS-058): the first limit hits of the search across collections,
// more when one more was found.
func (s *Server) suggestDocumentation(r *http.Request, sr suggestRequest) (*gen.DocumentationSuggestions, error) {
	out := &gen.DocumentationSuggestions{Items: []gen.DocumentationHit{}}
	text, ok := docSearchText(sr.text)
	if !ok {
		return out, nil
	}
	items, _, err := s.docSearch(r, text, "", nil, sr.limit)
	if errors.Is(err, errDocsTooBroad) {
		// A text too short to decide (a prefix of two letters on a mirror of every collection) suggests none and says that more exist.
		out.More = true
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if len(items) > sr.limit {
		items, out.More = items[:sr.limit], true
	}
	out.Items = append(out.Items, items...)
	return out, nil
}
