package bastionmain

import (
	"context"
	"fmt"
	"net/netip"
	"slices"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/auth/principal"
	"github.com/guided-traffic/quader/internal/auth/token"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// A Bastion registration the installation's configuration declares (R-BAS-127, Q-BAS-465 to Q-BAS-469): bastions.declared names it,
// its description, the networks its Bastion connects from and the file of its token, and quader migrate applies the entries on every
// run, so the registration and the token its Bastion holds exist again after a fresh database. What the configuration does not
// declare - the switches, the limits, the rules of its place, the enablement and tokens issued on the screen - stays an
// administrator's and survives a run; after a fresh database it is the registration's defaults.

// Declared is one entry of bastions.declared as quader migrate applies it: the networks validated by [ParseEgress], the token read
// from its file and checked by token.ParseDeclaredBastion. It holds no value of the token.
type Declared struct {
	Name        string
	Description string
	Egress      []netip.Prefix
	Token       token.DeclaredBastionToken
}

// DeclaredResult is what one application changed, by registration name, for the run's log.
type DeclaredResult struct {
	// Created are the registrations the run created; Marked the ones it marked as declared (a new one, or one an administrator had
	// created before it was declared).
	Created []string
	Marked  []string
	// Updated are the registrations whose description or networks followed their entry.
	Updated []string
	// TokensStored are the registrations whose declared token is a value the database did not hold as theirs; TokensTaken the ones
	// whose value was a token issued on the screen, which became the declared one; TokensReplaced the ones whose previous declared
	// token the run revoked because the Secret holds another value.
	TokensStored   []string
	TokensTaken    []string
	TokensReplaced []string
	// Released are the registrations the configuration no longer declares: the mark is cleared and the declared token revoked, the
	// registration stays.
	Released []string
}

// Changed reports whether the run wrote anything.
func (d DeclaredResult) Changed() bool {
	return len(d.Created)+len(d.Marked)+len(d.Updated)+len(d.TokensStored)+len(d.TokensTaken)+len(d.TokensReplaced)+len(d.Released) > 0
}

// declaredNow is a registration the database marks as declared, with its declared token as the run finds it.
type declaredNow struct {
	ID, Name string
	// TokenID is the declared token, empty when there is none; Hash its value's hash and Revoked whether it was revoked since.
	TokenID string
	Hash    []byte
	Revoked bool
}

// tokenStep is what the run does with an entry's token (planToken).
type tokenStep int

const (
	tokenKeep   tokenStep = iota // the declared token holds the value already
	tokenLookup                  // the value is not the declared token's: look it up, then store or take it
)

// planToken decides an entry's token from the registration's declared token: the same value is kept unless it was revoked, which
// stops the run (a revoked value never becomes valid again); another value, or none yet, is looked up.
func planToken(now *declaredNow, want token.DeclaredBastionToken) (tokenStep, error) {
	if now == nil || now.TokenID == "" || !slices.Equal(now.Hash, want.Hash[:]) {
		return tokenLookup, nil
	}
	if now.Revoked {
		return 0, fmt.Errorf("the token in the file was revoked (%s); a revoked token never becomes valid again: put a new value in its Secret and roll out", want.Hint)
	}
	return tokenKeep, nil
}

// released are the registrations marked as declared that the entries no longer name, in name order.
func released(now []declaredNow, in []Declared) []declaredNow {
	var out []declaredNow
	for _, d := range now {
		if !slices.ContainsFunc(in, func(e Declared) bool { return e.Name == d.Name }) {
			out = append(out, d)
		}
	}
	return out
}

// ApplyDeclared applies bastions.declared inside tx, under the configuration lock of the registrations (R-ARCH-017), with the
// caller of the configuration (R-BAS-127): for each entry the registration is created when it is missing and marked, its description
// and networks follow the entry, and its declared token is the value of the file - the same value writes nothing, another value is
// stored (or a token issued on the screen with that value is taken) and the previous declared token revoked with the reason rotated.
// A registration marked but no longer declared loses the mark and its declared token (reason admin) and stays. Each change records
// its event with the caller's actor. A value that is a revoked or expired token, or a token of another principal, stops the run with
// the entry named and writes nothing.
func (r *Registrations) ApplyDeclared(ctx context.Context, tx meta.Tx, c principal.Caller, in []Declared) (DeclaredResult, error) {
	var out DeclaredResult
	if err := cluster.LockConfigObject(ctx, tx, cluster.ConfigBastionRegistrations); err != nil {
		return out, err
	}
	qs := principal.Queries(tx)
	rows, err := qs.BastionDeclaredList(ctx)
	if err != nil {
		return out, err
	}
	now := make([]declaredNow, 0, len(rows))
	for _, row := range rows {
		now = append(now, declaredNow{ID: row.ID, Name: row.Name, TokenID: row.DeclaredToken, Hash: row.DeclaredSha256, Revoked: row.DeclaredRevoked})
	}
	sorted := slices.Clone(in)
	slices.SortFunc(sorted, func(a, b Declared) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		}
		return 0
	})
	for _, d := range sorted {
		var cur *declaredNow
		if i := slices.IndexFunc(now, func(n declaredNow) bool { return n.Name == d.Name }); i >= 0 {
			cur = &now[i]
		}
		if err := r.applyOne(ctx, tx, c, d, cur, &out); err != nil {
			return out, fmt.Errorf("bastions.declared.%s: %w", d.Name, err)
		}
	}
	for _, gone := range released(now, in) {
		if err := r.release(ctx, tx, c, gone); err != nil {
			return out, fmt.Errorf("the registration %s, no longer declared: %w", gone.Name, err)
		}
		out.Released = append(out.Released, gone.Name)
	}
	return out, nil
}

