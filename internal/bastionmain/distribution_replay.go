package bastionmain

import (
	"context"
	"strconv"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The replays of what narrows a Bastion's distribution (R-BAS-134, Q-BAS-521, R-HA-054): a restore to t brings back the rows of
// bastion_project and repository_bastion as they were at t, and these handlers delete them again where a removal came after t, so a
// restore never sends through a Bastion what an administrator or an Owner took off it. They write no audit event (Q-AUTHZ-232); a
// deleted row announces the change area bastion_distribution.
func init() {
	journal.Register(journal.Handler{
		Domain: "bastion", Events: []string{audit.EventBastionProjectRemoved, audit.EventRepositoryBastionRemoved}, Apply: replayDistribution,
	})
}

func replayDistribution(ctx context.Context, tx meta.Tx, env journal.Env, o journal.Object) (journal.Result, error) {
	if o.Event == audit.EventRepositoryBastionRemoved {
		return replayChoiceRemoved(ctx, tx, env, o)
	}
	return replayProjectRemoved(ctx, tx, env, o)
}

// replayProjectRemoved deletes the allow row {bastionId, projectId}; the trigger of 00159 deletes the project's choices for the
// Bastion with it.
func replayProjectRemoved(ctx context.Context, tx meta.Tx, env journal.Env, o journal.Object) (journal.Result, error) {
	var e struct {
		BastionID string `json:"bastionId"`
		ProjectID string `json:"projectId"`
		Name      string `json:"name"`
		Project   string `json:"project"`
	}
	if !o.Into(&e) || !journal.IsUUID(e.BastionID) || !journal.IsUUID(e.ProjectID) {
		return o.Malformed("bastionId or projectId is not an ID"), nil
	}
	subject := e.Name + "/" + e.Project
	removed, err := principal.Queries(tx).BastionProjectRemove(ctx, q.BastionProjectRemoveParams{BastionID: e.BastionID, ProjectID: e.ProjectID})
	if err != nil {
		return journal.Result{}, err
	}
	if len(removed) == 0 {
		return o.Result(journal.AlreadyInEffect, subject), nil
	}
	if err := notifyDistributionReplay(tx, env, e.BastionID); err != nil {
		return journal.Result{}, err
	}
	return o.Result(journal.Applied, subject), nil
}

// replayChoiceRemoved deletes the choice {repositoryId, bastionId}.
func replayChoiceRemoved(ctx context.Context, tx meta.Tx, env journal.Env, o journal.Object) (journal.Result, error) {
	var e struct {
		RepositoryID string `json:"repositoryId"`
		BastionID    string `json:"bastionId"`
		Name         string `json:"name"`
	}
	if !o.Into(&e) || !journal.IsUUID(e.BastionID) {
		return o.Malformed("bastionId is not an ID"), nil
	}
	id, perr := strconv.ParseInt(e.RepositoryID, 10, 64)
	if perr != nil || id < 1 {
		return o.Malformed("repositoryId is not a repository's ID"), nil //nolint:nilerr // a malformed entry is skipped, not an error of the replay
	}
	subject := e.RepositoryID + "/" + e.Name
	removed, err := principal.Queries(tx).RepositoryBastionRemoveByID(ctx, q.RepositoryBastionRemoveByIDParams{RepositoryID: id, BastionID: e.BastionID})
	if err != nil {
		return journal.Result{}, err
	}
	if len(removed) == 0 {
		return o.Result(journal.AlreadyInEffect, subject), nil
	}
	if err := notifyDistributionReplay(tx, env, e.BastionID); err != nil {
		return journal.Result{}, err
	}
	return o.Result(journal.Applied, subject), nil
}

// notifyDistributionReplay announces a replayed narrowing as the service announces one (Registrations.notifyDistribution): the change
// area bastion_distribution and the authorization's, whose epoch the grant index follows (authorization.md R-AUTHZ-043).
func notifyDistributionReplay(tx meta.Tx, env journal.Env, bastionID string) error {
	if err := env.Notify(tx, cluster.AreaBastionDistribution, 1, map[string]string{"bastion": bastionID}); err != nil {
		return err
	}
	return env.Notify(tx, cluster.AreaAuthz, 1, map[string]string{"type": "bastion_distribution", "id": bastionID})
}
