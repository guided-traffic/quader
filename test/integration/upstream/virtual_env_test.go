//go:build integration

package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/guided-traffic/quader/internal/auth/scope"
	"github.com/guided-traffic/quader/internal/authz"
	"github.com/guided-traffic/quader/internal/ci"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/journal"
	"github.com/guided-traffic/quader/internal/journal/replay"
	"github.com/guided-traffic/quader/internal/journal/ship"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/internal/wellknown"
	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/npmstub"
	"github.com/guided-traffic/quader/test/npmstub/corpus"
	"github.com/guided-traffic/quader/test/oidcstub"
)

// The installation of the cases of R-REPO-071, R-REPO-072 and R-AUTHZ-041 (virtual_members_test.go): the team's project web with
// npm-local and the virtual repository npm-all over npm-local and upstream/npmjs, a project mobile, the well-known repositories
// upstream/npmjs (npm, the counting test upstream of test/npmstub), upstream/docker-hub (OCI) and upstream/scanner-trivy (the system
// repository), and the people of the worked rows. The helpers are named virt… so that they meet nothing of the area's other files.

// virtEnv is the installation and its one web replica.
type virtEnv struct {
	t    *testing.T
	st   *testenv.Stack
	r    *testenv.Replica
	stub *npmstub.Server
	run  *npmstub.Running
	// pkg is a package of the test upstream that no member of the team holds.
	pkg *corpus.Package
	// The tokens: an administrator (admin scope), Ann (an internal user, reader in web), Erin (a user flagged external, reader in
	// web) and the service account web-deployer (a grant of read on npm-all).
	admin, ann, erin, deployer string
}

// virtWellKnownConfig is the stored document of a well-known remote repository whose upstream is url, disabled while it is not on.
func virtWellKnownConfig(url string, disabled bool) string {
	doc := map[string]any{
		"disabled": disabled,
		"remote": map[string]any{
			"url": url, "extraHosts": []any{}, "authMode": "none", "username": "", "password": nil, "clientCertificate": nil,
			"headers": []any{}, "forwardAuthOnRedirect": false, "outboundProxy": map[string]any{"mode": "direct"},
			"connectTimeoutMs": 5000, "readTimeoutMs": 15000, "sourceNetwork": nil, "cacheFillAction": "read",
			"cache": map[string]any{
				"storeArtifacts": true, "metadataTtlSeconds": 3600, "metadataRefreshTimeoutSeconds": 10, "notFoundTtlSeconds": 60,
				"maxStaleSeconds": 0, "unusedRetentionHours": 0,
			},
			"integrity":    map[string]any{"checksumPolicy": "verify-if-available", "blockUnexpectedContentTypes": true, "allowedContentTypes": []any{}},
			"availability": map[string]any{"mode": "online", "failureThreshold": 3, "circuitOpenSeconds": 30},
			"limits":       map[string]any{"maxArtifactBytes": nil, "maxMetadataBytes": nil, "maxConcurrentFetches": 16},
		},
	}
	b, _ := json.Marshal(doc)
	return string(b)
}

