package iface

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The public catalogue of a registration (R-BAS-101 to R-BAS-103): the anonymous view the main installation builds as
// documents for a Bastion whose web interface is on, which the Bastion mirrors through [PathCatalogue],
// [PathCatalogueDocuments], [PathCatalogueDocument] and [PathCataloguePublic] and serves its pages from. A document is named
// by a [CataloguePath].
//
// A document travels and is kept gzip-compressed (RFC 1952), each document compressed alone: the single read answers the
// compressed bytes as [CatalogueDocumentMediaType], and a frame of the batch read carries them. Its SHA-256 and its size are
// those of the raw, uncompressed bytes, everywhere: in a [CatalogueChange], in a [CatalogueFrame] and in the single read's ETag,
// so that the address of a document does not change with the compressor, and a Bastion decompresses what it receives, at most
// to the size it was told, and verifies the SHA-256 before it keeps the compressed bytes.

// CatalogueChanges is the answer of [PathCatalogue]: the changes of the catalogue after the request's cursor, in the
// order of their Seq. Entries is written as [] when empty.
type CatalogueChanges struct {
	Entries []CatalogueChange `json:"entries"`
	// Cursor is the Seq of the last entry returned, or the request's cursor when there is none.
	Cursor int64 `json:"cursor"`
	// More is true when the limit cut the answer.
	More bool `json:"more"`
	// Reset is true when the cursor is no longer valid (the catalogue was rebuilt, a restore reset it): the Bastion reads
	// the whole list again from 0 while it keeps serving what it holds.
	Reset bool `json:"reset"`
}

// MarshalJSON writes the answer with [] for no entry: the member is always present.
func (c CatalogueChanges) MarshalJSON() ([]byte, error) {
	type plain CatalogueChanges
	p := plain(c)
	if p.Entries == nil {
		p.Entries = []CatalogueChange{}
	}
	return json.Marshal(p)
}

// CatalogueChange is one change of a document: its path, and the SHA-256 (lowercase hex) and size of its raw bytes, or Deleted.
type CatalogueChange struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Deleted bool   `json:"deleted"`
	Seq     int64  `json:"seq"`
}

// Validate checks an answer to a request after the cursor after: every Seq above after and above the one before it, every
// path of the grammar ([ParseCataloguePath]), a document with a SHA-256 of 64 lowercase hex and a size not below 0, a
// deleted one with neither, and the cursor the last Seq (or after when there is no entry). A reset answer may start at any
// Seq above 0.
func (c CatalogueChanges) Validate(after int64) error {
	last := after
	if c.Reset {
		last = 0
	}
	for i, e := range c.Entries {
		if e.Seq <= last {
			return fmt.Errorf("bastion catalogue: entry %d has the seq %d after %d", i, e.Seq, last)
		}
		last = e.Seq
		if _, err := ParseCataloguePath(e.Path); err != nil {
			return fmt.Errorf("bastion catalogue: entry %d: %w", i, err)
		}
		switch {
		case e.Deleted && (e.SHA256 != "" || e.Size != 0):
			return fmt.Errorf("bastion catalogue: entry %d is deleted and names a document", i)
		case !e.Deleted && (!sha256Hex(e.SHA256) || e.Size < 0):
			return fmt.Errorf("bastion catalogue: entry %d has the sha256 %q and the size %d", i, e.SHA256, e.Size)
		}
	}
	if c.Cursor != last {
		return fmt.Errorf("bastion catalogue: the cursor %d is not the last seq %d", c.Cursor, last)
	}
	return nil
}

// CataloguePublic is the answer of [PathCataloguePublic]: the projects and repositories the catalogue shows, which a
// Bastion re-reads on a feed entry of the kinds grants and settings and hides what fell out of. The lists are written as []
// when empty.
type CataloguePublic struct {
	Projects     []CatalogueProject    `json:"projects"`
	Repositories []CatalogueRepository `json:"repositories"`
}

// MarshalJSON writes the set with [] for empty lists: the members are always present.
func (p CataloguePublic) MarshalJSON() ([]byte, error) {
	type plain CataloguePublic
	q := plain(p)
	if q.Projects == nil {
		q.Projects = []CatalogueProject{}
	}
	if q.Repositories == nil {
		q.Repositories = []CatalogueRepository{}
	}
	return json.Marshal(q)
}

