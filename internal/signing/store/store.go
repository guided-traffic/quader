// Package store is the PostgreSQL store of Quader's signing identity and of the job signing's work (signatures.md R-SIG-045 to
// R-SIG-049, R-SIG-055, R-SIG-057, R-SIG-059): the one door to signing_identities, signing_work and signing_settings.
//
//   - The management API creates a pending key, takes the CA's certificate for it, imports a key and its chain, discards a pending key,
//     removes the current identity and retires a previous one (R-SIG-047, R-SIG-048). Every role may read the identities.
//   - The worker registers the identity of the files of the Helm values, records a refused pair, lets the identity of the files go once
//     the values name none (R-SIG-046), and switches an announced identity after its announce period (R-SIG-048).
//   - The packages that sign enqueue work in the transaction of a commit; the job takes it by priority and time (R-SIG-049, work.go).
//
// Every write of an identity takes the lock of every identity that is not retired first (SigningLockIdentities), so the changes run one
// after the other; it records its audit event in its transaction (with the journal entry of a removal or a retirement, R-SIG-057),
// announces the change area signing_identities (R-SIG-059) and wakes the job. The store seals an uploaded key (internal/signing/key)
// and never opens one.
package store

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/errs"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
	"github.com/guided-traffic/quader/internal/secrets"
	"github.com/guided-traffic/quader/internal/signing"
	"github.com/guided-traffic/quader/internal/signing/key"
)

// The codes of the management API's refusals (R-SIG-055, Q-SIG-146), registered with their problem types in internal/errs.
const (
	// CodeManaged: the Helm values name the identity; uploads, key requests and removals are refused (409, R-SIG-046).
	CodeManaged = errs.CodeSigningIdentityManaged
	// CodePending: a pending key exists; discard it first (409, R-SIG-055).
	CodePending = errs.CodeSigningIdentityPending
	// CodeKeyMismatch: the certificate is not for the pending key (422, R-SIG-047).
	CodeKeyMismatch = errs.CodeSigningKeyMismatch
	// CodeNoPending: a certificate was uploaded while no key is pending (409).
	CodeNoPending = errs.CodeSigningNoPendingKey
	// CodeAnnounced: an identity is announced and becomes current within the announce period; an upload waits for it (409).
	CodeAnnounced = errs.CodeSigningIdentityAnnounced
	// CodeNotPrevious: only a previous identity can be retired (409).
	CodeNotPrevious = errs.CodeSigningIdentityNotPrevious
	// CodeKeyInUse: the identity's key is also the key of the current or the announced identity; retiring it would withdraw nothing (409).
	CodeKeyInUse = errs.CodeSigningKeyInUse
	// CodeExists: the certificate is the certificate of a stored identity (409).
	CodeExists = errs.CodeSigningIdentityExists
)

// Notifier announces a change area in a transaction; *cluster.Changes implements it.
type Notifier interface {
	Notify(tx meta.Tx, area string, version int, payload any) error
}

// Options configure [New].
type Options struct {
	// Runner runs the store's transactions.
	Runner meta.Runner
	// Ring seals an uploaded or imported key (R-SIG-047); nil refuses both.
	Ring *secrets.KeyRing
	// Audit records the events of R-SIG-057; nil records nothing.
	Audit audit.Recorder
	// Changes announces the change area signing_identities in every write's transaction (R-SIG-059); nil announces nothing.
	Changes Notifier
	// Wake wakes the job signing in a transaction; nil wakes nothing.
	Wake func(ctx context.Context, tx meta.Tx) error
	// AnnouncePeriod is signing.identity.announce_period: how long a new identity beside a current one is listed before it signs.
	AnnouncePeriod time.Duration
	// BaseURL is server.base_url, the default URI of a key request (R-SIG-047).
	BaseURL string
}

// Store is the store.
type Store struct{ o Options }

// New creates the store.
func New(o Options) *Store {
	if o.Audit == nil {
		o.Audit = audit.Nop{}
	}
	return &Store{o: o}
}

// AnnouncePeriod is the announce period the store installs with.
func (s *Store) AnnouncePeriod() time.Duration { return s.o.AnnouncePeriod }

var errForeignTx = errors.New("signing: not a PostgreSQL transaction")

func queries(tx meta.Tx) (*q.Queries, error) {
	ptx, ok := postgres.Unwrap(tx)
	if !ok {
		return nil, errForeignTx
	}
	return q.New(ptx), nil
}