// virtWellKnown writes a repository of the project upstream with its row of upstream_repository in one transaction, as the reconcile
// of quader migrate does (R-REPO-066, R-REPO-067): the deferred guard AI010 refuses a commit of the one without the other. A
// repository the installation holds already (a reconcile that ran) takes the document, the level and the kind given. It returns the
// repository's ID.
func virtWellKnown(t *testing.T, st *testenv.Stack, key, format, kind, cfg, level string) int64 {
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
	err = tx.QueryRow(ctx, `SELECT id FROM quader.repositories WHERE project_id = $1::uuid AND key = $2`, wellknown.ProjectID, key).Scan(&id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := tx.QueryRow(ctx, `INSERT INTO quader.repositories (key, class, format, config, project_id, visibility_artifacts, visibility_wiki)
			VALUES ($1, 'remote', $2, $3::text::jsonb, $4::uuid, $5, $5) RETURNING id`, key, format, cfg, wellknown.ProjectID, level).Scan(&id); err != nil {
			t.Fatalf("insert %s: %v", key, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO quader.upstream_repository (repository_id, kind) VALUES ($1, $2)`, id, kind); err != nil {
			t.Fatalf("upstream_repository of %s: %v", key, err)
		}
	case err != nil:
		t.Fatal(err)
	default:
		if _, err := tx.Exec(ctx, `UPDATE quader.repositories SET config = $2::text::jsonb, visibility_artifacts = $3, visibility_wiki = $3,
			config_version = config_version + 1 WHERE id = $1`, id, cfg, level); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit %s: %v", key, err)
	}
	return id
}

// virtSwitchOn switches a preset repository on through setUpstreamRepositoryState as an administrator (R-REPO-068, R-REPO-073): the
// operation writes disabled false, the first switch-on and the repo_config change in one transaction, and the replicas take the
// member up from that change.
func (e *virtEnv) virtSwitchOn(key string) {
	e.t.Helper()
	a := e.do("PUT", "/api/v1/upstream-repositories/"+key+"/state", e.admin, map[string]any{"state": "on"})
	if a.Status != 200 || a.JSON(e.t)["state"] != string(wellknown.StateActive) {
		e.t.Fatalf("switching upstream/%s on answered %d: %.300s", key, a.Status, a.Body)
	}
}

func virtNewEnv(t *testing.T) *virtEnv {
	t.Helper()
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
	st := testenv.NewStack(t, testenv.StackOptions{Name: "upvirt", Env: []string{
		"QUADER_PROXY_OUTBOUND_TLS_CA_FILES=[" + strconv.Quote(ca) + "]",
		// The test upstream and the issuer double listen on the loopback address, which the egress policy denies by default.
		`QUADER_PROXY_OUTBOUND_ADDRESS_POLICY_ALLOW_CIDRS=["127.0.0.0/8"]`,
		"QUADER_CLUSTER_EPOCH_POLL_INTERVAL=300ms", "QUADER_VULNERABILITIES_SOURCES_OSV_MODE=off",
	}})
	e := &virtEnv{t: t, st: st, stub: stub, run: run}
	for _, p := range corpus.Default().Packages {
		if !strings.HasPrefix(p.Name, "@") && len(p.Versions) > 0 {
			e.pkg = p
			break
		}
	}
	st.RepositoryIn("web", "npm-local", "local", "npm", `{"local":{"autoClaim":false}}`)
	st.RepositoryIn("mobile", "npm-local", "local", "npm", `{"local":{"autoClaim":false}}`)
	npmjs := virtWellKnown(t, st, "npmjs", "npm", "preset", virtWellKnownConfig(run.URL+"/", true), "internal")
	virtWellKnown(t, st, "docker-hub", "oci", "preset", virtWellKnownConfig("https://registry-1.docker.io/", true), "private")
	virtWellKnown(t, st, wellknown.ScannerTrivyKey, "oci", "system", virtWellKnownConfig(wellknown.ScannerTrivyURL, false), "private")
	st.RepositoryIn("web", "npm-all", "virtual", "npm", `{}`)
	all := e.id("web", "npm-all")
	for i, m := range []int64{e.id("web", "npm-local"), npmjs} {
		st.Exec(`INSERT INTO virtual_members (virtual_id, position, member_id) VALUES ($1, $2, $3)`, all, i, m)
	}
	ops, ann, erin, deployer := st.User("ops"), st.User("ann"), st.User("erin"), st.ServiceAccount("web-deployer")
	st.Bind("admin", ops)
	st.Member("web", ann, "reader")
	st.Member("web", erin, "reader")
	st.Exec("UPDATE app_user SET external = true WHERE principal_id = $1::uuid", erin.ID)
	st.Policy("web-deployer-reads", []string{"read"}, []string{"web/npm-all"}, deployer)
	e.admin = st.Token(ops, scope.Scope{Inherit: true, Admin: true})
	e.ann, e.erin, e.deployer = st.Token(ann, scope.Default()), st.Token(erin, scope.Default()), st.Token(deployer, scope.Default())
	e.r = st.Start(t, testenv.ReplicaOptions{Name: "web-1", Role: "web"})
	return e
}

// id is the ID of a repository.
func (e *virtEnv) id(project, key string) int64 {
	e.t.Helper()
	return e.st.Number(`SELECT r.id FROM repositories r JOIN project p ON p.id = r.project_id WHERE p.key = $1 AND r.key = $2`, project, key)
}

// do sends a request with a token to the replica.
func (e *virtEnv) do(method, path, token string, body any, hdr ...string) testenv.Answer {
	e.t.Helper()
	return e.st.Do(e.r, method, path, token, body, hdr...)
}

// npm is a GET below the registry URL of a repository of web.
func (e *virtEnv) npm(repo, path, token string) testenv.Answer {
	e.t.Helper()
	return e.do("GET", "/api/npm/web/"+repo+"/"+path, token, nil)
}

// tarball is the path of the newest version's tarball of the test package below a registry URL.
func (e *virtEnv) tarball() string {
	v := e.pkg.Versions[len(e.pkg.Versions)-1].Version
	return e.pkg.Name + "/-/" + corpus.TarballFile(e.pkg.Name, v)
}

// virtNpmError is the npm error body of an answer (R-NPM-005).
func virtNpmError(t *testing.T, a testenv.Answer) (code, message string) {
	t.Helper()
	var b struct{ Error, Message, Code string }
	if err := json.Unmarshal(a.Body, &b); err != nil || b.Error != b.Message {
		t.Fatalf("answer %d is no npm error body: %v\n%s", a.Status, err, a.Body)
	}
	return b.Code, b.Message
}

// virtSettled waits until the replica's grant index holds the authorization epoch of the database (R-AUTHZ-028).
func (e *virtEnv) virtSettled() {
	e.t.Helper()
	want := e.st.Number(`SELECT epoch FROM cluster_epoch WHERE area = $1`, cluster.AreaAuthz)
	eng, _ := e.r.App.Authorizer().(*authz.Engine)
	if eng == nil {
		e.t.Fatal("the replica has no grant index")
	}
	testenv.WaitFor(e.t, "the grant index at the epoch of the database", 10*time.Second, func() bool { return eng.Epoch() >= want })
}

// virtCITokens are two CI tokens of the project mapping web-build, which grants read on npm-all and nothing else (R-AUTHZ-041): one
// that inherits the mapping's grants and one whose job requested the scope read:web/npm-all (R-CI-007 step 8, R-AUTHZ-013). An issuer
// of the issuer double of test/oidcstub, the mapping of the project web, a subject token of the double for each, and the exchanges.
func (e *virtEnv) virtCITokens() (inherited, scoped string) {
	t := e.t
	t.Helper()
	stub, err := oidcstub.New(oidcstub.Config{Audience: "http://quader.test"})
	if err != nil {
		t.Fatal(err)
	}
	run, ca, err := stub.StartSelfSigned()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = run.Close() })
	bundle := filepath.Join(t.TempDir(), "gitlab-ca.crt")
	if err := os.WriteFile(bundle, ca.CAPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if a := e.do("POST", "/api/v1/ci/issuers", e.admin, map[string]any{
		"name": "gitlab-main", "kind": "gitlab", "issuer": stub.Issuer(), "publicIssuer": false, "caBundleRef": "file:" + bundle,
	}); a.Status != 201 {
		t.Fatalf("issuer: %d %s", a.Status, a.Body)
	}
	if a := e.do("POST", "/api/v1/projects/web/ci/mappings", e.admin, map[string]any{
		"name": "web-build", "priority": 100, "issuer": "gitlab-main",
		"match":  map[string]any{"all": []any{map[string]any{"claim": "project_id", "equals": "1234"}}},
		"grants": map[string]any{"inline": []any{map[string]any{"repositories": []string{"npm-all"}, "packages": map[string]any{"include": []string{"**"}}, "actions": []string{"read"}}}},
	}); a.Status != 201 {
		t.Fatalf("mapping: %d %s", a.Status, a.Body)
	}
	e.virtSettled()
	exchange := func(requested string) string {
		subject, err := stub.Mint(oidcstub.MintOptions{})
		if err != nil {
			t.Fatal(err)
		}
		form := url.Values{
			"grant_type": {ci.GrantTypeTokenExchange}, "subject_token_type": {ci.TokenTypeIDToken},
			"subject_token": {subject},
		}
		if requested != "" {
			form.Set("scope", requested)
		}
		a := e.do("POST", "/api/v1/auth/oidc/token", "", []byte(form.Encode()), "Content-Type", "application/x-www-form-urlencoded")
		var tok struct {
			Token string `json:"access_token"`
		}
		if a.Status != 200 || json.Unmarshal(a.Body, &tok) != nil || tok.Token == "" {
			t.Fatalf("exchange (scope %q): %d %s", requested, a.Status, a.Body)
		}
		return tok.Token
	}
	return exchange(""), exchange("read:web/npm-all")
}

// virtStore is a journal store in memory: what the shipper writes and the replay reads.
type virtStore struct {
	mu   sync.Mutex
	objs map[string][]byte
}

func (m *virtStore) Put(_ context.Context, key string, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objs[key] = append([]byte(nil), body...)
	return nil
}

func (m *virtStore) List(_ context.Context, prefix string, fn func(string) error) error {
	m.mu.Lock()
	var keys []string
	for k := range m.objs {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	m.mu.Unlock()
	virtSortStrings(keys)
	for _, k := range keys {
		if err := fn(k); err != nil {
			return err
		}
	}
	return nil
}

func (m *virtStore) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objs[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), b...), nil
}

func virtSortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// virtReplay ships the installation's revocation journal to a store in memory and replays it into the same database as a restore
// to restoredTo does (R-HA-054, R-BAK-024): the journal replay in process.
func (e *virtEnv) virtReplay(restoredTo time.Time) *journal.Summary {
	t := e.t
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := postgres.Open(ctx, e.st.DatabaseConfig(), "it-journal", meta.RoleAll, testenv.Quiet())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &virtStore{objs: map[string][]byte{}}
	const prefix = "quader"
	if _, err := ship.New(ship.Options{Outbox: ship.NewOutbox(db), Store: store, BlobPrefix: prefix, Log: testenv.Quiet()}).Ship(ctx); err != nil {
		t.Fatalf("ship: %v", err)
	}
	sum, err := replay.New(replay.Options{DB: db, Reader: replay.NewReader(store, prefix), Log: testenv.Quiet()}).
		Replay(ctx, journal.ReplayOptions{RestoredTo: restoredTo, Margin: 5 * time.Minute, Out: io.Discard})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	return sum
}
