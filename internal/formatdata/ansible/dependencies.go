package ansible

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/guided-traffic/quader/internal/formats/ansible/collection"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/packages/dependents"
)

// Dependencies reads the collections an Ansible Galaxy collection version depends on (R-API-086): the map of its manifest's
// dependencies, one list (kind ""), each with its range as written. A name is matched as Galaxy matches it, "<namespace>.<name>" in lower
// case; a key that is no collection name names nothing.
func Dependencies(deps map[string]string) []dependents.Dependency {
	var out []dependents.Dependency
	for name, rng := range deps {
		l := strings.ToLower(strings.TrimSpace(name))
		if _, _, ok := collection.SplitName(l); !ok {
			continue
		}
		out = append(out, dependents.Dependency{Name: l, Range: rng})
	}
	return out
}

// dependenciesOf reads the stored dependencies column (a JSON object of name and range); anything else names nothing.
func dependenciesOf(raw []byte) []dependents.Dependency {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	deps := make(map[string]string, len(m))
	for k, v := range m {
		var s string
		if json.Unmarshal(v, &s) == nil {
			deps[k] = s
		}
	}
	return Dependencies(deps)
}

// DependencySource reads the dependencies of Ansible Galaxy versions from their stored rows, for the task remote-describe (R-API-086).
func DependencySource(ctx context.Context, qs *q.Queries, versionIDs []int64) (map[int64][]dependents.Dependency, error) {
	rs, err := qs.PackageDependenciesAnsible(ctx, versionIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[int64][]dependents.Dependency, len(rs))
	for _, r := range rs {
		out[r.VersionID] = dependenciesOf(r.Dependencies)
	}
	return out, nil
}

// putDependencies writes the rows of the version an archive belongs to, in the transaction of qs that wrote the archive's row
// (R-API-086). An archive no version links yet writes nothing: the task remote-describe reads its version once it is linked.
func putDependencies(ctx context.Context, qs *q.Queries, itemID int64, deps []dependents.Dependency) error {
	versionID, found, err := dependents.VersionOfItem(ctx, qs, itemID)
	if err != nil || !found {
		return err
	}
	return dependents.Put(ctx, qs, versionID, deps)
}