// applyOne applies one entry; cur is the registration's declared state, nil when it is not marked as declared yet.
func (r *Registrations) applyOne(ctx context.Context, tx meta.Tx, c principal.Caller, d Declared, cur *declaredNow, out *DeclaredResult) error {
	qs := principal.Queries(tx)
	rows, err := qs.BastionLock(ctx, d.Name)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		if _, err := r.insertTx(ctx, tx, c, NewRegistration{Name: d.Name, Description: d.Description, Source: "configuration"}, d.Egress); err != nil {
			return err
		}
		out.Created = append(out.Created, d.Name)
		if rows, err = qs.BastionLock(ctx, d.Name); err != nil {
			return err
		}
	}
	lock := rows[0]
	subject := audit.Subject{Kind: "bastion", ID: lock.ID, Name: d.Name}
	marked, err := qs.BastionSetDeclared(ctx, q.BastionSetDeclaredParams{ID: lock.ID, Declared: true})
	if err != nil {
		return err
	}
	if len(marked) > 0 {
		out.Marked = append(out.Marked, d.Name)
		if err := r.recordDeclared(ctx, tx, c, subject, "declared", "configuration"); err != nil {
			return err
		}
	}
	reg, err := load(ctx, qs, d.Name)
	if err != nil {
		return err
	}
	updated := false
	if reg.Description != d.Description {
		if err := qs.BastionUpdateDescription(ctx, q.BastionUpdateDescriptionParams{ID: lock.ID, Description: d.Description}); err != nil {
			return err
		}
		if err := r.recordDeclared(ctx, tx, c, subject, "description", "configuration"); err != nil {
			return err
		}
		updated = true
	}
	if !slices.Equal(reg.Egress, d.Egress) {
		if err := r.setEgress(ctx, tx, c, subject, reg.Egress, d.Egress); err != nil {
			return err
		}
		updated = true
	}
	if updated && !slices.Contains(out.Created, d.Name) {
		out.Updated = append(out.Updated, d.Name)
	}
	return r.applyToken(ctx, tx, c, d, lock.ID, cur, out)
}