func (s *Store) in(ctx context.Context, fn func(qs *q.Queries, tx meta.Tx) error) error {
	if s.o.Runner == nil {
		return errors.New("signing: the store has no runner")
	}
	return s.o.Runner.InTx(ctx, func(tx meta.Tx) error {
		qs, err := queries(tx)
		if err != nil {
			return err
		}
		return fn(qs, tx)
	})
}

// ---- reads

// Identities answers every identity in every state, oldest first, without private parts.
func (s *Store) Identities(ctx context.Context) ([]signing.Identity, error) {
	var out []signing.Identity
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		var err error
		out, err = identities(ctx, qs)
		return err
	})
	return out, err
}

func identities(ctx context.Context, qs *q.Queries) ([]signing.Identity, error) {
	rows, err := qs.SigningIdentities(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]signing.Identity, 0, len(rows))
	for _, r := range rows {
		out = append(out, identityOf(r))
	}
	return out, nil
}

func identityOf(r q.SigningIdentitiesRow) signing.Identity {
	i := signing.Identity{
		ID: r.ID, Source: r.Source, State: r.State, PublicKey: r.PublicKey, NPMKeyID: r.NpmKeyid, CSR: r.Csr,
		Certificate: r.Certificate, Chain: r.Chain, Subject: r.Subject, Issuer: r.Issuer, Names: r.Names,
		NotBefore: parseTime(r.NotBefore), NotAfter: parseTime(r.NotAfter), AnnouncedAt: parseTime(r.AnnouncedAt),
		CurrentAt: parseTime(r.CurrentAt), ExpiresAt: parseTime(r.ExpiresAt), RetiredAt: parseTime(r.RetiredAt),
		CreatedAt: parseTime(r.CreatedAt), CreatedBy: r.CreatedByName,
	}
	copy(i.KeySHA256[:], r.KeySha256)
	copy(i.CertSHA256[:], r.CertSha256)
	return i
}

// Overview is what the API's GET /api/v1/signing/identity answers (R-SIG-055): the identities, the last refusal of the files and the
// backlog.
type Overview struct {
	Identities []signing.Identity
	Refused    *Refusal
	Backlog    []Backlog
}

// Refusal is the last pair of the files that failed R-SIG-045 (R-SIG-046), until a pair passes.
type Refusal struct {
	Reason      string
	Fingerprint string
	At          time.Time
}

// Overview reads the identities, the refusal and the backlog in one transaction.
func (s *Store) Overview(ctx context.Context) (Overview, error) {
	var out Overview
	err := s.in(ctx, func(qs *q.Queries, _ meta.Tx) error {
		ids, err := identities(ctx, qs)
		if err != nil {
			return err
		}
		out.Identities = ids
		st, err := qs.SigningSettings(ctx)
		if err != nil {
			return err
		}
		if st.RefusedReason != "" {
			out.Refused = &Refusal{Reason: st.RefusedReason, Fingerprint: st.RefusedFingerprint, At: parseTime(st.RefusedAt)}
		}
		out.Backlog, err = backlog(ctx, qs)
		return err
	})
	return out, err
}

// ---- the management API's writes (R-SIG-047, R-SIG-048)

// locked is every identity that is not retired, read after SigningLockIdentities took their locks.
type locked struct {
	all  []signing.Identity
	now  time.Time
	byID map[int64]signing.Identity
}

func (l locked) inState(state string) (signing.Identity, bool) {
	for _, i := range l.all {
		if i.State == state {
			return i, true
		}
	}
	return signing.Identity{}, false
}

func lockAll(ctx context.Context, qs *q.Queries) (locked, error) {
	if err := qs.SigningLockSettings(ctx); err != nil {
		return locked{}, err
	}
	if _, err := qs.SigningLockIdentities(ctx); err != nil {
		return locked{}, err
	}
	nowText, err := qs.SigningNow(ctx)
	if err != nil {
		return locked{}, err
	}
	ids, err := identities(ctx, qs)
	if err != nil {
		return locked{}, err
	}
	l := locked{now: parseTime(nowText), byID: map[int64]signing.Identity{}}
	for _, i := range ids {
		if i.State != signing.StateRetired {
			l.all = append(l.all, i)
			l.byID[i.ID] = i
		}
	}
	return l, nil
}

