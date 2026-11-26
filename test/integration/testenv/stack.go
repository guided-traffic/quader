package testenv

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	v1 "github.com/guided-traffic/quader/internal/api/v1"
	"github.com/guided-traffic/quader/internal/api/v1/apitest"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/secrets"
)

// The in-process harness: one Stack is an installation (a migrated database of
// its own, a Valkey keyspace and an S3 prefix of the stack's) and its replicas
// are the production composition root, app.New, started in the test process.
// Every integration suite that exercises more than one component uses it, so
// the suite runs the chain, the authentication stage and the wiring a replica
// runs in production; a suite of one component (the authorization engine, the
// coordination layer, the audit writer, the blob store) keeps its direct
// environment. docs/developer/build-and-test.md describes the harness.

// StackOptions configure a Stack.
type StackOptions struct {
	// Valkey false points every replica's Sentinel at a port nothing listens on:
	// the state "Valkey stopped" (degraded mode, R-HA-001). True uses the stack's
	// Sentinels.
	Valkey bool
	// Name is part of the database name, quader_<Name>_<random>; "it" if empty.
	Name string
	// Env is appended to the environment of every replica after the harness's own
	// settings, so it wins: the QUADER_* settings a whole suite changes.
	Env []string
	// TLS serves every listener of a replica with a pair of its role that the
	// harness issues from the stack's CA (RolePair), makes that CA the trust anchor
	// of the links between the replicas (server.internal_tls.ca_file, R-ARCH-038)
	// and sets the names each end of a link checks: the links of the image
	// scanning are then mutual TLS with the checks of the chart (Q-ARCH-553). The
	// harness's requests go over https and verify against the stack's CA.
	TLS bool
}

// Stack is one installation under test.
type Stack struct {
	tb     testing.TB
	name   string
	dir    string
	base   []string
	sealed string // the key file of the default key ring
	pg     config.Database
	valkey bool
	// tls is StackOptions.TLS: the replicas serve https and the harness verifies against the stack's CA.
	tls bool
	// pairs are the files of the pair of each role on a stack with TLS (rolePairs): role -> cert and key file.
	pairs   map[string][2]string
	adminMu sync.Mutex // the admin connection is not safe for concurrent use
	admin   *pgx.Conn
	val     *apitest.Validator

	mu   sync.Mutex
	reps []*Replica
	next atomic.Int64
}

// Quiet is a logger that discards.
func Quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// RandHex returns n random bytes in hex.
func RandHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// WriteFile writes content to name in dir (0600) and returns the path.
func WriteFile(tb testing.TB, dir, name, content string) string {
	tb.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		tb.Fatal(err)
	}
	return p
}

