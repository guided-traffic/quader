package npm

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats/npm/name"
	"github.com/guided-traffic/quader/internal/formats/npm/remote"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// What the tab Upstream shows of an npm package that is not stored (remote-curation.md R-CUR-028, formats/npm.md R-NPM-049): the facts
// of the version the latest dist-tag names and the README, both from the full package document the lookup reads anyway. The README is
// the document's top-level readme, which the registry writes for the latest version only; it is handed to the engine with the lookup,
// so the README call costs no further request.

var _ discover.Documented = remoteAdapter{}

// noReadme is what the registry writes into readme for a package published without one.
const noReadme = "ERROR: No README data found!"

// latestOf is the version the facts and the README are of: the latest dist-tag, else the newest version of the page's order.
func latestOf(tags map[string]string, newest []discover.VersionFacts, all map[string]json.RawMessage) string {
	if v := tags["latest"]; v != "" {
		if _, ok := all[v]; ok {
			return v
		}
	}
	if len(newest) > 0 {
		return newest[0].Version
	}
	return ""
}

// aboutOf reads the facts of the version from its entry, with the document's top-level members where the entry has none.
func aboutOf(p *remote.Parsed, ver string) discover.About {
	raw, ok := p.Versions[ver]
	if ver == "" || !ok {
		return discover.About{}
	}
	var e struct {
		License              json.RawMessage   `json:"license"`
		Licenses             json.RawMessage   `json:"licenses"`
		Homepage             json.RawMessage   `json:"homepage"`
		Repository           json.RawMessage   `json:"repository"`
		Bugs                 json.RawMessage   `json:"bugs"`
		Deprecated           json.RawMessage   `json:"deprecated"`
		Dependencies         map[string]string `json:"dependencies"`
		PeerDependencies     map[string]string `json:"peerDependencies"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
	}
	_ = json.Unmarshal(raw, &e) //nolint:errcheck // an entry that does not read tells nothing more than its version
	or := func(own json.RawMessage, member string) json.RawMessage {
		if len(own) > 0 && string(own) != "null" {
			return own
		}
		return p.Top[member]
	}
	a := discover.About{Version: ver}
	if ls := licenses(or(e.License, "license"), or(e.Licenses, "licenses")); len(ls) > 0 {
		a.License = strings.Join(ls, " OR ")
	}
	for _, l := range []struct {
		kind discover.LinkKind
		raw  json.RawMessage
	}{
		{discover.LinkHomepage, or(e.Homepage, "homepage")},
		{discover.LinkRepository, or(e.Repository, "repository")},
		{discover.LinkIssues, or(e.Bugs, "bugs")},
	} {
		if s := urlOf(l.raw); s != "" {
			a.Links = append(a.Links, discover.Link{Kind: l.kind, URL: s})
		}
	}
	for _, g := range []struct {
		group string
		deps  map[string]string
	}{{"", e.Dependencies}, {"peer", e.PeerDependencies}, {"optional", e.OptionalDependencies}} {
		names := make([]string, 0, len(g.deps))
		for n := range g.deps {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			a.Dependencies = append(a.Dependencies, discover.Dependency{Name: n, Range: g.deps[n], Group: g.group})
		}
	}
	if msg := text(e.Deprecated); msg != "" {
		a.Deprecation = &discover.Deprecation{Message: msg}
	}
	return a
}

// keywordsOf are the version's keywords, else the document's.
func keywordsOf(p *remote.Parsed, ver string) []string {
	var e struct {
		Keywords json.RawMessage `json:"keywords"`
	}
	_ = json.Unmarshal(p.Versions[ver], &e) //nolint:errcheck // an entry that does not read has no keywords
	if ks := stringList(e.Keywords); len(ks) > 0 {
		return ks
	}
	return stringList(p.Top["keywords"])
}

// readmeOf is the document's README: the top-level readme, which the registry writes for the latest version.
func readmeOf(p *remote.Parsed, ver string) discover.Readme {
	r := discover.Readme{Version: ver}
	s := text(p.Top["readme"])
	if s == "" || s == noReadme {
		return r
	}
	r.Found, r.Text = true, s
	return discover.CutReadme(r)
}

// Readme implements [discover.Documented]: the README of the full package document, read as the lookup reads it (from the store when
// the remote holds it). The engine has it from the lookup already and asks only after its cache period.
func (remoteAdapter) Readme(ctx context.Context, u discover.Upstream, pkg, _ string) (discover.Readme, error) {
	n, err := name.Parse(pkg)
	if err != nil {
		return discover.Readme{}, nil //nolint:nilerr // a text that is no npm name names no package of the upstream
	}
	doc, found, err := u.Document(ctx, n.String(), documentVariant)
	if err != nil || !found {
		return discover.Readme{}, err
	}
	parsed, err := remote.Parse(doc)
	if err != nil {
		return discover.Readme{}, errs.E(errs.UpstreamFailed, errs.CodeUpstreamError, "The package document of "+n.String()+" does not read.")
	}
	return readmeOf(parsed, latestOf(parsed.Tags(), nil, parsed.Versions)), nil
}