// refuseManaged answers 409 signing_identity_managed while the configuration names files or an identity of the files is current or
// announced: Helm wins (R-SIG-046, Q-SIG-54).
func refuseManaged(l locked, managed bool) error {
	if !managed {
		for _, i := range l.all {
			if i.Source == signing.SourceConfiguration && (i.State == signing.StateCurrent || i.State == signing.StateAnnounced) {
				managed = true
			}
		}
	}
	if managed {
		return errs.E(errs.Conflict, CodeManaged, "The Helm values name the signing identity (signing.identity.existingSecret or signing.identity.certManager); "+
			"it changes with the values, and uploads are refused while they name one.")
	}
	return nil
}

// CreateKey makes a pending key for a key request (R-SIG-047): an ECDSA P-256 key, sealed under the key ring's active key for its row in
// the transaction that writes the row, and the PKCS#10 request for the subject and alternative names. The plaintext key is forgotten
// when the call returns. 409 signing_identity_managed while the Helm values name an identity, 409 signing_identity_pending while a key
// is pending.
func (s *Store) CreateKey(ctx context.Context, actor audit.Actor, req signing.KeyRequest, managed bool) (signing.Identity, error) {
	req, err := req.Normalized(s.o.BaseURL)
	if err != nil {
		return signing.Identity{}, err
	}
	if s.o.Ring == nil {
		return signing.Identity{}, errs.E(errs.Unavailable, errs.CodeUnavailable, "The key ring is not loaded on this replica.")
	}
	k, err := signing.NewKey()
	if err != nil {
		return signing.Identity{}, err
	}
	csr, err := signing.CertificateRequest(k, req)
	if err != nil {
		return signing.Identity{}, err
	}
	pub, keySHA, npmID, err := publicParts(&k.PublicKey)
	if err != nil {
		return signing.Identity{}, err
	}
	var id int64
	err = s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		l, err := lockAll(ctx, qs)
		if err != nil {
			return err
		}
		if err := refuseManaged(l, managed); err != nil {
			return err
		}
		if _, ok := l.inState(signing.StatePending); ok {
			return errs.E(errs.Conflict, CodePending, "A signing key is pending: upload the certificate the CA issued for it, or discard it first.")
		}
		if id, err = qs.SigningNextID(ctx); err != nil {
			return err
		}
		sealed, err := key.Seal(s.o.Ring, id, k)
		if err != nil {
			return err
		}
		if err := qs.SigningInsertIdentity(ctx, q.SigningInsertIdentityParams{
			ID: id, Source: signing.SourceUpload, State: signing.StatePending, PublicKey: pub, KeySha256: keySHA[:], NpmKeyid: npmID,
			SealedKey: sealed, Csr: csr, Chain: [][]byte{}, Names: names(req.URIs, req.Emails), CreatedBy: principalOf(actor),
		}); err != nil {
			return err
		}
		if err := s.o.Audit.RecordTx(ctx, tx, keyCreatedEvent(actor, id, req, keySHA)); err != nil {
			return err
		}
		return s.notify(ctx, tx, id, signing.StatePending)
	})
	if err != nil {
		return signing.Identity{}, err
	}
	return s.identity(ctx, id)
}

// UploadCertificate takes the certificate the company's CA issued for the pending key, with its chain (R-SIG-047): checked by R-SIG-045
// and against the pending key's stored public key (422 signing_key_mismatch); the identity is announced, or current when none is.
func (s *Store) UploadCertificate(ctx context.Context, actor audit.Actor, chainPEM, caPEM []byte, managed bool) (signing.Identity, error) {
	chain, err := signing.ParseChain(chainPEM, caPEM)
	if err != nil {
		return signing.Identity{}, invalid("/chain", err)
	}
	var id int64
	err = s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		l, err := lockAll(ctx, qs)
		if err != nil {
			return err
		}
		if err := refuseManaged(l, managed); err != nil {
			return err
		}
		p, ok := l.inState(signing.StatePending)
		if !ok {
			return errs.E(errs.Conflict, CodeNoPending, "No signing key is pending: create a signing key first, then upload the certificate the CA issued for its request.")
		}
		pub, err := x509.ParsePKIXPublicKey(p.PublicKey)
		if err != nil {
			return fmt.Errorf("the pending key's public key: %w", err)
		}
		checked, err := chain.Check(pub, l.now)
		if err != nil {
			if r := signing.RefusalOf(err); r != nil && r.Check == signing.CheckPair {
				return errs.E(errs.Unprocessable, CodeKeyMismatch, "The certificate is not for the pending key: its public key differs from the key of the certificate request.")
			}
			return invalid("/chain", err)
		}
		if err := refuseAnnounced(l); err != nil {
			return err
		}
		id = p.ID
		state, prev := s.placement(l)
		c := certPartsOf(checked)
		n, err := qs.SigningInstallCertificate(ctx, q.SigningInstallCertificateParams{
			ID: id, State: state, Certificate: c.leaf, CertSha256: c.sha[:], Chain: c.chain, Subject: c.subject, Issuer: c.issuer,
			Names: c.names, NotBefore: stamp(c.notBefore), NotAfter: stamp(c.notAfter),
		})
		if err != nil {
			return mapUnique(err)
		}
		if n != 1 {
			return errs.E(errs.Conflict, CodeNoPending, "No signing key is pending.")
		}
		return s.installed(ctx, qs, tx, actor, id, prev)
	})
	if err != nil {
		return signing.Identity{}, err
	}
	return s.identity(ctx, id)
}