// NewStack creates the database quader_<Name>_<random> as a clone of the migrated
// template of the test process (template.go, Q-ARCH-581), gives it identity rows
// of its own and drops it when the test ends. No replica runs yet: seed the rows
// the first replica must see (Stack.Repository, Stack.User, Stack.Token) and then
// Start it.
func NewStack(tb testing.TB, o StackOptions) *Stack {
	tb.Helper()
	if o.Name == "" {
		o.Name = "it"
	}
	host, port := Get(tb, "QUADER_TEST_PG_HOST"), Get(tb, "QUADER_TEST_PG_PORT")
	user := Get(tb, "QUADER_TEST_PG_USER")
	ctx := context.Background()
	name := "quader_" + o.Name + "_" + RandHex(5)
	dir := tb.TempDir()
	cfg := databaseConfig(tb, dir, name)
	maint := cloneDatabase(tb, cfg)
	conn, err := pgx.Connect(ctx, PgDSN(tb, name))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = conn.Close(context.Background()) })
	if _, err := conn.Exec(ctx, "SET search_path TO quader"); err != nil {
		tb.Fatal(err)
	}
	if err := drawIdentity(ctx, conn); err != nil {
		tb.Fatalf("the identity rows of %s: %v", name, err)
	}

	s := &Stack{tb: tb, name: name, dir: dir, pg: cfg, valkey: o.Valkey, tls: o.TLS, admin: conn, val: apitest.New(tb, v1.EmbeddedYAML())}
	// Registered before any replica starts, so it runs after every replica has stopped (F-113).
	RemoveObjectsAtCleanup(tb, S3Config(tb).BlobsBucket, s.ObjectPrefix())
	var pooler []string
	if PgPooled(tb) {
		// The replicas' main pool goes through PgBouncer in transaction mode; the
		// session class stays direct (R-ARCH-019, R-HA-058).
		cfg = ThroughPgBouncer(tb, cfg, user, maint)
		s.pg = cfg
		pooler = []string{
			"QUADER_DATABASE_PORT=" + strconv.Itoa(cfg.Port), "QUADER_DATABASE_SESSION_HOST=" + cfg.SessionHost,
			"QUADER_DATABASE_SESSION_PORT=" + strconv.Itoa(cfg.SessionPort),
			"QUADER_DATABASE_POOLER_MODE=transaction", "QUADER_DATABASE_POOLER_SERVER_CONNECTIONS=20",
		}
	}
	s.sealed = WriteFile(tb, dir, "2026-10.key", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{42}, 32)))
	s.base = append(s.environ(host, port, name, cfg.UsernameFile, cfg.PasswordFile), pooler...)
	if o.TLS {
		// Every role gets a pair of its own, as every kind of pod does in the chart (R-HA-063, Q-ARCH-553); Start serves it, and
		// each end of a link checks the other's name.
		s.pairs = rolePairs(tb, dir)
		s.base = append(s.base, "QUADER_SERVER_INTERNAL_TLS_CA_FILE="+CAFile(tb),
			"QUADER_VULNERABILITIES_IMAGES_SCANNER_SERVER_NAME="+KindName("vuln-scanner-trivy"), "QUADER_VULNERABILITIES_IMAGES_SCANNER_CLIENT_NAME="+KindName("worker"),
			"QUADER_VULNERABILITIES_IMAGES_REGISTRY_SERVER_NAME="+KindName("packages"),
			// The name by which the scan listener records the image scanner's report of itself (R-OPS-048, Q-OPS-223).
			"QUADER_VULNERABILITIES_IMAGES_REGISTRY_CLIENT_NAME="+KindName("vuln-scanner-trivy"),
			ScanClients(),
			// The links of the malware scanning, named as the chart names them (R-HA-068, R-HA-069, Q-MAL-61).
			"QUADER_MALWARE_SCANNER_SERVER_NAME="+KindName("malware-scanner-clamav"), "QUADER_MALWARE_SCANNER_CLIENT_NAME="+KindName("worker"),
			"QUADER_MALWARE_SCAN_LISTENER_SERVER_NAME="+KindName("packages"), "QUADER_MALWARE_SCAN_LISTENER_CLIENT_NAME="+KindName("malware-scanner-clamav"),
			"QUADER_MALWARE_SCAN_LISTENER_UPDATER_CLIENT_NAME="+KindName("malware-updater-freshclam"),
			"QUADER_MALWARE_UPDATER_SERVER_NAME="+KindName("malware-updater-freshclam"), "QUADER_MALWARE_UPDATER_CLIENT_NAME="+KindName("worker"))
	}
	s.base = append(s.base, o.Env...)
	s.Project(DefaultProject)
	return s
}

