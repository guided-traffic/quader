package deb

import (
	"context"
	"sort"
	"strings"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/deb/control"
	"github.com/guided-traffic/quader/internal/formats/deb/rows"
	"github.com/guided-traffic/quader/internal/formats/deb/version"
	"github.com/guided-traffic/quader/internal/formats/versionkey"
)

// viewAdapter is the format's part of the package screens (R-DEB-030): the versions in dpkg order and, from the rows of the stored
// files, the first line of the Description, the architectures, the coordinates, the control fields with the relations parsed (R-DEB-038)
// and the licences of the copyright file (R-DEB-039). There is no README; the changelog, the copyright and the file list are read by
// their own operation (R-DEB-039). It runs no SQL: the data port answers.
type viewAdapter struct{}

var _ formats.ViewAdapter = viewAdapter{}

// Compare orders two versions by the dpkg rules. A string that is no version sorts before every version, so a stored value that cannot
// be read never hides a real one; two such strings are ordered as text.
func (viewAdapter) Compare(a, b string) int {
	okA, okB := ValidVersion(a), ValidVersion(b)
	switch {
	case !okA && !okB:
		return strings.Compare(a, b)
	case !okA:
		return -1
	case !okB:
		return 1
	}
	return version.Compare(a, b)
}

// SortKey is the stored key of Compare's order (Q-API-429): the dpkg key of the version package, then the string itself.
func (viewAdapter) SortKey(v string) []byte {
	return versionkey.Of(version.SortKey(v), ValidVersion(v), v)
}

// Prerelease is false for every version (api-and-ui.md Q-API-434): the dpkg order has no pre-release, APT installs the highest version
// its pinning allows whatever its spelling, and a "~" only sorts a version below the one it precedes, which marks an upstream pre-release
// (`1.0~rc1-1`) as often as a backport or a stable update (`1.0-1~bpo12+1`, `1.0-1~deb12u1`). What a repository's practice keeps apart
// is a distribution (`experimental`), not a version.
func (viewAdapter) Prerelease(string) bool { return false }

func viewStoreOf(ports formats.DataPorts) (Store, error) { return storeOf(ports.FormatData(ID)) }

// Describe describes the versions from their rows. Fields of the control paragraph that a version lacks are left out.
func (viewAdapter) Describe(ctx context.Context, ports formats.DataPorts, vs []formats.ViewVersion, want formats.ViewFields, _ func(string) bool) (map[formats.VersionID]formats.ViewData, error) {
	st, err := viewStoreOf(ports)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(vs))
	for _, v := range vs {
		ids = append(ids, int64(v.ID))
	}
	got, err := st.Describe(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[formats.VersionID]formats.ViewData, len(got))
	for id, v := range got {
		d := formats.ViewData{Description: v.Description, Licenses: v.Licenses}
		if want.Has(formats.ViewDetails) {
			d.Metadata = metadataOf(v)
		}
		out[formats.VersionID(id)] = d
	}
	return out, nil
}

// metadataOf is the `metadata` object of a version's details: the architectures, the coordinates and the control fields of R-DEB-030,
// and of R-DEB-038 the relations parsed into groups of alternatives (a field that does not follow the grammar is sent as text under
// relationsText), the extended description in its paragraphs, Multi-Arch and Essential.
func metadataOf(v rows.VersionView) map[string]any {
	m := map[string]any{}
	arch := append([]string(nil), v.Architectures...)
	sort.Strings(arch)
	m["architectures"] = arch
	coords := make([]string, 0, len(v.Coordinates))
	for _, c := range v.Coordinates {
		coords = append(coords, c.Suite+"/"+c.Component)
	}
	m["coordinates"] = coords
	for field, key := range map[string]string{
		"Maintainer": "maintainer", "Section": "section", "Priority": "priority", "Installed-Size": "installedSize",
		"Homepage": "homepage", "Source": "source", "Multi-Arch": "multiArch",
	} {
		if val, ok := v.Fields[field]; ok && val != "" {
			m[key] = val
		}
	}
	if strings.EqualFold(strings.TrimSpace(v.Fields["Essential"]), "yes") {
		m["essential"] = true
	}
	if long := control.LongDescription(v.Fields["Description"]); len(long) > 0 {
		m["longDescription"] = long
	}
	relations, text := map[string]any{}, map[string]string{}
	for _, f := range control.RelationFields {
		val := strings.TrimSpace(v.Fields[f.Field])
		if val == "" {
			continue
		}
		if groups, err := control.ParseRelations(val); err == nil {
			relations[f.Key] = groups
		} else {
			text[f.Key] = strings.Join(strings.Fields(val), " ")
		}
	}
	if len(relations) > 0 {
		m["relations"] = relations
	}
	if len(text) > 0 {
		m["relationsText"] = text
	}
	return m
}

// Find searches the control data of the versions of the repositories. Debian versions have no keywords: the keyword of a search is the
// source package, so that the list of a source's binaries is the package list filtered by it (R-DEB-038, Q-DEB-142).
func (viewAdapter) Find(ctx context.Context, ports formats.DataPorts, f formats.ViewFind, limit int) ([]formats.VersionID, error) {
	if f.Text == "" && f.Keyword == "" {
		return nil, nil
	}
	st, err := viewStoreOf(ports)
	if err != nil {
		return nil, err
	}
	var ids []int64
	if f.Keyword != "" {
		ids, err = st.FindSource(ctx, f.Repos, f.Keyword, limit+1)
	} else {
		ids, err = st.Find(ctx, f.Repos, f.Text, limit+1)
	}
	if err != nil {
		return nil, err
	}
	out := make([]formats.VersionID, 0, len(ids))
	for _, id := range ids {
		out = append(out, formats.VersionID(id))
	}
	return out, nil
}

// Withheld lists the versions whose files the replay withheld.
func (viewAdapter) Withheld(ctx context.Context, ports formats.DataPorts, repos []string) ([]formats.VersionID, error) {
	st, err := viewStoreOf(ports)
	if err != nil {
		return nil, err
	}
	ids, err := st.Withheld(ctx, repos)
	if err != nil {
		return nil, err
	}
	out := make([]formats.VersionID, 0, len(ids))
	for _, id := range ids {
		out = append(out, formats.VersionID(id))
	}
	return out, nil
}

// Stored lists the versions the repository serves of a package, newest arrival first. A remote repository's cached files are the
// remote class's.
func (viewAdapter) Stored(ctx context.Context, ports formats.DataPorts, q formats.StoredQuery) (formats.Stored, error) {
	if q.Repo.Class != formats.Local {
		return formats.Stored{}, nil
	}
	st, err := viewStoreOf(ports)
	if err != nil {
		return formats.Stored{}, err
	}
	vs, err := st.Stored(ctx, q.Repo.ID, q.Package, q.Limit)
	if err != nil {
		return formats.Stored{}, err
	}
	return formats.Stored{Versions: vs}, nil
}
