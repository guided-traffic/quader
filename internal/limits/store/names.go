package store

import (
	"context"
	"strings"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The names the pages of the limits and the usage show for the references the store keeps (R-LIM-032, R-LIM-034): a user's
// name and display name, a technical token's name and account, a CI project's path, a personal token's hint and description.
// The store keeps IDs only, so that a rename never rewrites a rule or a usage row; the names are read when an answer is built.

// UserName is a user's name and display name.
type UserName struct {
	Name, DisplayName string
}

// TokenName is a technical token's name and its account's stored name (the reference without `sa:`).
type TokenName struct {
	ID, Name, Account string
}

// Level is the account's level, read from its stored name: <name>, <project>/<name>, <project>/<repository>/<name>.
func (t TokenName) Level() string {
	switch strings.Count(t.Account, "/") {
	case 0:
		return "instance"
	case 1:
		return "project"
	}
	return "repository"
}

// CIProject is a CI project tokens were issued for.
type CIProject struct {
	IssuerID, Issuer, ProjectID, ProjectPath string
}

// TokenHint is a personal token's hint and description.
type TokenHint struct {
	Hint, Description string
}

// Names holds the names of one answer, by reference.
type Names struct {
	Users     map[string]UserName
	Tokens    map[string]TokenName
	CI        map[string]CIProject // by <issuer ID>:<project ID>
	APITokens map[string]TokenHint
}

// NameRefs are the references an answer names.
type NameRefs struct {
	Users, Tokens, CI, APITokens []string
}

func uuids(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		if isUUID(s) && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Names reads the names of the references in one transaction; a reference that names nothing (a deleted user or token) is
// missing from the maps.
func (s *Store) Names(ctx context.Context, refs NameRefs) (Names, error) {
	out := Names{Users: map[string]UserName{}, Tokens: map[string]TokenName{}, CI: map[string]CIProject{}, APITokens: map[string]TokenHint{}}
	users, tokens, apiTokens := uuids(refs.Users), uuids(refs.Tokens), uuids(refs.APITokens)
	if len(users)+len(tokens)+len(refs.CI)+len(apiTokens) == 0 {
		return out, nil
	}
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		if len(users) > 0 {
			rows, err := qs.LimitUserNames(ctx, users)
			if err != nil {
				return err
			}
			for _, r := range rows {
				out.Users[r.ID] = UserName{Name: r.Name, DisplayName: r.DisplayName}
			}
		}
		if len(tokens) > 0 {
			rows, err := qs.LimitTechnicalTokenNames(ctx, tokens)
			if err != nil {
				return err
			}
			for _, r := range rows {
				out.Tokens[r.ID] = TokenName{ID: r.ID, Name: r.Name, Account: r.Account}
			}
		}
		if len(refs.CI) > 0 {
			rows, err := qs.LimitCIProjects(ctx, q.LimitCIProjectsParams{Refs: refs.CI, RowLimit: int32(min(len(refs.CI), 10_000))}) //nolint:gosec // at most 10,000
			if err != nil {
				return err
			}
			for _, r := range rows {
				out.CI[r.IssuerID+":"+r.ProjectID] = CIProject{IssuerID: r.IssuerID, Issuer: r.Issuer, ProjectID: r.ProjectID, ProjectPath: r.ProjectPath}
			}
		}
		if len(apiTokens) > 0 {
			rows, err := qs.LimitTokenHints(ctx, apiTokens)
			if err != nil {
				return err
			}
			for _, r := range rows {
				out.APITokens[r.ID] = TokenHint{Hint: r.Hint, Description: r.Description}
			}
		}
		return nil
	})
	return out, err
}

// SearchTechnicalTokens finds technical tokens by the token's or the account's name (R-LIM-034); project narrows to the
// accounts of the project and of its repositories.
func (s *Store) SearchTechnicalTokens(ctx context.Context, text, project string, limit int) ([]TokenName, error) {
	var out []TokenName
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := qs.LimitSearchTechnicalTokens(ctx, q.LimitSearchTechnicalTokensParams{Q: text, Project: project, RowLimit: int32(min(limit, 200))}) //nolint:gosec // at most 200
		for _, r := range rows {
			out = append(out, TokenName{ID: r.ID, Name: r.Name, Account: r.Account})
		}
		return err
	})
	return out, err
}

// SearchCIProjects finds the CI projects tokens were issued for, by a text in the project path or the project ID (R-LIM-034).
func (s *Store) SearchCIProjects(ctx context.Context, text string, limit int) ([]CIProject, error) {
	var out []CIProject
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		rows, err := qs.LimitCIProjects(ctx, q.LimitCIProjectsParams{Refs: []string{}, Q: text, RowLimit: int32(min(limit, 200))}) //nolint:gosec // at most 200
		for _, r := range rows {
			out = append(out, CIProject{IssuerID: r.IssuerID, Issuer: r.Issuer, ProjectID: r.ProjectID, ProjectPath: r.ProjectPath})
		}
		return err
	})
	return out, err
}