// databaseConfig is the direct configuration of the database name on the stack's PostgreSQL, as the harness's replicas and
// migrations use it, with the credentials in files of dir.
func databaseConfig(tb testing.TB, dir, name string) config.Database {
	tb.Helper()
	port, err := strconv.Atoi(Get(tb, "QUADER_TEST_PG_PORT"))
	if err != nil {
		tb.Fatalf("QUADER_TEST_PG_PORT: %v", err)
	}
	return config.Database{
		Host: Get(tb, "QUADER_TEST_PG_HOST"), Port: port, Name: name, Schema: "quader",
		UsernameFile: WriteFile(tb, dir, "db-user", Get(tb, "QUADER_TEST_PG_USER")+"\n"),
		PasswordFile: WriteFile(tb, dir, "db-pass", Get(tb, "QUADER_TEST_PG_PASSWORD")+"\n"),
		SSLMode:      "verify-full", SSLRootCertFile: CAFile(tb), ConnectTimeout: 5 * time.Second, TargetSessionAttrs: "read-write",
		Pooler:        config.DatabasePooler{Mode: "none"},
		Pool:          config.DatabasePool{MaxConns: 8, MinConns: 1, MaxConnLifetime: 30 * time.Minute, MaxConnLifetimeJitter: time.Minute, MaxConnIdleTime: 5 * time.Minute, HealthCheckPeriod: 30 * time.Second},
		LeasePoolSize: 2, LeaseCheckInterval: time.Second,
		Session:         config.DatabaseSession{StatementTimeout: 30 * time.Second, LockTimeout: 5 * time.Second, IdleInTransactionSessionTimeout: 60 * time.Second, ClientConnectionCheckInterval: time.Second},
		Retry:           config.DatabaseRetry{InitialBackoff: 50 * time.Millisecond, MaxBackoff: 200 * time.Millisecond, MaxElapsed: 2 * time.Second},
		Keepalive:       config.DatabaseKeepalive{Idle: 15 * time.Second, Interval: 5 * time.Second, Count: 3, UserTimeout: 30 * time.Second},
		ServerKeepalive: config.DatabaseServerKeepalive{Idle: 60 * time.Second, Interval: 10 * time.Second, Count: 6},
		Preflight:       config.DatabasePreflight{ConnectionBudget: "enforce", ReservedConnections: 10, AllowSuperuser: true},
	}
}

// ScanClients is server.scan_clients of a stack with TLS, as the chart sets it: the names of the image scanner, the malware scanner and
// the updater, the kinds of pod the scan listener's handshake admits (R-ARCH-037, R-HA-066, R-HA-069).
func ScanClients() string {
	return `QUADER_SERVER_SCAN_CLIENTS=["` + KindName("vuln-scanner-trivy") + `","` + KindName("malware-scanner-clamav") + `","` +
		KindName("malware-updater-freshclam") + `"]`
}

