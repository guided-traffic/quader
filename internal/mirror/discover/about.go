package discover

import (
	"context"
	"net/url"
	"strings"
	"unicode/utf8"
)

// What the tab Upstream shows of a package that is not stored yet, beside its versions (remote-curation.md R-CUR-028): the facts of its
// newest version that the answers a catalogue call reads tell anyway ([About], on [PackageInfo]), and its README, read through and not
// stored by the one further call a format with a README answers ([Documented]). The facts are no wiki content (R-AUTHZ-034) and every
// reader of the tab sees them; the README is wiki content, and the API asks the wiki before it calls.

// About is what the answers a package's lookup reads tell of its newest version besides the versions (R-CUR-028): the facts a stored
// version's page shows in its head and its package details. Every text is the upstream's, bounded plain text ([CutAbout]); an address is
// kept only for http and https.
type About struct {
	// Version is the version the facts are of: npm's latest dist-tag, NuGet's newest registration leaf, the Galaxy's highest version.
	// "" when the answers name none, and then the rest is empty too.
	Version string
	// License is the version's licence as the upstream names it: an SPDX expression or a name.
	License string
	// Links are the package's addresses by kind, at most one of each kind, in the order of [LinkKinds].
	Links []Link
	// Dependencies are the version's dependencies as its document declares them, at most [MaxDependencies].
	Dependencies []Dependency
	// Deprecation is the upstream's word that the version is deprecated: its message or reasons, and the package to use instead; nil
	// when it is not deprecated.
	Deprecation *Deprecation
}

// Empty reports whether the facts tell nothing.
func (a About) Empty() bool {
	return a.Version == "" && a.License == "" && len(a.Links) == 0 && len(a.Dependencies) == 0 && a.Deprecation == nil
}

// LinkKind names an address of a package.
type LinkKind string

// The kinds of a package's addresses, in the order a screen lists them.
const (
	LinkHomepage      LinkKind = "homepage"
	LinkRepository    LinkKind = "repository"
	LinkIssues        LinkKind = "issues"
	LinkDocumentation LinkKind = "documentation"
	// LinkLicense is the address of the licence's text, where the upstream names one instead of an expression (NuGet's licenseUrl).
	LinkLicense LinkKind = "license"
)

// LinkKinds returns the kinds in the order of the screen and of the API's enum.
func LinkKinds() []LinkKind {
	return []LinkKind{LinkHomepage, LinkRepository, LinkIssues, LinkDocumentation, LinkLicense}
}

// Link is one address of a package.
type Link struct {
	Kind LinkKind
	URL  string
}

// Dependency is one dependency of a version.
type Dependency struct {
	// Name is the package the version depends on, Range the versions it takes as the document writes them ("" for any).
	Name, Range string
	// Group is what the dependency belongs to: npm's "peer" or "optional", NuGet's target framework; "" for a plain one.
	Group string
}

// Deprecation is the upstream's word that a version is deprecated.
type Deprecation struct {
	// Message is the upstream's sentence ("" when it gives none); Reasons are NuGet's reasons (Legacy, CriticalBugs, Other).
	Message string
	Reasons []string
	// Alternate is the package to use instead, with its range ("" for none).
	Alternate, AlternateRange string
}

// The bounds of [About] (R-CUR-008: every text of the upstream is bounded plain text).
const (
	MaxDependencies = 200
	MaxURLBytes     = 2048
	maxNameRunes    = 256
)

