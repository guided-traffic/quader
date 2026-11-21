package store

import (
	"context"
	"encoding/hex"
	"regexp"
	"strconv"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// The replay of what withdrew an identity (R-SIG-059, R-AUTHZ-031's rule). A restore to t brings the identities back as they were at t;
// the replay re-applies what withdrew one after t, so a withdrawn identity does not come back:
//
//   - signing.identity.removed makes the identity of the id and certificate previous again, if the restore brought it back current or
//     announced;
//   - signing.identity.retired retires it again, whatever state the restore brought back, and drops its sealed key.
//
// An identity installed after t is lost with its rows; a pair of the files is registered again by the workers at their next look. The
// replay writes no audit event (Q-AUTHZ-232).

func init() {
	journal.Register(journal.Handler{Domain: "signing", Events: []string{audit.EventSigningRemoved}, Apply: replayRemoved})
	journal.Register(journal.Handler{Domain: "signing", Events: []string{audit.EventSigningRetired}, Apply: replayRetired})
}

var certFingerprint = regexp.MustCompile(`^[0-9a-f]{64}$`)

func replayRemoved(ctx context.Context, tx meta.Tx, env journal.Env, o journal.Object) (journal.Result, error) {
	var e RemovedEntry
	if !o.Into(&e) || e.ID <= 0 || !certFingerprint.MatchString(e.CertSHA256) {
		return o.Malformed("is not a signing.identity.removed entry"), nil
	}
	return replayWithdrawal(ctx, tx, env, o, e.ID, e.CertSHA256, func(qs *q.Queries, cert []byte) (int64, error) {
		return qs.SigningJournalRemove(ctx, q.SigningJournalRemoveParams{ID: e.ID, CertSha256: cert})
	})
}

func replayRetired(ctx context.Context, tx meta.Tx, env journal.Env, o journal.Object) (journal.Result, error) {
	var e RetiredEntry
	if !o.Into(&e) || e.ID <= 0 || !certFingerprint.MatchString(e.CertSHA256) {
		return o.Malformed("is not a signing.identity.retired entry"), nil
	}
	return replayWithdrawal(ctx, tx, env, o, e.ID, e.CertSHA256, func(qs *q.Queries, cert []byte) (int64, error) {
		return qs.SigningJournalRetire(ctx, q.SigningJournalRetireParams{ID: e.ID, CertSha256: cert})
	})
}

func replayWithdrawal(ctx context.Context, tx meta.Tx, env journal.Env, o journal.Object, id int64, fp string,
	apply func(qs *q.Queries, cert []byte) (int64, error),
) (journal.Result, error) {
	cert, err := hex.DecodeString(fp)
	if err != nil {
		return o.Malformed("names no certificate"), nil //nolint:nilerr // a broken entry is skipped, not an error of the replay
	}
	qs, err := queries(tx)
	if err != nil {
		return journal.Result{}, err
	}
	subject := "signing identity " + strconv.FormatInt(id, 10)
	if _, err := qs.SigningLockIdentities(ctx); err != nil {
		return journal.Result{}, err
	}
	n, err := apply(qs, cert)
	if err != nil {
		return journal.Result{}, err
	}
	if n == 0 {
		return o.Result(journal.AlreadyInEffect, subject), nil
	}
	if err := env.Notify(tx, cluster.AreaSigningIdentities, 1, map[string]any{"id": id, "state": "replayed"}); err != nil {
		return journal.Result{}, err
	}
	return o.Result(journal.Applied, subject), nil
}
