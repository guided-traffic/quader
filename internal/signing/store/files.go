package store

import (
	"context"
	"encoding/hex"
	"time"

	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/signing"
)

// The worker's writes: the identity of the files of the Helm values (R-SIG-046) and the switch after the announce period (R-SIG-048).

// RegisterFiles registers a pair of the files that passed R-SIG-045 (R-SIG-046): a row of the source configuration is inserted when no
// row has its certificate, announced while another identity is current (an announced one it replaces, which never signed, becomes
// previous: Helm wins), current when none is or the announce period is 0. A row of a previous identity with the certificate is brought
// back the same way. A retired identity is never brought back: its certificate answers a refusal (Q-SIG-149). A passing pair clears the
// refusal the admin page shows. installed is true when a row was written.
func (s *Store) RegisterFiles(ctx context.Context, c signing.Checked) (id signing.Identity, installed bool, err error) {
	parts := certPartsOf(c)
	pub, keySHA, npmID, err := publicParts(c.Key)
	if err != nil {
		return signing.Identity{}, false, err
	}
	var newID int64
	err = s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		l, err := lockAll(ctx, qs)
		if err != nil {
			return err
		}
		all, err := identities(ctx, qs)
		if err != nil {
			return err
		}
		if _, err := qs.SigningClearRefused(ctx); err != nil {
			return err
		}
		for _, i := range all {
			if i.CertSHA256 != parts.sha {
				continue
			}
			switch i.State {
			case signing.StateRetired:
				return &signing.Refusal{Check: CheckRetired, Detail: "the certificate is the certificate of a retired signing identity; a retired identity never comes back"}
			case signing.StateCurrent, signing.StateAnnounced:
				newID = i.ID
				return nil
			}
			newID = i.ID
			return s.bringBack(ctx, qs, tx, l, i)
		}
		if a, ok := l.inState(signing.StateAnnounced); ok {
			if _, err := qs.SigningMakePrevious(ctx, a.ID); err != nil {
				return err
			}
			delete(l.byID, a.ID)
			l.all = without(l.all, a.ID)
		}
		state, prev := s.placement(l)
		if newID, err = qs.SigningNextID(ctx); err != nil {
			return err
		}
		if err := qs.SigningInsertIdentity(ctx, q.SigningInsertIdentityParams{
			ID: newID, Source: signing.SourceConfiguration, State: state, PublicKey: pub, KeySha256: keySHA[:], NpmKeyid: npmID,
			Certificate: parts.leaf, CertSha256: parts.sha[:], Chain: parts.chain, Subject: parts.subject, Issuer: parts.issuer,
			Names: parts.names, NotBefore: stamp(parts.notBefore), NotAfter: stamp(parts.notAfter),
		}); err != nil {
			return mapUnique(err)
		}
		installed = true
		return s.installed(ctx, qs, tx, SystemActor(), newID, prev)
	})
	if err != nil {
		return signing.Identity{}, false, err
	}
	id, err = s.identity(ctx, newID)
	return id, installed, err
}

// CheckRetired is the refusal of a pair of the files whose certificate is a retired identity's (Q-SIG-149).
const CheckRetired = "retired"

// bringBack announces a previous identity again (the files name it again, or it is the newest upload once the values name none), or
// makes it current at once when none is.
func (s *Store) bringBack(ctx context.Context, qs *q.Queries, tx meta.Tx, l locked, i signing.Identity) error {
	if a, ok := l.inState(signing.StateAnnounced); ok && a.ID != i.ID {
		if _, err := qs.SigningMakePrevious(ctx, a.ID); err != nil {
			return err
		}
	}
	if _, err := qs.SigningAnnounce(ctx, i.ID); err != nil {
		return err
	}
	cur, hasCur := l.inState(signing.StateCurrent)
	prev := int64(0)
	switch {
	case !hasCur:
		if _, err := qs.SigningMakeCurrent(ctx, i.ID); err != nil {
			return err
		}
	case s.o.AnnouncePeriod <= 0:
		if _, err := qs.SigningMakePrevious(ctx, cur.ID); err != nil {
			return err
		}
		if _, err := qs.SigningMakeCurrent(ctx, i.ID); err != nil {
			return err
		}
		prev = cur.ID
	}
	return s.installed(ctx, qs, tx, SystemActor(), i.ID, prev)
}

func without(all []signing.Identity, id int64) []signing.Identity {
	out := make([]signing.Identity, 0, len(all))
	for _, i := range all {
		if i.ID != id {
			out = append(out, i)
		}
	}
	return out
}

