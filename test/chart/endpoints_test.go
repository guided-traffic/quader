//go:build chart

package chart_test

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func endpointsSet() []string { return []string{filepath.Join(chartDir, "ci", "endpoints.yaml")} }
func poolSet() []string      { return []string{filepath.Join(chartDir, "ci", "endpoints-pool.yaml")} }

// containerPorts returns the containerPort numbers of the pod's container.
func containerPorts(pod any) []int {
	var out []int
	for _, p := range list(pod, "containers", "0", "ports") {
		out = append(out, at(p, "containerPort").(int))
	}
	return out
}

func hasInt(l []int, n int) bool {
	for _, v := range l {
		if v == n {
			return true
		}
	}
	return false
}

func ingressHosts(ing any) []string {
	var out []string
	for _, r := range list(ing, "spec", "rules") {
		out = append(out, str(r, "host"))
	}
	return out
}

func TestEndpoints(t *testing.T) {
	t.Run("R-EP-008 two endpoints render two Services, two Ingresses, two NetworkPolicies and the configuration block", func(t *testing.T) {
		_, docs := render(t, endpointsSet())

		// Service per endpoint with its own port, selecting the main packages pods (Q-EP-70).
		for name, port := range map[string]int{"customers": 8443, "partners": 8444} {
			svc := find(docs, "Service", "quader-ep-"+name)
			if svc == nil {
				t.Fatalf("Service quader-ep-%s not rendered", name)
			}
			if at(svc, "spec", "ports", "0", "port") != port || at(svc, "spec", "ports", "0", "targetPort") != port ||
				str(svc, "spec", "ports", "0", "name") != "ep-"+name || str(svc, "spec", "ports", "0", "appProtocol") != "https" {
				t.Errorf("Service %s ports = %v", name, at(svc, "spec", "ports"))
			}
			if str(svc, "spec", "sessionAffinity") != "None" {
				t.Errorf("Service %s sessionAffinity = %q", name, str(svc, "spec", "sessionAffinity"))
			}
			sel, _ := at(svc, "spec", "selector").(map[string]any)
			if sel["app.kubernetes.io/component"] != "packages" || sel["quader/pool"] != "" {
				t.Errorf("Service %s selector = %v, want the main packages pods", name, sel)
			}
		}
		pkgs := at(find(docs, "Deployment", "quader-packages"), "spec", "template", "spec")
		if got := containerPorts(pkgs); !hasInt(got, 8080) || !hasInt(got, 8443) || !hasInt(got, 8444) || !hasInt(got, 8081) {
			t.Errorf("packages container ports = %v, want 8080, 8081 and both endpoint ports", got)
		}
		// both endpoints' surfaces lie within packages, so no api pod opens their ports (R-EP-002)
		if got := containerPorts(at(find(docs, "Deployment", "quader-api"), "spec", "template", "spec")); hasInt(got, 8443) || hasInt(got, 8444) {
			t.Errorf("api container ports = %v, want no endpoint port", got)
		}
		if got := containerPorts(at(find(docs, "Deployment", "quader-worker"), "spec", "template", "spec")); len(got) != 1 || got[0] != 8081 {
			t.Errorf("worker container ports = %v, want 8081 only", got)
		}

		// Ingress per endpoint: class, hosts incl. the wildcard, TLS entry, backend.
		cust := find(docs, "Ingress", "quader-ep-customers")
		if cust == nil {
			t.Fatal("Ingress quader-ep-customers not rendered")
		}
		if str(cust, "spec", "ingressClassName") != "traefik-external" {
			t.Errorf("customers class = %q", str(cust, "spec", "ingressClassName"))
		}
		if got := strings.Join(ingressHosts(cust), ","); got != "packages.example.com,pkgs.example.org,*.registry.example.com" {
			t.Errorf("customers hosts = %s", got)
		}
		if str(cust, "spec", "tls", "0", "secretName") != "packages-tls" || len(list(cust, "spec", "tls", "0", "hosts")) != 2 {
			t.Errorf("customers tls = %v", at(cust, "spec", "tls"))
		}
		for _, r := range list(cust, "spec", "rules") {
			if str(r, "http", "paths", "0", "path") != "/" || str(r, "http", "paths", "0", "pathType") != "Prefix" ||
				str(r, "http", "paths", "0", "backend", "service", "name") != "quader-ep-customers" ||
				at(r, "http", "paths", "0", "backend", "service", "port", "number") != 8443 {
				t.Errorf("customers rule = %v", r)
			}
		}
		if str(cust, "metadata", "annotations", "cert-manager.io/cluster-issuer") != "letsencrypt" {
			t.Errorf("customers annotations = %v", at(cust, "metadata", "annotations"))
		}
		part := find(docs, "Ingress", "quader-ep-partners")
		if part == nil || str(part, "spec", "ingressClassName") != "" || str(part, "spec", "tls", "0", "secretName") != "partners-tls" {
			t.Errorf("partners Ingress = %v", part)
		}
		// the default Ingress is untouched
		def := find(docs, "Ingress", "quader")
		if str(def, "spec", "ingressClassName") != "haproxy" || strings.Join(ingressHosts(def), ",") != "quader.example.com" {
			t.Errorf("default Ingress = %v", def)
		}

		// The rule of an endpoint port is part of the policy of the Deployment that opens it (Q-HA-76):
		// from networkPolicy.from only, and no policy of its own.
		if find(docs, "NetworkPolicy", "quader-ep-customers") != nil {
			t.Error("a NetworkPolicy of its own is rendered for an endpoint")
		}
		byPort := map[int][]any{}
		var order []int
		for _, r := range list(find(docs, "NetworkPolicy", "quader-packages"), "spec", "ingress") {
			p, from, has := portsOf(r)
			if len(p) != 1 {
				t.Fatalf("a packages rule names %d ports", len(p))
			}
			order = append(order, p[0])
			if has {
				byPort[p[0]] = from
			}
		}
		// The main packages pods also open the scan listener's 8082, admitted from the scanner's pods only (R-HA-066).
		if len(order) != 5 || order[0] != 8080 || order[1] != 8082 || !hasInt(order, 8443) || !hasInt(order, 8444) || order[4] != 8081 {
			t.Errorf("packages policy ports = %v, want 8080, the scan listener's 8082, both endpoint ports and 8081", order)
		}
		if from := byPort[8443]; len(from) != 1 || str(from[0], "namespaceSelector", "matchLabels", "kubernetes.io/metadata.name") != "ingress-external" {
			t.Errorf("customers rule from %v, want the ingress-external namespace", from)
		}
		if _, has := byPort[8444]; has {
			t.Error("partners rule has a from, want 8444 from anywhere")
		}
		for _, r := range list(find(docs, "NetworkPolicy", "quader-api"), "spec", "ingress") {
			if p, _, _ := portsOf(r); hasInt(p, 8443) || hasInt(p, 8444) {
				t.Errorf("the api policy admits an endpoint port %v", p)
			}
		}

		// quader.yaml: the endpoints block and the default endpoint's hosts.
		cfg := configOf(t, docs)
		eps, _ := at(cfg, "server", "endpoints").(map[string]any)
		if len(eps) != 3 {
			t.Fatalf("server.endpoints = %v", eps)
		}
		c := eps["customers"]
		if str(c, "listen") != ":8443" || str(c, "base_url") != "https://packages.example.com" || str(c, "wildcard_host") != "*.registry.example.com" ||
			str(c, "exposure") != "rules" || str(c, "replica_header") != "never" ||
			at(c, "max_in_flight") != 1024 || str(c, "rate_limit_class") != "external" || str(c, "host_roots", "pkgs.example.org") != "web/docker-hub" {
			t.Errorf("customers config = %v", c)
		}
		if at(c, "auth", "bound_tokens_only") != true || at(c, "auth", "anonymous") != false || at(c, "cache_fill") != false {
			t.Errorf("customers defaults = %v", c)
		}
		if got := list(c, "surface"); len(got) != 2 || got[0] != "packages" || got[1] != "files" {
			t.Errorf("customers surface = %v", got)
		}
		if at(eps["partners"], "exposure") != "all" {
			t.Errorf("partners exposure = %v", at(eps["partners"], "exposure"))
		}
		hosts := list(eps["default"], "hosts")
		if len(hosts) != 2 || hosts[0] != "quader.example.com" || hosts[1] != "quader.quader.svc" {
			t.Errorf("default hosts = %v, want the explicit endpoints.default.hosts", hosts)
		}
	})

	t.Run("R-EP-008 the default endpoint's hosts come from the base URLs once a further endpoint exists", func(t *testing.T) {
		_, docs := render(t, poolSet(), "config.server.additional_base_urls={https://quader.internal.example.com:8443}")
		hosts := list(at(configOf(t, docs), "server", "endpoints", "default"), "hosts")
		if len(hosts) != 2 || hosts[0] != "quader.example.com" || hosts[1] != "quader.internal.example.com" {
			t.Errorf("default hosts = %v", hosts)
		}
		_, docs = render(t, required())
		if at(configOf(t, docs), "server", "endpoints") != nil {
			t.Error("a chart without endpoints renders server.endpoints")
		}
		_, docs = render(t, required(), "endpoints.default.surface={packages,api}", "endpoints.default.replicaHeader=always")
		d := at(configOf(t, docs), "server", "endpoints", "default")
		if len(list(d, "surface")) != 2 || str(d, "replica_header") != "always" || at(d, "hosts") != nil {
			t.Errorf("a tuned default endpoint alone = %v", d)
		}
	})

	t.Run("R-EP-008 the endpoint's pairs are mounted as directories and rendered as tls.certificates", func(t *testing.T) {
		_, docs := render(t, endpointsSet())
		web := at(find(docs, "Deployment", "quader-packages"), "spec", "template", "spec")
		for secret, path := range map[string]string{
			"quader-packages-tls":          "/var/run/secrets/quader/endpoints/customers/0",
			"quader-packages-wildcard-tls": "/var/run/secrets/quader/endpoints/customers/1",
			"quader-customer-ca":           "/var/run/secrets/quader/endpoints/customers/client-ca",
		} {
			v := secretVolume(web, secret)
			if v == nil {
				t.Errorf("packages does not mount Secret %s", secret)
				continue
			}
			m := mountOf(web, v)
			if str(m, "mountPath") != path || at(m, "subPath") != nil || at(m, "readOnly") != true {
				t.Errorf("mount of %s = %v, want %s read-only without subPath", secret, m, path)
			}
			if at(v, "secret", "defaultMode") != 288 || len(list(v, "secret", "items")) == 0 {
				t.Errorf("volume of %s = %v", secret, v)
			}
		}
		if secretVolume(at(find(docs, "Deployment", "quader-worker"), "spec", "template", "spec"), "quader-packages-tls") != nil {
			t.Error("a worker mounts an endpoint's certificate")
		}
		if secretVolume(at(find(docs, "Job", "quader-migrate"), "spec", "template", "spec"), "quader-packages-tls") != nil {
			t.Error("the migration Job mounts an endpoint's certificate")
		}
		c := at(configOf(t, docs), "server", "endpoints", "customers", "tls")
		certs := list(c, "certificates")
		if len(certs) != 2 || str(certs[0], "cert_file") != "/var/run/secrets/quader/endpoints/customers/0/tls.crt" ||
			str(certs[1], "key_file") != "/var/run/secrets/quader/endpoints/customers/1/tls.key" ||
			str(c, "client_ca_file") != "/var/run/secrets/quader/endpoints/customers/client-ca/ca.crt" {
			t.Errorf("customers tls = %v", c)
		}
		// partners names no Secret: it serves the pair of its kind (server.tls) and renders none.
		if at(configOf(t, docs), "server", "endpoints", "partners", "tls") != nil {
			t.Error("an endpoint without its own Secret renders a tls block")
		}
		// extraSecrets alone: position 0 is the pair of the kind of the pods that serve it.
		_, docs = render(t, required(), "endpoints.c.port=8443", "endpoints.c.baseUrl=https://p.example.com", "endpoints.c.tls.extraSecrets={wild}")
		certs = list(at(configOf(t, docs), "server", "endpoints", "c", "tls"), "certificates")
		if len(certs) != 2 || str(certs[0], "cert_file") != tlsMountPath+"/tls.crt" || str(certs[1], "cert_file") != "/var/run/secrets/quader/endpoints/c/1/tls.crt" {
			t.Errorf("certificates with extraSecrets only = %v", certs)
		}
	})

	t.Run("R-EP-008 Q-HA-348 backend TLS annotations follow the endpoint's controller: the kind's name, or the Service's own with a pair of its own", func(t *testing.T) {
		_, docs := render(t, endpointsSet())
		part := find(docs, "Ingress", "quader-ep-partners")
		if str(part, "metadata", "annotations", "haproxy-ingress.github.io/secure-backends") != "true" ||
			at(part, "metadata", "annotations", "haproxy-ingress.github.io/secure-sni") != nil {
			t.Errorf("partners Ingress annotations = %v", at(part, "metadata", "annotations"))
		}
		// partners serves its kind's pair, so HAProxy Ingress verifies the kind's name against the kind's Secret, read from the Service.
		if sa := at(find(docs, "Service", "quader-ep-partners"), "metadata", "annotations"); str(sa, "haproxy-ingress.github.io/secure-sni") != "quader-packages.default.svc" ||
			str(sa, "haproxy-ingress.github.io/secure-verify-ca-secret") != "quader-tls-packages" {
			t.Errorf("partners Service annotations = %v", sa)
		}
		// customers uses traefik with its own backendTLS: the scheme goes on its Service, no HAProxy keys on its Ingress.
		svc := find(docs, "Service", "quader-ep-customers")
		if str(svc, "metadata", "annotations", "traefik.ingress.kubernetes.io/service.serversscheme") != "https" {
			t.Errorf("customers Service annotations = %v", at(svc, "metadata", "annotations"))
		}
		for k := range at(find(docs, "Ingress", "quader-ep-customers"), "metadata", "annotations").(map[string]any) {
			if strings.HasPrefix(k, "haproxy-ingress") {
				t.Errorf("customers Ingress carries %s although its controller is traefik", k)
			}
		}
		if strings.Contains(toStr(at(find(docs, "Service", "quader-ep-partners"), "metadata", "annotations")), "traefik") {
			t.Error("partners Service carries a Traefik setting although its controller is haproxy")
		}
		// An endpoint with a pair of its own behind HAProxy Ingress: each Service verifies its own name against that pair's Secret.
		_, docs = render(t, required(), "endpoints.c.port=8443", "endpoints.c.baseUrl=https://p.example.com", "endpoints.c.surface={packages,files,api}",
			"endpoints.c.tls.existingSecret=c-tls", "endpoints.c.ingress.enabled=true")
		for _, svc := range []string{"quader-ep-c", "quader-api-ep-c"} {
			sa := at(find(docs, "Service", svc), "metadata", "annotations")
			if str(sa, "haproxy-ingress.github.io/secure-sni") != svc+".default.svc" || str(sa, "haproxy-ingress.github.io/secure-verify-ca-secret") != "c-tls" {
				t.Errorf("Service %s annotations = %v", svc, sa)
			}
		}
	})

	t.Run("R-EP-008 Q-EP-96 R-CLAMAV-010 the endpoint of the client certificates passes TLS through per controller and is refused where that cannot work", func(t *testing.T) {
		clamav := []string{
			"endpoints.clamav.port=8444", "endpoints.clamav.baseUrl=https://clamav.quader.example.com", "endpoints.clamav.exposure=all",
			"endpoints.clamav.auth.clientCertificates=true", "endpoints.clamav.tls.existingSecret=quader-clamav-tls",
			"endpoints.clamav.tls.clientCaSecret=company-device-ca", "endpoints.clamav.ingress.enabled=true",
			"endpoints.clamav.ingress.className=haproxy", "endpoints.clamav.ingress.tlsPassthrough=true",
		}
		with := func(extra ...string) []string { return append(append([]string{}, clamav...), extra...) }
		_, docs := render(t, required(), clamav...)
		ing := find(docs, "Ingress", "quader-ep-clamav")
		ann, _ := at(ing, "metadata", "annotations").(map[string]any)
		if ann["haproxy-ingress.github.io/ssl-passthrough"] != "true" || at(ing, "spec", "tls") != nil ||
			!slices.Equal(ingressHosts(ing), []string{"clamav.quader.example.com"}) || str(ing, "spec", "rules", "0", "http", "paths", "0", "path") != "/" ||
			str(ing, "spec", "rules", "0", "http", "paths", "0", "backend", "service", "name") != "quader-ep-clamav" ||
			at(ing, "spec", "rules", "0", "http", "paths", "0", "backend", "service", "port", "number") != 8444 {
			t.Errorf("Ingress = %v", ing)
		}
		for _, a := range []any{ann, at(find(docs, "Service", "quader-ep-clamav"), "metadata", "annotations")} {
			if strings.Contains(toStr(a), "secure-") {
				t.Errorf("a backend-TLS key beside the passthrough: %v", a)
			}
		}
		c := at(configOf(t, docs), "server", "endpoints", "clamav")
		if at(c, "auth", "client_certificates") != true || str(c, "tls", "client_ca_file") != "/var/run/secrets/quader/endpoints/clamav/client-ca/ca.crt" {
			t.Errorf("quader.yaml clamav = %v", c)
		}
		_, docs = render(t, required(), with("endpoints.clamav.ingress.backendTLS.controller=ingress-nginx")...)
		if a := at(find(docs, "Ingress", "quader-ep-clamav"), "metadata", "annotations"); str(a, "nginx.ingress.kubernetes.io/ssl-passthrough") != "true" ||
			strings.Contains(toStr(a), "backend-protocol") {
			t.Errorf("ingress-nginx annotations = %v", a)
		}
		for _, tc := range []struct {
			name string
			sets []string
			want []string
		}{
			{"traefik", with("endpoints.clamav.ingress.backendTLS.controller=traefik"), []string{"endpoints.clamav.ingress.tlsPassthrough", "traefik"}},
			{"no pair of its own", with("endpoints.clamav.tls.existingSecret="), []string{"endpoints.clamav.tls.existingSecret"}},
			{"tls entries", with("endpoints.clamav.ingress.tls[0].secretName=x"), []string{"endpoints.clamav.ingress.tls"}},
			{"a surface of both roles", with("endpoints.clamav.surface={packages,api}"), []string{"endpoints.clamav.surface"}},
			{"client certificates without a client CA", with("endpoints.clamav.tls.clientCaSecret="), []string{"endpoints.clamav.tls.clientCaSecret"}},
			{"client certificates on the default endpoint", []string{"endpoints.default.auth.clientCertificates=true"}, []string{"endpoints.default.auth.clientCertificates"}},
		} {
			t.Run(tc.name, func(t *testing.T) { renderFails(t, required(), tc.sets, tc.want...) })
		}
	})

	t.Run("R-EP-008 an endpoint pool renders its Deployment, disruption budget and a Service that selects only its pods", func(t *testing.T) {
		_, docs := render(t, poolSet())
		d := find(docs, "Deployment", "quader-packages-ep-customers")
		if d == nil {
			t.Fatal("Deployment quader-packages-ep-customers not rendered")
		}
		if find(docs, "Deployment", "quader-api-ep-customers") != nil {
			t.Error("an api pool is rendered for a surface without api families")
		}
		pod := at(d, "spec", "template", "spec")
		if got := strings.Join(containerArgs(pod), " "); got != "serve --role packages" {
			t.Errorf("pool args = %q", got)
		}
		if str(envByName(pod)["QUADER_POOL_NAME"], "value") != "ep-customers" {
			t.Errorf("QUADER_POOL_NAME = %v", envByName(pod)["QUADER_POOL_NAME"])
		}
		if at(d, "spec", "replicas") != 3 {
			t.Errorf("pool replicas = %v, want the pool's replicaCount", at(d, "spec", "replicas"))
		}
		if str(at(pod, "containers", "0", "resources", "limits"), "memory") != "1Gi" {
			t.Errorf("pool limits = %v, want the override", at(pod, "containers", "0", "resources", "limits"))
		}
		if got := str(at(pod, "containers", "0", "resources", "requests"), "cpu"); got != "1" {
			t.Errorf("pool requests.cpu = %q, want the packages value", got)
		}
		if str(d, "spec", "selector", "matchLabels", "quader/pool") != "ep-customers" || str(d, "spec", "template", "metadata", "labels", "quader/pool") != "ep-customers" {
			t.Errorf("pool labels: selector %v", at(d, "spec", "selector"))
		}
		if got := containerPorts(pod); len(got) != 2 || !hasInt(got, 8443) || !hasInt(got, 8081) || hasInt(got, 8080) {
			t.Errorf("pool container ports = %v, want 8443 and 8081 and no 8080", got)
		}
		// the spread constraints select the pool's own pods
		for _, c := range list(pod, "topologySpreadConstraints") {
			if str(c, "labelSelector", "matchLabels", "quader/pool") != "ep-customers" {
				t.Errorf("pool spread selector = %v", at(c, "labelSelector"))
			}
		}
		if pdb := find(docs, "PodDisruptionBudget", "quader-packages-ep-customers"); pdb == nil || str(pdb, "spec", "selector", "matchLabels", "quader/pool") != "ep-customers" {
			t.Errorf("pool PDB = %v", pdb)
		}
		svc := find(docs, "Service", "quader-ep-customers")
		if str(svc, "spec", "selector", "quader/pool") != "ep-customers" || str(svc, "spec", "selector", "app.kubernetes.io/component") != "packages" {
			t.Errorf("Service selector = %v", at(svc, "spec", "selector"))
		}
		// the main packages pods carry no container port 8443 and no QUADER_POOL_NAME, and mount no endpoint Secret
		web := at(find(docs, "Deployment", "quader-packages"), "spec", "template", "spec")
		if hasInt(containerPorts(web), 8443) || envByName(web)["QUADER_POOL_NAME"] != nil || secretVolume(web, "quader-packages-tls") != nil {
			t.Errorf("main packages pods serve the pooled endpoint: ports %v", containerPorts(web))
		}
		if secretVolume(pod, "quader-packages-tls") == nil || secretVolume(pod, "quader-tls-packages") == nil {
			t.Error("the pool does not mount its endpoint's certificate and its kind's pair")
		}
		// the pool's pods are selected by no other policy, so its policy admits the operations port too
		np := find(docs, "NetworkPolicy", "quader-packages-ep-customers")
		var all []int
		for _, r := range list(np, "spec", "ingress") {
			p, _, _ := portsOf(r)
			all = append(all, p...)
		}
		if len(all) != 2 || !hasInt(all, 8443) || !hasInt(all, 8081) {
			t.Errorf("pool policy ports = %v, want 8443 and 8081", all)
		}
		if str(np, "spec", "podSelector", "matchLabels", "quader/pool") != "ep-customers" {
			t.Errorf("pool policy selector = %v", at(np, "spec", "podSelector"))
		}
	})

	t.Run("R-EP-008 an endpoint pool can switch off what packages has on", func(t *testing.T) {
		_, docs := render(t, poolSet(), "highAvailability.enforce=false", "endpoints.customers.pool.podDisruptionBudget.enabled=false", "endpoints.customers.pool.replicaCount=1")
		if find(docs, "PodDisruptionBudget", "quader-packages-ep-customers") != nil {
			t.Error("the pool's disruption budget is rendered although the pool turned it off")
		}
		if find(docs, "PodDisruptionBudget", "quader-packages") == nil {
			t.Error("the packages disruption budget disappeared")
		}
	})

	t.Run("R-EP-008 the notes list each endpoint with its Service and hosts and warn about shared pods and open policies", func(t *testing.T) {
		// helm template does not print NOTES.txt; the text is the template quader.notes, rendered here through a ConfigMap in a copy of the chart.
		dir, err := os.MkdirTemp("", "notes-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		copyDir(t, chartDir, filepath.Join(dir, "quader"))
		probe := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: notes\ndata:\n  notes: |\n{{ include \"quader.notes\" . | indent 4 }}\n"
		if err := os.WriteFile(filepath.Join(dir, "quader", "templates", "notes-probe.yaml"), []byte(probe), 0o600); err != nil {
			t.Fatal(err)
		}
		notes := func(set []string) string {
			args := []string{"template", "quader", filepath.Join(dir, "quader"), "--show-only", "templates/notes-probe.yaml"}
			for _, f := range set {
				args = append(args, "-f", f)
			}
			out, stderr, err := helm(t, args...)
			if err != nil {
				t.Fatalf("%v\n%s", err, stderr)
			}
			return out
		}
		got := notes(endpointsSet())
		for _, want := range []string{
			"customers   Service quader-ep-customers:8443", "packages.example.com, pkgs.example.org, *.registry.example.com",
			"partners   Service quader-ep-partners:8444", "endpoint customers has exposure: rules and shares the pods of the default endpoint", "the main packages Deployment",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("notes lack %q:\n%s", want, got)
			}
		}
		if strings.Contains(got, "empty networkPolicy.from") && strings.Contains(got, "endpoint customers has exposure: rules and an empty") {
			t.Error("customers lists networkPolicy.from but is warned about an empty one")
		}
		got = notes(poolSet())
		if !strings.Contains(got, "pool quader-packages-ep-customers") || !strings.Contains(got, "endpoint customers has exposure: rules and an empty networkPolicy.from") {
			t.Errorf("pool notes:\n%s", got)
		}
	})

	t.Run("R-EP-008 rendering fails and names the value", func(t *testing.T) {
		base := func(extra ...string) []string {
			return append([]string{"endpoints.c.port=8443", "endpoints.c.baseUrl=https://p.example.com"}, extra...)
		}
		for _, tc := range []struct {
			name string
			sets []string
			want []string
		}{
			{"port 8080", []string{"endpoints.c.port=8080", "endpoints.c.baseUrl=https://p.example.com"}, []string{"endpoints.c.port", "8080"}},
			{"port 8081", []string{"endpoints.c.port=8081", "endpoints.c.baseUrl=https://p.example.com"}, []string{"endpoints.c.port", "8081"}},
			{"a port shared with another endpoint", base("endpoints.d.port=8443", "endpoints.d.baseUrl=https://q.example.com"), []string{"endpoints.d.port", "8443", "endpoint c"}},
			{"no port", []string{"endpoints.c.baseUrl=https://p.example.com"}, []string{"endpoints.c.port"}},
			{"no base URL", []string{"endpoints.c.port=8443"}, []string{"endpoints.c.baseUrl"}},
			{"a base URL with a path", []string{"endpoints.c.port=8443", "endpoints.c.baseUrl=https://p.example.com/x"}, []string{"endpoints.c.baseUrl"}},
			{"an invalid name", []string{"endpoints.Cust.port=8443", "endpoints.Cust.baseUrl=https://p.example.com"}, []string{"endpoints"}},
			{"a name that is too long", []string{"endpoints.abcdefghijklmnopqrstu.port=8443", "endpoints.abcdefghijklmnopqrstu.baseUrl=https://p.example.com"}, []string{"endpoints"}},
			{"a pool Deployment name over 63 characters", []string{"fullnameOverride=" + strings.Repeat("a", 37), "endpoints.abcdefghijklmnopqrst.port=8443", "endpoints.abcdefghijklmnopqrst.baseUrl=https://p.example.com", "endpoints.abcdefghijklmnopqrst.pool.enabled=true"}, []string{"endpoints.abcdefghijklmnopqrst.pool", "63"}},
			{"a host in two endpoints", base("endpoints.d.port=8444", "endpoints.d.baseUrl=https://q.example.com", "endpoints.d.hosts={p.example.com}"), []string{"endpoints.d", "p.example.com", "endpoints.c"}},
			{"a host of the default endpoint", base("endpoints.c.hosts={quader.example.com}"), []string{"endpoints.c", "quader.example.com"}},
			{"a wildcard host that covers another endpoint's host", base("endpoints.d.port=8444", "endpoints.d.baseUrl=https://q.example.com", "endpoints.d.wildcardHost=*.example.com"), []string{"endpoints.d.wildcardHost", "p.example.com"}},
			{"an invalid wildcard host", base("endpoints.c.wildcardHost=*.com"), []string{"endpoints.c.wildcardHost"}},
			{"ui on a further endpoint", base("endpoints.c.surface={ui,packages}"), []string{"endpoints.c.surface", "ui"}},
			{"endpoints set inside config", []string{"config.server.endpoints.x.listen=:9000"}, []string{"config.server.endpoints"}},
			{"a pool below two replicas", base("endpoints.c.pool.enabled=true", "endpoints.c.pool.replicaCount=1"), []string{"endpoints.c.pool.replicaCount"}},
			{"extra pairs without TLS", base("tls.enabled=false", "tls.certManager.issuerRef.name=", "endpoints.c.tls.extraSecrets={wild}"), []string{"endpoints.c.tls", "tls.enabled"}},
			{"a client CA without TLS", base("tls.enabled=false", "tls.certManager.issuerRef.name=", "endpoints.c.tls.clientCaSecret=ca"), []string{"endpoints.c.tls", "tls.enabled"}},
			{"an endpoint Ingress that verifies with traefik and names no ServersTransport", base("endpoints.c.ingress.enabled=true", "endpoints.c.ingress.backendTLS.controller=traefik"), []string{"endpoints.c.ingress.backendTLS.serversTransports.packages", "traefik"}},
			{"an endpoint of both roles behind traefik without a transport for api", base("endpoints.c.surface={packages,api}", "endpoints.c.ingress.enabled=true", "endpoints.c.ingress.backendTLS.controller=traefik", "endpoints.c.ingress.backendTLS.serversTransports.packages=p"), []string{"endpoints.c.ingress.backendTLS.serversTransports.api", "traefik"}},
			{"an endpoint with a client CA behind haproxy and no client certificate Secret", base("endpoints.c.ingress.enabled=true", "endpoints.c.ingress.backendTLS.caSecret=ca", "endpoints.c.tls.existingSecret=c-tls", "endpoints.c.tls.clientCaSecret=c-ca"), []string{"endpoints.c.ingress.backendTLS.clientCertSecret", "haproxy"}},
			{"a global CA does not help an endpoint that sets verify itself under another controller", base("ingress.backendTLS.caSecret=ca", "endpoints.c.ingress.enabled=true", "endpoints.c.ingress.backendTLS.controller=traefik"), []string{"endpoints.c.ingress.backendTLS.serversTransports"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				renderFails(t, required(), tc.sets, tc.want...)
			})
		}
	})

	t.Run("R-EP-008 the schema refuses keys the default endpoint may not set and unknown keys", func(t *testing.T) {
		renderFails(t, required(), []string{"endpoints.default.port=9000"}, "endpoints")
		renderFails(t, required(), []string{"endpoints.default.baseUrl=https://x.example.com"}, "endpoints")
		renderFails(t, required(), []string{"endpoints.c.port=8443", "endpoints.c.baseUrl=https://p.example.com", "endpoints.c.surfaces={packages}"}, "endpoints")
		renderFails(t, required(), []string{"endpoints.c.port=8443", "endpoints.c.baseUrl=https://p.example.com", "endpoints.c.exposure=some"}, "exposure")
	})

	t.Run("R-EP-008 a sixteen-endpoint set renders and an endpoint can switch the Service type", func(t *testing.T) {
		_, docs := render(t, required(), "endpoints.c.port=8443", "endpoints.c.baseUrl=https://p.example.com",
			"endpoints.c.service.type=LoadBalancer", "endpoints.c.service.loadBalancerSourceRanges={10.0.0.0/8}")
		svc := find(docs, "Service", "quader-ep-c")
		if str(svc, "spec", "type") != "LoadBalancer" || str(svc, "spec", "loadBalancerSourceRanges", "0") != "10.0.0.0/8" {
			t.Errorf("Service = %v", svc)
		}
		var sets []string
		for i := 0; i < 16; i++ {
			n := "e" + strconv.Itoa(i)
			sets = append(sets, "endpoints."+n+".port="+strconv.Itoa(9000+i), "endpoints."+n+".baseUrl=https://"+n+".example.com")
		}
		_, docs = render(t, required(), sets...)
		if n := len(ofKind(docs, "Ingress")); n != 0 {
			t.Errorf("%d Ingresses rendered without ingress.enabled", n)
		}
		// The three Services of every render, the two of the image scanning's links (R-HA-066), the malware scanner's (R-HA-068), the
		// updater's (R-HA-069) and one per endpoint.
		if n := len(ofKind(docs, "Service")); n != 3+2+1+1+16 {
			t.Errorf("%d Services", n)
		}
	})
}

// copyDir copies a directory tree.
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		b, err := os.ReadFile(p) //nolint:gosec // the chart's own files
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}
