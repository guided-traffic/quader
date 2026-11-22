//go:build chart

package chart_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type alertSeverity struct{ name, severity string }

// alertSeverities are the alerts the chart renders, in the order of templates/_rules.tpl, with their severity: every row of
// R-HA-045's table, and the alerts of the vulnerability scanning, the malware scanning, the signatures, the signing identity and the mirror.
func alertSeverities() []alertSeverity {
	return []alertSeverity{
		{"QuaderReplicasNotReady", "critical"},
		{"QuaderDatabaseUnreachable", "critical"},
		{"QuaderObjectStoreUnreachable", "critical"},
		{"QuaderObjectStoreConnectionUnreachable", "warning"},
		{"QuaderObjectStoreUnreachableSingleReplica", "warning"},
		{"QuaderStorageReplicationBacklog", "warning"},
		{"QuaderStorageCopyDamaged", "warning"},
		{"QuaderStorageScrubBehind", "warning"},
		{"QuaderStorageConnectionDetached", "warning"},
		{"QuaderDatabasePoolSaturated", "warning"},
		{"QuaderControlConnectionWaits", "warning"},
		{"QuaderJobsStuck", "warning"},
		{"QuaderLeaseLost", "warning"},
		{"QuaderMixedVersions", "warning"},
		{"QuaderSpoolNearlyFull", "warning"},
		{"QuaderVulnerabilityDataStale", "warning"},
		{"QuaderVulnerabilitySyncFailing", "warning"},
		{"QuaderVulnerabilityRecordsRejected", "warning"},
		{"QuaderVulnerabilityRecordsInvalid", "warning"},
		{"QuaderVulnerabilityMaliciousRecordRejected", "critical"},
		{"QuaderVulnerabilityClaimedNameMalicious", "critical"},
		{"QuaderVulnerabilityImageScansFailing", "warning"},
		{"QuaderVulnerabilityImageScannerUnavailable", "warning"},
		{"QuaderMalwareFound", "warning"},
		{"QuaderMalwareDatabaseStale", "warning"},
		{"QuaderMalwareScannerUnavailable", "critical"},
		{"QuaderMalwareBacklog", "warning"},
		{"QuaderSignatureInvalid", "warning"},
		{"QuaderSignatureBacklog", "warning"},
		{"QuaderSignatureTrustRootStale", "warning"},
		{"QuaderSignatureGatePending", "warning"},
		{"QuaderSigningIdentityExpiring", "warning"},
		{"QuaderSigningIdentityExpired", "critical"},
		{"QuaderSigningIdentityNotLoaded", "warning"},
		{"QuaderSigningBacklog", "warning"},
		{"QuaderMirrorFailing", "warning"},
		{"QuaderMirrorStale", "warning"},

		{"QuaderBlobMissing", "critical"},
		{"QuaderBlobCorrupt", "critical"},
		{"QuaderGCStale", "warning"},
		{"QuaderBackupCheckFailed", "critical"},
		{"QuaderRevocationJournalBacklog", "warning"},
		{"QuaderRevocationJournalQuarantined", "critical"},
		{"QuaderValkeyUnavailable", "critical"},
		{"QuaderValkeyUnavailableSingleReplica", "warning"},
		{"QuaderValkeySentinelsDegraded", "warning"},
		{"QuaderValkeyFailoverFlapping", "warning"},
		{"QuaderValkeyPrimaryDisagreement", "critical"},
		{"QuaderBackupStale", "critical"},
		{"QuaderBackupFailed", "critical"},
		{"QuaderBackupRunningLong", "warning"},
		{"QuaderBackupGlobalHoldLong", "warning"},
		{"QuaderBackupSourceDamage", "critical"},
		{"QuaderBackupVerifyFindings", "critical"},
	}
}

func renderedRules(t *testing.T, sets ...string) []any {
	t.Helper()
	_, docs := render(t, []string{monitoringSet()}, sets...)
	pr := find(docs, "PrometheusRule", "quader")
	if pr == nil {
		t.Fatal("no PrometheusRule")
	}
	return list(pr, "spec", "groups", "0", "rules")
}

