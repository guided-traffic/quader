package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/cluster"
	"github.com/guided-traffic/quader/internal/config"
	"github.com/guided-traffic/quader/internal/observability/health"
	"github.com/guided-traffic/quader/internal/scanreport"
)

// R-OPS-048, Q-OPS-223: a replica of the role scanner reports itself to the scan listener it pulls from: its pod name, its state, the
// analyser's version and its slots, at once after the start; at its shutdown it says draining and signs off. The scan listener is a
// stand-in here; what packages does with the report is the integration tier's.
func TestR_OPS_048_AScannerReportsItselfToItsScanListenerAndSignsOff(t *testing.T) {
	var mu sync.Mutex
	var puts []scanreport.Report
	var deletes int
	listener := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, scanreport.PathPrefix):
			rep, err := scanreport.Decode(r.Body)
			if err != nil || rep.Validate(config.ScannerKindTrivy) != nil || !scanreport.ValidUID(strings.TrimPrefix(r.URL.Path, scanreport.PathPrefix)) {
				t.Errorf("the report: %+v %v", rep, err)
			}
			puts = append(puts, rep)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, scanreport.PathPrefix):
			deletes++
		default:
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer listener.Close()
	dir := t.TempDir()
	env := baseEnv(t, map[string]string{
		"QUADER_STORAGE_SPOOL_DIR":                   filepath.Join(dir, "spool"),
		"QUADER_VULNERABILITIES_IMAGES_SCAN_DIR":     filepath.Join(dir, "spool", "imagescan"),
		"QUADER_VULNERABILITIES_IMAGES_REGISTRY_URL": listener.URL,
		"QUADER_POD_NAME":                            "quader-vuln-scanner-trivy-7d9f6c5b8-abcde",
		"QUADER_NODE_NAME":                           "pool-a-7x2k",
	})
	logs := &syncBuffer{}
	a, err := New(Options{Loaded: loadCfg(t, "scanner", env), Role: "scanner", Environ: env, LogWriter: logs, Vuln: &VulnOverride{ImageAnalyser: stubAnalyser{}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-a.Started():
	case err := <-a.fatal:
		t.Fatalf("start-up failed: %v\n%s", err, logs.String())
	case <-time.After(startGuard):
		t.Fatalf("not started\n%s", logs.String())
	}
	deadline := time.Now().Add(startGuard)
	for {
		mu.Lock()
		n := len(puts)
		mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no report\n%s", logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	first := puts[0]
	mu.Unlock()
	if first.Name != "quader-vuln-scanner-trivy-7d9f6c5b8-abcde" || first.Node != "pool-a-7x2k" || first.ToolVersion != "0.76.2" ||
		first.Slots != (scanreport.Slots{InUse: 0, Total: 1}) || first.Database != "" || first.State == scanreport.StateDraining {
		t.Fatalf("the first report: %+v", first)
	}
	if err := a.Shutdown(context.WithoutCancel(ctx)); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if deletes != 1 || puts[len(puts)-1].State != scanreport.StateDraining {
		t.Fatalf("the shutdown: %d sign-offs, last report %+v\n%s", deletes, puts[len(puts)-1], logs.String())
	}
}

// Q-OPS-223: the kinds the management API names, from the configuration: the scan listener records a kind's reports only with TLS and
// the kind's name; without an address no scanner of the kind is configured, the role all's image scanning included (Q-ARCH-563).
func TestQ_OPS_221_TheKindsSayWhetherTheirReplicasReport(t *testing.T) {
	cfg := &config.Config{}
	cfg.Vulnerabilities.Enabled, cfg.Vulnerabilities.Images.Enabled, cfg.Malware.Enabled = true, true, true
	modes := func(string) string {
		var out []string
		for _, k := range scannerKinds(cfg) {
			out = append(out, string(k.Kind)+"="+string(k.Replicas)+"/"+k.Deployment+"/"+k.Tool)
		}
		return strings.Join(out, " ")
	}
	if got := modes(config.RoleAll); got != "trivy=none/vuln-scanner-trivy/Trivy clamav=none/malware-scanner-clamav/ClamAV" {
		t.Errorf("the role all: %s", got)
	}
	cfg.Vulnerabilities.Images.Scanner.URL, cfg.Malware.Scanner.URL = "https://quader-vuln-scanner-trivy.ns.svc:8080", "https://quader-malware-scanner-clamav.ns.svc:8080"
	if got := modes(config.RoleAPI); got != "trivy=unverified/vuln-scanner-trivy/Trivy clamav=unverified/malware-scanner-clamav/ClamAV" {
		t.Errorf("without TLS: %s", got)
	}
	cfg.Server.TLS.Certificates = []config.TLSCertificate{{CertFile: "c", KeyFile: "k"}}
	cfg.Vulnerabilities.Images.Registry.ClientName = "quader-vuln-scanner-trivy.ns.svc"
	if got := modes(config.RoleAPI); got != "trivy=reporting/vuln-scanner-trivy/Trivy clamav=unverified/malware-scanner-clamav/ClamAV" {
		t.Errorf("without the malware scanner's name: %s", got)
	}
	cfg.Malware.ScanListener.ClientName = "quader-malware-scanner-clamav.ns.svc"
	if got := modes(config.RoleAPI); got != "trivy=reporting/vuln-scanner-trivy/Trivy clamav=reporting/malware-scanner-clamav/ClamAV" {
		t.Errorf("as the chart sets it: %s", got)
	}
	cfg.Vulnerabilities.Images.Enabled, cfg.Malware.Enabled = false, false
	if got := scannerKinds(cfg); len(got) != 0 {
		t.Errorf("the scannings off: %+v", got)
	}

	a := &App{cfg: cfg}
	kind := a.scannerKindOf()
	withNames := func(names ...string) *http.Request {
		r := httptest.NewRequest("PUT", "/scan/v1/replicas/x", nil)
		r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{DNSNames: names}}}
		return r
	}
	for want, r := range map[string]*http.Request{
		"trivy":  withNames("quader-vuln-scanner-trivy.ns.svc"),
		"clamav": withNames("QUADER-MALWARE-SCANNER-CLAMAV.ns.svc"),
		"":       withNames("quader-worker.ns.svc"),
	} {
		if got := kind(r); got != want {
			t.Errorf("%v: %q, want %q", r.TLS.PeerCertificates[0].DNSNames, got, want)
		}
	}
	if got := kind(withNames("quader-vuln-scanner-trivy.ns.svc", "quader-malware-scanner-clamav.ns.svc")); got != "" {
		t.Errorf("a certificate of both kinds: %q", got)
	}
	if got := kind(httptest.NewRequest("PUT", "/", nil)); got != "" {
		t.Errorf("no TLS: %q", got)
	}
}