// Import takes a key and its chain for a CA that hands out both, or a key moved from another installation (R-SIG-047): the key is read
// with its passphrase once (signing.ParseKey), checked with the chain by R-SIG-045, sealed under the key ring, and the identity is
// announced, or current when none is.
func (s *Store) Import(ctx context.Context, actor audit.Actor, keyPEM, passphrase, chainPEM, caPEM []byte, managed bool) (signing.Identity, error) {
	if s.o.Ring == nil {
		return signing.Identity{}, errs.E(errs.Unavailable, errs.CodeUnavailable, "The key ring is not loaded on this replica.")
	}
	k, err := signing.ParseKey(keyPEM, passphrase)
	if err != nil {
		return signing.Identity{}, invalid("/key", err)
	}
	chain, err := signing.ParseChain(chainPEM, caPEM)
	if err != nil {
		return signing.Identity{}, invalid("/chain", err)
	}
	pub, keySHA, npmID, err := publicParts(&k.PublicKey)
	if err != nil {
		return signing.Identity{}, err
	}
	var id int64
	err = s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		l, err := lockAll(ctx, qs)
		if err != nil {
			return err
		}
		if err := refuseManaged(l, managed); err != nil {
			return err
		}
		checked, err := chain.Check(&k.PublicKey, l.now)
		if err != nil {
			return invalid("/chain", err)
		}
		if err := refuseAnnounced(l); err != nil {
			return err
		}
		if id, err = qs.SigningNextID(ctx); err != nil {
			return err
		}
		sealed, err := key.Seal(s.o.Ring, id, k)
		if err != nil {
			return err
		}
		state, prev := s.placement(l)
		c := certPartsOf(checked)
		if err := qs.SigningInsertIdentity(ctx, q.SigningInsertIdentityParams{
			ID: id, Source: signing.SourceUpload, State: state, PublicKey: pub, KeySha256: keySHA[:], NpmKeyid: npmID, SealedKey: sealed,
			Certificate: c.leaf, CertSha256: c.sha[:], Chain: c.chain, Subject: c.subject, Issuer: c.issuer, Names: c.names,
			NotBefore: stamp(c.notBefore), NotAfter: stamp(c.notAfter), CreatedBy: principalOf(actor),
		}); err != nil {
			return mapUnique(err)
		}
		return s.installed(ctx, qs, tx, actor, id, prev)
	})
	if err != nil {
		return signing.Identity{}, err
	}
	return s.identity(ctx, id)
}

// DiscardPending removes the pending key and its request (R-SIG-047); 404 when none is pending.
func (s *Store) DiscardPending(ctx context.Context, _ audit.Actor) error {
	return s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		l, err := lockAll(ctx, qs)
		if err != nil {
			return err
		}
		p, ok := l.inState(signing.StatePending)
		if !ok {
			return errs.E(errs.NotFound, errs.CodeNotFound, "No signing key is pending.")
		}
		if _, err := qs.SigningDeletePending(ctx); err != nil {
			return err
		}
		return s.notify(ctx, tx, p.ID, "discarded")
	})
}

// Remove makes the current uploaded identity previous (R-SIG-048): with no identity current, no key and no npm signature of Quader is on
// the wire. An announced identity then becomes current at the job's next run. 409 signing_identity_managed for an identity of the files,
// 404 when none is current. Records signing.identity.removed with its journal entry.
func (s *Store) Remove(ctx context.Context, actor audit.Actor, managed bool) error {
	return s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		l, err := lockAll(ctx, qs)
		if err != nil {
			return err
		}
		cur, ok := l.inState(signing.StateCurrent)
		if !ok {
			return errs.E(errs.NotFound, errs.CodeNotFound, "No signing identity is current.")
		}
		if cur.Source == signing.SourceConfiguration || managed {
			return errs.E(errs.Conflict, CodeManaged, "The signing identity comes from the Helm values: take it out of the values to remove it.")
		}
		if _, err := qs.SigningMakePrevious(ctx, cur.ID); err != nil {
			return err
		}
		if err := s.o.Audit.RecordTx(ctx, tx, removedEvent(actor, cur)); err != nil {
			return err
		}
		if err := s.notify(ctx, tx, cur.ID, signing.StatePrevious); err != nil {
			return err
		}
		return s.wake(ctx, tx)
	})
}

