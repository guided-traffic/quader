// Package clamav is the format of ClamAV's definitions (formats/clamav.md R-CLAMAV-001 to R-CLAMAV-009, ADR-225): the signature
// databases main, daily and bytecode as a repository kind of their own. It has one repository, upstream/clamav, which the product
// declares and the malware scanning's database task writes in process (R-REPO-075, R-MAL-010); the format is system only
// (catalog.Facts.SystemOnly), so no caller creates a repository of it.
//
// A package is a database, a version one version of its container. The stored layout is <db>/<version>/<db>.cvd (or .cud) with
// the detached signature <db>/<version>/<db>-<version>.cvd.sign beside it; the side table clamav_version keeps every piece of a
// version's metadata through the data port (internal/formatdata/clamav). The endpoint below /api/clamav/{project}/{key}/ answers
// the two requests a stock freshclam with PrivateMirror makes, <db>.cvd and <db>-<version>.cvd.sign, with the version the set in
// use names (R-CLAMAV-004), and renders every refusal so that no answer is a 403 or a 429 (R-CLAMAV-005).
package clamav

import (
	"errors"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/formats/clamav/cvd"
)

// ID is the format name.
const ID formats.ID = "clamav"

// Layout is the name of the format's one layout (R-CLAMAV-002).
const Layout = "clamav"

// ClientFreshclam is the format's one client (R-CLAMAV-006).
const ClientFreshclam = "freshclam"

// The route names (R-CLAMAV-004).
const (
	RouteContainer = "clamav.container.get"
	RouteSignature = "clamav.signature.get"
)

// The route parameters.
const (
	paramDB      = "db"
	paramVersion = "version"
)

// Format implements formats.Format for ClamAV's definitions.
type Format struct{ formats.Base }

var (
	_ formats.Format        = Format{}
	_ formats.VersionPather = Format{}
)

// Descriptor returns the catalog entry of R-CLAMAV-001.
func (Format) Descriptor() formats.Descriptor {
	return formats.Descriptor{
		Facts: catalog.Facts{
			ID:               ID,
			DisplayName:      "ClamAV definitions",
			Classes:          []formats.Class{formats.Local},
			Layout:           Layout,
			ProtocolBasePath: "/api/clamav/{project}/{key}/",
			SystemOnly:       true,
			PatternSubject:   formats.SubjectPath,
			PackageNames:     true,
			// A rate limit answers 503 (R-CLAMAV-005, Rendered below), and so do a Bastion's own refusals of its limits on the
			// format's family (bastion.md R-BAS-056).
			Bastion: catalog.BastionFacts{RateLimitedStatus: http.StatusServiceUnavailable},
		},
		ContractVersion: formats.ContractVersion,
		Names:           dbNames{},
		Clients:         []formats.Client{{ID: ClientFreshclam, DisplayName: "freshclam"}},
		PropertyPrefix:  "clamav.",
		DataPort:        reflect.TypeFor[Store](),
		VersionFetch:    formats.FetchLeadFile,
	}
}

// dbNames is the name rule (R-CLAMAV-001): the three databases, compared as written.
type dbNames struct{}

func (dbNames) Normalize(name string) (string, bool) { return name, cvd.IsDB(name) }

var (
	containerRoute = regexp.MustCompile(`^(main|daily|bytecode)\.cvd$`)
	signatureRoute = regexp.MustCompile(`^(main|daily|bytecode)-([1-9][0-9]{0,9})\.cvd\.sign$`)
)

// Routes are the two requests a stock freshclam with PrivateMirror makes (R-CLAMAV-004), both of the kind Metadata
// (Q-CLAMAV-2): the bytes under <db>.cvd change with every new version in use. A HEAD reaches the GET route.
func (f Format) Routes() []formats.Route {
	return []formats.Route{
		{
			Name: RouteContainer, Method: http.MethodGet, Template: "/{db}.cvd", Examples: []string{"daily.cvd", "main.cvd", "bytecode.cvd"},
			Action: formats.ActionRead, Kind: formats.Metadata, PackageParam: paramDB, Handler: f.container,
			Match: func(p string) (formats.Params, bool) {
				m := containerRoute.FindStringSubmatch(p)
				if m == nil {
					return nil, false
				}
				return formats.Params{paramDB: m[1]}, true
			},
		},
		{
			Name: RouteSignature, Method: http.MethodGet, Template: "/{db}-{version}.cvd.sign", Examples: []string{"daily-28190.cvd.sign", "main-63.cvd.sign"},
			Action: formats.ActionRead, Kind: formats.Metadata, PackageParam: paramDB, Handler: f.signature,
			Match: func(p string) (formats.Params, bool) {
				m := signatureRoute.FindStringSubmatch(p)
				if m == nil {
					return nil, false
				}
				return formats.Params{paramDB: m[1], paramVersion: m[2]}, true
			},
		},
	}
}

// Match resolves a request to one of the two routes; daily.cld, safebrowsing.cvd and every other path match none.
func (f Format) Match(method, escapedPath string) (*formats.Route, formats.Params, error) {
	return formats.MatchRoutes(f.Routes(), method, escapedPath)
}