// RecordRefusal records a pair of the files that failed R-SIG-045 (R-SIG-046): the admin page shows it, and signing.identity.refused is
// recorded once per content (contentSHA, the SHA-256 of the three files). The identity in use keeps signing.
func (s *Store) RecordRefusal(ctx context.Context, contentSHA [32]byte, r *signing.Refusal, fingerprint string) error {
	var fresh bool
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		n, err := qs.SigningSetRefused(ctx, q.SigningSetRefusedParams{
			ContentSha256: contentSHA[:], Reason: bounded(r.Detail+" ("+r.Check+")", 1000), Fingerprint: fingerprint,
		})
		fresh = n == 1
		return err
	})
	if err == nil && fresh {
		s.o.Audit.Record(ctx, refusedEvent(r, fingerprint))
	}
	return err
}

// ConfigurationGone lets the identity of the files go once the values name none (R-SIG-046): a current or announced identity of the
// configuration becomes previous, and the newest previous upload with at least 24 hours of validity left is announced again, current at
// once when none is current. Without one Quader signs nothing from then on. The worker calls it at its start when
// signing.identity.cert_file is empty.
func (s *Store) ConfigurationGone(ctx context.Context) (changed bool, err error) {
	err = s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		l, err := lockAll(ctx, qs)
		if err != nil {
			return err
		}
		for _, i := range l.all {
			if i.Source != signing.SourceConfiguration || (i.State != signing.StateCurrent && i.State != signing.StateAnnounced) {
				continue
			}
			if _, err := qs.SigningMakePrevious(ctx, i.ID); err != nil {
				return err
			}
			changed = true
			if err := s.notify(ctx, tx, i.ID, signing.StatePrevious); err != nil {
				return err
			}
		}
		if !changed {
			return nil
		}
		if l, err = lockAll(ctx, qs); err != nil {
			return err
		}
		if _, ok := l.inState(signing.StateCurrent); ok {
			return s.wake(ctx, tx)
		}
		var back *signing.Identity
		for _, i := range l.all {
			if i.Source == signing.SourceUpload && i.State == signing.StatePrevious && i.NotAfter.Sub(l.now) >= signing.MinRemaining {
				if back == nil || i.ID > back.ID {
					c := i
					back = &c
				}
			}
		}
		if back == nil {
			return s.wake(ctx, tx)
		}
		return s.bringBack(ctx, qs, tx, l, *back)
	})
	return changed, err
}

// SwitchDue switches an announced identity to current once its announce period has passed, or at once when no identity is current
// (R-SIG-048): in one transaction that locks every identity exclusively, so it waits for each signing transaction that holds the current
// one; the current becomes previous with expires_at = clock_timestamp(). next is when the announced identity is due, zero when none is
// announced or it switched.
func (s *Store) SwitchDue(ctx context.Context) (switched bool, next time.Time, err error) {
	err = s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		l, err := lockAll(ctx, qs)
		if err != nil {
			return err
		}
		ann, ok := l.inState(signing.StateAnnounced)
		if !ok {
			return nil
		}
		cur, hasCur := l.inState(signing.StateCurrent)
		due := ann.AnnouncedAt.Add(s.o.AnnouncePeriod)
		if hasCur && l.now.Before(due) {
			next = due
			return nil
		}
		prev := int64(0)
		if hasCur {
			if _, err := qs.SigningMakePrevious(ctx, cur.ID); err != nil {
				return err
			}
			prev = cur.ID
			if err := s.notify(ctx, tx, cur.ID, signing.StatePrevious); err != nil {
				return err
			}
		}
		if _, err := qs.SigningMakeCurrent(ctx, ann.ID); err != nil {
			return err
		}
		if err := s.o.Audit.RecordTx(ctx, tx, switchedEvent(SystemActor(), ann.ID, prev)); err != nil {
			return err
		}
		switched = true
		return s.notify(ctx, tx, ann.ID, signing.StateCurrent)
	})
	return switched, next, err
}

// HoldCurrent is a signing transaction's hold on the identity it signs with (R-SIG-048): a shared lock on its row while it is current.
// ok is false when the identity is not current any more: the caller writes no signature and leaves the work due. notAfter is the
// certificate's end; an expired identity signs nothing (R-SIG-045).
func HoldCurrent(ctx context.Context, tx meta.Tx, id int64) (notAfter time.Time, ok bool, err error) {
	qs, err := queries(tx)
	if err != nil {
		return time.Time{}, false, err
	}
	rows, err := qs.SigningLockCurrentShared(ctx, id)
	if err != nil || len(rows) == 0 {
		return time.Time{}, false, err
	}
	return parseTime(rows[0].NotAfter), true, nil
}

// Fingerprint is the lower-case hex SHA-256 of a certificate's DER, as the event and the API name an identity.
func Fingerprint(der []byte) string {
	if len(der) == 0 {
		return ""
	}
	s := sumOf(der)
	return hex.EncodeToString(s[:])
}
