package catalogue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/guided-traffic/quader/internal/bastion/iface"
)

// Entry is a package of the catalogue's index (R-BAS-113): the members of the main installation's package summary (R-API-029)
// that are the same for every format, which the list, the search and the sitemap read, and the summary itself as the main
// installation answered it, stripped (R-BAS-106), for an answer in the main installation's shape.
type Entry struct {
	ID            string
	Format        string
	Name          string
	Key           string
	Description   string
	LatestVersion string
	Keywords      []string
	// Repositories are the qualified names "<project>/<repo>" of the repositories that hold a public version.
	Repositories []string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Deprecated   bool
	Summary      json.RawMessage
}

// index is the catalogue's index of packages, built from its documents index/<n>.
type index struct {
	// pages are the documents of the pages in their order; two indexes of the same pages are the same.
	pages   []string
	entries []*Entry
	byID    map[string]*Entry
	// byName and byKey find the packages of a format by their name and their key ("<format>\x00<name>"); a name a format holds
	// in two projects is two entries.
	byName map[string][]*Entry
	byKey  map[string][]*Entry
}

// scopedIndexes are the indexes of the project view (R-BAS-130): a project's by its key and a repository's by its qualified name, each
// built from its own pages (projects/<project>/index/<n>, repositories/<project>/<repo>/index/<n>), whose counts and latest version
// are the scope's own. A repository's index is held while its pages are, also while the repositories are hidden; the reader then
// neither shows nor searches it (R-BAS-131), and the builder's next run deletes its pages.
type scopedIndexes struct {
	projects     map[string]*index
	repositories map[string]*index
}

// indexesOf builds the indexes of a snapshot from its pages: the catalogue's own (index/<n>) and the index of every project and
// repository of its public set. A page the replica parsed before, for this or another index, is not read again (two scopes of the
// same packages share their page and its entries), and an index whose pages are cur's is cur's.
func (c *Catalogue) indexesOf(ctx context.Context, next, cur *snapshot) error {
	parsed := map[string][]*Entry{}
	var curIdx *index
	var curScoped scopedIndexes
	if cur != nil {
		curIdx, curScoped = cur.index, cur.scoped
	}
	idx, err := c.indexOf(ctx, next, curIdx, parsed, func(n int) iface.CataloguePath {
		return iface.CataloguePath{Kind: iface.CatalogueIndex, Index: n}
	})
	if err != nil {
		return err
	}
	scoped := scopedIndexes{projects: map[string]*index{}, repositories: map[string]*index{}}
	for _, p := range next.public.Projects {
		key := p.Key
		i, err := c.indexOf(ctx, next, curScoped.projects[key], parsed, func(n int) iface.CataloguePath {
			return iface.CataloguePath{Kind: iface.CatalogueProjectIndex, Project: key, Index: n}
		})
		if err != nil {
			return err
		}
		if i != nil {
			scoped.projects[key] = i
		}
	}
	for _, r := range next.public.Repositories {
		project, key, ok := strings.Cut(r.Name, "/")
		if !ok {
			continue
		}
		i, err := c.indexOf(ctx, next, curScoped.repositories[r.Name], parsed, func(n int) iface.CataloguePath {
			return iface.CataloguePath{Kind: iface.CatalogueRepositoryIndex, Project: project, Repository: key, Index: n}
		})
		if err != nil {
			return err
		}
		if i != nil {
			scoped.repositories[r.Name] = i
		}
	}
	c.pages = parsed
	next.index, next.scoped = idx, scoped
	return nil
}

// indexOf builds one index of a snapshot from the pages pathOf names, from 0 to the first the snapshot does not hold, reading only
// the pages neither the replica nor this build has parsed before (parsed collects them), and reuses cur when its pages are the same.
// A scope without a page 0 has no index (nil); the catalogue's own index is empty then.
func (c *Catalogue) indexOf(
	ctx context.Context, next *snapshot, cur *index, parsed map[string][]*Entry, pathOf func(int) iface.CataloguePath,
) (*index, error) {
	var pages []docRef
	for n := 0; ; n++ {
		r, ok := next.lookup(pathOf(n).String())
		if !ok {
			break
		}
		pages = append(pages, r)
	}
	if len(pages) == 0 && pathOf(0).Kind != iface.CatalogueIndex {
		return nil, nil
	}
	shas := make([]string, len(pages))
	for i, p := range pages {
		shas[i] = p.sha
	}
	if cur != nil && slices.Equal(cur.pages, shas) {
		// Its pages stay parsed for the next build.
		for _, sha := range shas {
			if e, ok := c.pages[sha]; ok {
				parsed[sha] = e
			}
		}
		return cur, nil
	}
	idx := &index{pages: shas, byID: map[string]*Entry{}, byName: map[string][]*Entry{}, byKey: map[string][]*Entry{}}
	for _, p := range pages {
		entries, ok := parsed[p.sha]
		if !ok {
			entries, ok = c.pages[p.sha]
		}
		if !ok {
			var err error
			if entries, err = c.readPage(ctx, p); err != nil {
				return nil, err
			}
		}
		parsed[p.sha] = entries
		for _, e := range entries {
			if _, dup := idx.byID[e.ID]; !dup {
				idx.byID[e.ID] = e
				idx.entries = append(idx.entries, e)
				idx.byName[e.Format+"\x00"+e.Name] = append(idx.byName[e.Format+"\x00"+e.Name], e)
				if e.Key != "" && e.Key != e.Name {
					idx.byKey[e.Format+"\x00"+e.Key] = append(idx.byKey[e.Format+"\x00"+e.Key], e)
				}
			}
		}
	}
	sort.Slice(idx.entries, func(i, j int) bool { return byName(idx.entries[i], idx.entries[j]) })
	return idx, nil
}

