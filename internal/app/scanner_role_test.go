package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/vuln/imagescan"
)

// stubAnalyser stands for the pinned binary in a scanner whose start is under test.
type stubAnalyser struct{}

func (stubAnalyser) Version() string { return "0.76.2" }
func (stubAnalyser) Analyse(context.Context, string, string, string, imagescan.Limits) error {
	return errors.New("not called")
}

func (stubAnalyser) Detect(context.Context, string, string, string, string, imagescan.Limits) error {
	return errors.New("not called")
}

// R-ARCH-036, Q-ARCH-527: a replica of the role scanner builds its one area, opens no connection to the database, Valkey or the object
// store (their secret files do not exist and no step reads them), becomes ready, and binds the scanner's listener and the ops listener
// and nothing else. The unit tier shows it: the role needs no infrastructure to start.
func TestR_ARCH_036_AScannerStartsWithoutTheInfrastructureAndServesItsOneRoute(t *testing.T) {
	sub, err := SubsystemsFor("scanner")
	if err != nil || !slices.Equal(sub.Areas, []Area{AreaScanner}) || sub.MainPool || sub.Valkey || sub.Registry || sub.ClientListener || !sub.Scanner {
		t.Fatalf("%+v %v", sub, err)
	}
	if _, err := planSteps(wiringSteps(), sub, "scanner"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	env := baseEnv(t, map[string]string{
		"QUADER_DATABASE_PASSWORD_FILE":              filepath.Join(dir, "missing", "db-password"),
		"QUADER_VALKEY_PASSWORD_FILE":                filepath.Join(dir, "missing", "valkey-password"),
		"QUADER_SECRETS_ENCRYPTION_KEY_FILES":        `["` + filepath.Join(dir, "missing", "key") + `"]`,
		"QUADER_STORAGE_SPOOL_DIR":                   filepath.Join(dir, "spool"),
		"QUADER_VULNERABILITIES_IMAGES_SCAN_DIR":     filepath.Join(dir, "spool", "imagescan"),
		"QUADER_VULNERABILITIES_IMAGES_REGISTRY_URL": "https://127.0.0.1:1",
	})
	logs := &syncBuffer{}
	a, err := New(Options{Loaded: loadCfg(t, "scanner", env), Role: "scanner", Environ: env, LogWriter: logs, Vuln: &VulnOverride{ImageAnalyser: stubAnalyser{}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = a.Shutdown(context.WithoutCancel(ctx))
	})
	select {
	case <-a.Started():
	case err := <-a.fatal:
		t.Fatalf("start-up failed: %v\n%s", err, logs.String())
	case <-time.After(startGuard):
		t.Fatalf("not started\n%s", logs.String())
	}
	if !slices.Equal(a.Areas(), []string{"scanner"}) || a.database() != nil || a.valkey() != nil || a.Blobs() != nil || a.ClientAddr() != nil {
		t.Fatalf("areas %v, a connection was built", a.Areas())
	}
	if len(a.EndpointNames()) != 0 || a.ScannerAddr() == nil {
		t.Fatalf("endpoints %v, scanner %v", a.EndpointNames(), a.ScannerAddr())
	}
	deadline := time.Now().Add(startGuard)
	for !a.health.Ready() {
		if time.Now().After(deadline) {
			t.Fatalf("not ready\n%s", logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	base := "http://" + a.ScannerAddr().String()
	if code, _, _ := get(t, base+"/api/v1/system/ping"); code != http.StatusNotFound {
		t.Fatalf("the scanner serves a route of an endpoint: %d", code)
	}
	res, err := http.Post(base+imagescan.AnalysesPath, "application/json", strings.NewReader(`{"repository":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("a malformed call: %d", res.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("something created the directory of the secret files")
	}
	// Q-OPS-237: the scanner's spool exports the two gauges of every spool from its start, so QuaderSpoolNearlyFull sees a pod of
	// vuln-scanner-trivy before its first image: nothing held, the free space of its volume.
	_, _, metrics := get(t, "http://"+a.OpsAddr().String()+"/metrics")
	used, free := metricValue(metrics, "quader_spool_used_bytes"), metricValue(metrics, "quader_spool_free_bytes")
	if used != "0" || free == "" || free == "0" {
		t.Fatalf("the scanner's spool exports used %q and free %q, want 0 and the volume's free bytes", used, free)
	}
}

// metricValue is the value of the one unlabelled series of a family in an exposition, "" when it has none.
func metricValue(exposition, family string) string {
	for line := range strings.SplitSeq(exposition, "\n") {
		if v, ok := strings.CutPrefix(line, family+" "); ok {
			return v
		}
	}
	return ""
}
