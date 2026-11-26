// Package dependents keeps who depends on a package (api-and-ui.md R-API-086, Q-API-455): one row of the table package_dependencies
// (migration 00139) per name a stored version depends on, in the kind its format names it, with the range it asks for, and the names a
// version provides (Debian's Provides). A format's extractor reads them from the format's own stored rows where those are written (a
// local publish's commit, a remote's fill or describe) and hands them to [Put]; the task remote-describe's entry [Fill] reads what was
// stored before, from the same rows, without reading an archive. The reads are the package view's (internal/packages/view).
package dependents

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// Reader is the version of the extractors: a version whose dependencies an older one read is read again by the task remote-describe.
// Raise it when an extractor reads more or other names.
const Reader = 1

// formats returns the formats whose versions name dependencies the table holds: npm, NuGet, Debian and Ansible Galaxy. An OCI image names
// none (its base image is no dependency the format states), a Helm chart's dependencies name a chart and a repository address, which
// no stored package matches by its name alone (Q-API-455, H-2221), and generic files and ClamAV definitions have none.
func formats() []string { return []string{"ansible", "deb", "npm", "nuget"} }

// Formats returns the formats whose versions name dependencies, sorted.
func Formats() []string { return formats() }

// Has reports whether a format's versions name dependencies.
func Has(format string) bool { return slices.Contains(formats(), format) }

// The bounds of a row (the table's checks) and of a version: a name or a kind beyond them is no name of a package or a kind of a format,
// and is not kept; a range is cut at a character boundary.
const (
	maxName    = 256
	maxKind    = 64
	maxRange   = 256
	maxPerItem = 2000
)

// Dependency is one row: a name the version depends on (or, with Provides, a name it provides) as the format matches names, the kind
// the format names the relation by ("" for a format with one list), and the range or constraint as written ("" for none).
type Dependency struct {
	Name     string
	Kind     string
	Range    string
	Provides bool
}

// clean drops the rows that are no rows of the table (an empty or overlong name or kind), cuts an overlong range, and keeps each row
// once, at most maxPerItem of them.
func clean(in []Dependency) []Dependency {
	type key struct {
		name, kind, rng string
		provides        bool
	}
	seen := map[key]bool{}
	out := make([]Dependency, 0, len(in))
	for _, d := range in {
		d.Name, d.Kind, d.Range = strings.TrimSpace(d.Name), strings.TrimSpace(d.Kind), cut(strings.TrimSpace(d.Range), maxRange)
		if d.Name == "" || utf8.RuneCountInString(d.Name) > maxName || utf8.RuneCountInString(d.Kind) > maxKind {
			continue
		}
		k := key{d.Name, d.Kind, d.Range, d.Provides}
		if seen[k] {
			continue
		}
		seen[k] = true
		if out = append(out, d); len(out) == maxPerItem {
			break
		}
	}
	return out
}

// cut shortens s to at most n characters.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

// Queries returns the statements of the transaction tx.
func Queries(tx meta.Tx) (*q.Queries, error) {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return nil, errs.E(errs.Internal, errs.CodeInternal, "dependents: the transaction is not a PostgreSQL transaction")
	}
	return q.New(ptx), nil
}

// Put replaces the rows of a version with deps and marks the version read by the current extractor, in the transaction of qs. A version
// that no longer exists writes nothing.
func Put(ctx context.Context, qs *q.Queries, versionID int64, deps []Dependency) error {
	if err := qs.PackageDependenciesClear(ctx, versionID); err != nil {
		return err
	}
	deps = clean(deps)
	p := q.PackageDependenciesPutParams{
		VersionID: versionID, Reader: Reader,
		Names: make([]string, len(deps)), Kinds: make([]string, len(deps)), Ranges: make([]string, len(deps)), Provides: make([]bool, len(deps)),
	}
	for i, d := range deps {
		p.Names[i], p.Kinds[i], p.Ranges[i], p.Provides[i] = d.Name, d.Kind, d.Range, d.Provides
	}
	return qs.PackageDependenciesPut(ctx, p)
}

// VersionOfItem returns the version a file belongs to; found is false for a file no version links (yet): the task remote-describe reads
// its version once it is linked.
func VersionOfItem(ctx context.Context, qs *q.Queries, itemID int64) (versionID int64, found bool, err error) {
	ids, err := qs.PackageDependenciesVersionOfItem(ctx, itemID)
	if err != nil || len(ids) == 0 {
		return 0, false, err
	}
	return ids[0], true, nil
}
