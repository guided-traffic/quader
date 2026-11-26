package repository

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// What decides a publish into a namespace besides the grants (api-and-ui.md R-UI-220, Q-UI-1292): the claims of a local repository
// that cover names of the namespace (R-REPO-023), its package ownership (R-AUTHZ-018) and whether its first publish of a name claims
// it. The page of a name reads them for the repositories whose configuration its reader may read.

// How a claim covers the names of a namespace.
const (
	// CoversAll: every name of the namespace (acme.* of acme, @acme/* of @acme, team/** of team).
	CoversAll = "all"
	// CoversSome: some names of it (an exact name inside it, a narrower prefix, team/* of team, whose names below team/x/ it misses).
	CoversSome = "some"
)

// Within says how the claim pattern covers the names that start with prefix, the namespace's prefix with its separator
// (catalog.NamespaceRule.Prefix): [CoversAll], [CoversSome], or "" when it covers none of them. deep says that a name of the
// namespace may hold a "/" after the prefix (a nested namespace, an image path), which a trailing "*" does not cross (R-AUTHZ-005).
func (p ClaimPattern) Within(prefix string, deep bool) string {
	switch {
	case strings.HasPrefix(prefix, p.literal) && p.kind != claimExact:
		rest := prefix[len(p.literal):]
		switch {
		case p.kind == claimGlobstar:
			return CoversAll
		case strings.Contains(rest, "/"):
			return "" // a "*" before the prefix's slash stops there
		case deep:
			return CoversSome
		}
		return CoversAll
	case strings.HasPrefix(p.literal, prefix) && p.literal != prefix:
		return CoversSome
	}
	return ""
}

// NamespaceClaim is a claim of a repository that covers names of a namespace.
type NamespaceClaim struct {
	Pattern string
	Scope   string
	Source  string
	// Covers is [CoversAll] or [CoversSome].
	Covers string
}

// ClaimsWithin reads the claims of the repositories (by numeric id) and keeps those that cover names starting with prefix, by
// repository, in pattern order (R-UI-220). One statement whatever the number of repositories.
func (s *Service) ClaimsWithin(ctx context.Context, repoIDs []int64, prefix string, deep bool) (map[int64][]NamespaceClaim, error) {
	out := map[int64][]NamespaceClaim{}
	if len(repoIDs) == 0 {
		return out, nil
	}
	err := s.run.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		rows, err := qs.ListClaimsOfRepositories(ctx, repoIDs)
		if err != nil {
			return err
		}
		for _, r := range rows {
			out[r.RepoID] = appendWithin(out[r.RepoID], r, prefix, deep)
		}
		return nil
	})
	return out, err
}

func appendWithin(list []NamespaceClaim, r q.ListClaimsOfRepositoriesRow, prefix string, deep bool) []NamespaceClaim {
	pat, err := ParseClaimPattern(r.Pattern)
	if err != nil {
		return list // a stored claim that does not parse protects nothing (buildClaimIndex)
	}
	if c := pat.Within(prefix, deep); c != "" {
		list = append(list, NamespaceClaim{Pattern: r.Pattern, Scope: r.Scope, Source: r.Source, Covers: c})
	}
	return list
}

// AutoClaim reports whether a first publish of a name no claim of the repository covers claims it (local.autoClaim with the format's
// default, R-REPO-023).
func (r Repository) AutoClaim() bool { return r.autoClaim }

// PackageOwnership is local.packageOwnership as the stored document holds it, "enforce" or "off" (R-AUTHZ-018: the document of a local
// repository of a format with package names always holds it); "" for another repository.
func (r Repository) PackageOwnership() string {
	var d struct {
		Local *struct {
			PackageOwnership string `json:"packageOwnership"`
		} `json:"local"`
	}
	if r.Class != Local || json.Unmarshal(r.Config, &d) != nil || d.Local == nil {
		return ""
	}
	return d.Local.PackageOwnership
}
