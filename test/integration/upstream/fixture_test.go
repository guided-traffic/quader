//go:build integration

package upstream

import (
	"context"
	"slices"
	"testing"
	"time"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/api/v1/apitest"
	"github.com/guided-traffic/quader/internal/app"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/formats/catalog"
	"github.com/guided-traffic/quader/internal/meta/postgres"
	"github.com/guided-traffic/quader/test/integration/stack"
	"github.com/guided-traffic/quader/test/integration/testenv"
)

// The area's helpers. A test of another package of the feature writes its cases in files of its own with helpers of its own.

const items = "/api/v1/upstream-repositories"

// fixture is a migrated database whose project upstream the reconcile filled with the catalog of the binary, two web replicas on
// it, an administrator, an auditor and two plain users.
type fixture struct {
	t              *testing.T
	e              *stack.Env
	cl             *stack.Cluster
	admin, auditor stack.Person
	alice, bob     stack.Person
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	e := stack.New(t)
	f := &fixture{t: t, e: e}
	f.admin = e.User("up-admin")
	e.Admin(f.admin)
	f.auditor = e.User("up-auditor")
	e.Exec("INSERT INTO authz_role_binding (role, principal_id) VALUES ('auditor', $1::uuid)", f.auditor.ID)
	f.alice, f.bob = e.User("alice"), e.User("bob")
	if err := reconcile(t, e, nil); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return f
}

// start starts the two replicas; the reconcile of a test runs before.
func (f *fixture) start() {
	f.t.Helper()
	o := stack.Options{Validator: apitest.New(f.t, v1.EmbeddedYAML())}
	f.cl = stack.NewCluster(f.e.NewReplica("web-a", o), f.e.NewReplica("web-b", o))
}

func (f *fixture) do(person stack.Person, method, path string, body any, hdr ...string) stack.Response {
	f.t.Helper()
	return f.cl.Do(person.Name, method, path, body, hdr...)
}