// Retire withdraws a previous identity, for a key believed taken (R-SIG-048): its key leaves npm's keys at once, none of its signatures
// is served, and its sealed key is dropped (Q-SIG-149). 409 signing_identity_not_previous for another state, 409 signing_key_in_use
// when the current or the announced identity has the same key. Records signing.identity.retired with its journal entry and wakes the job,
// whose packages sign again what the identity signed.
func (s *Store) Retire(ctx context.Context, actor audit.Actor, id int64) (signing.Identity, error) {
	err := s.in(ctx, func(qs *q.Queries, tx meta.Tx) error {
		l, err := lockAll(ctx, qs)
		if err != nil {
			return err
		}
		cur, ok := l.byID[id]
		if !ok {
			if retired, err := s.retiredExists(ctx, qs, id); err != nil || retired {
				if err == nil {
					err = errs.E(errs.Conflict, CodeNotPrevious, "The signing identity is retired already.")
				}
				return err
			}
			return errs.E(errs.NotFound, errs.CodeNotFound, "The signing identity does not exist.")
		}
		if cur.State != signing.StatePrevious {
			return errs.E(errs.Conflict, CodeNotPrevious, "Only a previous signing identity can be retired; this one is "+cur.State+".")
		}
		for _, o := range l.all {
			if o.ID != id && (o.State == signing.StateCurrent || o.State == signing.StateAnnounced) && o.KeySHA256 == cur.KeySHA256 {
				return errs.E(errs.Conflict, CodeKeyInUse, "The "+o.State+" signing identity has the same key: retiring this certificate would withdraw nothing. "+
					"Replace the current identity with one of a new key first.")
			}
		}
		count, err := qs.SigningSignatureCount(ctx, id)
		if err != nil {
			return err
		}
		if _, err := qs.SigningRetire(ctx, id); err != nil {
			return err
		}
		if err := s.o.Audit.RecordTx(ctx, tx, retiredEvent(actor, cur, count)); err != nil {
			return err
		}
		if err := s.notify(ctx, tx, id, signing.StateRetired); err != nil {
			return err
		}
		return s.wake(ctx, tx)
	})
	if err != nil {
		return signing.Identity{}, err
	}
	return s.identity(ctx, id)
}

func (s *Store) retiredExists(ctx context.Context, qs *q.Queries, id int64) (bool, error) {
	ids, err := identities(ctx, qs)
	if err != nil {
		return false, err
	}
	for _, i := range ids {
		if i.ID == id {
			return i.State == signing.StateRetired, nil
		}
	}
	return false, nil
}

// ---- placement and the shared steps of an installation

// refuseAnnounced answers 409 signing_identity_announced while an identity waits for its switch: an upload or an import waits for it
// (Q-SIG-146).
func refuseAnnounced(l locked) error {
	if a, ok := l.inState(signing.StateAnnounced); ok {
		return errs.E(errs.Conflict, CodeAnnounced, "A signing identity is announced and becomes current within the announce period (identity "+
			strconv.FormatInt(a.ID, 10)+"); upload the next one after the switch.")
	}
	return nil
}

// placement is the state a new identity gets (R-SIG-048): announced beside a current one, current when none is or the announce period
// is 0; prev is the current identity a period of 0 makes previous at once, 0 for none.
func (s *Store) placement(l locked) (state string, prev int64) {
	cur, ok := l.inState(signing.StateCurrent)
	switch {
	case !ok:
		return signing.StateCurrent, 0
	case s.o.AnnouncePeriod <= 0:
		return signing.StateCurrent, cur.ID
	}
	return signing.StateAnnounced, 0
}

