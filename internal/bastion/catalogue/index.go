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

// indexOf builds the index of a snapshot from its pages, reading only the pages the replica has not parsed before, and reuses the
// index of cur when the pages are the same.
func (c *Catalogue) indexOf(ctx context.Context, next, cur *snapshot) (*index, error) {
	var pages []docRef
	for n := 0; ; n++ {
		r, ok := next.lookup(iface.CataloguePath{Kind: iface.CatalogueIndex, Index: n}.String())
		if !ok {
			break
		}
		pages = append(pages, r)
	}
	shas := make([]string, len(pages))
	for i, p := range pages {
		shas[i] = p.sha
	}
	if cur != nil && cur.index != nil && slices.Equal(cur.index.pages, shas) {
		return cur.index, nil
	}
	idx := &index{pages: shas, byID: map[string]*Entry{}, byName: map[string][]*Entry{}, byKey: map[string][]*Entry{}}
	parsed := map[string][]*Entry{}
	for _, p := range pages {
		entries, ok := c.pages[p.sha]
		if !ok {
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
			if entries, err = parsePage(raw); err != nil {
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
	c.pages = parsed
	sort.Slice(idx.entries, func(i, j int) bool { return byName(idx.entries[i], idx.entries[j]) })
	return idx, nil
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