// applyToken makes the value of the entry's file the registration's declared token (planToken).
func (r *Registrations) applyToken(ctx context.Context, tx meta.Tx, c principal.Caller, d Declared, id string, cur *declaredNow, out *DeclaredResult) error {
	step, err := planToken(cur, d.Token)
	if err != nil || step == tokenKeep {
		return err
	}
	qs := principal.Queries(tx)
	found, err := qs.BastionTokenByHash(ctx, d.Token.Hash[:])
	if err != nil {
		return err
	}
	var tokenID string
	if len(found) == 0 {
		if tokenID, err = r.o.Tokens.InsertDeclaredBastionTx(ctx, tx, c, id, d.Name, d.Token); err != nil {
			return err
		}
		out.TokensStored = append(out.TokensStored, d.Name)
	} else {
		t := found[0]
		switch {
		case t.PrincipalID != id:
			return fmt.Errorf("the token in the file (%s) belongs to %q; one value belongs to one registration: put a new value in its Secret", d.Token.Hint, t.OwnerName)
		case t.Revoked:
			return fmt.Errorf("the token in the file was revoked (%s); a revoked token never becomes valid again: put a new value in its Secret and roll out", d.Token.Hint)
		case t.Expired:
			return fmt.Errorf("the token in the file has expired (%s): put a new value in its Secret and roll out", d.Token.Hint)
		}
		// A token issued on the screen whose value the Secret holds becomes the declared one: it no longer expires (Q-BAS-466).
		tokenID = t.ID
		if err := qs.BastionTokenUnexpire(ctx, tokenID); err != nil {
			return err
		}
		if err := r.o.Recorder.RecordTx(ctx, tx, audit.Event{
			Type: audit.EventBastionUpdated, Outcome: audit.Success, Actor: c.Actor, Source: c.Source,
			Subject: audit.Subject{Kind: "bastion", ID: id, Name: d.Name},
			Details: mustJSON(map[string]any{"fields": []string{"token"}, "reason": "configuration", "tokenId": tokenID}),
		}); err != nil {
			return err
		}
		out.TokensTaken = append(out.TokensTaken, d.Name)
	}
	if err := qs.BastionSetDeclaredToken(ctx, q.BastionSetDeclaredTokenParams{ID: id, TokenID: tokenID}); err != nil {
		return err
	}
	if cur != nil && cur.TokenID != "" && cur.TokenID != tokenID && !cur.Revoked {
		if err := r.o.Tokens.RevokeBastionTokenTx(ctx, tx, c, cur.TokenID, token.ReasonRotated); err != nil {
			return err
		}
		out.TokensReplaced = append(out.TokensReplaced, d.Name)
	}
	return nil
}

// release ends the declaration of a registration the configuration no longer names: the mark and the token pointer are cleared, the
// declared token is revoked, and bastion.updated records the field declared with the reason configuration_removed. The registration,
// its other tokens and everything an administrator set stay.
func (r *Registrations) release(ctx context.Context, tx meta.Tx, c principal.Caller, gone declaredNow) error {
	qs := principal.Queries(tx)
	if _, err := qs.BastionSetDeclared(ctx, q.BastionSetDeclaredParams{ID: gone.ID, Declared: false}); err != nil {
		return err
	}
	if gone.TokenID != "" && !gone.Revoked {
		if err := r.o.Tokens.RevokeBastionTokenTx(ctx, tx, c, gone.TokenID, token.ReasonAdmin); err != nil {
			return err
		}
	}
	return r.recordDeclared(ctx, tx, c, audit.Subject{Kind: "bastion", ID: gone.ID, Name: gone.Name}, "declared", "configuration_removed")
}

// recordDeclared records bastion.updated for a member the configuration changed, with the reason.
func (r *Registrations) recordDeclared(ctx context.Context, tx meta.Tx, c principal.Caller, subject audit.Subject, field, reason string) error {
	return r.o.Recorder.RecordTx(ctx, tx, audit.Event{
		Type: audit.EventBastionUpdated, Outcome: audit.Success, Actor: c.Actor, Source: c.Source, Subject: subject,
		Details: mustJSON(map[string]any{"fields": []string{field}, "reason": reason}),
	})
}
