package catalogue

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// The documentation of the catalogue across packages (R-BAS-125; formats/ansible.md R-ANS-058 at the Bastion): the entries (modules,
// plugins, roles) of the documentation tree of every public package's newest version, searched as the main installation searches
// them, and listed for the sitemap. The index is built with the snapshot from the trees the replica holds, and reads no format: a
// package whose newest version has a tree is in it.

// DocHit is an entry of a public package's documentation in its newest version, in the main installation's shape of a hit of
// GET /api/v1/search/documentation (DocumentationHit, R-ANS-058).
type DocHit struct {
	Format    string `json:"format"`
	PackageID string `json:"packageId"`
	// Package is the package's name (a collection's full name), Version its newest version, the one its page opens on (R-UI-218).
	Package string `json:"package"`
	Version string `json:"version"`
	// Project and Repository name the repository the tree was read from.
	Project          string          `json:"project"`
	Repository       string          `json:"repository"`
	Type             string          `json:"type"`
	Name             string          `json:"name"`
	ShortDescription string          `json:"shortDescription"`
	Deprecation      json.RawMessage `json:"deprecation,omitempty"`

	// words are the entry's search words (the tree's searchWords, R-BAS-124), lower is its name in lower case.
	words []string
	lower string
}

// docEntry is an entry of a parsed tree: what a hit takes of it.
type docEntry struct {
	typ, name, short string
	words            []string
}

// docTree is a parsed documentation tree: the repository it was read from and its entries.
type docTree struct {
	repository string
	entries    []docEntry
}

// docIndex is the snapshot's index of the documentation: one tree per public package that has one, by the package's catalogue ID.
type docIndex struct {
	// trees are the SHA-256 of the trees by package, so two indexes of the same trees are the same.
	trees map[string]string
	hits  []*DocHit
}

// docsOf builds the documentation index of a snapshot from the trees of its packages' newest versions, reading only the trees the
// replica has not parsed before, and reuses the index of cur when the trees are the same. A tree that cannot be read is left out and
// fetched again by the next round; it fails nothing else.
func (c *Catalogue) docsOf(ctx context.Context, next, cur *snapshot, idx *index) *docIndex {
	out := &docIndex{trees: map[string]string{}}
	if idx == nil {
		return out
	}
	refs := map[string]docRef{}
	for _, e := range idx.entries {
		if e.LatestVersion == "" {
			continue
		}
		p := iface.CataloguePath{Kind: iface.CatalogueDocs, Package: e.ID, Version: e.LatestVersion}.String()
		if r, ok := next.lookup(p); ok {
			refs[e.ID] = r
			out.trees[e.ID] = r.sha
		}
	}
	if cur != nil && cur.docIndex != nil && equalTrees(cur.docIndex.trees, out.trees) {
		return cur.docIndex
	}
	parsed := map[string]*docTree{}
	for _, e := range idx.entries {
		r, ok := refs[e.ID]
		if !ok {
			continue
		}
		t, ok := c.trees[r.sha]
		if !ok {
			gz, err := c.o.Store.GetCatalogueDocument(ctx, r.sha, maxDocGzip)
			var raw []byte
			if err == nil {
				raw, err = open(gz, r.sha, r.size)
			}
			if err == nil {
				t, err = parseTree(raw)
			}
			if err != nil {
				c.log.Warn("a documentation tree of the public catalogue cannot be read; it is fetched again", "package", e.ID, "error", err)
				c.lost(r.name)
				delete(out.trees, e.ID)
				continue
			}
		}
		parsed[r.sha] = t
		project, repository, _ := strings.Cut(t.repository, "/")
		var deprecation json.RawMessage
		var s struct {
			Deprecation json.RawMessage `json:"deprecation"`
		}
		if json.Unmarshal(e.Summary, &s) == nil && len(s.Deprecation) > 0 && string(s.Deprecation) != "null" {
			deprecation = s.Deprecation
		}
		for _, en := range t.entries {
			out.hits = append(out.hits, &DocHit{
				Format: e.Format, PackageID: e.ID, Package: e.Name, Version: e.LatestVersion, Project: project, Repository: repository,
				Type: en.typ, Name: en.name, ShortDescription: en.short, Deprecation: deprecation,
				words: en.words, lower: strings.ToLower(en.name),
			})
		}
	}
	c.trees = parsed
	return out
}

