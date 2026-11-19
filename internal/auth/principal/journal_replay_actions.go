package principal

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// actionSet is the order of the actions of a repository's account for the update rule of R-HA-054 (R-AUTHZ-039): after
// narrows current when it holds no action current does not.
type actionSet struct{}

// Narrows reports whether after grants nothing that current does not.
func (actionSet) Narrows(current, after []string) bool {
	for _, a := range after {
		if !slices.Contains(current, a) {
			return false
		}
	}
	return true
}

// authzPayloadVersion is the payload version of the change area authz (internal/authz): a change of a repository account's
// actions is an input of the grant index (R-AUTHZ-028).
const authzPayloadVersion = 1

// replayAccountActions replays serviceaccount.updated (R-AUTHZ-039, R-AUTHZ-031): the actions of a repository's account are set
// to the journaled after-set when it narrows the restored set; a change that only added actions is not restored; a narrowing
// on top of a broadening the restored database lacks disables the account (its tokens fail), with the after-set in the
// detail, so an administrator reviews it. An entry without actions (an account of the instance or a project, a description, a
// move) holds nothing access-bearing and is already in effect. The authorization area learns an applied change in the
// transaction of the replay.
func replayAccountActions(ctx context.Context, tx meta.Tx, env journal.Env, o journal.Object) (journal.Result, error) {
	var e struct {
		PrincipalID string `json:"principalId"`
		Actions     *struct {
			Before []string `json:"before"`
			After  []string `json:"after"`
		} `json:"actions"`
	}
	if !o.Into(&e) || !journal.IsUUID(e.PrincipalID) {
		return o.Malformed("principalId is not an ID"), nil
	}
	qs := Queries(tx)
	sa, found, err := principalOf(ctx, qs, e.PrincipalID, KindServiceAccount)
	switch {
	case err != nil:
		return journal.Result{}, err
	case !found:
		return o.Result(journal.SubjectUnknown, e.PrincipalID), nil
	case e.Actions == nil:
		return o.Result(journal.AlreadyInEffect, sa.Name), nil
	}
	if e.Actions.After == nil {
		return o.Malformed("actions.after is no set of repository actions"), nil
	}
	after, invalid := NormalizeActions(e.Actions.After)
	if invalid != nil {
		// An untrusted entry whose set is no set of actions is skipped as malformed (R-HA-054); it is no failure of the replay.
		return o.Malformed("actions.after is no set of repository actions"), nil //nolint:nilerr // the refusal is the entry's, not the replay's
	}
	current, err := loadServiceAccount(ctx, qs, sa.Name)
	if err != nil {
		return journal.Result{}, err
	}
	if current.Repository == "" {
		return o.Result(journal.SubjectUnknown, sa.Name), nil
	}
	if slices.Equal(current.Actions, after) {
		return o.Result(journal.AlreadyInEffect, sa.Name), nil
	}
	outcome, detail, err := journal.ApplyUpdate[[]string](actionSet{}, current.Actions, e.Actions.Before, after, journal.UpdateActions{
		Apply: func() error {
			if err := qs.IdentitySetRepositoryGrant(ctx, q.IdentitySetRepositoryGrantParams{ID: sa.ID, Actions: after}); err != nil {
				return err
			}
			return env.Notify(tx, cluster.AreaAuthz, authzPayloadVersion, map[string]string{"type": "principal", "id": sa.ID})
		},
		Suspend: func() (json.RawMessage, error) {
			if _, err := qs.IdentitySetPrincipalStatus(ctx, q.IdentitySetPrincipalStatusParams{ID: sa.ID, Status: string(StatusDisabled)}); err != nil {
				return nil, err
			}
			if err := NotifyPrincipals(env, tx, ChangePrincipal, sa.ID); err != nil {
				return nil, err
			}
			return json.Marshal(map[string][]string{"actions": after})
		},
	})
	if err != nil {
		return journal.Result{}, err
	}
	r := o.Result(outcome, sa.Name)
	r.Detail = detail
	return r, nil
}
