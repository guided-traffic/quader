//go:build integration

package ci

import (
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/ci"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
)

// R-CI-017, R-CI-030: the revocation of a mapping or an issuer runs while the issuer row is locked FOR UPDATE, which
// every exchange of the issuer waits for; it reads the unexpired issuance records only, through the expiry index, so
// its cost is the tokens of the last ci.token.max_ttl and not every record the retention keeps (a year by default).
func TestR_CI_017_ARevocationReadsOnlyTheUnexpiredRecords(t *testing.T) {
	const expired = 3000
	e := newCIEnv(t, true, "acme-release")
	m := e.maps["acme-release"]
	// A year of records whose tokens expired long ago, all listing the mapping.
	e.exec(`INSERT INTO ci_token_issuances (token_id, issuer_id, mapping_ids, mapping_names, subject_kind, project_id, job_id,
	        claims, grants, granted_scope, issued_at, expires_at)
	        SELECT gen_random_uuid(), $1::uuid, ARRAY[$2::uuid], '{acme-release}', 'id_token', '4711', 'job-' || g, '{}', '{}', '',
	               now() - make_interval(days => g % 360 + 2), now() - make_interval(days => g % 360 + 1)
	          FROM generate_series(1, $3::int) g`, e.issuerID, m.id, expired)
	// The other live tokens of the installation: so many that a plan driven by the live tokens costs more than one
	// driven by the issuer's records, as in an installation with many personal access tokens.
	e.exec(`INSERT INTO api_token (id, type, secret_sha256, hint, principal_id, description, scope, created_via, expires_at)
	        SELECT gen_random_uuid(), 'pat', sha256(('it-pat-' || g)::bytea), 'qdr_pat_…' || g, p.id, 'a token of ops', '{"inherit": true}',
	               'api', now() + interval '1 day'
	          FROM generate_series(1, 20000) g, principal p WHERE p.kind = 'user' AND p.name = 'ops'`)
	e.exec("ANALYZE ci_token_issuances")
	e.exec("ANALYZE api_token")
	a := e.web("web-a")
	// The tuples the revocation's own transaction read of ci_token_issuances: pg_stat_xact_user_tables counts the current
	// transaction's reads at once, so neither the delay with which a backend publishes its statistics nor a read of another
	// connection (the replica's own loops) is in the number.
	read := func(t *testing.T, tx meta.Tx) int64 {
		t.Helper()
		ptx, ok := postgres.Unwrap(tx)
		if !ok {
			t.Fatal("the transaction does not come from the PostgreSQL store")
		}
		var n int64
		if err := ptx.QueryRow(t.Context(), `SELECT coalesce(sum(seq_tup_read + coalesce(idx_tup_fetch, 0)), 0)::bigint
		        FROM pg_stat_xact_user_tables WHERE relname = 'ci_token_issuances'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	revoker := a.App.CIRevoker()
	_, db := a.App.CITokenIssuer()
	measure := func(t *testing.T, revoke func(meta.Tx) (ci.Revoked, error)) {
		t.Helper()
		tok := e.issue(a, e.bundle("4711", "acme/web", m.contribution(nil)), job{"900", "9"}, 10*time.Minute)
		var got ci.Revoked
		var tuples int64
		if err := db.InTx(t.Context(), func(tx meta.Tx) (err error) {
			before := read(t, tx)
			if got, err = revoke(tx); err != nil {
				return err
			}
			tuples = read(t, tx) - before
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if len(got.TokenIDs) != 1 || got.TokenIDs[0] != tok.id {
			t.Fatalf("revoked %v, want the one unexpired token", got.TokenIDs)
		}
		if tuples > expired/10 {
			t.Errorf("the revocation read %d tuples of ci_token_issuances; %d records are expired", tuples, expired)
		}
	}
	actor := ci.ConfigurationActor()
	t.Run("R-CI-017 a mapping's revocation", func(t *testing.T) {
		measure(t, func(tx meta.Tx) (ci.Revoked, error) {
			return revoker.RevokeMappingTx(t.Context(), tx, actor, ci.Mapping{
				ID: m.id, PrincipalID: m.principalID, IssuerID: e.issuerID, Issuer: ciIssuerName, Name: m.name, Tier: ci.TierSystem,
			}, ci.CauseMapping)
		})
	})
	t.Run("R-CI-017 an issuer's revocation", func(t *testing.T) {
		measure(t, func(tx meta.Tx) (ci.Revoked, error) {
			return revoker.RevokeIssuerTx(t.Context(), tx, actor, e.issuerID, ciIssuerName, ci.CauseIssuer)
		})
	})
}
