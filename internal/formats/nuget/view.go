package nuget

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/nuget/nupkg"
	"github.com/guided-traffic/quader/internal/formats/nuget/rows"
	"github.com/guided-traffic/quader/internal/formats/nuget/version"
	"github.com/guided-traffic/quader/internal/formats/versionkey"
)

// viewPort is the part of the data port the package view reads (R-API-029, R-EP-016): the
// statements are in internal/formatdata/nuget/view.go. It is a separate interface because
// the contract's DataPort ([Store]) is the port of the protocol and the writes; the same
// bound value implements both, and the twin of nugettest does too.
type viewPort interface {
	// ViewSummaries reads the description and the tags of the given versions; a version
	// without a NuGet row is absent from the answer.
	ViewSummaries(ctx context.Context, ids []int64) ([]rows.ViewSummary, error)
	// ViewDetails reads the metadata of the given versions and, when withReadme is set,
	// the text of their README file.
	ViewDetails(ctx context.Context, ids []int64, withReadme bool) ([]rows.ViewDetail, error)
	// ViewFind returns the ids of at most limit versions of the repositories (by key)
	// whose description contains text (case-insensitively) or whose tags contain keyword
	// exactly; exactly one of the two is set.
	ViewFind(ctx context.Context, repos []string, text, keyword string, limit int) ([]int64, error)
	// ViewStored lists the versions a repository serves of an ID, in the URL form: newest
	// arrival first for a local repository, newest published first for a remote one.
	ViewStored(ctx context.Context, repoID int64, lowerID string, remote bool, limit int) ([]string, error)
	// ViewDeprecated returns the ids of at most limit versions of the repositories that carry a deprecation (R-NUGET-036).
	ViewDeprecated(ctx context.Context, repos []string, limit int) ([]int64, error)
	// ViewEmbedded reads a file the .nuspec of a version embeds ("icon", "license") out of its stored package, at most max bytes;
	// found is false for none (R-NUGET-038).
	ViewEmbedded(ctx context.Context, versionID int64, file string, max int64) ([]byte, bool, error)
}

// ViewEmbedded reads the icon or the licence file a version embeds, for the screens (R-NUGET-038): the files of R-NUGET-024, out of the
// stored package. Which bytes are served, and as what, is the API's to decide (an icon only as PNG or JPEG by its bytes, a licence only
// as UTF-8 text); the format hands out what the package holds within the bound.
func (f *Format) ViewEmbedded(ctx context.Context, ports formats.DataPorts, v formats.ViewVersion, file string) ([]byte, bool, error) {
	if file != formats.EmbeddedIcon && file != formats.EmbeddedLicense {
		return nil, false, nil
	}
	p, err := portOf(ports)
	if err != nil {
		return nil, false, err
	}
	return p.ViewEmbedded(ctx, int64(v.ID), file, maxEmbedded)
}

// View returns the format's part of the package screens (R-API-029, Q-NUGET-48).
func (f *Format) View() formats.ViewAdapter { return viewAdapter{} }

type viewAdapter struct{}

var _ formats.ViewAdapter = viewAdapter{}

func portOf(ports formats.DataPorts) (viewPort, error) {
	if ports != nil {
		if p, ok := ports.FormatData(ID).(viewPort); ok {
			return p, nil
		}
	}
	return nil, errs.E(errs.Internal, errs.CodeInternal, "nuget: the data port has no package view")
}

