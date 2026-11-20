package principal

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

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

// recordClaimState compares the group claim of this sign-in, and the results of the group rules that were skipped, with the
// states stored with the identity, read under the lock of the identity's row, records each change if there is one, and puts
// the new states into claims, which the sign-in stores as the identity's last_claims in the same transaction (R-AUTH-008,
// Q-AUTH-400, Q-AUTH-403, Q-AUTH-422). A second sign-in of the identity waits for the lock and then reads what the first one
// stored, so one change is recorded once.
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
	skipped, skippedEv := skippedChange(parseSkipped(rows[0].GroupsSkipped), c)
	if skipped != nil {
		claims["groupsSkipped"] = *skipped
	}
	for _, e := range []*audit.Event{ev, skippedEv} {
		if e == nil {
			continue
		}
		e.Actor, e.Subject, e.Source = actor, subject, c.Source
		if err := p.rec.RecordTx(ctx, tx, *e); err != nil {
			return err
		}
	}
	return nil
}

// The bounds of the skipped results that the stored state and the event hold: a claim has up to 1000 values, and a result
// that is no group name can be any text of any length.
const (
	maxSkippedNames = 20
	maxSkippedRunes = 128
)

// reasonInvalidGroupName is the reason code of user.groups.result_skipped.
const reasonInvalidGroupName = "invalid_group_name"

// skippedResults is the stored state of an identity whose group rules gave results that are no valid group name at its last
// sign-in (R-AUTH-008): the names sorted, without duplicates, each cut to 128 characters, at most 20 of them, and how many
// there were. It is the member groupsSkipped of the identity's last_claims.
type skippedResults struct {
	Names []string `json:"names"`
	Count int      `json:"count"`
}

// parseSkipped reads a stored state; a value without a name is no state.
func parseSkipped(raw string) *skippedResults {
	if raw == "" || raw == "null" {
		return nil
	}
	var s skippedResults
	if json.Unmarshal([]byte(raw), &s) != nil || len(s.Names) == 0 {
		return nil
	}
	return &s
}

// skippedOf is the state of a sign-in with these skipped results, nil when there are none. A name is stored as text that
// PostgreSQL's jsonb accepts and that reads back as it was written: the NUL character and bytes that are no UTF-8 become
// U+FFFD, so the comparison with the stored state does not depend on what the store did to them.
func skippedOf(results []string) *skippedResults {
	if len(results) == 0 {
		return nil
	}
	names := make([]string, 0, len(results))
	for _, n := range results {
		n = strings.Map(func(r rune) rune {
			if r == 0 {
				return utf8.RuneError
			}
			return r
		}, strings.ToValidUTF8(n, string(utf8.RuneError)))
		if r := []rune(n); len(r) > maxSkippedRunes {
			n = string(r[:maxSkippedRunes])
		}
		names = append(names, n)
	}
	slices.Sort(names)
	names = slices.Compact(names)
	s := &skippedResults{Count: len(names)}
	s.Names = names[:min(len(names), maxSkippedNames)]
	return s
}

// skippedChange decides what a sign-in records about the results of the group rules that are no valid group name
// (R-AUTH-008): from the stored state prev (nil: the identity's last sign-in skipped none, or there was none) and this
// sign-in's results, the state to store (nil: none) and the event to record (nil: none skipped, or the same set as at the last
// sign-in). A rule that keeps producing the same name is recorded once per identity, not at every sign-in; the caller fills
// the event's actor, subject and source.
func skippedChange(prev *skippedResults, c LoginClaims) (*skippedResults, *audit.Event) {
	next := skippedOf(c.SkippedGroups)
	if next == nil {
		return nil, nil
	}
	if prev != nil && prev.Count == next.Count && slices.Equal(prev.Names, next.Names) {
		return next, nil
	}
	return next, &audit.Event{
		Type: audit.EventUserGroupsSkipped, Outcome: audit.Warning, Reason: reasonInvalidGroupName,
		Details: details(map[string]any{"provider": c.Provider, "skipped": next.Names, "count": next.Count}),
	}
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