// R-OPS-048: the reason a scanner reports is its failing check's, without the synthetic draining, and none while it is ready or drains.
func TestR_OPS_048_TheReasonIsTheFailingChecks(t *testing.T) {
	for want, snap := range map[string]health.Snapshot{
		"database": {State: health.StateStarting, Checks: []health.CheckResult{{Name: "scanner", Status: health.Fail, Reason: "database"}, {Name: health.CheckDraining, Status: health.Pass}}},
		"scanner":  {State: health.StateUnready, Checks: []health.CheckResult{{Name: "scanner", Status: health.Fail}}},
		"":         {State: health.StateDraining, Checks: []health.CheckResult{{Name: health.CheckDraining, Status: health.Fail}}},
	} {
		if got := failingReason(snap); got != want {
			t.Errorf("%+v: %q, want %q", snap, got, want)
		}
	}
	if got := failingReason(health.Snapshot{State: health.StateReady}); got != "" {
		t.Errorf("ready: %q", got)
	}
}

func clusterRow(now time.Time) cluster.ScannerReplica {
	return cluster.ScannerReplica{
		InstanceUID: "8a0b5f8e-3c1d-4f2a-9b7e-1c2d3e4f5a6b", Kind: "clamav", Name: "quader-malware-scanner-clamav-6d9c7b5f4-x2v4q",
		Version: "0.42.0", Revision: "abc", State: "starting", Reason: "database", StateSince: now, StartedAt: now, HeartbeatAt: now,
		ToolVersion: "1.5.5", DatabaseVersion: "main-62.daily-27841.bytecode-339", DatabaseBuiltAt: now.Add(-6 * time.Hour),
		SlotsInUse: 1, Slots: 2,
	}
}

// R-OPS-048: a row of the store is answered in the API's shape: the database's build time only for the version in use.
func TestR_OPS_048_ARowIsAnsweredInTheShapeOfTheAPI(t *testing.T) {
	now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	got := scannerReplicaOf(clusterRow(now))
	b, _ := json.Marshal(got)
	for _, want := range []string{
		`"kind":"clamav"`, `"reason":"database"`, `"toolVersion":"1.5.5"`, `"slots":{"inUse":1,"total":2}`,
		`"database":{"builtAt":"2026-11-21T04:00:00Z","version":"main-62.daily-27841.bytecode-339"}`, `"node":null`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s in %s", want, b)
		}
	}
	row := clusterRow(now)
	row.DatabaseVersion, row.ToolVersion, row.Reason = "", "", ""
	b, _ = json.Marshal(scannerReplicaOf(row))
	for _, want := range []string{`"database":null`, `"toolVersion":null`, `"reason":null`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s in %s", want, b)
		}
	}
	var back gen.ScannerReplica
	if err := json.NewDecoder(io.LimitReader(strings.NewReader(string(b)), 1<<20)).Decode(&back); err != nil || back.Kind != gen.ScannerReplicaKindClamav {
		t.Errorf("the answer does not read back: %v", err)
	}
}
