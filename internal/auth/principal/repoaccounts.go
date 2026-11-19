package principal

import (
	"context"
	"slices"
	"strings"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/qname"
)

// The service accounts of a repository (R-AUTH-046, R-AUTHZ-039, Q-AUTHZ-296, Q-AUTHZ-297). Such an account belongs to one
// repository: its principal row stores the name <project>/<repository>/<name>, its project is the repository's and follows a
// move of the repository, and it is deleted with the repository's row. Its rights are a set of repository actions on its own
// repository, stored at the account; it is no policy principal, group member, project member or role holder. Who may act on
// it (an Owner of the repository's project, or a holder of identities:write) is the API layer's decision, as for the accounts
// of a project.

// RepositoryAccount addresses a repository's service account: the repository and the account's name within it.
type RepositoryAccount struct {
	Repo qname.Repo
	Name string
}

// String is the stored name, <project>/<repository>/<name>.
func (a RepositoryAccount) String() string { return a.Repo.String() + "/" + a.Name }

// ParseRepositoryAccount reads the reference of a repository's account without its "sa:": <project>/<repository>/<name>. ok
// is false for any other text, the name of an account of the instance or a project included.
func ParseRepositoryAccount(ref string) (RepositoryAccount, bool) {
	parts := strings.Split(ref, "/")
	if len(parts) != 3 {
		return RepositoryAccount{}, false
	}
	repo := qname.Repo{Project: parts[0], Key: parts[1]}
	if !repo.Valid() || !ValidServiceAccountName(parts[2]) {
		return RepositoryAccount{}, false
	}
	return RepositoryAccount{Repo: repo, Name: parts[2]}, true
}

// RepositoryActions are the repository actions of R-AUTHZ-006 in their canonical order: the actions a repository's account may
// hold (R-AUTHZ-039).
func RepositoryActions() []string {
	return []string{"read", "annotate", "deploy", "overwrite", "delete", "manage"}
}

