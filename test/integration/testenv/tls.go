package testenv

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/tlsfiles/tlstest"
)

// Every connection of the integration stack is TLS, verified against the stack's own CA
// (INV-20, R-HA-063). The accessors below hand out the CA and the TLS settings of each
// dependency; [Main] and [RequireTLS] fail a suite whose environment names a plaintext endpoint.

// CAFile is the path of the CA certificate that signed every certificate of the stack
// (QUADER_TEST_CA_FILE).
func CAFile(tb testing.TB) string {
	tb.Helper()
	return Get(tb, "QUADER_TEST_CA_FILE")
}

// CertDir is the directory that holds the stack's certificates and keys
// (QUADER_TEST_CERT_DIR); see test/integration/certs.sh for the layout.
func CertDir(tb testing.TB) string {
	tb.Helper()
	return Get(tb, "QUADER_TEST_CERT_DIR")
}

// KindName is the name the pair of a kind of pod carries on a stack with TLS, and that the links check (R-HA-063, Q-ARCH-553): the
// chart's <fullname>-<kind>.<namespace>.svc, here <kind>.quader.test.
func KindName(kind string) string { return kind + ".quader.test" }

// roleKinds are the kinds whose names the pair of a role carries: web serves api and packages, all is every kind in one process but
// the malware scanner. The two scanners are processes of the role scanner with a pair of their kind each, vuln-scanner-trivy and
// malware-scanner-clamav (R-HA-065, R-HA-068, Q-HA-372), which a replica names in ReplicaOptions.Pair.
var roleKinds = map[string][]string{
	"web": {"api", "packages"}, "api": {"api"}, "packages": {"packages"}, "worker": {"worker"},
	"vuln-scanner-trivy": {"vuln-scanner-trivy"}, "malware-scanner-clamav": {"malware-scanner-clamav"},
	"all": {"api", "packages", "worker", "vuln-scanner-trivy"},
}

// clientRoles are the roles whose pair a link presents as a client, and so carries the usage client auth.
var clientRoles = map[string]bool{"worker": true, "vuln-scanner-trivy": true, "malware-scanner-clamav": true, "all": true}

// rolePairs issues a pair per role from the stack's CA into dir: the names of its kinds, localhost, 127.0.0.1 and ::1 (the harness
// verifies the replicas by address), server authentication, and client authentication for the roles that call a link (worker,
// both scanners, all), as the chart's Certificates have.
func rolePairs(tb testing.TB, dir string) map[string][2]string {
	tb.Helper()
	ca := tlstest.LoadCA(tb, CAFile(tb), filepath.Join(CertDir(tb), "ca.key"))
	out := map[string][2]string{}
	for role, kinds := range roleKinds {
		names := []string{"localhost"}
		for _, k := range kinds {
			names = append(names, KindName(k))
		}
		usages := []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		if clientRoles[role] {
			usages = append(usages, x509.ExtKeyUsageClientAuth)
		}
		leaf := ca.Issue(tb, tlstest.Options{
			DNSNames: names, IPs: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}, ExtKeyUsage: usages,
			NotAfter: time.Now().Add(6 * time.Hour),
		})
		c, k := leaf.Files(tb, dir, "tls-"+role)
		out[role] = [2]string{c, k}
	}
	return out
}

// RolePair is the pair of a role on a stack with TLS, for a test that presents it itself (a call to the scanner with the pair of
// packages, which must be refused).
func (s *Stack) RolePair(tb testing.TB, role string) tls.Certificate {
	tb.Helper()
	p, ok := s.pairs[role]
	if !ok {
		tb.Fatalf("the stack has no pair of the role %q (StackOptions.TLS)", role)
	}
	pair, err := tls.LoadX509KeyPair(p[0], p[1])
	if err != nil {
		tb.Fatal(err)
	}
	return pair
}

// RootCAs is a pool with only the stack's CA: the system roots are deliberately not part of it,
// so a client built from it cannot be satisfied by a certificate from anywhere else.
func RootCAs(tb testing.TB) *x509.CertPool {
	tb.Helper()
	pem, err := os.ReadFile(CAFile(tb))
	if err != nil {
		tb.Fatalf("read QUADER_TEST_CA_FILE: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		tb.Fatalf("QUADER_TEST_CA_FILE %s holds no certificate", CAFile(tb))
	}
	return pool
}

// TLSConfig is a client configuration that verifies the server against [RootCAs] (TLS 1.2 or later).
func TLSConfig(tb testing.TB) *tls.Config {
	tb.Helper()
	return &tls.Config{RootCAs: RootCAs(tb), MinVersion: tls.VersionTLS12}
}

// PgDSN is a verify-full DSN of the stack's PostgreSQL (direct) for the database name, using the
// stack's superuser. The CA is in the DSN, so a pgx client needs nothing else.
func PgDSN(tb testing.TB, database string) string {
	tb.Helper()
	return pgDSN(tb, Get(tb, "QUADER_TEST_PG_PORT"), database)
}

// PgBouncerDSNFor is the same through PgBouncer for the database name.
func PgBouncerDSNFor(tb testing.TB, database string) string {
	tb.Helper()
	return pgDSN(tb, Get(tb, "QUADER_TEST_PG_PGBOUNCER_PORT"), database)
}

func pgDSN(tb testing.TB, port, database string) string {
	tb.Helper()
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(Get(tb, "QUADER_TEST_PG_USER"), Get(tb, "QUADER_TEST_PG_PASSWORD")),
		Host:     Get(tb, "QUADER_TEST_PG_HOST") + ":" + port,
		Path:     "/" + database,
		RawQuery: url.Values{"sslmode": {"verify-full"}, "sslrootcert": {CAFile(tb)}}.Encode(),
	}
	return u.String()
}

