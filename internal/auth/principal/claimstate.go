package principal

import (
	"context"
	"errors"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The on_missing_claim policies the provisioner applies; deny is the login's and never reaches it (R-AUTH-008).
const (
	onMissingClear = "clear"
	onMissingKeep  = "keep"
)

// errIdentityGone ends a sign-in whose identity row is not there when its group claim is compared: the row was found or
// created earlier in the same transaction, so this is a failure of the system, never a state of the claim.
var errIdentityGone = errors.New("principal: the identity of the sign-in has no row")

// recordClaimState compares the group claim of this sign-in with the state stored with the identity, read under the lock of
// the identity's row, records the change if there is one, and puts the new state into claims, which the sign-in stores as the
// identity's last_claims in the same transaction (R-AUTH-008, Q-AUTH-400, Q-AUTH-403). A second sign-in of the identity waits
// for the lock and then reads what the first one stored, so one change is recorded once.
func (p *PGProvisioner) recordClaimState(ctx context.Context, tx meta.Tx, qs *q.Queries, c LoginClaims, actor audit.Actor, subject audit.Subject, claims map[string]any) error {
	rows, err := qs.IdentityLockGroupClaim(ctx, q.IdentityLockGroupClaimParams{Issuer: c.Issuer, Subject: c.Subject})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return errIdentityGone
	}
	next, ev := claimChange(parseClaimMissing(rows[0].ClaimMissing), c, rows[0].Now.Time)
	if next != nil {
		claims["groupClaimMissing"] = *next
	}
	if ev == nil {
		return nil
	}
	ev.Actor, ev.Subject, ev.Source = actor, subject, c.Source
	return p.rec.RecordTx(ctx, tx, *ev)
}

// claimChange is the table of R-AUTH-008: from the stored state prev (nil: the claim was usable at the identity's last
// sign-in, or there was none) and the claim of this sign-in at the transaction's time now, the state to store (nil: none) and
// the event to record (nil: the state did not change). The caller fills the event's actor, subject and source.
func claimChange(prev *ClaimMissing, c LoginClaims, now time.Time) (*ClaimMissing, *audit.Event) {
	policy := onMissingClear
	if c.KeepOnMissing {
		policy = onMissingKeep
	}
	if c.ClaimState == ClaimPresent {
		if prev == nil {
			return nil, nil
		}
		was := prev.OnMissingClaim
		if was == "" {
			was = policy
		}
		return nil, &audit.Event{
			Type: audit.EventUserGroupsClaimBack, Outcome: audit.Success,
			Details: details(map[string]string{
				"provider": c.Provider, "reason": prev.Reason, "missingSince": prev.Since.UTC().Format(time.RFC3339Nano), "onMissingClaim": was,
			}),
		}
	}
	reason := string(c.ClaimState)
	if prev != nil && prev.Reason == reason && prev.Provider == c.Provider {
		kept := *prev
		kept.OnMissingClaim = policy
		return &kept, nil
	}
	d := map[string]string{"provider": c.Provider, "reason": reason, "onMissingClaim": policy}
	if prev != nil && prev.Reason != reason {
		d["previousReason"] = prev.Reason
	}
	return &ClaimMissing{Provider: c.Provider, Reason: reason, Since: now.UTC(), OnMissingClaim: policy},
		&audit.Event{Type: audit.EventUserGroupsClaimMiss, Outcome: audit.Warning, Reason: reason, Details: details(d)}
}
