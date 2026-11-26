// Package page writes what a reader that runs no script reads of a page of the Bastion's web interface (W-35's B.11, R-BAS-121): the
// head of the page (title, description, canonical address, the other languages' addresses, the robots rule, JSON-LD) and its text in
// the application's root element, and the page a visitor reads when the class gui refuses a page's address (R-BAS-123). It knows the
// web interface's own address grammar and the members of the main installation's shapes that are the same for every format, and
// nothing else: no format's metadata, no client, no request to anything. The handler of internal/bastion/gui reads the catalogue and
// hands the members to it.
package page

import (
	"net/url"
	"strings"
)

// Locales are the languages of the application, English first (W-35's B.3).
func Locales() []string { return []string{"en", "de"} }

// Kind is the kind of page an address names.
type Kind int

// The kinds of page of the application's routes (R-UI-227, web/src/bastion/app.routes.ts).
const (
	// Unknown is an address no route of the application serves: the application shows "Page not found".
	Unknown Kind = iota
	// Start is the start page: the projects.
	Start
	// List is a project's packages: projects/<project>/packages, and projects/<project>, which leads there.
	List
	// Package is a package's page: projects/<project>/packages/<format>/<name…>.
	Package
	// Versions is the table of a package's versions where the package's own address is its newest version:
	// projects/<project>/packages/<format>/<name…>/-/versions.
	Versions
	// Version is a version's page: projects/<project>/packages/<format>/<name…>@<version>.
	Version
	// Legal is the imprint or the privacy notice: legal/imprint, legal/privacy.
	Legal
)

// Address is an address of a page below /ui/<locale>/ as the application's routes read it.
type Address struct {
	Locale string
	Kind   Kind
	// Project, Format, Name and Version are a package's or a version's (Name with its slashes, an npm scope's); Project alone a list's.
	Project string
	Format  string
	Name    string
	Version string
	// LegalPage is imprint or privacy.
	LegalPage string
	// Tab is the query's tab of a package's or a version's page; Doc its documentation page (`doc`, `<type>/<name>`).
	Tab string
	Doc string
	// Filtered is true for a list whose query searches, filters or sorts: a page of results, never indexed.
	Filtered bool
	// Findings is true for an address that opens the open findings (`tab=vulnerabilities`, a severity): never indexed (W-35's B.11).
	Findings bool
}

// Parse reads the path below /ui/<locale>/ (escaped, as the request carries it) and the query of a page's address. A path the routes
// do not serve is [Unknown].
func Parse(locale, tail string, q url.Values) Address {
	a := Address{Locale: locale}
	segs, ok := segments(tail)
	if !ok {
		return a
	}
	switch {
	case len(segs) == 0:
		a.Kind = Start
	case len(segs) == 2 && segs[0] == "legal" && (segs[1] == "imprint" || segs[1] == "privacy"):
		a.Kind, a.LegalPage = Legal, segs[1]
	case len(segs) >= 2 && segs[0] == "projects":
		a.Project = segs[1]
		a.parseProject(segs[2:], q)
	}
	return a
}

// parseProject reads what follows projects/<project>: nothing or packages (the list), or a package's, a version's or the versions'
// address.
func (a *Address) parseProject(rest []string, q url.Values) {
	if len(rest) == 0 || len(rest) == 1 && rest[0] == "packages" {
		a.Kind = List
		for k := range q {
			if k == "q" || k == "format" || k == "repository" || k == "deprecated" || k == "sort" || k == "cursor" {
				a.Filtered = true
			}
		}
		return
	}
	if rest[0] != "packages" || len(rest) < 3 {
		return
	}
	a.Format = rest[1]
	names := rest[2:]
	if n := len(names); n >= 3 && names[n-2] == "-" && names[n-1] == "versions" {
		if _, v := splitVersion(names[n-3]); v != "" {
			return
		}
		a.Kind, a.Name = Versions, strings.Join(names[:n-2], "/")
		return
	}
	last := names[len(names)-1]
	name, version := splitVersion(last)
	names[len(names)-1] = name
	a.Name = strings.Join(names, "/")
	a.Kind = Package
	if version != "" {
		a.Kind, a.Version = Version, version
	}
	a.Tab, a.Doc = q.Get("tab"), q.Get("doc")
	a.Findings = a.Tab == "vulnerabilities" || q.Has("severity")
}

// splitVersion splits the last segment of a package's address at its last "@" after the first character (R-UI-111): `ui@1.4.0` is
// `ui` and `1.4.0`; `@team-a` is a scope and has no version.
func splitVersion(seg string) (name, version string) {
	at := strings.LastIndexByte(seg, '@')
	if at > 0 && at < len(seg)-1 {
		return seg[:at], seg[at+1:]
	}
	return seg, ""
}

// segments decodes the path segment by segment; an empty, a dot or an undecodable segment is no address of the routes. A trailing
// slash is the address without it.
func segments(tail string) ([]string, bool) {
	tail = strings.TrimSuffix(tail, "/")
	if tail == "" {
		return nil, true
	}
	raw := strings.Split(tail, "/")
	out := make([]string, len(raw))
	for i, r := range raw {
		v, err := url.PathUnescape(r)
		if err != nil || v == "" || v == "." || v == ".." || strings.ContainsAny(v, "/\x00") {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

// Route is the address's path below the locale in the one spelling the server writes (canonical, alternates, sitemap): each segment
// escaped as a path segment, a package's name segment by segment. It is "" for the start page.
func (a Address) Route() string {
	switch a.Kind {
	case Start:
		return ""
	case Legal:
		return "legal/" + a.LegalPage
	case List:
		return "projects/" + url.PathEscape(a.Project) + "/packages"
	case Package, Versions, Version:
		segs := strings.Split(a.Name, "/")
		for i, s := range segs {
			segs[i] = url.PathEscape(s)
		}
		if a.Kind == Version {
			segs[len(segs)-1] += "@" + url.PathEscape(a.Version)
		}
		p := "projects/" + url.PathEscape(a.Project) + "/packages/" + url.PathEscape(a.Format) + "/" + strings.Join(segs, "/")
		if a.Kind == Versions {
			p += "/-/versions"
		}
		return p
	}
	return ""
}

// PackageRoute is the route of a package's page in the project of the address.
func PackageRoute(project, format, name string) string {
	return Address{Kind: Package, Project: project, Format: format, Name: name}.Route()
}

// VersionRoute is the route of a version's page in the project of the address.
func VersionRoute(project, format, name, version string) string {
	return Address{Kind: Version, Project: project, Format: format, Name: name, Version: version}.Route()
}

// URL is the absolute address of a route in a locale on the Bastion's own base URL (W-35's B.13: never the main installation's), with
// the query when there is one; locale "" is the address without a locale, which redirects by the visitor's language (x-default).
func URL(base, locale, route string, query url.Values) string {
	u := strings.TrimSuffix(base, "/") + "/ui/"
	if locale != "" {
		u += locale + "/"
	}
	u += route
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}