// CatalogueProject is one public project of the catalogue (R-BAS-128): its key, the display name and the description an anonymous
// reader of the main installation reads of it, so that the Bastion's pages name a project as the main installation's do. Description
// is "" for a project that has none.
type CatalogueProject struct {
	Key         string `json:"key"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

// ProjectKeys are the keys of the projects, in their order.
func (p CataloguePublic) ProjectKeys() []string {
	out := make([]string, 0, len(p.Projects))
	for _, x := range p.Projects {
		out = append(out, x.Key)
	}
	return out
}

// CatalogueRepository is one public repository of the catalogue: its qualified name "<projectKey>/<repoKey>", its format
// and class (local, remote or virtual), the description an anonymous reader of the main installation reads of it with its links
// rewritten as a document's text is ("" for none; R-BAS-130), and whether its wiki is public, so that its documentation is part
// of the catalogue while docs is on.
type CatalogueRepository struct {
	Name        string `json:"name"`
	Format      string `json:"format"`
	Class       string `json:"class"`
	Description string `json:"description"`
	Wiki        bool   `json:"wiki"`
}

// CatalogueStateDocument is the document of the catalogue's state ([CatalogueState], path "state"): the time of the builder's last
// run that changed a document, today's signatures of the malware scan (malware-scanning.md R-MAL-042) and today's vulnerability
// database of the image scanning, which the Bastion's web interface compares every malware state and every image's grade of a page
// with. The main installation writes it; the Bastion reads Malware and Vulnerabilities.
type CatalogueStateDocument struct {
	BuiltAt string `json:"builtAt"`
	// Malware is null while the main installation answers no set (its scanning is off, or its anonymous caller may not read it).
	Malware *CatalogueMalware `json:"malware"`
	// Vulnerabilities is today's vulnerability database of the image scanning (vulnerability-scanning.md R-VULN-052), which the Bastion's
	// grades compare an image's scan with; null while the main installation answers none (its image scanning is off).
	Vulnerabilities *CatalogueVulnerabilityDatabase `json:"vulnerabilities"`
}

// CatalogueVulnerabilityDatabase is what a Bastion shows of the main installation's vulnerability database in use (Q-BAS-490): the unit
// that scans with it, the digest of its layer and its build time. When the main installation last confirmed it and whether that is stale
// are facts of its operation and stay there, as the malware scan's time of import does.
type CatalogueVulnerabilityDatabase struct {
	// Scanner is the unit that scans images, `vuln-scanner-trivy`.
	Scanner string `json:"scanner"`
	// Digest is the database's digest (`sha256:<hex>`) and BuiltAt its build time (RFC 3339); each null while none is in use.
	Digest  *string `json:"digest"`
	BuiltAt *string `json:"builtAt"`
}

// CatalogueMalware is what a Bastion shows of the main installation's signatures in use (R-MAL-042, Q-BAS-490): the set, its daily
// and its build time, the unit that checks, and the two bounds of the classes of a check (malware-scanning.md R-MAL-037), from which
// the Bastion's pages class every check themselves. It holds no time of an import, no engine version and no replica: those are
// facts of the main installation's operation (W-36's B.15).
type CatalogueMalware struct {
	// Version is the set in use (`main-63.daily-28201.bytecode-339`), Daily the number of its daily, BuiltAt the daily's build time
	// (RFC 3339); each null while no set is in use.
	Version *string `json:"version"`
	Daily   *int64  `json:"daily"`
	BuiltAt *string `json:"builtAt"`
	// Scanner is the unit that checks, `malware-scanner-clamav`.
	Scanner string `json:"scanner"`
	// CycleDays is `rescan_cycle` and OverdueAfterDays `rescan_overdue_after` in days: a check whose signatures are CycleDays older
	// than the set in use is due, and overdue OverdueAfterDays after that.
	CycleDays        float64 `json:"cycleDays"`
	OverdueAfterDays float64 `json:"overdueAfterDays"`
}

// CatalogueKind is the kind of a document of the catalogue.
type CatalogueKind string

// The kinds of document and the form of their path. A placeholder is one segment percent-encoded by the rule of
// [CataloguePath]; <file...> is one or more of them.
const (
	CatalogueIndex    CatalogueKind = "index"    // index/<n>
	CataloguePackage  CatalogueKind = "package"  // packages/<id>
	CatalogueVersions CatalogueKind = "versions" // packages/<id>/versions
	// CatalogueProjectIndex and CatalogueRepositoryIndex are the index of one project's and of one repository's public packages
	// (R-BAS-130), in pages of the global index's size: the main installation's answers with the scope, whose counts and latest
	// version are the scope's own. A repository's index is built only while the registration shows its repositories (R-BAS-131).
	CatalogueProjectIndex    CatalogueKind = "projectIndex"    // projects/<project>/index/<n>
	CatalogueRepositoryIndex CatalogueKind = "repositoryIndex" // repositories/<project>/<repo>/index/<n>
	// CataloguePackageImages is an image's list by content (R-BAS-130): one entry per manifest, led by its highest tag, the image's
	// other tags as its aliases, the main installation's anonymous answer of its versions with group=content.
	CataloguePackageImages CatalogueKind = "packageImages" // packages/<id>/images
	// CataloguePackageVulnerabilities is a package's open findings per version: the grade and the counts of every version
	// (R-BAS-128), the main installation's anonymous answer of GET /api/v1/packages/{id}/vulnerabilities.
	CataloguePackageVulnerabilities CatalogueKind = "packageVulnerabilities" // packages/<id>/vulnerabilities
	CatalogueVersion                CatalogueKind = "version"                // packages/<id>/versions/<v>
	CatalogueReadme                 CatalogueKind = "readme"                 // packages/<id>/versions/<v>/readme
	CatalogueVulnerabilities        CatalogueKind = "vulnerabilities"        // packages/<id>/versions/<v>/vulnerabilities
	CatalogueSignatures             CatalogueKind = "signatures"             // packages/<id>/versions/<v>/signatures
	CatalogueContents               CatalogueKind = "contents"               // packages/<id>/versions/<v>/contents
	CatalogueDocs                   CatalogueKind = "docs"                   // packages/<id>/versions/<v>/docs
	CatalogueDocEntry               CatalogueKind = "docEntry"               // packages/<id>/versions/<v>/docs/entries/<type>/<name>
	CatalogueDocFile                CatalogueKind = "docFile"                // packages/<id>/versions/<v>/docs/files/<file...>
	CatalogueSetup                  CatalogueKind = "setup"                  // setup/<project>/<repo>/<client>[/<package>[/<version>]]
	CatalogueSignatureObject        CatalogueKind = "signatureObject"        // signatures/objects/<sha256>
	CatalogueTrustedRoot            CatalogueKind = "trustedRoot"            // signing/trusted-root.json
	CatalogueChain                  CatalogueKind = "chain"                  // signing/chain.pem
	// CatalogueVersionState is the times of a version's documents (built, checked, scanned) that the documents themselves do not
	// carry, so that a document changes only with its content (R-BAS-103).
	CatalogueVersionState CatalogueKind = "versionState" // packages/<id>/versions/<v>/state
	// CatalogueState is the state of the catalogue as a whole, a [CatalogueStateDocument].
	CatalogueState CatalogueKind = "state" // state
	// The generic files of a raw repository (/files/): a folder's listing, a file's description and a file's set-up per client.
	// <path...> is the file's or folder's path in the repository, one segment per path segment.
	CatalogueFolder    CatalogueKind = "folder"    // files/<project>/<repo>/folder[/<path...>]
	CatalogueFile      CatalogueKind = "file"      // files/<project>/<repo>/file/<path...>
	CatalogueFileSetup CatalogueKind = "fileSetup" // files/<project>/<repo>/setup/<client>/<path...>
)

// versionDoc is the version-scoped document of the last segment of its path after packages/<id>/versions/<v>/.
func versionDoc(seg string) (CatalogueKind, bool) {
	switch seg {
	case "readme":
		return CatalogueReadme, true
	case "vulnerabilities":
		return CatalogueVulnerabilities, true
	case "signatures":
		return CatalogueSignatures, true
	case "contents":
		return CatalogueContents, true
	case "docs":
		return CatalogueDocs, true
	case "state":
		return CatalogueVersionState, true
	}
	return "", false
}

// Bounds of the grammar.
const (
	// MaxCataloguePathBytes bounds the text of a path.
	MaxCataloguePathBytes = 4096
	// MaxCatalogueSegmentBytes bounds the decoded value of one placeholder.
	MaxCatalogueSegmentBytes = 512
)

// CataloguePath names one document of the catalogue (R-BAS-103). Its text form ([CataloguePath.String]) and
// [ParseCataloguePath] are each other's inverse: a value is written segment by segment, every byte outside the unreserved
// characters of RFC 3986 (A-Z a-z 0-9 - . _ ~) as %XX with upper-case hex, and the parser refuses any other spelling, so a
// document has exactly one path. No value contains "/" (an encoded slash is refused), none is "." or "..", none is empty,
// and each is valid UTF-8 of at most [MaxCatalogueSegmentBytes] bytes without a control character. Only the members of the
// Kind are set.
type CataloguePath struct {
	Kind CatalogueKind
	// Index is the page of index/<n>, of a project's index (with Project) and of a repository's index (with Project and
	// Repository).
	Index int
	// Package and Version are the package's catalogue ID and the version of the package documents.
	Package string
	Version string
	// EntryType and EntryName are a documentation entry's type and name (docEntry).
	EntryType string
	EntryName string
	// File is a documentation file's path (docFile), or a generic file's or folder's path in its repository (folder, file,
	// fileSetup; empty for the root folder), its segments joined by "/".
	File string
	// Project, Repository, Client and, on demand, SetupPackage and SetupVersion are a set-up document's (setup), the first
	// three a generic file's too (folder, file, fileSetup); SetupPackage
	// is the package's catalogue ID, as Package is, since a name may hold a "/" (npm's scopes) that no segment holds. A set-up with
	// SetupPackage and no SetupVersion is a package's without a version, the form a pull by digest starts from (R-BAS-130).
	Project      string
	Repository   string
	Client       string
	SetupPackage string
	SetupVersion string
	// SHA256 is a signature object's content address, 64 lowercase hex (signatureObject).
	SHA256 string
}

// Documentation reports whether the document is part of the documentation (R-BAS-102), served only while the
// registration's switch docs is on: the README, the docs tree, its entries and its files.
func (p CataloguePath) Documentation() bool {
	switch p.Kind {
	case CatalogueReadme, CatalogueDocs, CatalogueDocEntry, CatalogueDocFile:
		return true
	}
	return false
}

// String writes the path's text form. A path whose members break the grammar writes a text [ParseCataloguePath] refuses.
func (p CataloguePath) String() string {
	e := escapeSegment
	version := "packages/" + e(p.Package) + "/versions/" + e(p.Version)
	switch p.Kind {
	case CatalogueIndex:
		return "index/" + strconv.Itoa(p.Index)
	case CataloguePackage:
		return "packages/" + e(p.Package)
	case CatalogueVersions:
		return "packages/" + e(p.Package) + "/versions"
	case CataloguePackageVulnerabilities:
		return "packages/" + e(p.Package) + "/vulnerabilities"
	case CataloguePackageImages:
		return "packages/" + e(p.Package) + "/images"
	case CatalogueProjectIndex:
		return "projects/" + e(p.Project) + "/index/" + strconv.Itoa(p.Index)
	case CatalogueRepositoryIndex:
		return "repositories/" + e(p.Project) + "/" + e(p.Repository) + "/index/" + strconv.Itoa(p.Index)
	case CatalogueVersion:
		return version
	case CatalogueReadme, CatalogueVulnerabilities, CatalogueSignatures, CatalogueContents, CatalogueDocs:
		return version + "/" + string(p.Kind)
	case CatalogueVersionState:
		return version + "/state"
	case CatalogueState:
		return "state"
	case CatalogueFolder:
		s := "files/" + e(p.Project) + "/" + e(p.Repository) + "/folder"
		if p.File != "" {
			s += "/" + escapePath(p.File)
		}
		return s
	case CatalogueFile:
		return "files/" + e(p.Project) + "/" + e(p.Repository) + "/file/" + escapePath(p.File)
	case CatalogueFileSetup:
		return "files/" + e(p.Project) + "/" + e(p.Repository) + "/setup/" + e(p.Client) + "/" + escapePath(p.File)
	case CatalogueDocEntry:
		return version + "/docs/entries/" + e(p.EntryType) + "/" + e(p.EntryName)
	case CatalogueDocFile:
		return version + "/docs/files/" + escapePath(p.File)
	case CatalogueSetup:
		s := "setup/" + e(p.Project) + "/" + e(p.Repository) + "/" + e(p.Client)
		if p.SetupPackage != "" || p.SetupVersion != "" {
			s += "/" + e(p.SetupPackage)
		}
		if p.SetupVersion != "" {
			s += "/" + e(p.SetupVersion)
		}
		return s
	case CatalogueSignatureObject:
		return "signatures/objects/" + p.SHA256
	case CatalogueTrustedRoot:
		return "signing/trusted-root.json"
	case CatalogueChain:
		return "signing/chain.pem"
	}
	return ""
}

// errCataloguePath starts every refusal of [ParseCataloguePath].
var errCataloguePath = errors.New("bastion catalogue: not a document path")

// ParseCataloguePath reads the text form of a [CataloguePath] and refuses everything else: an unknown form, a dot segment,
// an encoded slash, an empty segment, a spelling other than the canonical one, an index that is not a decimal without a
// leading zero, a SHA-256 that is not 64 lowercase hex.
func ParseCataloguePath(s string) (CataloguePath, error) {
	bad := func() (CataloguePath, error) { return CataloguePath{}, fmt.Errorf("%w: %q", errCataloguePath, s) }
	if s == "" || len(s) > MaxCataloguePathBytes {
		return bad()
	}
	raw := strings.Split(s, "/")
	segs := make([]string, len(raw))
	for i, r := range raw {
		v, ok := unescapeSegment(r)
		if !ok {
			return bad()
		}
		segs[i] = v
	}
	n := len(segs)
	switch segs[0] {
	case "index", "projects", "repositories":
		return parseIndexPath(raw, segs, bad)
	case "packages":
		return parsePackagePath(raw, segs, bad)
	case "state":
		if n == 1 {
			return CataloguePath{Kind: CatalogueState}, nil
		}
	case "files":
		return parseFilesPath(raw, segs, bad)
	case "setup":
		return parseSetupPath(segs, bad)
	case "signatures":
		if n != 3 || raw[1] != "objects" || !sha256Hex(raw[2]) {
			return bad()
		}
		return CataloguePath{Kind: CatalogueSignatureObject, SHA256: raw[2]}, nil
	case "signing":
		switch {
		case n == 2 && raw[1] == "trusted-root.json":
			return CataloguePath{Kind: CatalogueTrustedRoot}, nil
		case n == 2 && raw[1] == "chain.pem":
			return CataloguePath{Kind: CatalogueChain}, nil
		}
	}
	return bad()
}

// parseIndexPath reads the forms of an index's page: index/<n>, projects/<project>/index/<n> and
// repositories/<project>/<repo>/index/<n>. The page is a decimal written as itself.
func parseIndexPath(raw, segs []string, bad func() (CataloguePath, error)) (CataloguePath, error) {
	n := len(segs)
	var p CataloguePath
	switch {
	case segs[0] == "index" && n == 2:
		p.Kind = CatalogueIndex
	case segs[0] == "projects" && n == 4 && raw[2] == "index":
		p.Kind, p.Project = CatalogueProjectIndex, segs[1]
	case segs[0] == "repositories" && n == 5 && raw[3] == "index":
		p.Kind, p.Project, p.Repository = CatalogueRepositoryIndex, segs[1], segs[2]
	default:
		return bad()
	}
	i, ok := index(raw[n-1])
	if !ok || raw[n-1] != segs[n-1] {
		return bad()
	}
	p.Index = i
	return p, nil
}

// parseSetupPath reads the forms below setup/: a repository's set-up, a package's without a version, and a version's.
func parseSetupPath(segs []string, bad func() (CataloguePath, error)) (CataloguePath, error) {
	p := CataloguePath{Kind: CatalogueSetup}
	switch len(segs) {
	case 4:
		p.Project, p.Repository, p.Client = segs[1], segs[2], segs[3]
	case 5:
		p.Project, p.Repository, p.Client, p.SetupPackage = segs[1], segs[2], segs[3], segs[4]
	case 6:
		p.Project, p.Repository, p.Client, p.SetupPackage, p.SetupVersion = segs[1], segs[2], segs[3], segs[4], segs[5]
	default:
		return bad()
	}
	return p, nil
}

// parseFilesPath reads the forms below files/.
func parseFilesPath(raw, segs []string, bad func() (CataloguePath, error)) (CataloguePath, error) {
	n := len(segs)
	if n < 4 {
		return bad()
	}
	p := CataloguePath{Project: segs[1], Repository: segs[2]}
	switch {
	case raw[3] == "folder":
		p.Kind, p.File = CatalogueFolder, strings.Join(segs[4:], "/")
		return p, nil
	case raw[3] == "file" && n >= 5:
		p.Kind, p.File = CatalogueFile, strings.Join(segs[4:], "/")
		return p, nil
	case raw[3] == "setup" && n >= 6:
		p.Kind, p.Client, p.File = CatalogueFileSetup, segs[4], strings.Join(segs[5:], "/")
		return p, nil
	}
	return bad()
}

// parsePackagePath reads the forms below packages/.
func parsePackagePath(raw, segs []string, bad func() (CataloguePath, error)) (CataloguePath, error) {
	n := len(segs)
	if n < 2 {
		return bad()
	}
	p := CataloguePath{Kind: CataloguePackage, Package: segs[1]}
	if n == 2 {
		return p, nil
	}
	if n == 3 && raw[2] == "vulnerabilities" {
		p.Kind = CataloguePackageVulnerabilities
		return p, nil
	}
	if n == 3 && raw[2] == "images" {
		p.Kind = CataloguePackageImages
		return p, nil
	}
	if raw[2] != "versions" {
		return bad()
	}
	if n == 3 {
		p.Kind = CatalogueVersions
		return p, nil
	}
	p.Kind, p.Version = CatalogueVersion, segs[3]
	if n == 4 {
		return p, nil
	}
	kind, ok := versionDoc(raw[4])
	switch {
	case !ok:
		return bad()
	case n == 5:
		p.Kind = kind
		return p, nil
	case kind != CatalogueDocs:
		return bad()
	case raw[5] == "entries" && n == 8:
		p.Kind, p.EntryType, p.EntryName = CatalogueDocEntry, segs[6], segs[7]
		return p, nil
	case raw[5] == "files" && n >= 7:
		p.Kind, p.File = CatalogueDocFile, strings.Join(segs[6:], "/")
		return p, nil
	}
	return bad()
}

// index reads a page number: a decimal without a sign or a leading zero, at most nine digits.
func index(s string) (int, bool) {
	if s == "0" {
		return 0, true
	}
	return positiveDecimal(s)
}

// sha256Hex reports whether s is 64 lowercase hex digits.
func sha256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// unreserved reports whether b is an unreserved character of RFC 3986: written as itself, never encoded.
func unreserved(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '.' || b == '_' || b == '~'
}

// escapePath writes a path of segments joined by "/" as one segment each.
func escapePath(v string) string {
	segs := strings.Split(v, "/")
	for i := range segs {
		segs[i] = escapeSegment(segs[i])
	}
	return strings.Join(segs, "/")
}

// escapeSegment writes a value as one segment of the grammar.
func escapeSegment(v string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := range len(v) {
		c := v[i]
		if unreserved(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}

// unescapeSegment reads one segment written by escapeSegment and refuses every other spelling and every value the grammar
// does not admit.
func unescapeSegment(seg string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		if unreserved(c) {
			b.WriteByte(c)
			continue
		}
		if c != '%' || i+2 >= len(seg) {
			return "", false
		}
		hi, ok1 := upperHexDigit(seg[i+1])
		lo, ok2 := upperHexDigit(seg[i+2])
		d := hi<<4 | lo
		if !ok1 || !ok2 || unreserved(d) || d == '/' {
			return "", false
		}
		b.WriteByte(d)
		i += 2
	}
	v := b.String()
	if v == "" || v == "." || v == ".." || len(v) > MaxCatalogueSegmentBytes || !utf8.ValidString(v) {
		return "", false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return "", false
		}
	}
	return v, true
}

// The batch read of documents (R-BAS-101): POST [PathCatalogueDocuments] with a [CatalogueBatch], answered by a sequence of
// frames of [CatalogueBatchMediaType], one per requested path in the request's order. A frame is one line of JSON, a
// [CatalogueFrame], ended by "\n", followed by exactly GzipSize bytes: the document gzip-compressed, nothing for a missing one.
// The main installation stops before the frame that would take the answer's compressed bytes above its bound (package
// cataloguebounds), but answers at least one: a path the answer does not reach is asked for again.
const (
	// CatalogueBatchMediaType is the content type of the batch read's answer.
	CatalogueBatchMediaType = "application/vnd.quader.catalogue-batch"
	// CatalogueDocumentMediaType is the content type of the single read's answer: the document gzip-compressed.
	CatalogueDocumentMediaType = "application/gzip"
	// MaxCatalogueFrameHeaderBytes bounds the JSON line of a frame.
	MaxCatalogueFrameHeaderBytes = 8 << 10
)

// CatalogueBatch is the body of the batch read: the paths to read, each of the grammar, none twice, at least one.
type CatalogueBatch struct {
	Paths []string `json:"paths"`
}

// Validate checks the request: one to maxPaths paths (the main installation's bound), each of the grammar, none twice.
func (b CatalogueBatch) Validate(maxPaths int) error {
	if len(b.Paths) == 0 || len(b.Paths) > maxPaths {
		return fmt.Errorf("bastion catalogue: a batch of %d paths, 1 to %d", len(b.Paths), maxPaths)
	}
	seen := make(map[string]bool, len(b.Paths))
	for _, p := range b.Paths {
		if _, err := ParseCataloguePath(p); err != nil || seen[p] {
			return fmt.Errorf("bastion catalogue: the batch names %q, which is no document path or comes twice", p)
		}
		seen[p] = true
	}
	return nil
}

// CatalogueFrame is the header of one frame of the batch read: the path, and the SHA-256 and size of the raw document with
// the number of compressed bytes that follow, or Missing for a path the catalogue does not hold or does not serve (the
// switch docs off for a documentation path).
type CatalogueFrame struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	GzipSize int64  `json:"gzipSize"`
	Missing  bool   `json:"missing"`
}

// validate checks a frame's header: a path of the grammar, a missing document with no bytes, a document with its SHA-256,
// sizes not below 0 and compressed bytes of at most maxGzip.
func (f CatalogueFrame) validate(maxGzip int64) error {
	if _, err := ParseCataloguePath(f.Path); err != nil {
		return err
	}
	switch {
	case f.Missing && (f.SHA256 != "" || f.Size != 0 || f.GzipSize != 0):
		return fmt.Errorf("bastion catalogue: the missing frame of %q carries a document", f.Path)
	case !f.Missing && (!sha256Hex(f.SHA256) || f.Size < 0 || f.GzipSize < 1 || f.GzipSize > maxGzip):
		return fmt.Errorf("bastion catalogue: the frame of %q has the sha256 %q, the size %d and %d compressed bytes", f.Path, f.SHA256, f.Size, f.GzipSize)
	}
	return nil
}

// WriteCatalogueFrame writes one frame: its header line and the compressed bytes, len(gz) of them, which it sets as GzipSize.
func WriteCatalogueFrame(w io.Writer, f CatalogueFrame, gz []byte) error {
	f.GzipSize = int64(len(gz))
	head, err := Marshal(f)
	if err != nil {
		return err
	}
	if _, err := w.Write(append(head, '\n')); err != nil {
		return err
	}
	_, err = w.Write(gz)
	return err
}

// ReadCatalogueFrame reads the next frame: its header and its compressed bytes, at most maxGzip of them. It returns io.EOF at
// the end of the answer, and an error for a header that does not decode strictly, breaks the frame's rules or ends early.
func ReadCatalogueFrame(r *bufio.Reader, maxGzip int64) (CatalogueFrame, []byte, error) {
	line, err := r.ReadSlice('\n')
	switch {
	case errors.Is(err, io.EOF) && len(line) == 0:
		return CatalogueFrame{}, nil, io.EOF
	case errors.Is(err, bufio.ErrBufferFull) || len(line) > MaxCatalogueFrameHeaderBytes:
		return CatalogueFrame{}, nil, errors.New("bastion catalogue: a frame's header is too long")
	case err != nil:
		return CatalogueFrame{}, nil, fmt.Errorf("bastion catalogue: a frame's header ends early: %w", err)
	}
	var f CatalogueFrame
	if err := DecodeStrict(bytes.NewReader(line[:len(line)-1]), &f); err != nil {
		return CatalogueFrame{}, nil, err
	}
	if err := f.validate(maxGzip); err != nil {
		return CatalogueFrame{}, nil, err
	}
	gz := make([]byte, f.GzipSize)
	if _, err := io.ReadFull(r, gz); err != nil {
		return CatalogueFrame{}, nil, fmt.Errorf("bastion catalogue: the frame of %q ends early: %w", f.Path, err)
	}
	return f, gz, nil
}