// installed finishes an installation in its transaction: the current identity a period of 0 replaced becomes previous (its switch), the
// event signing.identity.installed (and signing.identity.switched) is recorded, the change area is announced and the job is woken.
func (s *Store) installed(ctx context.Context, qs *q.Queries, tx meta.Tx, actor audit.Actor, id, prev int64) error {
	if prev != 0 {
		if _, err := qs.SigningMakePrevious(ctx, prev); err != nil {
			return err
		}
	}
	ids, err := identities(ctx, qs)
	if err != nil {
		return err
	}
	for _, i := range ids {
		if i.ID == id {
			if err := s.o.Audit.RecordTx(ctx, tx, installedEvent(actor, i)); err != nil {
				return err
			}
			if prev != 0 {
				if err := s.o.Audit.RecordTx(ctx, tx, switchedEvent(actor, id, prev)); err != nil {
					return err
				}
			}
			if err := s.notify(ctx, tx, id, i.State); err != nil {
				return err
			}
			return s.wake(ctx, tx)
		}
	}
	return fmt.Errorf("the installed identity %d is gone", id)
}

func (s *Store) identity(ctx context.Context, id int64) (signing.Identity, error) {
	ids, err := s.Identities(ctx)
	if err != nil {
		return signing.Identity{}, err
	}
	for _, i := range ids {
		if i.ID == id {
			return i, nil
		}
	}
	return signing.Identity{}, errs.E(errs.NotFound, errs.CodeNotFound, "The signing identity does not exist.")
}

// notify announces the change area signing_identities (R-SIG-059).
func (s *Store) notify(_ context.Context, tx meta.Tx, id int64, state string) error {
	if s.o.Changes == nil {
		return nil
	}
	if err := s.o.Changes.Notify(tx, cluster.AreaSigningIdentities, 1, map[string]any{"id": id, "state": state}); err != nil {
		return fmt.Errorf("announce the change of the signing identities: %w", err)
	}
	return nil
}

func (s *Store) wake(ctx context.Context, tx meta.Tx) error {
	if s.o.Wake == nil {
		return nil
	}
	return s.o.Wake(ctx, tx)
}

// ---- helpers

// certParts are the columns of an installed certificate.
type certParts struct {
	leaf                []byte
	sha                 [32]byte
	chain               [][]byte
	subject, issuer     string
	names               []string
	notBefore, notAfter time.Time
}

func certPartsOf(c signing.Checked) certParts {
	p := certParts{
		leaf: c.Leaf.Raw, sha: sumOf(c.Leaf.Raw), subject: bounded(c.Leaf.Subject.String(), 1024), issuer: bounded(c.Leaf.Issuer.String(), 1024),
		notBefore: c.Leaf.NotBefore, notAfter: c.Leaf.NotAfter, chain: make([][]byte, 0, len(c.Chain)),
	}
	for _, x := range c.Chain {
		p.chain = append(p.chain, x.Raw)
	}
	uris := make([]string, 0, len(c.Leaf.URIs))
	for _, u := range c.Leaf.URIs {
		uris = append(uris, u.String())
	}
	p.names = names(uris, c.Leaf.EmailAddresses)
	return p
}

func names(uris, emails []string) []string {
	out := make([]string, 0, len(uris)+len(emails))
	out = append(out, uris...)
	out = append(out, emails...)
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

func bounded(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// publicParts are the public key's DER, its SHA-256 and npm's keyid.
func publicParts(pub *ecdsa.PublicKey) ([]byte, [32]byte, string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, [32]byte{}, "", err
	}
	id, err := signing.NPMKeyID(pub)
	if err != nil {
		return nil, [32]byte{}, "", err
	}
	return der, sumOf(der), id, nil
}

// invalid answers material refused by R-SIG-045 as 422 validation_failed with the member and the check (R-SIG-055).
func invalid(pointer string, err error) error {
	r := signing.RefusalOf(err)
	if r == nil {
		return err
	}
	return errs.E(errs.Invalid, errs.CodeValidation, "The signing identity's material fails a check of R-SIG-045 ("+r.Check+").",
		errs.WithFields(errs.FieldError{Pointer: pointer, Code: signing.CodeInvalidValue, Detail: r.Detail + " (" + r.Check + ")"}))
}

// mapUnique answers the certificate of a stored identity as 409 signing_identity_exists.
func mapUnique(err error) error {
	if postgres.IsState(err, "23505") {
		return errs.E(errs.Conflict, CodeExists, "The certificate is the certificate of a stored signing identity.")
	}
	return err
}

func sumOf(b []byte) [32]byte { return sha256.Sum256(b) }

func principalOf(a audit.Actor) string {
	if len(a.ID) == 36 {
		return a.ID
	}
	return ""
}

func parseTime(text string) time.Time {
	if text == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