// SecureDatabase sets the TLS settings of a quader database configuration: ssl_mode verify-full
// and ssl_root_cert_file pointing at the stack's CA. It never touches allow_insecure.
func SecureDatabase(tb testing.TB, cfg config.Database) config.Database {
	tb.Helper()
	cfg.SSLMode = "verify-full"
	cfg.SSLRootCertFile = CAFile(tb)
	cfg.AllowInsecure = false
	return cfg
}

// SecureS3 sets the TLS settings of a quader S3 storage configuration: an https endpoint and
// ca_file pointing at the stack's CA, with the insecure-endpoint switch off.
func SecureS3(tb testing.TB, cfg config.StorageS3) config.StorageS3 {
	tb.Helper()
	cfg.Endpoint = S3Config(tb).Endpoint
	// Under QUADER_TEST_SCOPE=s3 the store may be one with a publicly trusted certificate (AWS S3):
	// without a stack CA the system roots verify it. Every other run has the stack's CA.
	if os.Getenv("QUADER_TEST_SCOPE") != "s3" || os.Getenv("QUADER_TEST_CA_FILE") != "" {
		cfg.CAFile = CAFile(tb)
	}
	cfg.AllowInsecureEndpoint = false
	return cfg
}

// RequireTLS fails tb when the environment names an endpoint that is not TLS-verified: a
// PostgreSQL DSN without sslmode=verify-full and sslrootcert, an S3 endpoint that is not https, a
// missing or unreadable CA file. A variable that is unset fails too; the stack is not configured.
func RequireTLS(tb testing.TB) {
	tb.Helper()
	if err := checkTLS(os.Getenv); err != nil {
		tb.Fatalf("%v", err)
	}
}

// Main is the TestMain body of an integration suite: it runs [RequireTLS]'s check once before
// any test and exits non-zero with the reason if it fails. Use it as
//
//	func TestMain(m *testing.M) { os.Exit(testenv.Main(m)) }
func Main(m *testing.M) int {
	if err := CheckTLS(); err != nil {
		fmt.Fprintf(os.Stderr, "integration environment: %v\n", err)
		return 1
	}
	return m.Run()
}

// CheckTLS is the check [Main] runs, for a suite whose TestMain does more than run the tests.
func CheckTLS() error { return checkTLS(os.Getenv) }

// checkTLS validates the TLS side of the environment through get (os.Getenv in production).
//
// QUADER_TEST_SCOPE selects what the environment must provide. Unset (or "stack") is the
// integration stack of make integration-up: every endpoint must be set and TLS-verified. "s3" is
// the S3 conformance leg of Test and Release against another store (MinIO; AWS S3 once an account exists), which runs only the
// Conformance tests and provides only the QUADER_TEST_S3_* variables: the S3 endpoint must be https,
// and a CA file, when set, must be readable; without one the system roots verify the store.
func checkTLS(get func(string) string) error {
	hint := "; run `make integration-up`, then `make test-integration`"
	switch scope := get("QUADER_TEST_SCOPE"); scope {
	case "", "stack":
	case "s3":
		if ca := get("QUADER_TEST_CA_FILE"); ca != "" {
			if st, err := os.Stat(ca); err != nil || st.IsDir() {
				return fmt.Errorf("QUADER_TEST_CA_FILE %s is not a readable file", ca)
			}
		}
		return checkS3Endpoint(get, "")
	default:
		return fmt.Errorf("QUADER_TEST_SCOPE %q is not stack or s3", scope)
	}
	ca := get("QUADER_TEST_CA_FILE")
	if ca == "" {
		return fmt.Errorf("QUADER_TEST_CA_FILE is not set: the integration stack is not configured for TLS%s", hint)
	}
	if st, err := os.Stat(ca); err != nil || st.IsDir() {
		return fmt.Errorf("QUADER_TEST_CA_FILE %s is not a readable file%s", ca, hint)
	}
	for _, name := range []string{"QUADER_TEST_PG_DSN", "QUADER_TEST_PG_PGBOUNCER_DSN"} {
		dsn := get(name)
		if dsn == "" {
			return fmt.Errorf("%s is not set%s", name, hint)
		}
		u, err := url.Parse(dsn)
		if err != nil {
			return fmt.Errorf("%s is not a URL: %w", name, err)
		}
		q := u.Query()
		if q.Get("sslmode") != "verify-full" || q.Get("sslrootcert") == "" {
			return fmt.Errorf("%s is plaintext or unverified (need sslmode=verify-full and sslrootcert); the stack is TLS-only (INV-20)", name)
		}
	}
	if err := checkS3Endpoint(get, hint); err != nil {
		return err
	}
	if get("QUADER_TEST_VALKEY_CA_FILE") == "" {
		return fmt.Errorf("QUADER_TEST_VALKEY_CA_FILE is not set%s", hint)
	}
	return nil
}

// HTTPClient is an HTTP client that verifies servers against [RootCAs]. It is for tests that talk
// to the stack directly (the S3 gateway); quader's own code gets its clients from internal/outbound.
func HTTPClient(tb testing.TB) *http.Client {
	tb.Helper()
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: TLSConfig(tb)},
	}
}

// checkS3Endpoint requires QUADER_TEST_S3_ENDPOINT to be set and https.
func checkS3Endpoint(get func(string) string, hint string) error {
	ep := get("QUADER_TEST_S3_ENDPOINT")
	if ep == "" {
		return fmt.Errorf("QUADER_TEST_S3_ENDPOINT is not set%s", hint)
	}
	if !strings.HasPrefix(strings.ToLower(ep), "https://") {
		return fmt.Errorf("QUADER_TEST_S3_ENDPOINT %q is plaintext; the stack is TLS-only (INV-20)", ep)
	}
	return nil
}