// reconcile runs `quader migrate up` with the reconcile of the project upstream against cat (nil: the catalog of the binary), as
// the command composes it; the error is the one the command would exit with.
func reconcile(t *testing.T, e *stack.Env, cat *catalog.Registry) error {
	t.Helper()
	cfg := &config.Config{Database: e.Cfg}
	cfg.Cluster = config.Cluster{EpochPollInterval: time.Second, EpochMaxStaleness: 20 * time.Second, ChangeRetention: time.Hour}
	cfg.Health.WatchdogTimeout = 30 * time.Second
	cfg.Audit = config.Audit{AsyncBuffer: 100, RetentionDays: 365}
	if cat == nil {
		cat = catalog.Default()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, err := postgres.Migrate(ctx, e.Cfg, postgres.MigrateOptions{
		Instance: "it-migrate", Version: "0.1.0", Log: testenv.Quiet(), AuditRetentionDays: &cfg.Audit.RetentionDays,
		Migrate:  config.Migrate{AdvisoryLockWait: 20 * time.Second, DDLLockTimeout: 2 * time.Second, DDLLockRetries: 3},
		Declared: app.UpstreamStep(cfg, cat, "it-migrate", testenv.Quiet()),
	})
	return err
}

// catalogWith is a copy of the binary's catalog whose presets change edits.
func catalogWith(t *testing.T, change func(id catalog.ID, presets []catalog.UpstreamPreset) []catalog.UpstreamPreset) *catalog.Registry {
	t.Helper()
	r := catalog.NewRegistry()
	for _, e := range catalog.Default().List() {
		e.UpstreamPresets = change(e.ID, slices.Clone(e.UpstreamPresets))
		r.Register(e)
	}
	return r
}

// wantStatus asserts the status and the code of an answer; a 422 names the field code in errors[], the top-level code being
// validation_failed (R-API-009).
func wantStatus(t *testing.T, r stack.Response, status int, code string) {
	t.Helper()
	if status == 422 && code != "" && code != "validation_failed" {
		if r.Status != 422 || r.Code() != "validation_failed" || !hasFieldCode(t, r, code) {
			t.Fatalf("answered %d %q, want 422 validation_failed with %q in errors[]\n%s", r.Status, r.Code(), code, r.Body)
		}
		return
	}
	if r.Status != status || (code != "" && r.Code() != code) {
		t.Fatalf("answered %d %q, want %d %q\n%s", r.Status, r.Code(), status, code, r.Body)
	}
}

func hasFieldCode(t *testing.T, r stack.Response, code string) bool {
	list, _ := r.JSON(t)["errors"].([]any)
	for _, e := range list {
		if e.(map[string]any)["code"] == code {
			return true
		}
	}
	return false
}

// hasField reports whether errors[] holds the code at the pointer.
func hasField(t *testing.T, r stack.Response, pointer, code string) bool {
	t.Helper()
	list, _ := r.JSON(t)["errors"].([]any)
	for _, e := range list {
		m := e.(map[string]any)
		if m["pointer"] == pointer && m["code"] == code {
			return true
		}
	}
	return false
}

// events counts the audit events of an action on a repository of the project upstream; detail reads a member of the newest one's
// details.
func (f *fixture) events(action, key string) int64 {
	return f.e.Number("SELECT count(*) FROM audit_event WHERE action = $1 AND repository = $2", action, "upstream/"+key)
}

func (f *fixture) detail(action, key, member string) string {
	return f.e.Text(`SELECT coalesce((SELECT details->>$3 FROM audit_event WHERE action = $1 AND repository = $2
	                 ORDER BY occurred_at DESC, id DESC LIMIT 1), '<none>')`, action, "upstream/"+key, member)
}

// config reads a member of the stored configuration of upstream/<key> by a JSON path.
func (f *fixture) config(key, path string) string {
	return f.e.Text(`SELECT coalesce(r.config #>> $2::text[], '<none>') FROM repositories r JOIN project p ON p.id = r.project_id
	                 WHERE p.key = 'upstream' AND r.key = $1`, key, path)
}

// trivyScope says whether a repository of the project upstream delivers what scanner-trivy is for and nothing else: the mode onlyAllowed
// with the product's one rule "package aquasec/trivy-db allow" and no include pattern (R-REPO-070, remote-curation.md R-CUR-002).
func (f *fixture) trivyScope(key string) (string, bool) {
	rules := f.e.Text(`SELECT coalesce(string_agg(d.kind || ' ' || coalesce(d.name, d.pattern) || ' ' || d.effect, ', '), '') FROM remote_delivery_rules d
	                   WHERE d.repo_id = $1`, f.id(key))
	got := f.config(key, "{remote,delivery,mode}") + "; " + rules + "; " + f.config(key, "{includePatterns}")
	// The stored row may keep an empty list; a remote's document reads back without it.
	return got, got == "onlyAllowed; package aquasec/trivy-db allow; <none>" || got == "onlyAllowed; package aquasec/trivy-db allow; []"
}

func (f *fixture) id(key string) int64 {
	return f.e.Number("SELECT r.id FROM repositories r JOIN project p ON p.id = r.project_id WHERE p.key = 'upstream' AND r.key = $1", key)
}

// listVirtual makes a virtual repository of the project web list upstream/<key>, straight in the member table (the guard AI006 admits
// a member of upstream); the member naming of the API is another package's.
func (f *fixture) listVirtual(project, virtual, format, key string) {
	f.t.Helper()
	f.e.RepoIn(project, virtual, "virtual", format)
	f.e.Exec(`INSERT INTO virtual_members (virtual_id, position, member_id)
	          SELECT v.id, 0, m.id FROM repositories v JOIN project vp ON vp.id = v.project_id, repositories m JOIN project mp ON mp.id = m.project_id
	           WHERE vp.key = $1 AND v.key = $2 AND mp.key = 'upstream' AND m.key = $3`, project, virtual, key)
}

// within polls cond until it holds, for at most five seconds: a change reaches the other replica within the bound of the change log.
func within(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("the condition did not hold within 5 s")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