// NormalizeActions checks a set of repository actions and returns it in the canonical order without duplicates: at least one,
// each one of [RepositoryActions] (422 validation_failed at /actions otherwise). nil asks for the default, read.
func NormalizeActions(in []string) ([]string, error) {
	if in == nil {
		return []string{"read"}, nil
	}
	all := RepositoryActions()
	out := make([]string, 0, len(all))
	for _, a := range in {
		if !slices.Contains(all, a) {
			return nil, actionsInvalid("an action is one of read, annotate, deploy, overwrite, delete and manage, not " + a)
		}
	}
	for _, a := range all {
		if slices.Contains(in, a) {
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		return nil, actionsInvalid("a service account of a repository holds at least one action on it")
	}
	return out, nil
}

func actionsInvalid(detail string) error {
	return errs.E(errs.Unprocessable, errs.CodeValidation, "1 field is invalid.",
		errs.WithFields(errs.FieldError{Pointer: "/actions", Code: "invalid_value", Detail: detail}))
}

// CreateRepositoryServiceAccountTx creates the account in.Name of the repository with the actions (R-AUTH-046) in the caller's
// transaction: unique within the repository (409 already_exists), no role (in.Admin: 422 role_not_allowed), the repository
// active (404 otherwise). actions nil is read. The authorization area learns the new grant in the same transaction.
func (m *Manager) CreateRepositoryServiceAccountTx(ctx context.Context, tx meta.Tx, c Caller, repo qname.Repo, in NewServiceAccount, actions []string) (ServiceAccount, error) {
	if !ValidServiceAccountName(in.Name) {
		return ServiceAccount{}, errs.E(errs.Invalid, errs.CodeValidation, "The service-account name does not match ^[a-z0-9][a-z0-9-]{1,62}$.")
	}
	if in.Admin {
		return ServiceAccount{}, errs.E(errs.Unprocessable, errs.CodeRoleNotAllowed, "A service account of a repository holds no system role.")
	}
	acts, err := NormalizeActions(actions)
	if err != nil {
		return ServiceAccount{}, err
	}
	if !repo.Valid() {
		return ServiceAccount{}, NotFound("The repository")
	}
	sa, err := m.createRepositoryAccount(ctx, tx, c, repo, in, acts)
	return sa, DBErr("principal.CreateRepositoryServiceAccount", err)
}

func (m *Manager) createRepositoryAccount(ctx context.Context, tx meta.Tx, c Caller, repo qname.Repo, in NewServiceAccount, acts []string) (ServiceAccount, error) {
	qs := Queries(tx)
	rows, err := qs.IdentityRepositoryShare(ctx, q.IdentityRepositoryShareParams{Project: repo.Project, Key: repo.Key})
	if err != nil {
		return ServiceAccount{}, err
	}
	if len(rows) == 0 {
		return ServiceAccount{}, NotFound("The repository")
	}
	id := NewID()
	stored := RepositoryAccount{Repo: repo, Name: in.Name}.String()
	inserted, err := qs.IdentityInsertPrincipal(ctx, q.IdentityInsertPrincipalParams{ID: id, Kind: string(KindServiceAccount), Name: stored, CreatedBy: c.Actor.ID})
	if err != nil {
		return ServiceAccount{}, err
	}
	if len(inserted) == 0 {
		return ServiceAccount{}, errs.E(errs.Conflict, errs.CodeAlreadyExists, "The repository has a service account with this name.")
	}
	if err := qs.IdentityInsertRepositoryServiceAccount(ctx, q.IdentityInsertRepositoryServiceAccountParams{
		ID: id, Description: in.Description, ProjectID: rows[0].ProjectID, RepositoryID: rows[0].ID,
	}); err != nil {
		return ServiceAccount{}, err
	}
	if err := qs.IdentitySetRepositoryGrant(ctx, q.IdentitySetRepositoryGrantParams{ID: id, Actions: acts}); err != nil {
		return ServiceAccount{}, err
	}
	if err := notifyAuthz(ctx, m.authz, tx, id); err != nil {
		return ServiceAccount{}, err
	}
	sa := ServiceAccount{ID: id, Name: stored, Project: repo.Project, Repository: repo.Key, RepositoryID: rows[0].ID, Actions: acts}
	if err := m.record(ctx, tx, audit.Event{
		Type: audit.EventSACreated, Outcome: audit.Success, Actor: c.Actor, Source: c.Source,
		Subject: audit.Subject{Kind: "service_account", ID: id, Name: stored},
		Details: details(levelDetails(map[string]any{"source": sourceOf(c), "admin": false, "actions": acts}, sa)),
	}); err != nil {
		return ServiceAccount{}, err
	}
	return loadServiceAccount(ctx, qs, stored)
}

// repositoryAccountOf reads the account a of its repository; 404 for none, for an account of another repository or place, and
// for a repository that is not active (being deleted, R-REPO-013).
func repositoryAccountOf(ctx context.Context, qs *q.Queries, a RepositoryAccount) (ServiceAccount, error) {
	if !a.Repo.Valid() || !ValidServiceAccountName(a.Name) {
		return ServiceAccount{}, NotFound("The service account")
	}
	active, err := qs.IdentityActiveRepository(ctx, q.IdentityActiveRepositoryParams{Project: a.Repo.Project, Key: a.Repo.Key})
	if err != nil {
		return ServiceAccount{}, err
	}
	if len(active) == 0 {
		return ServiceAccount{}, NotFound("The repository")
	}
	sa, err := loadServiceAccount(ctx, qs, a.String())
	if err == ErrNotFound || err == nil && (sa.RepositoryID != active[0] || sa.Project != a.Repo.Project) { //nolint:errorlint // sentinel returned unwrapped
		return ServiceAccount{}, NotFound("The service account")
	}
	return sa, err
}

// GetRepositoryServiceAccount reads the account of its repository; 404 as [repositoryAccountOf].
func (m *Manager) GetRepositoryServiceAccount(ctx context.Context, a RepositoryAccount) (ServiceAccount, error) {
	var sa ServiceAccount
	err := m.db.Read(ctx, func(db q.DBTX) error {
		var err error
		sa, err = repositoryAccountOf(ctx, q.New(db), a)
		return err
	})
	return sa, DBErr("principal.GetRepositoryServiceAccount", err)
}

// ListRepositoryServiceAccounts lists the accounts of an active repository ordered by name (404 for a repository that is not);
// after is the stored name of the last account of the previous page, query a case-insensitive substring of the name within
// the repository.
func (m *Manager) ListRepositoryServiceAccounts(ctx context.Context, repo qname.Repo, query, after string, limit int) ([]ServiceAccount, error) {
	var out []ServiceAccount
	err := m.db.Read(ctx, func(db q.DBTX) error {
		qs := q.New(db)
		active, err := qs.IdentityActiveRepository(ctx, q.IdentityActiveRepositoryParams{Project: repo.Project, Key: repo.Key})
		if err != nil {
			return err
		}
		if len(active) == 0 {
			return NotFound("The repository")
		}
		rows, err := qs.IdentityListRepositoryServiceAccounts(ctx, q.IdentityListRepositoryServiceAccountsParams{
			RepositoryID: active[0], After: after, Query: query, MaxRows: int32(clampLimit(limit)), //nolint:gosec // bounded
		})
		if err != nil {
			return err
		}
		for _, r := range rows {
			out = append(out, ServiceAccount{
				Revision: r.Revision, ID: r.ID, Name: r.Name, Description: r.Description, Status: Status(r.Status), CreatedAt: r.CreatedAt.Time,
				Project: r.Project, Repository: r.Repository, RepositoryID: r.RepositoryID, Actions: r.Actions, TechnicalTokens: r.TechnicalTokens,
			})
		}
		return nil
	})
	return out, DBErr("principal.ListRepositoryServiceAccounts", err)
}

// RepositoryAccountChange is a change of a repository's account: a nil field is unchanged.
type RepositoryAccountChange struct {
	Description *string
	Actions     []string
}

// UpdateRepositoryServiceAccount changes the description and the actions of the account (R-AUTH-046, R-AUTHZ-039); expected is
// the revision the caller loaded or errs.AnyRevision. A change of the actions notifies the change area authz in the transaction
// of the write (R-AUTHZ-028) and is journaled with the actions before and after, so a restore replays a narrowing
// (R-AUTHZ-031).
func (m *Manager) UpdateRepositoryServiceAccount(ctx context.Context, c Caller, a RepositoryAccount, ch RepositoryAccountChange, expected int64) (ServiceAccount, error) {
	var acts []string
	if ch.Actions != nil {
		var err error
		if acts, err = NormalizeActions(ch.Actions); err != nil {
			return ServiceAccount{}, err
		}
	}
	err := m.db.InTx(ctx, func(tx meta.Tx) error {
		qs := Queries(tx)
		sa, err := repositoryAccountOf(ctx, qs, a)
		if err != nil {
			return err
		}
		if err := checkRevision(ctx, qs, sa.ID, expected, "The service account"); err != nil {
			return err
		}
		var fields []string
		if ch.Description != nil && *ch.Description != sa.Description {
			if err := qs.IdentityUpdateServiceAccount(ctx, q.IdentityUpdateServiceAccountParams{ID: sa.ID, Description: *ch.Description}); err != nil {
				return err
			}
			fields = append(fields, "description")
		}
		after := sa.Actions
		if acts != nil && !slices.Equal(acts, sa.Actions) {
			if err := qs.IdentitySetRepositoryGrant(ctx, q.IdentitySetRepositoryGrantParams{ID: sa.ID, Actions: acts}); err != nil {
				return err
			}
			if err := notifyAuthz(ctx, m.authz, tx, sa.ID); err != nil {
				return err
			}
			fields, after = append(fields, "actions"), acts
		}
		if len(fields) == 0 {
			return nil
		}
		d := map[string]any{"fields": fields}
		if slices.Contains(fields, "actions") {
			d["actions"] = after
		}
		return m.record(ctx, tx, accountUpdated(c, sa, d, sa.Actions, after))
	})
	if err != nil {
		return ServiceAccount{}, DBErr("principal.UpdateRepositoryServiceAccount", err)
	}
	return m.GetRepositoryServiceAccount(ctx, a)
}

// DisableRepositoryServiceAccount disables the account; with revokeTokens its tokens are revoked too.
func (m *Manager) DisableRepositoryServiceAccount(ctx context.Context, c Caller, a RepositoryAccount, revokeTokens bool) error {
	return m.disableAccount(ctx, c, byRepository(a), revokeTokens)
}

// EnableRepositoryServiceAccount enables the account.
func (m *Manager) EnableRepositoryServiceAccount(ctx context.Context, c Caller, a RepositoryAccount) error {
	return m.enableAccount(ctx, c, byRepository(a))
}

// DeleteRepositoryServiceAccount deletes the account with its tokens and its actions; the authorization area learns that its
// grant is gone in the same transaction.
func (m *Manager) DeleteRepositoryServiceAccount(ctx context.Context, c Caller, a RepositoryAccount) error {
	return m.deleteAccount(ctx, c, byRepository(a))
}

// EndRepositoryAccountsTx ends the service accounts of a repository whose row the transaction removes (R-AUTH-046): each
// account's tokens are revoked with their token.revoked, serviceaccount.deleted is recorded and the caches of every replica
// learn it, before the row goes. The row's removal deletes the accounts and their principals (ON DELETE CASCADE and the
// trigger of 00080). The caller is the deletion's actor.
func (m *Manager) EndRepositoryAccountsTx(ctx context.Context, tx meta.Tx, c Caller, repositoryID int64) error {
	qs := Queries(tx)
	rows, err := qs.IdentityRepositoryAccounts(ctx, repositoryID)
	if err != nil {
		return err
	}
	for _, r := range rows {
		sa, err := loadServiceAccount(ctx, qs, r.Name)
		if err != nil {
			return err
		}
		tokens, err := qs.IdentityCountTokensOfPrincipal(ctx, sa.ID)
		if err != nil {
			return err
		}
		if _, err := RevokeAllTokensTx(ctx, tx, m.rec, c, sa.ID, "principal_disabled", false); err != nil {
			return err
		}
		if err := notifyAuthz(ctx, m.authz, tx, sa.ID); err != nil {
			return err
		}
		if err := NotifyPrincipals(m.notify, tx, ChangePrincipal, sa.ID); err != nil {
			return err
		}
		if err := m.record(ctx, tx, audit.Event{
			Type: audit.EventSADeleted, Outcome: audit.Success, Actor: c.Actor, Source: c.Source,
			Subject: audit.Subject{Kind: "service_account", ID: sa.ID, Name: sa.Name},
			Details: details(levelDetails(map[string]any{"tokensDeleted": tokens, "membershipsDeleted": 0, "repositoryDeleted": true}, sa)),
			Journal: details(map[string]string{"principalId": sa.ID, "name": sa.Name}),
		}); err != nil {
			return err
		}
	}
	return nil
}

// MoveRepositoryAccountsTx moves the service accounts of a repository with it to another project, in the transaction of the
// move (R-AUTH-046, R-REPO-060, Q-AUTHZ-297): each stored name becomes <project>/<repository>/<name> and its project the
// target. The ID, the actions, the technical tokens and their budgets stay. serviceaccount.updated records "moved" with the
// old and the new name, and the caches of every replica learn the new names. rec and n are the recorder and the change log of
// the caller's service; toKey is the target project's key and toID its ID.
func MoveRepositoryAccountsTx(ctx context.Context, tx meta.Tx, rec audit.Recorder, n Notifier, actor audit.Actor, repo qname.Repo, repositoryID int64, toKey, toID string) error {
	qs := Queries(tx)
	before, err := qs.IdentityRepositoryAccounts(ctx, repositoryID)
	if err != nil || len(before) == 0 {
		return err
	}
	renamed, err := qs.IdentityRenameRepositoryAccounts(ctx, q.IdentityRenameRepositoryAccountsParams{
		Prefix: toKey + "/" + repo.Key + "/", RepositoryID: repositoryID,
	})
	if err != nil {
		return err
	}
	if _, err := qs.IdentityMoveRepositoryAccounts(ctx, q.IdentityMoveRepositoryAccountsParams{ProjectID: toID, RepositoryID: repositoryID}); err != nil {
		return err
	}
	old := make(map[string]string, len(before))
	for _, r := range before {
		old[r.ID] = r.Name
	}
	ids := make([]string, 0, len(renamed))
	for _, r := range renamed {
		ids = append(ids, r.ID)
		sa, err := loadServiceAccount(ctx, qs, r.Name)
		if err != nil {
			return err
		}
		d := levelDetails(map[string]any{"fields": []string{"name", "project"}, "moved": map[string]string{"from": old[r.ID], "to": r.Name}}, sa)
		if err := rec.RecordTx(ctx, tx, accountUpdated(Caller{Actor: actor}, sa, d, sa.Actions, sa.Actions)); err != nil {
			return err
		}
	}
	return NotifyPrincipals(n, tx, ChangePrincipal, ids...)
}