// TestR_HA_045_PrometheusRule (R-HA-045) checks the PrometheusRule of the chart: every alert of the requirement's table is
// rendered with its severity and duration, reads a metric that code under internal/ registers (or kube-state-metrics), and has
// a rule test in which it fires and one in which it stays silent (deploy/helm/quader/tests/rules_test.yaml, run by
// TestR_VULN_024_PromtoolRules). The acceptance criteria that need a Prometheus scraping a running profile are the
// end-to-end suite's: the rule tests prove the expressions against given series, not that a stopped component produces them.
func TestR_HA_045_PrometheusRule(t *testing.T) {
	t.Run("it is rendered when metrics.prometheusRule.enabled is true and carries the configured labels", func(t *testing.T) {
		_, docs := render(t, []string{monitoringSet()})
		pr := find(docs, "PrometheusRule", "quader")
		if pr == nil {
			t.Fatal("no PrometheusRule with metrics.prometheusRule.enabled: true")
		}
		if str(pr, "apiVersion") != "monitoring.coreos.com/v1" {
			t.Errorf("apiVersion = %q", str(pr, "apiVersion"))
		}
		if str(pr, "metadata", "labels", "release") != "kube-prometheus-stack" {
			t.Errorf("labels = %v, want release: kube-prometheus-stack from metrics.prometheusRule.labels", at(pr, "metadata", "labels"))
		}
		if str(pr, "metadata", "labels", "app.kubernetes.io/name") == "" {
			t.Errorf("the chart's own labels are missing: %v", at(pr, "metadata", "labels"))
		}
		if got := str(pr, "spec", "groups", "0", "name"); got != "quader" {
			t.Errorf("group name = %q, want the full name of the release", got)
		}
	})

	t.Run("it is not rendered when metrics.prometheusRule.enabled is false, whatever else is set", func(t *testing.T) {
		out, _ := render(t, []string{monitoringSet()}, "metrics.prometheusRule.enabled=false")
		if strings.Contains(out, "kind: PrometheusRule") {
			t.Error("a PrometheusRule is rendered with metrics.prometheusRule.enabled: false")
		}
		out, _ = render(t, required())
		if strings.Contains(out, "kind: PrometheusRule") {
			t.Error("a PrometheusRule is rendered by the defaults")
		}
	})

	t.Run("the chart renders exactly these alerts with these severities", func(t *testing.T) {
		want := alertSeverities()
		got := renderedRules(t)
		if len(got) != len(want) {
			t.Errorf("%d alerts rendered, want %d", len(got), len(want))
		}
		for i, w := range want {
			if i >= len(got) {
				t.Errorf("alert %s is not rendered", w.name)
				continue
			}
			if str(got[i], "alert") != w.name {
				t.Errorf("alert %d = %s, want %s", i, str(got[i], "alert"), w.name)
				continue
			}
			if sev := str(got[i], "labels", "severity"); sev != w.severity {
				t.Errorf("%s severity = %q, want %q", w.name, sev, w.severity)
			}
		}
	})

	t.Run("every alert has an expression, a summary and a description, and a duration only where the table has one", func(t *testing.T) {
		forOf := map[string]string{
			"QuaderReplicasNotReady": "5m", "QuaderDatabaseUnreachable": "2m", "QuaderObjectStoreUnreachable": "2m",
			"QuaderObjectStoreUnreachableSingleReplica": "5m", "QuaderObjectStoreConnectionUnreachable": "2m",
			"QuaderStorageReplicationBacklog": "30m", "QuaderStorageCopyDamaged": "1h", "QuaderStorageScrubBehind": "6h",
			"QuaderStorageConnectionDetached": "15m", "QuaderBlobCorrupt": "", "QuaderDatabasePoolSaturated": "10m", "QuaderControlConnectionWaits": "5m",
			"QuaderJobsStuck": "5m", "QuaderLeaseLost": "", "QuaderMixedVersions": "2h", "QuaderSpoolNearlyFull": "10m",
			"QuaderBlobMissing": "", "QuaderGCStale": "1h", "QuaderValkeyUnavailable": "2m", "QuaderValkeyUnavailableSingleReplica": "5m",
			"QuaderValkeySentinelsDegraded": "5m", "QuaderValkeyFailoverFlapping": "", "QuaderValkeyPrimaryDisagreement": "5m",
			"QuaderBackupCheckFailed": "1h", "QuaderRevocationJournalBacklog": "15m", "QuaderRevocationJournalQuarantined": "", "QuaderBackupStale": "", "QuaderBackupFailed": "", "QuaderBackupRunningLong": "",
			"QuaderBackupGlobalHoldLong": "30m", "QuaderBackupSourceDamage": "", "QuaderBackupVerifyFindings": "",
			// R-SIG-039's table and R-SIG-075's.
			"QuaderSignatureInvalid": "", "QuaderSignatureBacklog": "15m", "QuaderSignatureTrustRootStale": "1h", "QuaderSignatureGatePending": "15m",
			// R-SIG-057's table.
			"QuaderSigningIdentityExpiring": "1h", "QuaderSigningIdentityExpired": "", "QuaderSigningIdentityNotLoaded": "15m", "QuaderSigningBacklog": "15m",
		}
		for _, r := range renderedRules(t) {
			name := str(r, "alert")
			if str(r, "expr") == "" || str(r, "annotations", "summary") == "" || str(r, "annotations", "description") == "" {
				t.Errorf("%s: expr, summary or description is empty", name)
			}
			if want, ok := forOf[name]; ok && str(r, "for") != want {
				t.Errorf("%s: for = %q, want %q (R-HA-045's table)", name, str(r, "for"), want)
			}
		}
	})

	t.Run("the role regex of QuaderReplicasNotReady follows the full name and covers every role and pool Deployment", func(t *testing.T) {
		_, docs := renderRelease(t, "acme", []string{monitoringSet()})
		pr := find(docs, "PrometheusRule", "acme-quader")
		if pr == nil {
			t.Fatal("no PrometheusRule for release acme (full name acme-quader)")
		}
		found := false
		for _, r := range list(pr, "spec", "groups", "0", "rules") {
			if str(r, "alert") != "QuaderReplicasNotReady" {
				continue
			}
			found = true
			want := `deployment=~"acme-quader-(api|packages|worker|vuln-scanner-trivy|malware-scanner-clamav)(-.*)?"`
			if !strings.Contains(str(r, "expr"), want) {
				t.Errorf("expr = %q, want it to contain %s", str(r, "expr"), want)
			}
			if !strings.Contains(str(r, "expr"), `min by (deployment)`) {
				t.Errorf("expr = %q is not per Deployment", str(r, "expr"))
			}
		}
		if !found {
			t.Error("QuaderReplicasNotReady is not rendered")
		}
	})

	t.Run("every row of the table of R-HA-045 is rendered, with the expression the table gives", func(t *testing.T) {
		exprs := map[string]string{}
		for _, r := range renderedRules(t) {
			exprs[str(r, "alert")] = str(r, "expr")
		}
		for name, want := range map[string]string{
			"QuaderBackupCheckFailed":        `min(quader_backup_check_ok{ namespace="default" }) == 0`,
			"QuaderRevocationJournalBacklog": `max by (connection) (quader_revocation_journal_pending{ namespace="default" }) > 0`,
			"QuaderObjectStoreUnreachable":   `min(quader_storage_s3_circuit_open{ namespace="default" }) == 1`,
			"QuaderObjectStoreConnectionUnreachable": `min by (connection) (quader_storage_s3_circuit_open{ namespace="default" }) == 1 unless on () ` +
				`(min(quader_storage_s3_circuit_open{ namespace="default" }) == 1)`,
			"QuaderObjectStoreUnreachableSingleReplica": `max by (connection) (quader_storage_s3_circuit_open{ namespace="default" }) == 1 and ` +
				`min by (connection) (quader_storage_s3_circuit_open{ namespace="default" }) == 0`,
			"QuaderStorageReplicationBacklog": `max by (connection) (quader_storage_reconcile_queue_oldest_seconds{ namespace="default" }) > 3600 ` +
				`unless on (connection) (max by (connection) (quader_storage_connection_filling{ namespace="default" }) == 1)`,
			"QuaderStorageCopyDamaged":        `max by (connection) (quader_storage_copies{ namespace="default",state=~"missing|corrupt" }) > 0`,
			"QuaderStorageScrubBehind":        `max by (connection) (quader_storage_scrub_overdue_copies{ namespace="default" }) > 0`,
			"QuaderStorageConnectionDetached": `max by (connection) (quader_storage_connection_detached{ namespace="default" }) == 1`,
			"QuaderBlobCorrupt":               `sum(increase(quader_storage_blobs_damaged_total{ namespace="default",status="corrupt" }[1h])) > 0`,
			"QuaderBackupFailed":              `max(quader_backup_last_failure_timestamp_seconds{ namespace="default" }) > (max(quader_backup_last_success_timestamp_seconds{ namespace="default" }) or vector(0))`,
		} {
			if exprs[name] != want {
				t.Errorf("%s: expr = %q, want %q", name, exprs[name], want)
			}
		}
	})

	t.Run("every metric an expression reads is a metric of the catalogue the binary exports or of kube-state-metrics", func(t *testing.T) {
		// The families the binary registers, found by name in the Go sources: a rule that reads a name no code registers fails here.
		var src strings.Builder
		for _, dir := range []string{"internal"} {
			_ = filepath.WalkDir(filepath.Join(repoRoot, dir), func(path string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
					if b, rerr := os.ReadFile(path); rerr == nil {
						src.Write(b)
					}
				}
				return nil
			})
		}
		for _, r := range renderedRules(t) {
			for _, m := range metricNames(str(r, "expr")) {
				if strings.HasPrefix(m, "kube_") || metricsOfLaterPackages[m] != "" {
					continue
				}
				// A histogram's series carry the suffixes _bucket, _count and _sum of the family the code registers.
				base := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(m, "_bucket"), "_count"), "_sum")
				if !strings.Contains(src.String(), `"`+base+`"`) {
					t.Errorf("%s reads %s, which no code of internal/ registers", str(r, "alert"), base)
				}
			}
		}
	})

	t.Run("disabledAlerts accepts the names of the table", func(t *testing.T) {
		got := renderedRules(t, "metrics.prometheusRule.disabledAlerts={QuaderJobsStuck,QuaderGCStale,QuaderReplicasNotReady}")
		for _, r := range got {
			switch str(r, "alert") {
			case "QuaderJobsStuck", "QuaderGCStale", "QuaderReplicasNotReady":
				t.Errorf("%s is rendered although disabled", str(r, "alert"))
			}
		}
		if len(got) != len(alertSeverities())-3 {
			t.Errorf("%d alerts, want %d", len(got), len(alertSeverities())-3)
		}
	})

	t.Run("a name that is no alert is refused", func(t *testing.T) {
		renderFails(t, []string{monitoringSet()}, []string{"metrics.prometheusRule.disabledAlerts={QuaderBackupCheckFaild}"}, "disabledAlerts", "QuaderBackupCheckFaild", "QuaderBackupCheckFailed")
	})

	t.Run("the backup alerts can be disabled one by one", func(t *testing.T) {
		got := renderedRules(t, "metrics.prometheusRule.disabledAlerts={QuaderBackupStale,QuaderBackupGlobalHoldLong,QuaderRevocationJournalBacklog}")
		for _, r := range got {
			switch str(r, "alert") {
			case "QuaderBackupStale", "QuaderBackupGlobalHoldLong", "QuaderRevocationJournalBacklog":
				t.Errorf("%s is rendered although disabled", str(r, "alert"))
			}
		}
		if len(got) != len(alertSeverities())-3 {
			t.Errorf("%d alerts, want %d", len(got), len(alertSeverities())-3)
		}
	})
}

// metricsOfLaterPackages are the metrics of the object-store alerts (R-HA-045, storage.md R-STOR-060) that a later package of the
// several connections registers (docs/developer/blob-store.md, "Several connections: the build plan"), by the package. The chart
// renders their alerts already; an alert on a series that does not exist stays silent. The package that registers a metric removes
// its line here.
var metricsOfLaterPackages = map[string]string{
	"quader_storage_reconcile_queue_oldest_seconds": "C, the reconciliation",
	"quader_storage_connection_filling":             "C, the reconciliation (Q-HA-393)",
	"quader_storage_scrub_overdue_copies":           "D, the scrub",
	"quader_storage_copies":                         "F, the storage summary per connection",
	"quader_storage_connection_detached":            "the detached state of R-STOR-051, which package A does not export",
}

// metricNames returns the identifiers of an expression that look like a metric: they start with quader_ or kube_.
func metricNames(expr string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(expr, func(r rune) bool {
		return r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
	}) {
		if strings.HasPrefix(f, "quader_") || strings.HasPrefix(f, "kube_") {
			out = append(out, f)
		}
	}
	return out
}
