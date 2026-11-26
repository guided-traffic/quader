package bastionmain

import (
	"context"
	"encoding/json"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/meta"
)

// The switch of the handover per blob to a Bastion (R-BAS-071, Q-BAS-232): a member of the registration, off by default,
// kept in bastion.handover (00110). The serve point decides a forward from the replica's snapshot of the registrations
// (snapshot.go), which the change area bastion_handover refreshes within the bound of R-ARCH-018 (2 s), so a download costs no
// statement for it (R-ARCH-027). Switching it off is journaled; the replay below switches it off again after a restore.

// handoverEntry is the journal entry of bastion.handover.disabled: the registration's principal ID and, for display, its name.
func handoverEntry(id, name string) json.RawMessage { return registrationEntry(id, name) }

// The replay of a handover switched off (R-BAS-071, R-HA-054): a restore to t brings a registration's member back as it was at
// t, and the replay switches it off again when it was switched off after t, so a restore never hands a Bastion more than the
// administrator last allowed. It writes no audit event (Q-AUTHZ-232); every replica's snapshot follows the change area.
func init() {
	journal.Register(journal.Handler{Domain: "bastion", Events: []string{audit.EventBastionHandoverDisabled}, Apply: replayHandoverOff})
}

func replayHandoverOff(ctx context.Context, tx meta.Tx, env journal.Env, o journal.Object) (journal.Result, error) {
	var e struct {
		PrincipalID string `json:"principalId"`
		Name        string `json:"name"`
	}
	if !o.Into(&e) || !journal.IsUUID(e.PrincipalID) {
		return o.Malformed("principalId is not an ID"), nil
	}
	qs := principal.Queries(tx)
	rows, err := qs.IdentityPrincipalByID(ctx, e.PrincipalID)
	if err != nil {
		return journal.Result{}, err
	}
	if len(rows) == 0 || rows[0].Kind != string(principal.KindBastion) {
		return o.Result(journal.SubjectUnknown, e.PrincipalID), nil
	}
	name := rows[0].Name
	changed, err := qs.BastionHandoverOff(ctx, e.PrincipalID)
	if err != nil {
		return journal.Result{}, err
	}
	if len(changed) == 0 {
		return o.Result(journal.AlreadyInEffect, name+".handover"), nil
	}
	if err := env.Notify(tx, cluster.AreaBastionHandover, 1, map[string]string{"bastion": e.PrincipalID}); err != nil {
		return journal.Result{}, err
	}
	return o.Result(journal.Applied, name+".handover"), nil
}