func equalTrees(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// parseTree reads a documentation tree in the main installation's shape (GET …/docs, R-ANS-036) with the catalogue's searchWords.
func parseTree(raw []byte) (*docTree, error) {
	var t struct {
		Repository string `json:"repository"`
		Groups     []struct {
			Types []struct {
				Entries []struct {
					Type             string `json:"type"`
					Name             string `json:"name"`
					ShortDescription string `json:"shortDescription"`
					SearchWords      string `json:"searchWords"`
				} `json:"entries"`
			} `json:"types"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("a documentation tree does not decode: %w", err)
	}
	out := &docTree{repository: t.Repository}
	for _, g := range t.Groups {
		for _, ty := range g.Types {
			for _, e := range ty.Entries {
				out.entries = append(out.entries, docEntry{typ: e.Type, name: e.Name, short: e.ShortDescription, words: strings.Fields(e.SearchWords)})
			}
		}
	}
	return out, nil
}

// DocQuery is a search of the documentation across packages (R-BAS-125, R-ANS-058): Q the text, of 2 to 100 characters after its
// control characters are taken out; Project a filter; After the Next of the page before; Limit 50 by default, at most 100.
type DocQuery struct {
	Q       string
	Project string
	After   string
	Limit   int
}

// DocPage is one page of hits: Next is the cursor of the following page, "" for the last.
type DocPage struct {
	Items []DocHit
	Next  string
}

// The bounds of the text of a search across packages: the main installation's (R-ANS-058).
const (
	docTextMin = 2
	docTextMax = 100
)

// docText is the text of a search as the main installation reads it: white space a space, control characters taken out, trimmed, in
// lower case; ok is false outside the bounds.
func docText(raw string) (string, bool) {
	text := strings.TrimSpace(strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, raw))
	if n := utf8.RuneCountInString(text); n < docTextMin || n > docTextMax {
		return "", false
	}
	return strings.Join(strings.Fields(strings.ToLower(text)), " "), true
}

// docWords are the words of a text the search matches (rows.ViewQuery's): the runs of a to z and 0 to 9, upper case read as lower.
func docWords(text string) []string {
	var out []string
	var w strings.Builder
	flush := func() {
		if w.Len() > 0 {
			out = append(out, w.String())
			w.Reset()
		}
	}
	for _, r := range text {
		switch {
		case r >= 'A' && r <= 'Z':
			w.WriteRune(r - 'A' + 'a')
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			w.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	return out
}

// hitMatches reports whether every word begins a word of the entry's search words (the vector's prefix match).
func (h *DocHit) hitMatches(words []string) bool {
	for _, w := range words {
		found := false
		for _, x := range h.words {
			if strings.HasPrefix(x, w) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// tier ranks the entry's name against the text: 0 equal, 1 starts with it, 2 holds it, 3 a match of the words elsewhere.
func (h *DocHit) tier(text string) int {
	switch {
	case h.lower == text:
		return 0
	case strings.HasPrefix(h.lower, text):
		return 1
	case strings.Contains(h.lower, text):
		return 2
	}
	return 3
}

// docKey is a hit's place in the order: the tier, the entry's name, the collection, the repository and the type.
type docKey struct {
	T int    `json:"t"`
	N string `json:"n"`
	P string `json:"p"`
	R string `json:"r"`
	Y string `json:"y"`
}

func (k docKey) less(o docKey) bool {
	if k.T != o.T {
		return k.T < o.T
	}
	for _, c := range [][2]string{{k.N, o.N}, {k.P, o.P}, {k.R, o.R}, {k.Y, o.Y}} {
		if c[0] != c[1] {
			return c[0] < c[1]
		}
	}
	return false
}

// visibleHit reports whether the snapshot shows the hit: docs is on and the repository its tree was read from is public with a
// public wiki (R-BAS-112, R-AUTHZ-034), and its package is public.
func (s *snapshot) visibleHit(h *DocHit) bool {
	if !s.docs {
		return false
	}
	r, ok := s.repos[h.Project+"/"+h.Repository]
	return ok && r.Wiki && s.entry(h.PackageID) != nil
}

// SearchDocs implements [Reader]: the entries of the newest version of every public package with documentation whose every word
// begins a word of the entry's search words, the best first (R-ANS-058's order), a page at a time. A text the search does not take
// is [ErrQuery]; a text without a letter or a digit finds nothing.
func (c *Catalogue) SearchDocs(q DocQuery) (DocPage, error) {
	s, err := c.served()
	if err != nil {
		return DocPage{}, err
	}
	limit, err := pageLimit(q.Limit)
	if err != nil {
		return DocPage{}, err
	}
	text, ok := docText(q.Q)
	if !ok {
		return DocPage{}, fmt.Errorf("%w: the text holds %d to %d characters", ErrQuery, docTextMin, docTextMax)
	}
	var from *docKey
	if q.After != "" {
		raw, err := base64.RawURLEncoding.DecodeString(q.After)
		var k docKey
		if err != nil || json.Unmarshal(raw, &k) != nil {
			return DocPage{}, fmt.Errorf("%w: the cursor", ErrQuery)
		}
		from = &k
	}
	words := docWords(text)
	page := DocPage{Items: []DocHit{}}
	if len(words) == 0 || s.docIndex == nil {
		return page, nil
	}
	type hit struct {
		h *DocHit
		k docKey
	}
	var hits []hit
	for _, h := range s.docIndex.hits {
		if (q.Project != "" && h.Project != q.Project) || !s.visibleHit(h) || !h.hitMatches(words) {
			continue
		}
		k := docKey{T: h.tier(text), N: h.Name, P: h.Package, R: h.Project + "/" + h.Repository, Y: h.Type}
		if from != nil && !from.less(k) {
			continue
		}
		hits = append(hits, hit{h, k})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].k.less(hits[j].k) })
	for i, h := range hits {
		if i == limit {
			b, _ := json.Marshal(hits[i-1].k)
			page.Next = base64.RawURLEncoding.EncodeToString(b)
			break
		}
		page.Items = append(page.Items, *h.h)
	}
	return page, nil
}

// DocEntries implements [Reader]: every entry the documentation search could find, in the order of their packages and their tree,
// for the sitemap (W-35's B.11: the newest version's entries while docs is on). Nothing while docs is off.
func (c *Catalogue) DocEntries() ([]DocHit, error) {
	s, err := c.served()
	if err != nil {
		return nil, err
	}
	out := []DocHit{}
	if s.docIndex == nil {
		return out, nil
	}
	for _, h := range s.docIndex.hits {
		if s.visibleHit(h) {
			out = append(out, *h)
		}
	}
	return out, nil
}

// Doc is the hit's page below its package's address: the value of the query parameter doc (R-ANS-049).
func (h DocHit) Doc() string { return h.Type + "/" + h.Name }
