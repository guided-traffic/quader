//go:build integration

package limits

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/quader/internal/wellknown"
	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/npmstub"
	"github.com/guided-traffic/quader/test/npmstub/corpus"
)

// virtWellKnownRow writes an active preset repository of the project upstream with its row of upstream_repository in one transaction,
// as the reconcile of quader migrate does (R-REPO-066: the deferred guard AI010 refuses the one without the other), and returns its ID.
// A repository the installation holds already (a reconcile that ran) takes the document and is switched on.
func virtWellKnownRow(t *testing.T, st *testenv.Stack, key, url string) int64 {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, testenv.PgDSN(t, st.Database()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id int64
	cfg := fmt.Sprintf(`{"remote":{"url":%q,"outboundProxy":{"mode":"direct"},"cache":{"metadataTtlSeconds":3600}}}`, url)
	err = tx.QueryRow(ctx, `SELECT id FROM quader.repositories WHERE project_id = $1::uuid AND key = $2`, wellknown.ProjectID, key).Scan(&id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := tx.QueryRow(ctx, `INSERT INTO quader.repositories (key, class, format, config, project_id, visibility_artifacts, visibility_wiki)
			VALUES ($1, 'remote', 'npm', $2::text::jsonb, $3::uuid, 'internal', 'internal') RETURNING id`, key, cfg, wellknown.ProjectID).Scan(&id); err != nil {
			t.Fatalf("insert %s: %v", key, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO quader.upstream_repository (repository_id, kind, first_switched_on_at) VALUES ($1, 'preset', now())`, id); err != nil {
			t.Fatal(err)
		}
	case err != nil:
		t.Fatal(err)
	default:
		if _, err := tx.Exec(ctx, `UPDATE quader.repositories SET config = $2::text::jsonb, visibility_artifacts = 'internal', visibility_wiki = 'internal',
			config_version = config_version + 1 WHERE id = $1`, id, cfg); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE quader.upstream_repository SET first_switched_on_at = COALESCE(first_switched_on_at, now()), retired_at = NULL
			WHERE repository_id = $1`, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestR_LIM_027_AWellKnownMemberServedThroughATeamsVirtualRepositoryRecordsTheVirtualRepository: a tarball a user fetches through the
// virtual repository npm-all of the default project, served by the well-known repository upstream/npmjs, is recorded under the serving
// repository with via_repository_id naming npm-all (Q-LIM-63), so the usage of npm-all's project holds it (R-LIM-029) and the central
// repository's own rows keep it as well; one 1 KiB tarball of the test upstream.
func TestR_LIM_027_AWellKnownMemberServedThroughATeamsVirtualRepositoryRecordsTheVirtualRepository(t *testing.T) {
	stub, err := npmstub.New(npmstub.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ss, err := npmstub.NewSelfSigned("localhost", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	run, err := stub.Start("127.0.0.1:0", ss.ServerConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = run.Close() })
	ca := filepath.Join(t.TempDir(), "stub-ca.pem")
	if err := os.WriteFile(ca, ss.CAPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	var npmjs, all int64
	e := newEnvWith(t, envOptions{valkey: true, env: []string{
		"QUADER_PROXY_OUTBOUND_TLS_CA_FILES=[" + strconv.Quote(ca) + "]", `QUADER_PROXY_OUTBOUND_ADDRESS_POLICY_ALLOW_CIDRS=["127.0.0.1/32"]`,
	}, readable: []string{"npm-all"}, seed: func(st *testenv.Stack) {
		npmjs = virtWellKnownRow(t, st, "npmjs", run.URL+"/")
		st.Repository("npm-local", "local", "npm", `{"local":{"autoClaim":false}}`)
		st.Repository("npm-all", "virtual", "npm", `{}`)
		all = st.Number(`SELECT id FROM repositories WHERE key = 'npm-all'`)
		for i, key := range []string{"npm-local", "npmjs"} {
			st.Exec(`INSERT INTO virtual_members (virtual_id, position, member_id)
				SELECT $1, $2, r.id FROM repositories r JOIN project p ON p.id = r.project_id
				 WHERE r.key = $3 AND p.key = ANY(ARRAY[$4, $5])`, all, i, key, testenv.DefaultProject, wellknown.ProjectKey)
		}
	}})
	var p *corpus.Package
	for _, c := range corpus.Default().Packages {
		if !strings.HasPrefix(c.Name, "@") && len(c.Versions) > 0 {
			p = c
			break
		}
	}
	v := p.Versions[0].Version
	path := "/api/npm/" + testenv.DefaultProject + "/npm-all/" + p.Name + "/-/" + corpus.TarballFile(p.Name, v)
	if a := e.st.Do(e.r, "GET", path, e.reader, nil); a.Status != 200 || a.Header.Get("Quader-Resolved-From") != "upstream/npmjs" {
		t.Fatalf("the tarball through npm-all: %d from %q", a.Status, a.Header.Get("Quader-Resolved-From"))
	}
	testenv.WaitFor(t, "the usage row of the fetch through npm-all", 10*time.Second, func() bool {
		n, err := e.st.TryNumber(`SELECT count(*) FROM usage_bucket WHERE granularity = '5m' AND subject_ref = $1
			AND repository_id = $2 AND via_repository_id = $3 AND requests >= 1 AND uncached_bytes > 0`, e.readerPrincipl, npmjs, all)
		return err == nil && n == 1
	})
	if n := e.st.Number(`SELECT count(*) FROM usage_bucket WHERE subject_ref = $1 AND repository_id = $2 AND via_repository_id IS NULL`,
		e.readerPrincipl, npmjs); n != 0 {
		t.Fatalf("%d rows of upstream/npmjs without the virtual repository", n)
	}
	// The usage of npm-all, as its project's Owner reads it, holds what it took from upstream/npmjs (R-LIM-029).
	a := e.st.Do(e.r, "GET", "/api/v1/projects/"+testenv.DefaultProject+"/usage?kind=user&repository=npm-all", e.admin, nil)
	if a.Status != 200 {
		t.Fatalf("%d %s", a.Status, a.Body)
	}
	items, _ := a.JSON(t)["items"].([]any)
	found := false
	for _, it := range items {
		row, _ := it.(map[string]any)
		if sub, _ := row["subject"].(map[string]any); sub["name"] == "reader" {
			ub, _ := row["uncachedBytes"].(float64)
			found = ub > 0
		}
	}
	if !found {
		t.Fatalf("the usage of npm-all does not hold the reader's fetch through upstream/npmjs: %s", a.Body)
	}
}
