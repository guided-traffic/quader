package bastionmain

import (
	"context"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/bastion/iface"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// Disabling a Bastion (R-BAS-133): a member of the registration's configuration, soft or hard, that every replica learns at its next
// handshake and confirms with its status. The registration's principal and its tokens are not touched, so the Bastion keeps its line
// to the main installation; the main installation refuses its forwards, checks and catalogue reads meanwhile (recognize.go), and a hard
// disable cuts its forwards in flight (inflight.go). The state of a registration is computed here, once, for the API and the command
// line.

// Disable is how and since when a registration is disabled.
type Disable struct {
	Mode iface.DisableMode
	At   time.Time
}

// disableOf reads the columns disabled and disabled_at; nil while the registration works.
func disableOf(mode string, at time.Time) *Disable {
	if mode == "" {
		return nil
	}
	return &Disable{Mode: iface.DisableMode(mode), At: at}
}

// DisabledMode is the way the registration is disabled, empty while it works.
func (r Registration) DisabledMode() iface.DisableMode {
	if r.Disabled == nil {
		return ""
	}
	return r.Disabled.Mode
}

// State is a Bastion's state as the main installation knows it (R-BAS-133, Q-BAS-511).
type State string

// The states.
const (
	// StateConnected: not disabled, and a replica was heard within [FreshContact].
	StateConnected State = "connected"
	// StateNoContact: not disabled, and no replica was ever heard.
	StateNoContact State = "noContact"
	// StateContactLost: not disabled, and no replica was heard within [FreshContact].
	StateContactLost State = "contactLost"
	// StateDisabling: disabled, and a replica heard since has not confirmed it yet within [ConfirmWithin], or the confirming replicas
	// still run requests (a soft disable's downloads).
	StateDisabling State = "disabling"
	// StateDisabled: disabled, and every replica heard since confirms it and runs nothing.
	StateDisabled State = "disabled"
	// StateDisableUnconfirmed: disabled, and [ConfirmWithin] after the disable a replica heard since has not confirmed it or none was
	// heard since; at once when no replica was heard within [FreshContact] before the disable.
	StateDisableUnconfirmed State = "disableUnconfirmed"
)

// FreshContact is how recently a replica must have been heard for its Bastion to count as connected: five minutes, the freshness of
// a contact on the card (R-UI-240).
const FreshContact = 5 * time.Minute

// ConfirmWithin is how long after a disable the main installation waits for the replicas' confirmation: three polls of the feed and a
// flush of the status beyond the expected 4 seconds.
const ConfirmWithin = 15 * time.Second

// StateOf is the state of a registration at now and the requests the replicas that confirm its disable still run (R-BAS-133). A
// replica is heard at its last contact (a handshake) or its last status (every second while it reports), whichever is later; it
// confirms the disable with a status reported after the disable that names the same way.
func StateOf(r Registration, now time.Time) (State, int64) {
	heard := map[string]time.Time{}
	note := func(replica string, at time.Time) {
		if at.After(heard[replica]) {
			heard[replica] = at
		}
	}
	for _, c := range r.Contacts {
		note(c.Replica, c.ContactAt)
	}
	for _, st := range r.Status {
		note(st.Replica, st.ReportedAt)
	}
	latest := time.Time{}
	for _, at := range heard {
		if at.After(latest) {
			latest = at
		}
	}
	if r.Disabled == nil {
		switch {
		case len(heard) == 0:
			return StateNoContact, 0
		case now.Sub(latest) <= FreshContact:
			return StateConnected, 0
		}
		return StateContactLost, 0
	}
	d := *r.Disabled
	waited := now.Sub(d.At) >= ConfirmWithin
	confirmed, unconfirmed := 0, 0
	var running int64
	for _, st := range r.Status {
		if !st.ReportedAt.After(d.At) {
			continue
		}
		if op := st.Status.Operation; op.Disabled != nil && *op.Disabled == d.Mode {
			confirmed++
			running += op.Running
			continue
		}
		unconfirmed++
	}
	// A replica that made a contact since the disable but has not reported a status since is not confirming either.
	for _, c := range r.Contacts {
		if c.ContactAt.After(d.At) && !reportedSince(r.Status, c.Replica, d.At) {
			unconfirmed++
		}
	}
	switch {
	case confirmed == 0 && unconfirmed == 0:
		// None heard since: nothing will confirm when none was heard within FreshContact before the disable either.
		if waited || latest.IsZero() || d.At.Sub(latest) > FreshContact {
			return StateDisableUnconfirmed, 0
		}
		return StateDisabling, 0
	case unconfirmed > 0:
		if waited {
			return StateDisableUnconfirmed, running
		}
		return StateDisabling, running
	case running > 0:
		return StateDisabling, running
	}
	return StateDisabled, 0
}

// reportedSince reports whether the replica reported a status after t.
func reportedSince(states []ReplicaState, replica string, t time.Time) bool {
	for _, st := range states {
		if st.Replica == replica && st.ReportedAt.After(t) {
			return true
		}
	}
	return false
}

// setDisable disables the registration subject soft or hard, or enables it with the empty way, inside the update's transaction
// (R-BAS-133): the member and its time, the change area of the configuration, so every replica's snapshot refuses or admits the
// Bastion's work within its bound, and bastion.disabled with the way and the journal entry, or bastion.enabled. Both map to the feed's
// settings, so every Bastion handshakes again within one poll and applies it. A way already set writes nothing.
func (r *Registrations) setDisable(ctx context.Context, tx meta.Tx, c principal.Caller, subject audit.Subject, mode iface.DisableMode) error {
	changed, err := principal.Queries(tx).BastionSetDisabled(ctx, q.BastionSetDisabledParams{ID: subject.ID, Disabled: string(mode)})
	if err != nil || len(changed) == 0 {
		return err
	}
	if err := r.notifyArea(tx, cluster.AreaBastionConfiguration, subject.ID); err != nil {
		return err
	}
	ev := audit.Event{
		Type: audit.EventBastionEnabled, Outcome: audit.Success, Actor: c.Actor, Source: c.Source,
		Subject: subject, Details: mustJSON(map[string]any{}),
	}
	if mode != "" {
		ev.Type, ev.Journal = audit.EventBastionDisabled, registrationEntry(subject.ID, subject.Name, mode)
		ev.Details = mustJSON(map[string]any{"mode": string(mode)})
	}
	return r.o.Recorder.RecordTx(ctx, tx, ev)
}

// The replay of a disable or a deletion (R-BAS-133, R-HA-054, Q-BAS-145): a restore to t brings a registration back as it was at t, and
// the replay disables it again in the entry's way when it was disabled or deleted after t, never more lightly than it is, so a restore
// never hands a Bastion more than the administrator last allowed. An entry without a way (written before the way existed) is hard. Its
// tokens are not touched here: a deletion's token.revoked entries revoke them. It writes no audit event (Q-AUTHZ-232); every replica's
// snapshot follows the change area.
func init() {
	journal.Register(journal.Handler{Domain: "bastion", Events: []string{audit.EventBastionDisabled, audit.EventBastionDeleted}, Apply: replayDisable})
}

func replayDisable(ctx context.Context, tx meta.Tx, env journal.Env, o journal.Object) (journal.Result, error) {
	var e struct {
		PrincipalID string `json:"principalId"`
		Mode        string `json:"mode"`
	}
	if !o.Into(&e) || !journal.IsUUID(e.PrincipalID) {
		return o.Malformed("principalId is not an ID"), nil
	}
	mode := iface.DisableMode(e.Mode)
	switch {
	case o.Event == audit.EventBastionDeleted || e.Mode == "":
		mode = iface.DisableHard
	case !mode.Valid():
		return o.Malformed("mode is neither soft nor hard"), nil
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
	changed, err := qs.BastionReplayDisable(ctx, q.BastionReplayDisableParams{ID: e.PrincipalID, Disabled: string(mode)})
	if err != nil {
		return journal.Result{}, err
	}
	if len(changed) == 0 {
		return o.Result(journal.AlreadyInEffect, name), nil
	}
	if err := env.Notify(tx, cluster.AreaBastionConfiguration, 1, map[string]string{"bastion": e.PrincipalID}); err != nil {
		return journal.Result{}, err
	}
	return o.Result(journal.Applied, name), nil
}