// environ is the environment every replica shares: the database, the stack's
// Valkey (or a Sentinel that never answers), the object store, the key ring and
// the timings that keep the tests quick. A replica adds its identity and its
// ops listener.
func (s *Stack) environ(host, port, name, userFile, passFile string) []string {
	tb, dir := s.tb, s.dir
	vk := ValkeyConfig(tb)
	sentinels := make([]string, 0, len(vk.Sentinels))
	for _, a := range vk.Sentinels {
		sentinels = append(sentinels, strconv.Quote(a))
	}
	// "Valkey stopped" is silence, so a replica would hold its first readiness for valkey.startup_answer_timeout
	// (R-HA-008); the suites that use it test the degraded mode, not the start, and do not wait.
	answerWait := "30s" // the default
	if !s.valkey {
		sentinels = []string{strconv.Quote("127.0.0.1:1")}
		answerWait = "0s"
	}
	return []string{
		"QUADER_DATABASE_HOST=" + host, "QUADER_DATABASE_PORT=" + port, "QUADER_DATABASE_NAME=" + name,
		"QUADER_DATABASE_USERNAME_FILE=" + userFile, "QUADER_DATABASE_PASSWORD_FILE=" + passFile,
		"QUADER_DATABASE_SSL_MODE=verify-full", "QUADER_DATABASE_SSL_ROOT_CERT_FILE=" + CAFile(tb), "QUADER_DATABASE_PREFLIGHT_ALLOW_SUPERUSER=true",
		"QUADER_DATABASE_POOL_MAX_CONNS=6", "QUADER_DATABASE_POOL_MIN_CONNS=1", "QUADER_DATABASE_LEASE_POOL_SIZE=1",
		"QUADER_DATABASE_RETRY_MAX_ELAPSED=3s", "QUADER_HEALTH_DATABASE_OUTAGE_GRACE=2s",
		"QUADER_VALKEY_SENTINEL_ADDRESSES=[" + strings.Join(sentinels, ",") + "]", "QUADER_VALKEY_SENTINEL_PRIMARY_NAME=" + vk.PrimaryName,
		"QUADER_VALKEY_SENTINEL_PASSWORD_FILE=" + WriteFile(tb, dir, "vk-sentinel-pass", vk.SentinelPassword),
		"QUADER_VALKEY_USERNAME_FILE=" + WriteFile(tb, dir, "vk-user", vk.Username), "QUADER_VALKEY_PASSWORD_FILE=" + WriteFile(tb, dir, "vk-pass", vk.Password),
		"QUADER_VALKEY_TLS_CA_FILE=" + vk.CAFile, "QUADER_VALKEY_KEY_PREFIX=" + vk.KeyPrefix,
		"QUADER_VALKEY_STARTUP_ANSWER_TIMEOUT=" + answerWait,
		// One connection on the blob bucket; a prefix of the installation keeps parallel suites apart in one bucket.
		"QUADER_STORAGE_CONNECTIONS=" + ConnectionsJSON(tb, dir, s.ObjectPrefix()),
		"QUADER_SECRETS_ENCRYPTION_KEY_FILES=[" + strconv.Quote(s.sealed) + "]", "QUADER_SECRETS_ACTIVE_KEY_ID=2026-10",
		"QUADER_SERVER_BASE_URL=http://quader.test", "QUADER_SERVER_LISTEN=127.0.0.1:0",
		// The management API's own classes are tested where they are the subject;
		// elsewhere they would only slow the suite (a suite that tests them sets
		// QUADER_API_RATE_LIMITS_ENABLED=true).
		"QUADER_API_RATE_LIMITS_ENABLED=false",
		// The advisory sources of the vulnerability scanning default to the public internet. A test never reaches it: the
		// default hosts are .invalid (RFC 6761), so a scheduled sync of a worker fails at once instead of holding the one
		// maintenance worker while it downloads the public archives; a test of the scanning sets the test advisory source.
		"QUADER_VULNERABILITIES_SOURCES_OSV_BASE_URL=https://osv.invalid/",
		"QUADER_VULNERABILITIES_SOURCES_KEV_URL=https://kev.invalid/known_exploited_vulnerabilities.json",
		"QUADER_HEALTH_CHECK_INTERVAL=300ms", "QUADER_HEALTH_CHECK_TIMEOUT=200ms",
		// No edge routes to a replica of the harness, so a stop has nobody to drain for; a test of the drain sets the delay in
		// StackOptions.Env or ReplicaOptions.Env, which come after this and win (Q-ARCH-582).
		"QUADER_HEALTH_SHUTDOWN_DRAIN_DELAY=0s",
		"QUADER_SERVER_SHUTDOWN_TIMEOUT=5s", "QUADER_STORAGE_SPOOL_DIR=" + filepath.Join(dir, "spool"),
		"QUADER_SYSTEM_HEARTBEAT_INTERVAL=1s", "QUADER_SYSTEM_HEARTBEAT_STALE_AFTER=3s",
		"QUADER_CONFIG_FILE=" + WriteFile(tb, dir, "quader.yaml", ""),
	}
}

// ObjectPrefix is the installation's prefix in the stack's blob bucket, it/<database>.
func (s *Stack) ObjectPrefix() string { return "it/" + s.name }

// Database returns the name of the installation's database.
func (s *Stack) Database() string { return s.name }

// DatabaseConfig is the configuration a component opens the installation's
// database with, the way a replica does (through PgBouncer when
// QUADER_TEST_PG_POOLED is set). It is for the few assertions that need the
// component itself next to the replicas, such as counting the transactions of a
// repository reader.
func (s *Stack) DatabaseConfig() config.Database { return s.pg }

// Dir is the installation's scratch directory.
func (s *Stack) Dir() string { return s.dir }

// Environ is a copy of the environment every replica of the stack shares, for a
// suite that runs the quader binary instead of the in-process replica.
func (s *Stack) Environ() []string { return append([]string(nil), s.base...) }

// KeyFile writes a base64 key file into the scratch directory and returns its
// path: a replica whose key ring differs from the others sets
// QUADER_SECRETS_ENCRYPTION_KEY_FILES to it.
func (s *Stack) KeyFile(name string, key []byte) string {
	s.tb.Helper()
	return WriteFile(s.tb, s.dir, name, base64.StdEncoding.EncodeToString(key))
}

// Validator checks API answers against the OpenAPI document and records which
// operations the suite reached.
func (s *Stack) Validator() *apitest.Validator { return s.val }

