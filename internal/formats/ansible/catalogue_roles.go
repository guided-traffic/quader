package ansible

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/mirror/discover"
)

// A role in the tab Upstream (R-ANS-056): the package "role/<id>" the refusal of a rule names (its "Ask for it at …upstream?name=role/16")
// is looked up there as a collection is, with the versions of the role's version list, so a person sees which of them the rules
// deliver and allows or blocks the role by the name the rules see. The keyword search and the namespaces stay the collections'.

// maxCatalogueRolePages bounds the pages of a role's version list the catalogue reads (50 versions a page).
const maxCatalogueRolePages = 20

// roleVersionEntry is one entry of a role's version list as the catalogue reads it.
type roleVersionEntry struct {
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
	CommitDate  string `json:"commit_date"`
	ReleaseDate string `json:"release_date"`
	Created     string `json:"created"`
}

// catalogueRoleVersions reads the version list of a role page by page through u; found is false when the upstream has no such role.
func catalogueRoleVersions(ctx context.Context, u discover.Upstream, id string) (list []roleVersionEntry, found bool, err error) {
	for page := 1; page <= maxCatalogueRolePages; page++ {
		doc, ok, err := u.Document(ctx, RoleVersionsDocumentPath(id), pageVariant(page))
		if err != nil || !ok {
			return list, found, err
		}
		found = true
		var p struct {
			Results  []roleVersionEntry `json:"results"`
			NextLink *string            `json:"next_link"`
			Next     *string            `json:"next"`
		}
		if json.Unmarshal(doc, &p) != nil {
			return nil, false, invalidDocument("a role's version list", "it is not a JSON object")
		}
		for _, e := range p.Results {
			if roleRefOK(e.Name) && e.DownloadURL != "" {
				list = append(list, e)
			}
		}
		if (p.NextLink == nil || *p.NextLink == "") && (p.Next == nil || *p.Next == "") {
			break
		}
	}
	return list, found, nil
}

// rolePackageInfo is the catalogue's answer for a role: its versions, newest first by the date the upstream names, then by name.
func rolePackageInfo(ctx context.Context, u discover.Upstream, id string, rq discover.VersionsRequest) (discover.PackageInfo, error) {
	entries, found, err := catalogueRoleVersions(ctx, u, id)
	if err != nil || !found {
		return discover.PackageInfo{}, err
	}
	all := make([]discover.VersionFacts, 0, len(entries))
	for _, e := range entries {
		at := parseTime(e.CommitDate)
		if at.IsZero() {
			at = parseTime(e.ReleaseDate)
		}
		if at.IsZero() {
			at = parseTime(e.Created)
		}
		all = append(all, discover.VersionFacts{Version: e.Name, Published: at})
	}
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].Published.Equal(all[j].Published) {
			return all[i].Published.After(all[j].Published)
		}
		return all[i].Version > all[j].Version
	})
	info := discover.PackageInfo{Found: true, Name: RolePackage(id)}
	info.Versions, info.Total, info.Cursor, info.Done = discover.PageVersions(all, rq)
	return info, nil
}

// roleVersionProbe names the archive of a role version for a download from the tab Upstream: the version list records its address.
func roleVersionProbe(ctx context.Context, u discover.Upstream, id, ref string) (formats.Probe, bool, error) {
	entries, found, err := catalogueRoleVersions(ctx, u, id)
	if err != nil || !found {
		return formats.Probe{}, false, err
	}
	for _, e := range entries {
		if e.Name == ref {
			return formats.Probe{Name: RolePackage(id), Version: ref, Files: []formats.Path{{Path: RoleArchivePath(id, ref)}}}, true, nil
		}
	}
	return formats.Probe{}, false, nil
}

// roleIDOf is the id of a role's package name, "" for another name.
func roleIDOf(name string) string {
	id, ok := strings.CutPrefix(strings.TrimSpace(strings.ToLower(name)), RolePrefix)
	if !ok || !digits(id, maxRoleIDDigits) {
		return ""
	}
	return id
}
