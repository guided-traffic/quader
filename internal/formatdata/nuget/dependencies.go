package nuget

import (
	"context"
	"encoding/json"

	"github.com/guided-traffic/quader/internal/formats/nuget/id"
	"github.com/guided-traffic/quader/internal/formats/nuget/rows"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/packages/dependents"
)

// Dependencies reads the IDs a NuGet version depends on from its stored metadata (R-API-086): the dependencies of every group of
// dependencyGroups, one list whatever the target framework (kind ""), each with its range as the .nuspec writes it. An ID is matched as
// NuGet matches IDs, without case (the lower-case ID, packages.name); an ID no client asks for in that form names no stored package and
// is left out. A dependency in two frameworks with one range is one row; with two ranges, two.
func Dependencies(metadata []byte) []dependents.Dependency {
	var m rows.Metadata
	if json.Unmarshal(metadata, &m) != nil {
		return nil
	}
	var out []dependents.Dependency
	for _, g := range m.DependencyGroups {
		for _, d := range g.Dependencies {
			lower := id.Lower(d.ID)
			if d.ID == "" || !id.ClientForm(lower) {
				continue
			}
			out = append(out, dependents.Dependency{Name: lower, Range: d.Version})
		}
	}
	return out
}

// DependencySource reads the dependencies of NuGet versions from their stored metadata, for the task remote-describe (R-API-086).
func DependencySource(ctx context.Context, qs *q.Queries, versionIDs []int64) (map[int64][]dependents.Dependency, error) {
	rs, err := qs.PackageDependenciesNuGetMetadata(ctx, versionIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[int64][]dependents.Dependency, len(rs))
	for _, r := range rs {
		out[r.VersionID] = Dependencies(r.Metadata)
	}
	return out, nil
}