// KeyRing is the stack's default key ring, the one every replica loads: a test signs what a worker signs with it, such as the
// blob token of the malware scanner's read (R-MAL-006).
func (s *Stack) KeyRing(tb testing.TB) *secrets.KeyRing {
	tb.Helper()
	ring, err := secrets.Load(config.Secrets{EncryptionKeyFiles: []string{s.sealed}, ActiveKeyID: "2026-10"})
	if err != nil {
		tb.Fatalf("the stack's key ring: %v", err)
	}
	return ring
}

// LimitsMode sets the mode of the download limits (R-LIM-026). A new instance starts in enforce; a test of another mechanism
// that sends more than a profile admits (many parallel transfers of one user, a load) asks for audit, in which nothing on a
// protocol path is refused or slowed (Q-LIM-4). Before a replica starts, or wait for the change area after it.
func (s *Stack) LimitsMode(mode string) {
	s.tb.Helper()
	s.Exec(`UPDATE limit_settings SET mode = $1, version = version + 1, updated_at = now()`, mode)
}

// Exec runs a statement on the installation's database as its owner.
func (s *Stack) Exec(sql string, args ...any) {
	s.tb.Helper()
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	if _, err := s.admin.Exec(context.Background(), sql, args...); err != nil {
		s.tb.Fatalf("%s: %v", sql, err)
	}
}

// Text runs a query that returns one text value.
func (s *Stack) Text(sql string, args ...any) string {
	s.tb.Helper()
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	var v string
	if err := s.admin.QueryRow(context.Background(), sql, args...).Scan(&v); err != nil {
		s.tb.Fatalf("%s: %v", sql, err)
	}
	return v
}

// TryText is Text without failing the test: a diagnostic that samples a running system reports the error instead.
func (s *Stack) TryText(sql string, args ...any) (string, error) {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	var v string
	err := s.admin.QueryRow(context.Background(), sql, args...).Scan(&v)
	return v, err
}

// TryQuery is Query without failing the test.
func (s *Stack) TryQuery(sql string, scan func(pgx.Rows) error, args ...any) error {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	rows, err := s.admin.Query(context.Background(), sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Number runs a query that returns one integer.
func (s *Stack) Number(sql string, args ...any) int64 {
	s.tb.Helper()
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	var v int64
	if err := s.admin.QueryRow(context.Background(), sql, args...).Scan(&v); err != nil {
		s.tb.Fatalf("%s: %v", sql, err)
	}
	return v
}

// TryNumber is Number for a goroutine other than the test's: it reports an error
// instead of failing the test.
func (s *Stack) TryNumber(sql string, args ...any) (int64, error) {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	var v int64
	err := s.admin.QueryRow(context.Background(), sql, args...).Scan(&v)
	return v, err
}

// Query runs a query and hands every row to scan.
func (s *Stack) Query(sql string, scan func(pgx.Rows) error, args ...any) {
	s.tb.Helper()
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	rows, err := s.admin.Query(context.Background(), sql, args...)
	if err != nil {
		s.tb.Fatalf("%s: %v", sql, err)
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			s.tb.Fatalf("%s: %v", sql, err)
		}
	}
	if err := rows.Err(); err != nil {
		s.tb.Fatalf("%s: %v", sql, err)
	}
}

// Replicas returns the replicas started so far, in start order.
func (s *Stack) Replicas() []*Replica {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*Replica(nil), s.reps...)
}

// Pick returns the web replicas in turn: the dispatcher of the scatter profile,
// so that consecutive requests of a test land on different replicas (R-ARCH-032).
func (s *Stack) Pick() *Replica {
	var web []*Replica
	for _, r := range s.Replicas() {
		if r.Role != "worker" && r.Role != "scanner" {
			web = append(web, r)
		}
	}
	if len(web) == 0 {
		s.tb.Fatal("no web replica is running")
	}
	return web[int(s.next.Add(1)-1)%len(web)]
}

// WaitFor polls cond until it holds or the time is up and returns how long it
// took.
func WaitFor(tb testing.TB, what string, within time.Duration, cond func() bool) time.Duration {
	tb.Helper()
	start := time.Now()
	for time.Since(start) < within {
		if cond() {
			return time.Since(start)
		}
		time.Sleep(20 * time.Millisecond)
	}
	tb.Fatalf("%s did not happen within %s", what, within)
	return 0
}