// Kind is what a stored path of the layout holds.
type Kind uint8

// The kinds of a stored path.
const (
	// KindContainer is <db>/<version>/<db>.cvd or <db>/<version>/<db>.cud.
	KindContainer Kind = iota + 1
	// KindSignature is <db>/<version>/<db>-<version>.cvd.sign.
	KindSignature
)

// Stored is a path of the layout (R-CLAMAV-002).
type Stored struct {
	Kind    Kind
	DB      string
	Version int
	// File is the last segment: daily.cvd, daily.cud or daily-28190.cvd.sign.
	File string
}

// ParsePath reads a stored path of the layout; ok is false for every other path.
func ParsePath(p string) (Stored, bool) {
	seg := strings.Split(p, "/")
	if len(seg) != 3 || !cvd.IsDB(seg[0]) {
		return Stored{}, false
	}
	v, err := strconv.Atoi(seg[1])
	if err != nil || v < 1 || strconv.Itoa(v) != seg[1] {
		return Stored{}, false
	}
	if db, ext, ok := cvd.Container(seg[2]); ok && db == seg[0] && (ext == cvd.ExtCVD || ext == cvd.ExtCUD) {
		return Stored{Kind: KindContainer, DB: db, Version: v, File: seg[2]}, true
	}
	if seg[2] == cvd.SignName(seg[0], v) {
		return Stored{Kind: KindSignature, DB: seg[0], Version: v, File: seg[2]}, true
	}
	return Stored{}, false
}

// ContainerPath is the stored path of a container: daily/28190/daily.cvd; file is daily.cvd or daily.cud.
func ContainerPath(db string, version int, file string) string {
	return db + "/" + strconv.Itoa(version) + "/" + file
}

// SignaturePath is the stored path of a detached signature: daily/28190/daily-28190.cvd.sign.
func SignaturePath(db string, version int) string {
	return db + "/" + strconv.Itoa(version) + "/" + cvd.SignName(db, version)
}

// Classify says what a path is (R-CLAMAV-002): a path of the layout is content of its database and version; a folder of a database
// or of a version is the listing Quader renders; <db>.cvd and <db>-<n>.cvd.sign at the root are the routes of R-CLAMAV-004, which
// serve a stored item of the version in use and are no stored path themselves; every other path is no path of the format.
func (Format) Classify(p formats.Path) formats.PathInfo {
	if s, ok := ParsePath(p.Path); ok && !p.Folder {
		return formats.PathInfo{Kind: formats.Content, Package: s.DB, Version: strconv.Itoa(s.Version)}
	}
	seg := strings.Split(strings.TrimSuffix(p.Path, "/"), "/")
	switch {
	case p.Path == "":
		return formats.PathInfo{Kind: formats.PathComputed}
	case p.Folder && len(seg) == 1 && cvd.IsDB(seg[0]):
		return formats.PathInfo{Kind: formats.PathComputed, Package: seg[0]}
	case p.Folder && len(seg) == 2 && cvd.IsDB(seg[0]) && positive(seg[1]):
		return formats.PathInfo{Kind: formats.PathComputed, Package: seg[0], Version: seg[1]}
	case !p.Folder && len(seg) == 1 && (containerRoute.MatchString(seg[0]) || signatureRoute.MatchString(seg[0])):
		return formats.PathInfo{Kind: formats.PathMetadata}
	}
	return formats.PathInfo{Kind: formats.Content}
}

func positive(s string) bool {
	v, err := strconv.Atoi(s)
	return err == nil && v >= 1 && strconv.Itoa(v) == s
}

// PackageDocumentPath reports ok false: a database has no document of its own (R-CLAMAV-007); the decision point decides a database
// on the paths of its files.
func (Format) PackageDocumentPath(string) (formats.Path, bool) { return formats.Path{}, false }

// VersionPath is the container of a version, the file a version is decided on (Q-AUTHZ-255). A version from an installation's own
// repository is a .cud; the path of its .cvd decides it the same way, since a path pattern names the folder of the version.
func (Format) VersionPath(name, version string) (formats.Path, bool) {
	v, err := strconv.Atoi(version)
	if err != nil || v < 1 || !cvd.IsDB(name) || strconv.Itoa(v) != version {
		return formats.Path{}, false
	}
	return formats.Path{Path: ContainerPath(name, v, name+"."+cvd.ExtCVD)}, true
}

// Exposure is the paths-only adapter: the path patterns of an endpoint apply as to any repository, and nothing is narrowed by
// database or version (R-CLAMAV-008).
func (Format) Exposure() formats.ExposureAdapter { return pathsOnly{} }

type pathsOnly struct{}

var errNoRanges = errors.New("the clamav format narrows no versions on an endpoint")

func (pathsOnly) ValidateRange(string) error             { return errNoRanges }
func (pathsOnly) Prerelease(string) (bool, bool)         { return false, false }
func (pathsOnly) InRange(string, string) bool            { return false }
func (pathsOnly) Dimensions() formats.ExposureDimensions { return formats.ExposureDimensions{} }