// CutAbout bounds the facts: each text cut as [CutText] cuts it, an address kept only when it is an absolute http or https address of
// at most [MaxURLBytes] with a host and no credential, one link of each kind, at most [MaxDependencies] dependencies with a name.
func CutAbout(a About) About {
	out := About{Version: cutRunes(a.Version, maxNameRunes), License: cutRunes(a.License, maxNameRunes)}
	if out.Version == "" {
		return About{}
	}
	seen := map[LinkKind]bool{}
	for _, k := range LinkKinds() {
		for _, l := range a.Links {
			if l.Kind != k || seen[k] {
				continue
			}
			if u, ok := WebAddress(l.URL); ok {
				out.Links = append(out.Links, Link{Kind: k, URL: u})
				seen[k] = true
			}
		}
	}
	for _, d := range a.Dependencies {
		if len(out.Dependencies) == MaxDependencies {
			break
		}
		if n := cutRunes(d.Name, maxNameRunes); n != "" {
			out.Dependencies = append(out.Dependencies, Dependency{Name: n, Range: cutRunes(d.Range, maxNameRunes), Group: cutRunes(d.Group, maxNameRunes)})
		}
	}
	if d := a.Deprecation; d != nil {
		c := &Deprecation{Message: CutText(d.Message), Alternate: cutRunes(d.Alternate, maxNameRunes), AlternateRange: cutRunes(d.AlternateRange, maxNameRunes)}
		for _, r := range d.Reasons {
			if r = cutRunes(r, 64); r != "" && len(c.Reasons) < 8 {
				c.Reasons = append(c.Reasons, r)
			}
		}
		out.Deprecation = c
	}
	return out
}

// WebAddress returns the address s names when it is an absolute http or https address with a host, no credential and at most
// [MaxURLBytes]: the only addresses a screen makes a link of. A repository address in npm's git form (git+https://…, git://…, the
// scp-like git@host:path) is read as the https address of the same host and path, as npm's own pages show it.
func WebAddress(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > MaxURLBytes || strings.ContainsAny(s, " \t\r\n") {
		return "", false
	}
	s = strings.TrimPrefix(s, "git+")
	if rest, ok := strings.CutPrefix(s, "git@"); ok {
		if host, path, ok := strings.Cut(rest, ":"); ok && !strings.Contains(host, "/") {
			s = "https://" + host + "/" + strings.TrimPrefix(path, "/")
		}
	}
	if rest, ok := strings.CutPrefix(s, "git://"); ok {
		s = "https://" + rest
	}
	if rest, ok := strings.CutPrefix(s, "ssh://git@"); ok {
		s = "https://" + rest
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	if strings.HasSuffix(u.Path, ".git") {
		u.Path = strings.TrimSuffix(u.Path, ".git")
		u.RawPath = ""
	}
	return u.String(), true
}

func cutRunes(s string, n int) string {
	s = strings.TrimSpace(strings.ToValidUTF8(s, ""))
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// Readme is the README of a package's version as its upstream serves it, read through and not stored (R-CUR-028).
type Readme struct {
	// Version is the version the README is of.
	Version string
	// Found says that the upstream has a README for the version; false with Unreachable empty: it has none.
	Found bool
	// HTML says that Text is HTML the upstream rendered (the Galaxy's docs-blob, a role's readme_html), sanitized as such; otherwise
	// Text is Markdown, rendered and sanitized as a stored README is (R-UI-008).
	HTML bool
	Text string
	// Truncated says that the text was cut at [MaxReadmeBytes].
	Truncated bool
	// Unreachable is the host the upstream names for the README that the repository does not reach (neither the upstream's nor one of
	// its extraHosts, cleartext from an https upstream, or refused by the egress rules): nothing was asked there, and Found is false.
	Unreachable string
}

// MaxReadmeBytes bounds the README a format reads for the tab Upstream: a longer text is cut, and the renderer cuts it again at its own
// source bound (ui.readme.max_source_bytes).
const MaxReadmeBytes = 1 << 20

// Documented is the optional part of a format's catalogue whose upstream serves a package's README (R-CUR-028): npm's package
// document, NuGet's ReadmeUriTemplate, the Galaxy's docs-blob and a role's content. A format without one answers no README call.
type Documented interface {
	// Readme reads the README of the package's version through u, one request at most beyond what the package's lookup reads: the
	// version is the one [About] named. A version the upstream does not have, or one without a README, is Readme{Found: false}.
	Readme(ctx context.Context, u Upstream, name, version string) (Readme, error)
}

// CutReadme bounds a README's text to [MaxReadmeBytes] of valid UTF-8, setting Truncated where it cut.
func CutReadme(r Readme) Readme {
	r.Text = strings.ToValidUTF8(r.Text, "�")
	if len(r.Text) > MaxReadmeBytes {
		cut := MaxReadmeBytes
		for cut > 0 && !utf8.RuneStart(r.Text[cut]) {
			cut--
		}
		r.Text, r.Truncated = r.Text[:cut], true
	}
	return r
}
