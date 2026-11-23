package testenv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/meta/postgres"
)

// Every stack's database is a clone of a migrated template (Q-ARCH-581): running the 100-odd migration steps into a new database
// took 0.57 to 0.69 s per stack, the clone takes 30 to 70 ms. The first stack of a test process on a PostgreSQL server makes the
// template if the server has none for the embedded migrations of the day, under an advisory lock, so that several test binaries
// and several stacks that start at once make it once; every later stack of the process clones it.

// templateFormat is part of the template's hash. Raise it when what the harness puts into a template changes other than through
// the migration files (the options of the migration, the settings of the database), so that no stack clones a template of the
// old kind.
const templateFormat = "1"

// templateMarker is the comment of every template database the harness makes. The harness drops a database only when its name
// matches templateNames and it carries this comment, so it never drops a database it did not make.
const templateMarker = "quader integration harness: a migrated template of the stacks (testenv/template.go)"

// templateNames matches the names of the harness's templates, quader_ittpl_<day>_<hash>, and of a template being built, the same
// with _build. No other database of the stacks' PostgreSQL has such a name: a stack's own database is quader_<Name>_<10 hex
// digits>, and the areas with databases of their own use other prefixes.
var templateNames = regexp.MustCompile(`^quader_ittpl_[0-9]{8}_[0-9a-f]{16}(_build)?$`)

// templateLock is the key of the session advisory lock under which a process makes a template and drops the old ones. It is
// taken in the maintenance database postgres, which every test process of the server connects to; Quader's own advisory locks
// live in an installation's database and use the two-key form, so nothing else takes this key.
const templateLock int64 = 0x7164725f74706c // "qdr_tpl"

// templateName is the name of the template of the migration files in steps on day (UTC, YYYYMMDD): a template is made anew when
// a migration file changes and every day, so that what the migrations write with the time of their run (the audit partitions of
// the month and the three after it, the timestamps of the rows they seed) is never older than a day.
func templateName(steps fs.FS, day string) (string, error) {
	entries, err := fs.ReadDir(steps, ".")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "format %s\n", templateFormat)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := fs.ReadFile(steps, e.Name())
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(h, "%s %d\n", e.Name(), len(b))
		_, _ = h.Write(b)
	}
	return "quader_ittpl_" + day + "_" + hex.EncodeToString(h.Sum(nil))[:16], nil
}

// templateDay is the day of t in the template's name.
func templateDay(t time.Time) string { return t.UTC().Format("20060102") }

// migrateOptions are the options every stack's database was migrated with before the template, and the template is now.
func migrateOptions(steps fs.FS) postgres.MigrateOptions {
	return postgres.MigrateOptions{
		Instance: "it-migrate", Version: "0.1.0", Log: Quiet(), Steps: steps,
		Migrate: config.Migrate{AdvisoryLockWait: 20 * time.Second, DDLLockTimeout: 2 * time.Second, DDLLockRetries: 3},
	}
}

// templates are the templates this process clones, one per PostgreSQL server (host:port).
var templates struct {
	mu    sync.Mutex
	names map[string]string
}