// Compare orders versions by NuGet's precedence (R-NUGET-003): the numeric parts, then a
// release above its pre-releases and the labels identifier by identifier, case-insensitively.
// Two spellings that NuGet treats as one version (the case of a label) still compare
// unequal, so that the order is total over the strings the format stores; a string that is
// no version sorts before every version.
func (viewAdapter) Compare(a, b string) int {
	va, ea := version.Parse(a)
	vb, eb := version.Parse(b)
	switch {
	case ea != nil && eb != nil:
		return strings.Compare(a, b)
	case ea != nil:
		return -1
	case eb != nil:
		return 1
	}
	if c := version.Compare(va, vb); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// SortKey is the stored key of Compare's order (Q-API-429): the precedence key of the version package (R-NUGET-003), the labels
// lower-cased, then the string itself.
func (viewAdapter) SortKey(v string) []byte {
	nv, err := version.Parse(v)
	if err != nil {
		return versionkey.Of(nil, false, v)
	}
	return versionkey.Of(nv.SortKey(), true, v)
}

// Prerelease reports whether a version has a pre-release label (`2.0.0-beta.1`, R-NUGET-003), the versions NuGet clients show and
// install only when asked for them and an exposure rule without pre-releases hides. A string that is no version is none.
func (viewAdapter) Prerelease(v string) bool {
	nv, err := version.Parse(v)
	return err == nil && nv.IsPrerelease()
}

// Stored lists what the repository serves of the package (R-EP-016, Q-EP-61): every version
// it holds, listed or not, in the URL form. A hosted repository lists them in arrival order,
// newest first; a remote one lists the versions of its cache, newest published first. A
// virtual repository stores nothing of its own.
func (viewAdapter) Stored(ctx context.Context, ports formats.DataPorts, q formats.StoredQuery) (formats.Stored, error) {
	if q.Repo.Class != formats.Local && q.Repo.Class != formats.Remote {
		return formats.Stored{}, nil
	}
	p, err := portOf(ports)
	if err != nil {
		return formats.Stored{}, err
	}
	versions, err := p.ViewStored(ctx, q.Repo.ID, strings.ToLower(q.Package), q.Repo.Class == formats.Remote, q.Limit)
	return formats.Stored{Versions: versions}, err
}

// Withheld is empty: NuGet withholds no stored version. A removed version has no row, only
// a tombstone that keeps its number from returning (R-NUGET-006), and the replay of a
// restore reports the versions it would revive without touching them (Q-NUGET-10).
func (viewAdapter) Withheld(context.Context, formats.DataPorts, []string) ([]formats.VersionID, error) {
	return nil, nil
}

// Find asks the data port for the versions whose description or tags match.
func (viewAdapter) Find(ctx context.Context, ports formats.DataPorts, f formats.ViewFind, limit int) ([]formats.VersionID, error) {
	p, err := portOf(ports)
	if err != nil {
		return nil, err
	}
	ids, err := p.ViewFind(ctx, f.Repos, f.Text, f.Keyword, limit+1)
	out := make([]formats.VersionID, len(ids))
	for i, id := range ids {
		out[i] = formats.VersionID(id)
	}
	return out, err
}

// Describe reads the description and the tags of the versions, and with ViewDetails the
// licenses, the README flag and the metadata of Q-NUGET-48. The README text is read only
// when asked for. NuGet has no member that names other versions, so the visibility function
// is not needed.
func (viewAdapter) Describe(ctx context.Context, ports formats.DataPorts, vs []formats.ViewVersion, want formats.ViewFields, _ func(string) bool) (map[formats.VersionID]formats.ViewData, error) {
	p, err := portOf(ports)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(vs))
	for i, v := range vs {
		ids[i] = int64(v.ID)
	}
	out := make(map[formats.VersionID]formats.ViewData, len(vs))
	if !want.Has(formats.ViewDetails) {
		ss, err := p.ViewSummaries(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, s := range ss {
			data := formats.ViewData{
				Description: s.Description, Keywords: s.Tags, Deprecation: deprecationOf(s.Deprecation), Unlisted: !s.Listed,
			}
			if k := kindOf(s.PackageTypes); k != "" {
				// The kind a list's row asks the commands of (R-NUGET-037), as an image's kind is read (VersionFilter.WithKind).
				data.Metadata = map[string]any{"kind": k}
			}
			out[formats.VersionID(s.VersionID)] = data
		}
		return out, nil
	}
	ds, err := p.ViewDetails(ctx, ids, want.Has(formats.ViewReadme))
	if err != nil {
		return nil, err
	}
	for _, d := range ds {
		data, err := describeMetadata(d)
		if err != nil {
			return nil, errs.Wrap(err, "nuget: version metadata")
		}
		out[formats.VersionID(d.VersionID)] = data
	}
	return out, nil
}

// metadataView is the part of nuget_version.metadata the screens read (R-NUGET-005). Every
// member is raw: the document is what a publisher wrote, so a member of an unexpected type is
// left out and fails nothing.
type metadataView struct {
	Title        json.RawMessage `json:"title"`
	Summary      json.RawMessage `json:"summary"`
	Description  json.RawMessage `json:"description"`
	ReleaseNotes json.RawMessage `json:"releaseNotes"`
	Copyright    json.RawMessage `json:"copyright"`
	Language     json.RawMessage `json:"language"`
	Authors      json.RawMessage `json:"authors"`
	Tags         json.RawMessage `json:"tags"`
	ProjectURL   json.RawMessage `json:"projectUrl"`
	LicenseURL   json.RawMessage `json:"licenseUrl"`
	License      json.RawMessage `json:"license"`
	Readme       json.RawMessage `json:"readme"`
	Repository   json.RawMessage `json:"repository"`
	MinClient    json.RawMessage `json:"minClientVersion"`
	Groups       json.RawMessage `json:"dependencyGroups"`
	Types        json.RawMessage `json:"packageTypes"`
	Acceptance   json.RawMessage `json:"requireLicenseAcceptance"`
	DevDep       json.RawMessage `json:"developmentDependency"`
	Deprecation  json.RawMessage `json:"deprecation"`
	Icon         json.RawMessage `json:"icon"`
	Contents     json.RawMessage `json:"contents"`
}

// deprecationOf reads the member deprecation of a version's metadata (R-NUGET-036); nil for none, and for one that is not the
// registration's shape.
func deprecationOf(raw []byte) *formats.Deprecation {
	if len(raw) == 0 {
		return nil
	}
	var d rows.Deprecation
	if json.Unmarshal(raw, &d) != nil || len(d.Reasons) == 0 && d.Message == "" && d.AlternatePackage == nil {
		return nil
	}
	out := &formats.Deprecation{Message: strings.TrimSpace(d.Message), Reasons: d.Reasons}
	if a := d.AlternatePackage; a != nil && a.ID != "" {
		out.Alternate = &formats.DeprecationAlternate{Package: a.ID, Range: a.Range}
	}
	return out
}

// describeMetadata turns the stored metadata of a version into the view data of Q-NUGET-48.
func describeMetadata(d rows.ViewDetail) (formats.ViewData, error) {
	var m metadataView
	if err := json.Unmarshal(d.Metadata, &m); err != nil {
		return formats.ViewData{}, err
	}
	data := formats.ViewData{
		Description: firstText(m.Description, m.Summary),
		Keywords:    tagList(m.Tags),
		Licenses:    licenseExpression(m.License),
		HasReadme:   text(m.Readme) != "",
		Readme:      d.Readme,
		ReadmeFile:  d.ReadmeFile,
		Metadata:    map[string]any{"listed": d.Listed},
		Deprecation: deprecationOf(m.Deprecation),
		Unlisted:    !d.Listed,
	}
	md := data.Metadata
	if d.DeclaredID != "" {
		md["id"] = d.DeclaredID
	}
	for key, raw := range map[string]json.RawMessage{
		"title": m.Title, "summary": m.Summary, "releaseNotes": m.ReleaseNotes, "copyright": m.Copyright,
		"language": m.Language, "minClientVersion": m.MinClient, "licenseUrl": m.LicenseURL,
	} {
		if s := text(raw); s != "" {
			md[key] = s
		}
	}
	if s := text(m.ProjectURL); s != "" {
		md["homepage"] = s
	}
	repositoryFacts(md, m.Repository)
	licenseFile(md, m.License)
	if text(m.Icon) != "" {
		// The picture the package embeds (R-NUGET-038): the screen reads it through the management API, never from an address of the
		// package's own.
		md["hasIcon"] = true
	}
	contentFacts(md, m.Contents, m.Groups)
	if a := authorList(m.Authors); len(a) > 0 {
		md["authors"] = a
	}
	if t := tagList(m.Tags); len(t) > 0 {
		md["tags"] = t
	}
	if g := dependencyGroups(m.Groups); len(g) > 0 {
		md["dependencyGroups"] = g
	}
	if t := packageTypes(m.Types); len(t) > 0 {
		md["packageTypes"] = t
	}
	if k := kindOf(packageTypes(m.Types)); k != "" {
		md["kind"] = k
	}
	for key, raw := range map[string]json.RawMessage{"requireLicenseAcceptance": m.Acceptance, "developmentDependency": m.DevDep} {
		var b bool
		if json.Unmarshal(raw, &b) == nil && b {
			md[key] = true
		}
	}
	return data, nil
}

func text(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func firstText(raws ...json.RawMessage) string {
	for _, r := range raws {
		if s := text(r); s != "" {
			return s
		}
	}
	return ""
}

// tagList reads the tags: the list the ingestion stores, or, from a row written around it,
// the white-space separated string of the .nuspec.
func tagList(raw json.RawMessage) []string {
	var list []any
	if json.Unmarshal(raw, &list) != nil {
		return strings.Fields(text(raw))
	}
	var out []string
	for _, v := range list {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

// authorList splits the authors string of the .nuspec, a comma separated list.
func authorList(raw json.RawMessage) []string {
	var out []string
	for a := range strings.SplitSeq(text(raw), ",") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// licenseExpression is the declared license expression; a license file and the legacy
// licenseUrl declare no SPDX identifier.
func licenseExpression(raw json.RawMessage) []string {
	var l struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &l) != nil || (l.Type != "" && !strings.EqualFold(l.Type, "expression")) {
		return nil
	}
	if v := strings.TrimSpace(l.Value); v != "" {
		return []string{v}
	}
	return nil
}

// repositoryFacts sends the repository element (R-NUGET-038): its address as the link of the source, and its type, branch and commit as
// they are written. The screen links the branch and the commit only where the address is an https address it can build them on.
func repositoryFacts(md map[string]any, raw json.RawMessage) {
	var r struct {
		Type   string `json:"type"`
		URL    string `json:"url"`
		Branch string `json:"branch"`
		Commit string `json:"commit"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return
	}
	for key, v := range map[string]string{"repositoryUrl": r.URL, "repositoryType": r.Type, "repositoryBranch": r.Branch, "repositoryCommit": r.Commit} {
		if v = strings.TrimSpace(v); v != "" {
			md[key] = v
		}
	}
}

// licenseFile sends the path of a licence of type file (R-NUGET-038), which the management API serves as text; a licence expression
// is the version's licences instead.
func licenseFile(md map[string]any, raw json.RawMessage) {
	var l struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &l) == nil && strings.EqualFold(l.Type, "file") && strings.TrimSpace(l.Value) != "" {
		md["licenseFile"] = strings.TrimSpace(l.Value)
	}
}

// contentFacts sends what the file list of the package says (R-NUGET-038, R-NUGET-039): its target frameworks, the frameworks of its
// dependency groups that its folders do not name, and the files that run code when a project uses it. A version whose file list was
// never read sends none of them, and so no word that it has none.
func contentFacts(md map[string]any, raw, groups json.RawMessage) {
	var c rows.Contents
	if len(raw) == 0 || json.Unmarshal(raw, &c) != nil || string(raw) == "null" || c.Unreadable {
		return
	}
	md["frameworks"] = nonNil(c.Frameworks)
	var other []string
	for _, g := range dependencyGroups(groups) {
		fw := nupkg.ShortFramework(g.TargetFramework)
		if fw != "" && !slices.Contains(c.Frameworks, fw) && !slices.Contains(other, g.TargetFramework) {
			other = append(other, g.TargetFramework)
		}
	}
	if len(other) > 0 {
		md["dependencyFrameworks"] = other
	}
	md["buildFiles"] = nonNil(c.Build)
	md["analyzers"] = nonNil(c.Analyzers)
	md["scripts"] = nonNil(c.Scripts)
	if c.More > 0 {
		md["contentsMore"] = c.More
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// The kinds of package whose install forms differ (R-NUGET-037), by their declared package type: a .NET tool, a template package and
// an MSBuild project SDK. Every other type, Dependency first, is installed as a library and has no kind.
const (
	KindTool     = "dotnet-tool"
	KindTemplate = "template"
	KindSDK      = "msbuild-sdk"
)

// kindOf is the kind of a package by its declared types, "" for a library.
func kindOf(types []string) string {
	for _, want := range []struct{ typ, kind string }{{"dotnettool", KindTool}, {"template", KindTemplate}, {"msbuildsdk", KindSDK}} {
		if slices.ContainsFunc(types, func(t string) bool { return strings.EqualFold(t, want.typ) }) {
			return want.kind
		}
	}
	return ""
}

// dependencyGroup is a group of dependencies of the version details (Q-NUGET-48): the target
// framework as the .nuspec spells it, "" for the group of a flat list.
type dependencyGroup struct {
	TargetFramework string       `json:"targetFramework,omitempty"`
	Dependencies    []dependency `json:"dependencies"`
}

type dependency struct {
	ID    string `json:"id"`
	Range string `json:"range,omitempty"`
}

// dependencyGroups reads the groups; a dependency's range is the "range" member, or the
// "version" member that the table of R-NUGET-005 names.
func dependencyGroups(raw json.RawMessage) []dependencyGroup {
	var in []struct {
		TargetFramework string `json:"targetFramework"`
		Dependencies    []struct {
			ID      string `json:"id"`
			Range   string `json:"range"`
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if json.Unmarshal(raw, &in) != nil {
		return nil
	}
	var out []dependencyGroup
	for _, g := range in {
		og := dependencyGroup{TargetFramework: g.TargetFramework, Dependencies: []dependency{}}
		for _, d := range g.Dependencies {
			if d.ID == "" {
				continue
			}
			r := d.Range
			if r == "" {
				r = d.Version
			}
			og.Dependencies = append(og.Dependencies, dependency{ID: d.ID, Range: r})
		}
		out = append(out, og)
	}
	return out
}

func packageTypes(raw json.RawMessage) []string {
	var in []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &in) != nil {
		return nil
	}
	var out []string
	for _, t := range in {
		if t.Name != "" {
			out = append(out, t.Name)
		}
	}
	return out
}
