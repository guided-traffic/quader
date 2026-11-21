//go:build chart

package chart_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// The role scanner and the links of the image scanning in the chart (R-HA-065, R-HA-066, ADR-212): what only the rendered
// manifests can show - the scanner pod's volumes and environment, its Services and the ingress rules of the links.

func scannerPod(t *testing.T, docs []doc) any {
	t.Helper()
	d := find(docs, "Deployment", "quader-scanner")
	if d == nil {
		t.Fatal("no Deployment quader-scanner")
	}
	return at(d, "spec", "template", "spec")
}

func volumeNames(pod any) []string {
	var out []string
	for _, v := range list(pod, "volumes") {
		out = append(out, str(v, "name"))
	}
	slices.Sort(out)
	return out
}

func renderedConfig(t *testing.T, docs []doc) map[string]any {
	t.Helper()
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(str(find(docs, "ConfigMap", "quader-config"), "data", "quader.yaml")), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestR_HA_065_TheScannerHoldsNoSecretOfTheInstallation(t *testing.T) {
	_, docs := render(t, required(), "extraEnv[0].name=SHARED", "extraEnv[0].value=x",
		"extraEnvFrom[0].secretRef.name=quader-db-app", "extraVolumes[0].name=shared", "extraVolumes[0].secret.secretName=quader-valkey",
		"extraVolumeMounts[0].name=shared", "extraVolumeMounts[0].mountPath=/var/run/secrets/shared")
	pod := scannerPod(t, docs)
	if got := containerArgs(pod); !slices.Equal(got, []string{"serve", "--role", "scanner"}) {
		t.Fatalf("args %v", got)
	}
	if got := volumeNames(pod); !slices.Equal(got, []string{"config", "secret-tls", "spool", "tmp"}) {
		t.Fatalf("the scanner mounts %v, the four volumes of R-HA-065 only", got)
	}
	if at(pod, "automountServiceAccountToken") != false || str(pod, "serviceAccountName") != "" || at(pod, "shareProcessNamespace") != nil {
		t.Fatalf("service account %q, automount %v, shareProcessNamespace %v", str(pod, "serviceAccountName"), at(pod, "automountServiceAccountToken"), at(pod, "shareProcessNamespace"))
	}
	if len(list(pod, "initContainers")) != 0 || len(list(pod, "containers")) != 1 {
		t.Fatal("the scanner is not the first process of its one container")
	}
	secrets := secretVolumes(pod)
	if secrets["secret-tls"] != "quader-tls-scanner" || len(secrets) != 1 {
		t.Fatalf("secret volumes %v, want the scanner's own Secret only", secrets)
	}
	items := func(name string) []string {
		var out []string
		for _, v := range list(pod, "volumes") {
			if str(v, "name") == name {
				for _, it := range list(v, "secret", "items") {
					out = append(out, str(it, "path"))
				}
			}
		}
		return out
	}
	if got := items("secret-tls"); !slices.Equal(got, []string{"tls.crt", "tls.key", "ca.crt"}) {
		t.Fatalf("the files of the scanner's Secret %v, want its pair and the CA", got)
	}
	c := at(pod, "containers", "0")
	if list(c, "envFrom") != nil {
		t.Fatal("a shared envFrom reaches the scanner")
	}
	for name, e := range envByName(pod) {
		if name == "SHARED" || at(e, "valueFrom", "secretKeyRef") != nil {
			t.Errorf("the scanner's environment carries %s", name)
		}
	}
	for _, p := range mountPaths(pod) {
		if strings.HasPrefix(p, "/var/run/secrets/") && p != "/var/run/secrets/quader/tls" {
			t.Errorf("the scanner mounts %s", p)
		}
	}
	if str(c, "securityContext", "readOnlyRootFilesystem") != "" || at(c, "securityContext", "readOnlyRootFilesystem") != true ||
		at(c, "securityContext", "allowPrivilegeEscalation") != false {
		t.Fatalf("security context %v", at(c, "securityContext"))
	}
	if str(c, "resources", "limits", "memory") != "2Gi" || str(c, "resources", "requests", "cpu") != "500m" || at(c, "resources", "limits", "cpu") != nil {
		t.Fatalf("resources %v", at(c, "resources"))
	}
	if d := find(docs, "Deployment", "quader-scanner"); at(d, "spec", "replicas") != 2 || at(pod, "terminationGracePeriodSeconds") != 60 {
		t.Fatalf("replicas %v, grace %v", at(d, "spec", "replicas"), at(pod, "terminationGracePeriodSeconds"))
	}
	if find(docs, "PodDisruptionBudget", "quader-scanner") == nil {
		t.Fatal("no PodDisruptionBudget of the scanner")
	}
	// With named Secrets the scanner mounts its own and no other kind's (Q-HA-347).
	_, docs = render(t, required(), "tls.certManager.issuerRef.name=", "tls.existingSecrets.api=a", "tls.existingSecrets.packages=p",
		"tls.existingSecrets.worker=w", "tls.existingSecrets.scanner=s", "tls.existingSecrets.malware-scanner=m")
	if got := secretVolumes(scannerPod(t, docs)); got["secret-tls"] != "s" || len(got) != 1 {
		t.Fatalf("with tls.existingSecrets the scanner's secret volumes are %v", got)
	}
	// R-HA-061: the spool follows spool.type like every role's.
	_, docs = render(t, required(), "spool.type=ephemeral", "spool.ephemeral.size=20Gi")
	for _, v := range list(scannerPod(t, docs), "volumes") {
		if str(v, "name") == "spool" && at(v, "ephemeral", "volumeClaimTemplate") == nil {
			t.Fatalf("with spool.type ephemeral the scanner's spool is %v", v)
		}
	}
}

func TestR_HA_065_TheImageScanningOffRendersNoScanner(t *testing.T) {
	// Both scannings off: the malware scanning reads through the scan listener too (R-HA-068), its own off case is in malware_test.go.
	_, docs := render(t, required(), "config.vulnerabilities.images.enabled=false", "config.malware.enabled=false")
	for _, d := range docs {
		if name := str(d, "metadata", "name"); strings.Contains(name, "scanner") || strings.HasSuffix(name, "packages-scan") {
			t.Errorf("%s %s rendered", str(d, "kind"), name)
		}
	}
	for name, pod := range podSpecs(docs) {
		if envByName(pod)["QUADER_SERVER_SCAN_LISTEN"] != nil || slices.Contains(tlsItems(pod), "ca.crt") {
			t.Errorf("%s has the scan listener or the internal CA", name)
		}
	}
	if cfg := renderedConfig(t, docs); at(cfg, "vulnerabilities", "images", "scanner") != nil || at(cfg, "server", "internal_tls") != nil {
		t.Fatal("the links are configured without a scanner")
	}
}

func TestQ_HA_318_TheSpoolsAreCheckedAgainstWhatTheyHold(t *testing.T) {
	t.Run("the scanner's budget holds every slot's largest image plus 64 MiB", func(t *testing.T) {
		renderFails(t, required(), []string{"scanner.spool.sizeLimit=4Gi"}, "scanner.spool.sizeLimit", "spool.headroom", "max_image_size")
		renderFails(t, required(), []string{"config.vulnerabilities.images.scanner.slots=2"}, "scanner.spool.sizeLimit", "slots")
		render(t, required(), "config.vulnerabilities.images.scanner.slots=2", "scanner.spool.sizeLimit=9Gi")
		// The sum is exact: 3 GiB + 64 MiB + the 2 GiB of headroom is 5184 MiB; a MiB less does not hold it.
		render(t, required(), "scanner.spool.sizeLimit=5184Mi")
		renderFails(t, required(), []string{"scanner.spool.sizeLimit=5183Mi"}, "scanner.spool.sizeLimit")
		renderFails(t, required(), []string{"config.vulnerabilities.images.max_image_size=6GiB"}, "max_image_size (6GiB)")
	})
	t.Run("the worker of the queue vuln holds the database and 512 MiB, by default in 5 Gi", func(t *testing.T) {
		_, docs := render(t, required())
		for _, v := range list(podSpecs(docs)["quader-worker"], "volumes") {
			if str(v, "name") == "spool" && str(v, "emptyDir", "sizeLimit") != "5Gi" {
				t.Fatalf("the worker's spool is %v", at(v, "emptyDir", "sizeLimit"))
			}
		}
		renderFails(t, required(), []string{"worker.spool.sizeLimit=4Gi"}, "queue vuln", "2GiB")
		// The sum is exact: 2 GiB + 512 MiB + the 2 GiB of headroom is 4608 MiB; a MiB less does not hold it.
		render(t, required(), "worker.spool.sizeLimit=4608Mi")
		renderFails(t, required(), []string{"worker.spool.sizeLimit=4607Mi"}, "queue vuln")
		// The image limit is the scanner's now: a larger one raises the scanner's spool, not the worker's.
		render(t, required(), "config.vulnerabilities.images.max_image_size=6GiB", "scanner.spool.sizeLimit=9Gi")
	})
	t.Run("the pool that claims the queue vuln is the one checked", func(t *testing.T) {
		pool := []string{"worker.pools[0].name=v", "worker.pools[0].queues={vuln}", "worker.pools[0].replicaCount=2"}
		renderFails(t, required(), append(pool, "worker.pools[0].spool.sizeLimit=4Gi"), "queue vuln", "quader-worker-v")
		render(t, required(), append(pool, "worker.pools[0].spool.sizeLimit=5Gi", "worker.spool.sizeLimit=3Gi")...)
	})
	t.Run("the scanning of images off, or the scanning off, is not checked", func(t *testing.T) {
		render(t, required(), "worker.spool.sizeLimit=3Gi", "scanner.spool.sizeLimit=1Gi", "config.vulnerabilities.images.enabled=false")
		render(t, required(), "worker.spool.sizeLimit=3Gi", "scanner.spool.sizeLimit=1Gi", "config.vulnerabilities.enabled=false")
	})
	t.Run("R-HA-029 the scanner's replicas, and a Secret of its own with named Secrets", func(t *testing.T) {
		renderFails(t, required(), []string{"scanner.replicaCount=0"}, "scanner.replicaCount")
		renderFails(t, required(), []string{
			"tls.certManager.issuerRef.name=", "tls.existingSecrets.api=a", "tls.existingSecrets.packages=p",
			"tls.existingSecrets.worker=w",
		}, "tls.existingSecrets.scanner")
	})
}

func TestR_HA_066_TheLinksOfTheImageScanning(t *testing.T) {
	_, docs := render(t, required(), "service.type=LoadBalancer")
	t.Run("two ClusterIP Services of their own, whatever the client Services are", func(t *testing.T) {
		for name, want := range map[string]struct {
			port      int
			component string
		}{"quader-scanner": {8080, "scanner"}, "quader-packages-scan": {8082, "packages"}} {
			s := find(docs, "Service", name)
			if s == nil {
				t.Fatalf("no Service %s", name)
			}
			if str(s, "spec", "type") != "ClusterIP" || str(s, "spec", "sessionAffinity") != "None" || at(s, "spec", "ports", "0", "port") != want.port ||
				str(s, "spec", "ports", "0", "appProtocol") != "https" || str(s, "spec", "selector", "app.kubernetes.io/component") != want.component {
				t.Errorf("%s: %v", name, at(s, "spec"))
			}
		}
		if at(find(docs, "Service", "quader-packages-scan"), "spec", "selector", "quader/pool") != "" {
			t.Fatal("the scan Service selects a pool")
		}
	})
	t.Run("the scan listener on the main packages Deployment only, and the internal CA where a link ends", func(t *testing.T) {
		_, docs := render(t, required(), "packages.pools[0].name=npm", "packages.pools[0].paths[0]=/api/npm",
			"worker.pools[0].name=vuln", "worker.pools[0].queues[0]=vuln")
		for name, pod := range podSpecs(docs) {
			scan := envByName(pod)["QUADER_SERVER_SCAN_LISTEN"]
			if (scan != nil) != (name == "quader-packages") || (scan != nil && str(scan, "value") != ":8082") {
				t.Errorf("%s: QUADER_SERVER_SCAN_LISTEN %v", name, scan)
			}
			ca := slices.Contains(tlsItems(pod), "ca.crt")
			wantCA := name == "quader-packages" || name == "quader-scanner" || name == "quader-malware-scanner" || strings.HasPrefix(name, "quader-worker")
			if ca != wantCA {
				t.Errorf("%s: internal CA mounted %v", name, ca)
			}
			if strings.HasPrefix(name, "quader-worker") && secretVolumes(pod)["secret-tls"] != "quader-tls-worker" {
				t.Errorf("%s does not have the worker's pair, which it presents to the scanner", name)
			}
		}
	})
	t.Run("the chart-managed keys of the links", func(t *testing.T) {
		cfg := renderedConfig(t, docs)
		for path, want := range map[string]string{
			"vulnerabilities.images.scanner.url":          "https://quader-scanner.default.svc:8080",
			"vulnerabilities.images.scanner.server_name":  "quader-scanner.default.svc",
			"vulnerabilities.images.scanner.client_name":  "quader-worker.default.svc",
			"vulnerabilities.images.registry.url":         "https://quader-packages-scan.default.svc:8082",
			"vulnerabilities.images.registry.server_name": "quader-packages.default.svc",
			"server.internal_tls.ca_file":                 "/var/run/secrets/quader/tls/ca.crt",
		} {
			if got := str(cfg, strings.Split(path, ".")...); got != want {
				t.Errorf("%s = %q, want %q", path, got, want)
			}
		}
		renderFails(t, required(), []string{"config.vulnerabilities.images.scanner.url=https://evil.example.com"}, "config.vulnerabilities.images.scanner.url is managed by the chart")
		renderFails(t, required(), []string{"config.server.scan_listen=:9999"}, "config.server.scan_listen is managed by the chart")
		renderFails(t, required(), []string{"config.vulnerabilities.images.scanner.client_name=x"}, "config.vulnerabilities.images.scanner.client_name is managed by the chart")
		// The scan listener admits every kind of scanner the release renders, one entry each (quader.tlsKindTable).
		if got := toStr(at(cfg, "server", "scan_clients")); got != "- quader-scanner.default.svc\n- quader-malware-scanner.default.svc\n" {
			t.Errorf("server.scan_clients = %q, want the names of the two scanners", got)
		}
		renderFails(t, required(), []string{"config.server.scan_clients[0]=x"}, "config.server.scan_clients is managed by the chart")
	})
	t.Run("Q-HA-322 without TLS the links are plain and nothing of TLS mounts", func(t *testing.T) {
		_, docs := render(t, required(), "tls.enabled=false", "tls.certManager.issuerRef.name=")
		cfg := renderedConfig(t, docs)
		if str(cfg, "vulnerabilities", "images", "scanner", "url") != "http://quader-scanner.default.svc:8080" || at(cfg, "server", "internal_tls") != nil {
			t.Fatalf("%v", at(cfg, "vulnerabilities"))
		}
		if got := volumeNames(scannerPod(t, docs)); !slices.Equal(got, []string{"config", "spool", "tmp"}) {
			t.Fatalf("volumes %v", got)
		}
	})
}

// tlsItems lists the files the pod projects from its kind's Secret at /var/run/secrets/quader/tls.
func tlsItems(pod any) []string {
	var out []string
	for _, v := range list(pod, "volumes") {
		if str(v, "name") == "secret-tls" {
			for _, it := range list(v, "secret", "items") {
				out = append(out, str(it, "path"))
			}
		}
	}
	return out
}

// R-HA-042, ADR-154, Q-HA-320: the scanner admits 8080 from the worker Deployment that claims the queue vuln, the policy of the main
// packages pods admits 8082 from the scanner's pods (one rule more of it), and no policy of the links has an egress rule.
func TestR_HA_042_TheLinksAdmitTheirOneSender(t *testing.T) {
	from := func(t *testing.T, docs []doc, policy string, port int) map[string]any {
		t.Helper()
		p := find(docs, "NetworkPolicy", policy)
		if p == nil {
			t.Fatalf("no NetworkPolicy %s", policy)
		}
		if at(p, "spec", "egress") != nil || slices.Contains(list(p, "spec", "policyTypes"), any("Egress")) {
			t.Fatalf("%s has an egress rule", policy)
		}
		for _, rule := range list(p, "spec", "ingress") {
			ports, peers, _ := portsOf(rule)
			if slices.Equal(ports, []int{port}) && len(peers) == 1 {
				m, _ := at(peers[0], "podSelector", "matchLabels").(map[string]any)
				return m
			}
		}
		t.Fatalf("%s has no rule of one peer for %d", policy, port)
		return nil
	}
	_, docs := render(t, required(), "config.malware.enabled=false")
	if m := from(t, docs, "quader-scanner", 8080); m["app.kubernetes.io/component"] != "worker" || m["quader/pool"] != "" {
		t.Fatalf("the scanner admits %v", m)
	}
	if m := from(t, docs, "quader-packages", 8082); m["app.kubernetes.io/component"] != "scanner" {
		t.Fatalf("the scan listener admits %v", m)
	}
	_, docs = render(t, required(), "worker.pools[0].name=vuln", "worker.pools[0].queues[0]=vuln",
		"packages.pools[0].name=npm", "packages.pools[0].paths[0]=/api/npm")
	if m := from(t, docs, "quader-scanner", 8080); m["quader/pool"] != "vuln" {
		t.Fatalf("with a pool of the queue vuln the scanner admits %v", m)
	}
	// The scan listener opens on the main packages Deployment only: neither a pool's policy nor the policy of a render with both
	// scannings off admits 8082.
	_, off := render(t, required(), "config.vulnerabilities.images.enabled=false", "config.malware.enabled=false")
	for _, p := range []doc{find(docs, "NetworkPolicy", "quader-packages-npm"), find(off, "NetworkPolicy", "quader-packages")} {
		for _, rule := range list(p, "spec", "ingress") {
			if ports, _, _ := portsOf(rule); slices.Contains(ports, 8082) {
				t.Errorf("%s admits 8082", str(p, "metadata", "name"))
			}
		}
	}
}

// Q-VULN-185, Q-HA-347: the dev profile lets the chart issue its pairs, and the manual's example of named Secrets gives the worker and the
// scanner client authentication and each kind its name, which the links check.
func TestQ_VULN_185_TheDocumentedPairsAllowTheLinksOfEachKind(t *testing.T) {
	read := func(t *testing.T, path string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(repoRoot, path))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if pki := read(t, "deploy/dev/pki.yaml"); strings.Contains(pki, "name: quader-tls\n") {
		t.Fatal("the dev profile still issues a pair for all kinds")
	}
	if dev := read(t, "deploy/dev/values-dev.yaml"); !strings.Contains(dev, "    issuerRef:\n      name: quader-dev-ca\n      kind: Issuer\n") {
		t.Fatal("the dev profile does not let the chart issue its pairs from the dev CA")
	}
	manual := read(t, "docs/manual/admin/tls-and-certificates.md")
	for _, kind := range []string{"worker", "scanner"} {
		if !strings.Contains(manual, "  name: quader-tls-"+kind+"\n") || !strings.Contains(manual, "- quader-"+kind+".quader.svc\n") {
			t.Errorf("the manual's example of named Secrets lacks the pair of %s with its name", kind)
		}
	}
	if !strings.Contains(manual, "usages: [digital signature, server auth, client auth]") {
		t.Fatal("the manual's example does not give the worker and the scanner client authentication, which their links need")
	}
}
