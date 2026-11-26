package deb

import (
	"context"

	"github.com/guided-traffic/quader/internal/formats/deb/control"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/packages/dependents"
)

// usesFields are the relation fields of a binary package by which it uses another (R-API-086, Q-API-457), each a kind named by its field:
// Pre-Depends, Depends, Recommends, Suggests and Enhances. Breaks, Conflicts and Replaces name packages it keeps away or takes the files
// of, which is no use. Provides names what the package stands in for: those names are kept as provided, so that a package that depends
// on one of them is listed as a user of the provider as well.
var usesFields = [...]string{"Pre-Depends", "Depends", "Recommends", "Suggests", "Enhances"}

// Dependencies reads the names a Debian version uses and provides from the control paragraphs of its package files, one per
// architecture (R-API-086): a version depends on what any of its files depends on. Every alternative of a group is a row ("a | b" uses
// a and b), named as Debian matches names, without its architecture qualifier ("python3:any" is python3), with its version constraint
// as the range ("(>= 2.0)" is ">= 2.0"). A field that does not parse names nothing; the screen shows it as text.
func Dependencies(controls []string) []dependents.Dependency {
	var out []dependents.Dependency
	for _, text := range controls {
		p, err := control.Parse([]byte(text))
		if err != nil {
			continue
		}
		for _, field := range usesFields {
			out = append(out, relationRows(p.Value(field), field, false)...)
		}
		out = append(out, relationRows(p.Value("Provides"), "Provides", true)...)
	}
	return out
}

func relationRows(value, kind string, provides bool) []dependents.Dependency {
	groups, err := control.ParseRelations(value)
	if err != nil {
		return nil
	}
	var out []dependents.Dependency
	for _, g := range groups {
		for _, r := range g {
			rng := ""
			if r.Op != "" && r.Version != "" {
				rng = r.Op + " " + r.Version
			}
			out = append(out, dependents.Dependency{Name: r.Name, Kind: kind, Range: rng, Provides: provides})
		}
	}
	return out
}

// DependencySource reads the dependencies of Debian versions from the stored control paragraphs of their files, for the task
// remote-describe (R-API-086).
func DependencySource(ctx context.Context, qs *q.Queries, versionIDs []int64) (map[int64][]dependents.Dependency, error) {
	rs, err := qs.PackageDependenciesDebControls(ctx, versionIDs)
	if err != nil {
		return nil, err
	}
	controls := map[int64][]string{}
	for _, r := range rs {
		controls[r.VersionID] = append(controls[r.VersionID], r.Control)
	}
	out := make(map[int64][]dependents.Dependency, len(controls))
	for id, c := range controls {
		out[id] = Dependencies(c)
	}
	return out, nil
}

// putDependencies writes the rows of the version a package file belongs to, from the control paragraphs of every file of that version,
// in the transaction of qs that wrote the file's row (R-API-086). A file no version links yet writes nothing: the task remote-describe
// reads its version once it is linked.
func putDependencies(ctx context.Context, qs *q.Queries, itemID int64) error {
	versionID, found, err := dependents.VersionOfItem(ctx, qs, itemID)
	if err != nil || !found {
		return err
	}
	deps, err := DependencySource(ctx, qs, []int64{versionID})
	if err != nil {
		return err
	}
	return dependents.Put(ctx, qs, versionID, deps[versionID])
}