// stackTemplate returns the template the stacks of this process clone on the server of cfg, making it when the server has none.
// renew asks again after a clone found the template gone: another process made a newer one (a changed migration in another
// checkout, a new day) and dropped this one.
func stackTemplate(tb testing.TB, cfg config.Database, renew bool) string {
	tb.Helper()
	server := cfg.Host + ":" + strconv.Itoa(cfg.Port)
	templates.mu.Lock()
	defer templates.mu.Unlock()
	if name := templates.names[server]; name != "" && !renew {
		return name
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	name, _, err := ensureTemplate(ctx, PgDSN(tb, "postgres"), cfg, postgres.EmbeddedFS(), templateDay(time.Now()), tb.Logf)
	if err != nil {
		tb.Fatalf("the template database of the stacks: %v", err)
	}
	if templates.names == nil {
		templates.names = map[string]string{}
	}
	templates.names[server] = name
	return name
}

// ensureTemplate makes sure the server has the template of steps on day and returns its name; built says whether this call made
// it. A template is built under a name of its own (<name>_build), marked a template that takes no connections only after its
// migration succeeded, and renamed to its name as the last step: a database of the template's name is a complete template or
// there is none, and a build that fails is dropped. Making it and dropping the harness's other templates (another hash, an
// earlier day, a build left by a process that died) happen under the advisory lock templateLock, which the session of maintDSN
// holds; a call that waited for the lock finds the template another process made meanwhile and builds nothing.
func ensureTemplate(ctx context.Context, maintDSN string, cfg config.Database, steps fs.FS, day string, logf func(string, ...any)) (name string, built bool, err error) {
	if name, err = templateName(steps, day); err != nil {
		return "", false, err
	}
	maint, err := pgx.Connect(ctx, maintDSN)
	if err != nil {
		return "", false, fmt.Errorf("connect to the maintenance database: %w", err)
	}
	defer func() { _ = maint.Close(context.WithoutCancel(ctx)) }()
	if ok, err := templateReady(ctx, maint, name); err != nil || ok {
		return name, false, err
	}
	if _, err := maint.Exec(ctx, "SELECT pg_advisory_lock($1)", templateLock); err != nil {
		return "", false, fmt.Errorf("wait for the template lock: %w", err)
	}
	defer func() { _, _ = maint.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", templateLock) }()
	if ok, err := templateReady(ctx, maint, name); err != nil || ok {
		return name, false, err
	}
	build := name + "_build"
	if err := dropTemplate(ctx, maint, build); err != nil {
		return "", false, err
	}
	if _, err := maint.Exec(ctx, "CREATE DATABASE "+build+" ENCODING 'UTF8' TEMPLATE template0"); err != nil {
		return "", false, fmt.Errorf("create %s: %w", build, err)
	}
	if err := buildTemplate(ctx, maint, cfg, steps, build, name); err != nil {
		if derr := dropTemplate(context.WithoutCancel(ctx), maint, build); derr != nil {
			logf("the failed template %s was not dropped: %v", build, derr)
		}
		return "", false, err
	}
	dropOtherTemplates(ctx, maint, name, logf)
	return name, true, nil
}

// buildTemplate marks build as the harness's, migrates it, marks it a template that takes no connections and renames it to name.
func buildTemplate(ctx context.Context, maint *pgx.Conn, cfg config.Database, steps fs.FS, build, name string) error {
	if _, err := maint.Exec(ctx, "COMMENT ON DATABASE "+build+" IS '"+templateMarker+"'"); err != nil {
		return fmt.Errorf("mark %s: %w", build, err)
	}
	cfg.Name = build
	if _, err := postgres.Migrate(ctx, cfg, migrateOptions(steps)); err != nil {
		return fmt.Errorf("migrate %s: %w", build, err)
	}
	if _, err := maint.Exec(ctx, "ALTER DATABASE "+build+" WITH ALLOW_CONNECTIONS false IS_TEMPLATE true"); err != nil {
		return fmt.Errorf("make %s a template: %w", build, err)
	}
	if _, err := maint.Exec(ctx, "ALTER DATABASE "+build+" RENAME TO "+name); err != nil {
		return fmt.Errorf("rename %s to %s: %w", build, name, err)
	}
	return nil
}

// templateReady reports whether name exists and is a template.
func templateReady(ctx context.Context, maint *pgx.Conn, name string) (bool, error) {
	var ok bool
	err := maint.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1 AND datistemplate)", name).Scan(&ok)
	return ok, err
}

// dropTemplate drops the harness's template or build name if it exists. It refuses every other name.
func dropTemplate(ctx context.Context, maint *pgx.Conn, name string) error {
	if !templateNames.MatchString(name) {
		return fmt.Errorf("%s is not a template of the harness; not dropped", name)
	}
	var exists bool
	if err := maint.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&exists); err != nil || !exists {
		return err
	}
	// A template cannot be dropped; the flag goes first.
	if _, err := maint.Exec(ctx, "ALTER DATABASE "+name+" WITH IS_TEMPLATE false"); err != nil {
		return fmt.Errorf("unmark %s: %w", name, err)
	}
	if _, err := maint.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
		return fmt.Errorf("drop %s: %w", name, err)
	}
	return nil
}

// dropOtherTemplates drops every template of the harness but keep: those of other migrations and of earlier days, and builds that a
// process left when it died. A process that still clones one of them finds it gone and asks for the template again
// (stackTemplate). A drop waits at most five seconds for a clone that reads the template at the moment; one that does not get its
// lock in time stays until the next template is made.
func dropOtherTemplates(ctx context.Context, maint *pgx.Conn, keep string, logf func(string, ...any)) {
	rows, err := maint.Query(ctx, `SELECT datname FROM pg_database d WHERE datname LIKE 'quader\_ittpl\_%' AND datname <> $1
		AND shobj_description(d.oid, 'pg_database') = $2 ORDER BY datname`, keep, templateMarker)
	if err != nil {
		logf("list the old templates: %v", err)
		return
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		logf("list the old templates: %v", err)
		return
	}
	if _, err := maint.Exec(ctx, "SET lock_timeout = '5s'"); err != nil {
		logf("set the lock timeout of the drops: %v", err)
		return
	}
	defer func() { _, _ = maint.Exec(context.WithoutCancel(ctx), "RESET lock_timeout") }()
	for _, n := range names {
		if !templateNames.MatchString(n) {
			continue
		}
		if err := dropTemplate(ctx, maint, n); err != nil {
			logf("the old template %s stays: %v", n, err)
		}
	}
}

