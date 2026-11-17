//go:build e2e

package e2e

// The chart's live checks and the metric table of R-BAS-049 that need no destructive step: the render acceptance of
// R-BAS-045 and the metrics of R-BAS-040 on every replica. They run after the scenarios of bastion_scenarios_test.go and
// before the cut, the mismatch and the floods.

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestBastion_R_BAS_049_ChartRender (R-BAS-045): helm template with only the required values renders a Deployment with two
// replicas, a PodDisruptionBudget, the Ingress with the edge-limit annotations and a NetworkPolicy, and no Secret, no Role and no
// PersistentVolumeClaim; an edge limit at or below the Bastion's own fails and names both values.
func TestBastion_R_BAS_049_ChartRender(t *testing.T) {
	chart := filepath.Join(cfg.repo, "deploy/helm/quader-bastion")
	required := []string{
		"--set", "config.server.base_url=https://packages.example.com",
		"--set", "config.bastion.main.url=https://quader.example.com",
		"--set", "secrets.mainToken.existingSecret=quader-bastion-token",
		"--set", "tls.certManager.issuerRef.name=internal-ca",
		"--set", "config.server.trusted_proxies[0]=10.244.0.0/16", // required behind an Ingress (Q-BAS-130)
	}
	render := func(extra ...string) (string, error) {
		cmd := exec.Command("helm", append(append([]string{"template", "t", chart, "-n", "bastion"}, required...), extra...)...)
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		err := cmd.Run()
		return out.String() + errb.String(), err
	}
	out, err := render()
	if err != nil {
		t.Fatalf("helm template with the required values only: %v\n%s", err, out)
	}
	kinds := map[string]int{}
	var deployment, ingress map[string]any
	dec := yaml.NewDecoder(strings.NewReader(out))
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			break
		}
		k, _ := doc["kind"].(string)
		kinds[k]++
		switch k {
		case "Deployment":
			deployment = doc
		case "Ingress":
			ingress = doc
		}
	}
	for _, k := range []string{"Deployment", "PodDisruptionBudget", "Ingress", "NetworkPolicy", "Service", "ConfigMap", "ServiceAccount", "Certificate"} {
		if kinds[k] == 0 {
			t.Errorf("no %s rendered: %v", k, kinds)
		}
	}
	for _, k := range []string{"Secret", "Role", "RoleBinding", "ClusterRole", "PersistentVolumeClaim", "StatefulSet"} {
		if kinds[k] != 0 {
			t.Errorf("%d %s rendered, want none", kinds[k], k)
		}
	}
	if r, _ := at(deployment, "spec", "replicas").(int); r != 2 {
		t.Errorf("replicas = %v, want 2", at(deployment, "spec", "replicas"))
	}
	ann, _ := at(ingress, "metadata", "annotations").(map[string]any)
	for _, k := range []string{"haproxy-ingress.github.io/limit-rps", "haproxy-ingress.github.io/limit-connections"} {
		if ann[k] == nil {
			t.Errorf("the Ingress has no %s annotation: %v", k, ann)
		}
	}
	// an edge limit at or below the Bastion's own limit is refused, naming both values
	out, err = render("--set", "edgeLimit.requestsPerSecond=50")
	if err == nil {
		t.Fatalf("helm template with edgeLimit.requestsPerSecond=50 succeeded, want a failure")
	}
	for _, want := range []string{"edgeLimit.requestsPerSecond", "requests_per_second"} {
		if !strings.Contains(out, want) {
			t.Errorf("the failure does not name %q:\n%s", want, tail(out, 600))
		}
	}
}

// TestBastion_R_BAS_049_Metrics (R-BAS-040): every metric of the table is exported by both replicas of both Bastions.
func TestBastion_R_BAS_049_Metrics(t *testing.T) {
	env := basFx(t)
	names := []string{
		"quader_bastion_requests_total", "quader_bastion_bytes_served_total", "quader_bastion_main_requests_total",
		"quader_bastion_main_request_duration_seconds", "quader_bastion_main_in_flight", "quader_bastion_main_budget",
		"quader_bastion_main_budget_exhausted_total", "quader_bastion_main_reachable",
		"quader_bastion_main_unreachable_since_timestamp_seconds", "quader_bastion_base_url_mismatch", "quader_bastion_interface_mismatch",
		"quader_bastion_interface_version", "quader_bastion_grace_period_seconds", "quader_bastion_answer_period_seconds",
		"quader_bastion_answers", "quader_bastion_answers_total", "quader_bastion_feed_lag_seconds", "quader_bastion_feed_entries_applied_total",
		"quader_bastion_feed_resets_total", "quader_bastion_token_remaining_seconds", "quader_bastion_token_warn_before_seconds",
		"quader_bastion_main_token_refused", "quader_bastion_token_reloads_total", "quader_bastion_cache_bytes",
		"quader_bastion_cache_capacity_bytes", "quader_bastion_cache_evictions_total", "quader_bastion_cache_corrupt_total",
		"quader_bastion_cache_errors_total", "quader_bastion_refused_total", "quader_bastion_blocks_total", "quader_bastion_blocked_addresses",
		"quader_bastion_audit_events_total", "quader_bastion_audit_buffer_events", "quader_bastion_audit_events_dropped_total",
	}
	for _, s := range env.sites() {
		for _, p := range basReadyPods(t, s) {
			code, body := basOps(p.Name, "/metrics")
			if code != 200 {
				t.Fatalf("%s /metrics = %d", p.Name, code)
			}
			for _, n := range names {
				if !strings.Contains(body, "# TYPE "+n+" ") {
					t.Errorf("%s does not export %s", p.Name, n)
				}
			}
			m := parseMetrics(body)
			if s.variant == "bucket" && m["quader_valkey_available"] != 1 {
				t.Errorf("%s: quader_valkey_available = %v with Valkey configured", p.Name, m["quader_valkey_available"])
			}
			if m["quader_bastion_main_reachable"] != 1 {
				t.Errorf("%s: not reachable", p.Name)
			}
		}
	}
}

// at walks a decoded YAML document.
func at(v any, path ...string) any {
	for _, k := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}
