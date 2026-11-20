package config

import (
	"strings"
	"testing"
	"time"
)

const (
	testScannerURL  = "QUADER_VULNERABILITIES_IMAGES_SCANNER_URL=https://quader-scanner.quader.svc:8080"
	testRegistryURL = "QUADER_VULNERABILITIES_IMAGES_REGISTRY_URL=https://quader-packages-scan.quader.svc:8082"
)

func loadRole(t *testing.T, role string, env ...string) error {
	t.Helper()
	_, err := Load(Options{Environ: servableEnv(append(env, "QUADER_PROCESS_ROLE="+role)...), Command: CommandServe, ReadFile: fakeFS(nil)})
	return err
}

func TestR_VULN_025_TheLinksOfTheImageScanningAreValidatedForTheRoleThatUsesThem(t *testing.T) {
	t.Run("a worker with the image scanning on needs the scanner's address", func(t *testing.T) {
		containsAll(t, issuesOf(t, loadRole(t, "worker")), "vulnerabilities.images.scanner.url is required in a worker")
		if err := loadRole(t, "worker", testScannerURL); err != nil {
			t.Fatal(err)
		}
		for _, off := range []string{"QUADER_VULNERABILITIES_IMAGES_ENABLED=false", "QUADER_VULNERABILITIES_ENABLED=false"} {
			if err := loadRole(t, "worker", off); err != nil {
				t.Errorf("%s: %v", off, err)
			}
		}
	})
	t.Run("Q-ARCH-531 the role all may leave both addresses empty: its own listeners", func(t *testing.T) {
		if err := loadRole(t, "all"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a scanner needs the scan listener's address", func(t *testing.T) {
		containsAll(t, issuesOf(t, loadRole(t, "scanner")), "vulnerabilities.images.registry.url is required in a scanner")
	})
	t.Run("an address is an origin", func(t *testing.T) {
		for _, bad := range []string{"ftp://scanner:8080", "https://scanner:8080/scan", "https://user:pw@scanner:8080", "scanner:8080"} {
			err := loadRole(t, "worker", "QUADER_VULNERABILITIES_IMAGES_SCANNER_URL="+bad)
			containsAll(t, issuesOf(t, err), "vulnerabilities.images.scanner.url")
			if strings.Contains(err.Error(), "pw@") {
				t.Errorf("the password is printed: %v", err)
			}
		}
		if err := loadRole(t, "worker", "QUADER_VULNERABILITIES_IMAGES_SCANNER_URL=http://127.0.0.1:18080"); err != nil {
			t.Errorf("an http origin, for an installation without TLS: %v", err)
		}
	})
	t.Run("the pull's timeout and the token's lifetime lie between a minute and an hour", func(t *testing.T) {
		for _, d := range []string{"2h", "30s"} {
			containsAll(t, issuesOf(t, loadRole(t, "worker", testScannerURL, "QUADER_VULNERABILITIES_IMAGES_PULL_TIMEOUT="+d)), "vulnerabilities.images.pull_timeout")
		}
	})
	t.Run("the defaults", func(t *testing.T) {
		l := mustLoad(t, Options{Environ: servableEnv(testScannerURL, "QUADER_PROCESS_ROLE=worker"), Command: CommandServe, ReadFile: fakeFS(nil)})
		i := l.Config.Vulnerabilities.Images
		if i.PullTimeout != 10*time.Minute || i.Concurrency != 2 || i.Scanner.Slots != 1 || l.Config.Server.ScanListen != "" || l.Config.Server.InternalTLS.CAFile != "" {
			t.Fatalf("%+v %+v", i, l.Config.Server)
		}
	})
}

func TestQ_ARCH_527_TheScannerReadsOnlyTheKeysItUsesAndResolvesNoSecretFile(t *testing.T) {
	// A database, Valkey and object-store configuration whose files the scanner's pod does not have, and keys outside the role's
	// sections that would fail their checks: the scanner loads; every other role refuses the same configuration.
	env := []string{
		testRegistryURL,
		"QUADER_DATABASE_PASSWORD_FILE=/var/run/secrets/quader/db/password",
		"QUADER_VALKEY_PASSWORD_FILE=/var/run/secrets/quader/valkey/password",
		"QUADER_STORAGE_S3_CREDENTIALS_SOURCE=static",
		"QUADER_SECRETS_ENCRYPTION_KEY_FILES=[]",
	}
	if err := loadRole(t, "scanner", env...); err != nil {
		t.Fatalf("the scanner: %v", err)
	}
	containsAll(t, issuesOf(t, loadRole(t, "packages", env...)), "secrets.encryption_key_files is required", "storage.s3.credentials")
	// What the role reads is still validated.
	containsAll(t, issuesOf(t, loadRole(t, "scanner", testRegistryURL, "QUADER_SERVER_LISTEN=nonsense")), "server.listen")
	for _, path := range []string{"server.listen", "server.internal_tls.ca_file", "vulnerabilities.images.registry.url", "storage.spool.max_bytes", "health.check_interval"} {
		if !ScannerReads(path, ScannerKindImage) {
			t.Errorf("the scanner does not read %s", path)
		}
	}
	for _, path := range []string{"database.password_file", "valkey.password_file", "storage.s3.bucket", "secrets.encryption_key_files", "auth.oidc.issuer", "proxy.outbound.proxy_from_environment", "vulnerabilities.sources.osv.mode"} {
		if ScannerReads(path, ScannerKindImage) {
			t.Errorf("the scanner reads %s", path)
		}
	}
}

func TestR_ARCH_037_TheScanListenerAndTheInternalTrustAnchorAreValidated(t *testing.T) {
	t.Run("R-EP-001 the endpoint name scan is reserved", func(t *testing.T) {
		err := loadRole(t, "web", `QUADER_SERVER_ENDPOINTS={"scan":{"listen":":8443","base_url":"https://scan.example.com"}}`)
		containsAll(t, issuesOf(t, err), "server.endpoints.scan names the reserved endpoint of the scan listener")
	})
	t.Run("the scan listener has a port of its own", func(t *testing.T) {
		for _, addr := range []string{":8080", ":8081"} {
			containsAll(t, issuesOf(t, loadRole(t, "packages", "QUADER_SERVER_SCAN_LISTEN="+addr)), "server.scan_listen uses port")
		}
		containsAll(t, issuesOf(t, loadRole(t, "packages", "QUADER_SERVER_SCAN_LISTEN=8082")), "server.scan_listen")
		if err := loadRole(t, "packages", "QUADER_SERVER_SCAN_LISTEN=:8082"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("R-ARCH-038 an internal CA without a pair is refused", func(t *testing.T) {
		containsAll(t, issuesOf(t, loadRole(t, "packages", "QUADER_SERVER_INTERNAL_TLS_CA_FILE=/ca.crt")), "server.internal_tls.ca_file is set while tls.certificates is empty")
	})
}

// R-ARCH-038, Q-ARCH-552, Q-ARCH-553: with TLS, the listener of each link names the one kind it admits; the role all may leave the
// names empty, and no name may hold a wildcard or a port.
func TestR_ARCH_038_TheListenersOfTheLinksNameTheKindTheyAdmit(t *testing.T) {
	tlsOn := []string{`QUADER_SERVER_TLS_CERTIFICATES=[{"cert_file":"/tls/tls.crt","key_file":"/tls/tls.key"}]`, "QUADER_SERVER_INTERNAL_TLS_CA_FILE=/tls/ca.crt"}
	with := func(env ...string) []string { return append(append([]string{}, tlsOn...), env...) }
	t.Run("a scanner with TLS needs the worker's name", func(t *testing.T) {
		containsAll(t, issuesOf(t, loadRole(t, "scanner", with(testRegistryURL)...)), "vulnerabilities.images.scanner.client_name is required in a scanner")
		if err := loadRole(t, "scanner", with(testRegistryURL, "QUADER_VULNERABILITIES_IMAGES_SCANNER_CLIENT_NAME=quader-worker.quader.svc")...); err != nil {
			t.Fatal(err)
		}
		if err := loadRole(t, "scanner", testRegistryURL); err != nil {
			t.Fatalf("without TLS no name is checked: %v", err)
		}
	})
	t.Run("a process that opens the scan listener with TLS needs the scanner's name", func(t *testing.T) {
		for _, role := range []string{"packages", "web"} {
			containsAll(t, issuesOf(t, loadRole(t, role, with("QUADER_SERVER_SCAN_LISTEN=:8082")...)), "server.scan_clients is required while scan_listen is open")
			// Every kind of scanner the scan listener admits is one entry: a second kind adds its name (Q-ARCH-552).
			if err := loadRole(t, role, with("QUADER_SERVER_SCAN_LISTEN=:8082", `QUADER_SERVER_SCAN_CLIENTS=["quader-scanner.quader.svc","quader-other-scanner.quader.svc"]`)...); err != nil {
				t.Errorf("%s: %v", role, err)
			}
		}
		if err := loadRole(t, "packages", tlsOn...); err != nil {
			t.Fatalf("packages without a scan listener: %v", err)
		}
	})
	t.Run("Q-ARCH-553 the role all may leave both names empty", func(t *testing.T) {
		if err := loadRole(t, "all", with("QUADER_SERVER_SCAN_LISTEN=:8082")...); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a name is compared exactly, so it holds no wildcard, port or path", func(t *testing.T) {
		for _, bad := range []string{"*.quader.svc", "quader-worker.quader.svc:8080", "quader-worker/x"} {
			containsAll(t, issuesOf(t, loadRole(t, "worker", testScannerURL, "QUADER_VULNERABILITIES_IMAGES_SCANNER_SERVER_NAME="+bad)), "vulnerabilities.images.scanner.server_name")
			containsAll(t, issuesOf(t, loadRole(t, "packages", `QUADER_SERVER_SCAN_CLIENTS=["`+bad+`"]`)), "server.scan_clients[0]")
		}
		if err := loadRole(t, "worker", testScannerURL, "QUADER_VULNERABILITIES_IMAGES_SCANNER_SERVER_NAME=::1"); err != nil {
			t.Fatalf("an IPv6 address: %v", err)
		}
	})
}
