package ansible

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/guided-traffic/quader/internal/backup/tables"
	"github.com/guided-traffic/quader/internal/formats/ansible/rows"
	"github.com/guided-traffic/quader/internal/jobs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// roleStore is the group of the data port that keeps the facts of the roles a remote read (R-ANS-056): the statements are
// internal/meta/postgres/queries/ansible_roles.sql, the table is ansible_roles (migration ansible_roles).
type roleStore struct{ *group }

// ansible_roles is derived from the upstream's documents and no source of truth: a backup does not export it, and a restore leaves it
// empty; the next lookup of a role writes it again (R-BAK-004, R-ANS-056). Registered here and nowhere else.
func init() {
	tables.Register("ansible_roles", tables.Transient, tables.Options{})
}

// rolePackagePrefix is the package name of a role without its id (Q-ANS-36), the key of the job of a role's README.
const rolePackagePrefix = "role/"

// PutRoles implements ansible.RoleStore.
func (s roleStore) PutRoles(ctx context.Context, tx meta.Tx, repoID int64, roles []rows.Role) error {
	if len(roles) == 0 {
		return nil
	}
	qs, err := txQueries(tx)
	if err != nil {
		return err
	}
	n := len(roles)
	p := q.AnsibleRolesPutParams{
		RepoID: repoID, Ids: make([]int64, n), Owners: make([]string, n), Names: make([]string, n), Descriptions: make([]string, n),
		GithubUsers: make([]string, n), GithubRepos: make([]string, n), GithubBranches: make([]string, n), Tags: make([]string, n),
		Dependencies: make([]string, n), Downloads: make([]int64, n), Commits: make([]string, n), Imported: make([]string, n),
	}
	for i, r := range roles {
		tags, err := json.Marshal(nonNil(r.Tags))
		if err != nil {
			return err
		}
		deps, err := json.Marshal(nonNil(r.Dependencies))
		if err != nil {
			return err
		}
		p.Ids[i], p.Owners[i], p.Names[i], p.Descriptions[i] = r.ID, r.Owner, r.Name, r.Description
		p.GithubUsers[i], p.GithubRepos[i], p.GithubBranches[i] = r.GitHubUser, r.GitHubRepo, r.GitHubBranch
		p.Tags[i], p.Dependencies[i], p.Downloads[i], p.Commits[i] = string(tags), string(deps), r.Downloads, r.Commit
		if !r.Imported.IsZero() {
			p.Imported[i] = r.Imported.UTC().Format(time.RFC3339Nano)
		}
	}
	return qs.AnsibleRolesPut(ctx, p)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Role implements ansible.RoleStore.
func (s roleStore) Role(ctx context.Context, tx meta.Tx, repoID, id int64) (rows.Role, bool, error) {
	var out rows.Role
	found := false
	err := s.read(ctx, tx, func(qs *q.Queries) error {
		list, err := qs.AnsibleRoleGet(ctx, q.AnsibleRoleGetParams{RepoID: repoID, RoleID: id})
		if err != nil || len(list) == 0 {
			return err
		}
		r := list[0]
		found = true
		out = rows.Role{
			ID: r.RoleID, Owner: r.Owner, Name: r.Name, Description: r.Description, GitHubUser: r.GithubUser, GitHubRepo: r.GithubRepo,
			GitHubBranch: r.GithubBranch, Tags: stringList(r.Tags), Dependencies: stringList(r.Dependencies), Downloads: count(r.Downloads),
			Commit: r.CommitSha, Imported: timeOf(r.ImportedAt), Packaged: r.Packaged,
			Readme: rows.RoleReadme{State: r.ReadmeState, Reason: r.ReadmeReason, File: r.ReadmeFile, Commit: r.ReadmeCommit},
		}
		return nil
	})
	return out, found, err
}

// SetRoleReadme implements ansible.RoleStore.
func (s roleStore) SetRoleReadme(ctx context.Context, tx meta.Tx, repoID, id int64, r rows.RoleReadme) error {
	return s.read(ctx, tx, func(qs *q.Queries) error {
		_, err := qs.AnsibleRoleReadmeSet(ctx, q.AnsibleRoleReadmeSetParams{
			RepoID: repoID, RoleID: id, State: r.State, Reason: r.Reason, File: r.File, Html: r.HTML,
		})
		return err
	})
}

// EnqueueRoleReadme implements ansible.RoleStore: the state queued and the job ansible-docs-fill with the role's package name and no
// version, in tx. The job kind coalesces on its key, so a second enqueue finds the first and queues nothing.
func (s roleStore) EnqueueRoleReadme(ctx context.Context, tx meta.Tx, repoID, id int64) error {
	if s.o.Jobs == nil {
		return errNotBuilt("the documentation fetch queue")
	}
	qs, err := txQueries(tx)
	if err != nil {
		return err
	}
	if _, err := qs.AnsibleRoleQueued(ctx, q.AnsibleRoleQueuedParams{RepoID: repoID, RoleID: id}); err != nil {
		return err
	}
	name := rolePackagePrefix + strconv.FormatInt(id, 10)
	_, err = s.o.Jobs.Enqueue(ctx, tx, jobs.EnqueueSpec{
		Kind: DocsFillJobKind, DedupKey: DocsFillKey(repoID, name, ""), Args: DocsFillArgs{RepoID: repoID, Name: name},
		TargetKind: "repository", TargetKey: strconv.FormatInt(repoID, 10),
	})
	return err
}

// ViewRoles implements ansible.ViewStore: the facts of the roles of the given generic versions.
func (s viewStore) ViewRoles(ctx context.Context, ids []int64, readme bool) ([]rows.RoleView, error) {
	var out []rows.RoleView
	err := s.read(ctx, nil, func(qs *q.Queries) error {
		list, err := qs.AnsibleRoleViewRows(ctx, q.AnsibleRoleViewRowsParams{Ids: ids, Readme: readme})
		if err != nil {
			return err
		}
		for _, r := range list {
			out = append(out, rows.RoleView{VersionID: r.VersionID, Package: r.Package, Role: rows.Role{
				ID: r.RoleID, Owner: r.Owner, Name: r.Name, Description: r.Description, GitHubUser: r.GithubUser, GitHubRepo: r.GithubRepo,
				GitHubBranch: r.GithubBranch, Tags: stringList(r.Tags), Dependencies: stringList(r.Dependencies), Downloads: count(r.Downloads),
				Imported: timeOf(r.ImportedAt),
				Readme:   rows.RoleReadme{State: r.ReadmeState, Reason: r.ReadmeReason, File: r.ReadmeFile, HTML: r.ReadmeHtml},
			}})
		}
		return nil
	})
	return out, err
}

func stringList(raw json.RawMessage) []string {
	var out []string
	_ = json.Unmarshal(raw, &out) //nolint:errcheck // a column this package wrote; an unreadable one shows nothing
	return out
}

func count(v pgtype.Int8) int64 {
	if !v.Valid {
		return -1
	}
	return v.Int64
}

func timeOf(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time.UTC()
}
