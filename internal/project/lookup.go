package project

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The principal lookup of a project (Q-AUTHZ-182, R-API-077): how an Owner of a project, who does not hold identities:read,
// finds the users, groups and service accounts it names as members and in the project's policies. The bounds are the
// service's own, so that no caller can widen them: a prefix of at least MinLookupPrefix characters, at most MaxLookup results.
// One statement per kind asked, each ranked and cut at the limit and one more; the kinds are then ranked and cut together.

// Lookup is one lookup.
type Lookup struct {
	// Prefix is the text as the caller typed it; the lookup trims and lower-cases it.
	Prefix string
	// Kinds are the kinds asked; none asks for every kind.
	Kinds []PrincipalKind
	// Project is the project whose own service accounts are found.
	Project string
	// InstanceAccounts adds the instance-wide service accounts: the caller holds identities:read, which lists them anyway.
	InstanceAccounts bool
	// Limit is 1 to MaxLookup; 0 takes MaxLookup.
	Limit int
}

// Found is one principal of a lookup: what a member's row shows and the reference to write.
type Found struct {
	Kind PrincipalKind
	// Name is the username, the group's name or the service account's stored name (<project>/<name> for a project's).
	Name string
	// ID, DisplayName and AvatarSHA256 are a user's: the seed of its generated avatar, its display name and the digest of its
	// uploaded avatar; empty for the other kinds and for none.
	ID, DisplayName, AvatarSHA256 string
	// Project is a service account's project; empty for an instance-wide one.
	Project string
	// rank orders the matches: 0 the name equals the prefix, 1 it starts with it, 2 a word does.
	rank int32
}

// Ref is the reference of the principal, as a member or a policy entry names it.
func (f Found) Ref() string {
	if f.Kind == KindServiceAccount {
		return "sa:" + f.Name
	}
	return string(f.Kind) + ":" + f.Name
}

// kindOrder places the kinds of one rank: users, groups, service accounts.
func kindOrder(k PrincipalKind) int {
	switch k {
	case KindUser:
		return 0
	case KindGroup:
		return 1
	}
	return 2
}

// checkLookup refuses a prefix that is too short, a kind the lookup does not know and a negative limit (422).
func checkLookup(query Lookup) error {
	var fields []errs.FieldError
	if utf8.RuneCountInString(strings.TrimSpace(query.Prefix)) < MinLookupPrefix {
		fields = append(fields, errs.FieldError{
			Pointer: "/q", Code: "prefix_too_short",
			Detail: fmt.Sprintf("The prefix has at least %d characters.", MinLookupPrefix),
		})
	}
	for _, k := range query.Kinds {
		if k != KindUser && k != KindGroup && k != KindServiceAccount {
			fields = append(fields, errs.FieldError{Pointer: "/kind", Code: "invalid_kind", Detail: "The kind is user, group or service_account."})
			break
		}
	}
	if query.Limit < 0 {
		fields = append(fields, errs.FieldError{Pointer: "/limit", Code: "invalid_limit", Detail: fmt.Sprintf("The limit is 1 to %d.", MaxLookup)})
	}
	if len(fields) > 0 {
		return errs.E(errs.Invalid, errs.CodeValidation, fields[0].Detail, errs.WithFields(fields...))
	}
	return nil
}

// LookupPrincipals answers the lookup of Q-AUTHZ-182.
func (s *service) LookupPrincipals(ctx context.Context, query Lookup) ([]Found, error) {
	if err := checkLookup(query); err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit == 0 || limit > MaxLookup {
		limit = MaxLookup
	}
	text := strings.ToLower(strings.TrimSpace(query.Prefix))
	var out []Found
	err := s.inTx(ctx, func(_ meta.Tx, qs *q.Queries) error {
		var err error
		out, err = lookupKinds(ctx, qs, query, text, int32(limit+1)) //nolint:gosec // at most MaxLookup + 1
		return err
	})
	if err != nil {
		return nil, err
	}
	return rankFound(out, limit), nil
}

// lookupKinds runs the statement of every kind asked, each cut at rows.
func lookupKinds(ctx context.Context, qs *q.Queries, query Lookup, text string, rows int32) ([]Found, error) {
	wants := func(k PrincipalKind) bool { return len(query.Kinds) == 0 || slices.Contains(query.Kinds, k) }
	var out []Found
	if wants(KindUser) {
		users, err := qs.ProjectLookupUsers(ctx, q.ProjectLookupUsersParams{Text: text, MaxRows: rows})
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			out = append(out, Found{Kind: KindUser, Name: u.Name, ID: u.ID, DisplayName: u.DisplayName, AvatarSHA256: u.AvatarSha256, rank: u.Rank})
		}
	}
	if wants(KindGroup) {
		groups, err := qs.ProjectLookupGroups(ctx, q.ProjectLookupGroupsParams{Text: text, MaxRows: rows})
		if err != nil {
			return nil, err
		}
		for _, g := range groups {
			out = append(out, Found{Kind: KindGroup, Name: g.Name, rank: g.Rank})
		}
	}
	if wants(KindServiceAccount) {
		accounts, err := qs.ProjectLookupServiceAccounts(ctx, q.ProjectLookupServiceAccountsParams{
			Text: text, Instance: query.InstanceAccounts, ProjectKey: query.Project, MaxRows: rows,
		})
		if err != nil {
			return nil, err
		}
		for _, a := range accounts {
			out = append(out, Found{Kind: KindServiceAccount, Name: a.Name, Project: a.ProjectKey, rank: a.Rank})
		}
	}
	return out, nil
}

// rankFound orders the matches of every kind together, best first (rank, then users before groups before service accounts,
// then by name), and cuts them at the limit.
func rankFound(found []Found, limit int) []Found {
	slices.SortStableFunc(found, func(a, b Found) int {
		return cmp.Or(cmp.Compare(a.rank, b.rank), cmp.Compare(kindOrder(a.Kind), kindOrder(b.Kind)), strings.Compare(a.Name, b.Name))
	})
	return found[:min(limit, len(found))]
}