// cloneDatabase creates the database cfg.Name as a clone of the template of the test process on cfg's server and drops it when the
// test ends. It returns the connection to the maintenance database that drops it; the caller may use it until then.
func cloneDatabase(tb testing.TB, cfg config.Database) *pgx.Conn {
	tb.Helper()
	ctx := context.Background()
	maint, err := pgx.Connect(ctx, PgDSN(tb, "postgres"))
	if err != nil {
		tb.Fatalf("connect to PostgreSQL: %v", err)
	}
	tpl := stackTemplate(tb, cfg, false)
	err = cloneTemplate(ctx, maint, cfg.Name, tpl)
	if templateGone(err) {
		// Another process made a newer template and dropped this one (a changed migration in another checkout, a new day).
		tpl = stackTemplate(tb, cfg, true)
		err = cloneTemplate(ctx, maint, cfg.Name, tpl)
	}
	if err != nil {
		_ = maint.Close(ctx)
		tb.Fatalf("create the database %s from the template %s: %v", cfg.Name, tpl, err)
	}
	tb.Cleanup(func() {
		_, _ = maint.Exec(context.Background(), "DROP DATABASE IF EXISTS "+cfg.Name+" WITH (FORCE)")
		_ = maint.Close(context.Background())
	})
	return maint
}

// MigratedDatabase creates the database name, migrated to the embedded migrations, for an area that builds its components on
// pools of its own instead of a Stack (blob, repository, identity, files, coordination): a clone of the template of the test
// process with identity rows of its own (Q-ARCH-581), dropped when the test ends. The clone takes 30 to 70 ms where migrating a
// new database took 0.6 s on an idle host and more than the areas' minute on a starved one (2026-11-23, at host loads of 30 to
// 110). An area whose subject is the migration itself (postgres, backup, stack) migrates a database of its own instead.
func MigratedDatabase(tb testing.TB, name string) {
	tb.Helper()
	cfg := databaseConfig(tb, tb.TempDir(), name)
	cloneDatabase(tb, cfg)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, PgDSN(tb, name))
	if err != nil {
		tb.Fatalf("connect to %s: %v", name, err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, "SET search_path TO quader"); err != nil {
		tb.Fatal(err)
	}
	if err := drawIdentity(ctx, conn); err != nil {
		tb.Fatalf("the identity rows of %s: %v", name, err)
	}
}

// cloneTemplate creates the database name from the template tpl.
func cloneTemplate(ctx context.Context, maint *pgx.Conn, name, tpl string) error {
	_, err := maint.Exec(ctx, "CREATE DATABASE "+name+" TEMPLATE "+tpl)
	return err
}

// templateGone reports whether a clone failed because its template no longer exists (SQLSTATE 3D000).
func templateGone(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "3D000"
}

// identityKeys are the rows of system_state that the first migration draws at random and that tell one installation from another
// (R-OPS-024): every Valkey key carries keyspace_id (R-ARCH-030), so two stacks of one keyspace would share their Valkey state;
// the object store's connection markers (R-STOR-051), the backup target's marker and the signed statements name installation_id,
// and a backup names incarnation. A clone holds the template's; drawIdentity draws them again. Nothing else the migrations write
// differs between two fresh migrations in a way a stack shares with another: the built-in limit profiles get random ids, which
// stay inside the installation's database, and the object store's keys of a stack lie below its database's name.
var identityKeys = []string{"installation_id", "keyspace_id", "incarnation"}

// drawIdentity gives the installation behind conn (search_path at its schema) identity rows of its own, as the first migration
// would have drawn them.
func drawIdentity(ctx context.Context, conn *pgx.Conn) error {
	tag, err := conn.Exec(ctx, `UPDATE system_state SET value = to_jsonb(gen_random_uuid()::text), updated_at = now() WHERE key = ANY($1)`,
		identityKeys)
	if err != nil {
		return err
	}
	if n := tag.RowsAffected(); n != int64(len(identityKeys)) {
		return fmt.Errorf("system_state holds %d of the %d identity rows %v", n, len(identityKeys), identityKeys)
	}
	return nil
}