// readPage reads and parses one page of an index from the store, verified.
func (c *Catalogue) readPage(ctx context.Context, p docRef) ([]*Entry, error) {
	gz, err := c.o.Store.GetCatalogueDocument(ctx, p.sha, maxDocGzip)
	if err != nil {
		c.lost(p.name)
		return nil, fmt.Errorf("a page of the index: %w", err)
	}
	raw, err := open(gz, p.sha, p.size)
	if err != nil {
		c.lost(p.name)
		return nil, fmt.Errorf("a page of the index: %w", err)
	}
	return parsePage(raw)
}

func byName(a, b *Entry) bool {
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.ID < b.ID
}

// parsePage reads a page of the index: {"items": [summary, …]}.
func parsePage(raw []byte) ([]*Entry, error) {
	var p struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("a page of the index does not decode: %w", err)
	}
	out := make([]*Entry, 0, len(p.Items))
	for _, item := range p.Items {
		var s struct {
			ID            string          `json:"id"`
			Format        string          `json:"format"`
			Name          string          `json:"name"`
			Key           string          `json:"key"`
			Description   *string         `json:"description"`
			LatestVersion string          `json:"latestVersion"`
			Keywords      []string        `json:"keywords"`
			Repositories  []string        `json:"repositories"`
			CreatedAt     time.Time       `json:"createdAt"`
			UpdatedAt     time.Time       `json:"updatedAt"`
			Deprecation   json.RawMessage `json:"deprecation"`
		}
		if json.Unmarshal(item, &s) != nil || s.ID == "" {
			continue
		}
		e := &Entry{
			ID: s.ID, Format: s.Format, Name: s.Name, Key: s.Key, LatestVersion: s.LatestVersion, Keywords: s.Keywords,
			Repositories: s.Repositories, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, Summary: bytes.Clone(item),
			Deprecated: len(s.Deprecation) > 0 && string(s.Deprecation) != "null",
		}
		if s.Description != nil {
			e.Description = *s.Description
		}
		out = append(out, e)
	}
	return out, nil
}

// visible reports whether a package of the index is public in the snapshot: one of its repositories is in the public set
// (R-BAS-112).
func (s *snapshot) visible(e *Entry) bool {
	for _, r := range e.Repositories {
		if _, ok := s.repos[r]; ok {
			return true
		}
	}
	return false
}

// wiki reports whether the documentation of a package is public in the snapshot: docs is on and one of its public repositories
// has a public wiki (R-BAS-102, R-AUTHZ-034).
func (s *snapshot) wiki(e *Entry) bool {
	if !s.docs {
		return false
	}
	for _, r := range e.Repositories {
		if rp, ok := s.repos[r]; ok && rp.Wiki {
			return true
		}
	}
	return false
}

// matches reports whether an entry holds q in its name, its description or a keyword, without regard to case.
func matches(e *Entry, q string) bool {
	if q == "" {
		return true
	}
	if strings.Contains(strings.ToLower(e.Name), q) || strings.Contains(strings.ToLower(e.Description), q) {
		return true
	}
	for _, k := range e.Keywords {
		if strings.Contains(strings.ToLower(k), q) {
			return true
		}
	}
	return false
}

// relevance ranks a match of q as the main installation's list does: a name equal to it, one that starts with it, one that holds
// it, then a match in the description or a keyword (Q-ANS-121, without the format's rank).
func relevance(e *Entry, q string) int {
	name := strings.ToLower(e.Name)
	switch {
	case q == "":
		return 0
	case name == q:
		return 0
	case strings.HasPrefix(name, q):
		return 1
	case strings.Contains(name, q):
		return 2
	}
	return 3
}
