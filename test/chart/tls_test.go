//go:build chart

package chart_test

import (
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// tlsMountPath is where the chart mounts the listener certificate (R-HA-063).
const tlsMountPath = "/var/run/secrets/quader/tls"

func haIngress() []string { return []string{filepath.Join(chartDir, "ci", "ha-ingress.yaml")} }

// secretVolume returns the volume of pod that projects the Secret secretName, or nil.
func secretVolume(pod any, secretName string) any {
	for _, v := range list(pod, "volumes") {
		if str(v, "secret", "secretName") == secretName {
			return v
		}
	}
	return nil
}

// mountOf returns the container volumeMount that uses the volume.
func mountOf(pod any, volume any) any {
	for _, m := range list(pod, "containers", "0", "volumeMounts") {
		if volume != nil && str(m, "name") == str(volume, "name") {
			return m
		}
	}
	return nil
}

func configOf(t *testing.T, docs []doc) doc {
	t.Helper()
	var cfg doc
	if err := yaml.Unmarshal([]byte(str(find(docs, "ConfigMap", "quader-config"), "data", "quader.yaml")), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// kindSecrets lists the Secrets of the kinds' pairs (quader-tls-<kind>) that pod projects, in volume order.
func kindSecrets(pod any) []string {
	var out []string
	for _, v := range list(pod, "volumes") {
		if s := str(v, "secret", "secretName"); strings.HasPrefix(s, "quader-tls-") {
			out = append(out, s)
		}
	}
	return out
}

// noIssuer is the set that leaves the cert-manager path, for the renders that name Secrets or turn TLS off.
const noIssuer = "tls.certManager.issuerRef.name="

func TestTLS(t *testing.T) {
	t.Run("R-HA-063 the default render serves HTTPS: probes, its kind's pair without subPath, config paths, appProtocol", func(t *testing.T) {
		_, docs := render(t, required())
		// The pods of the links get ca.crt beside their pair, the api pods do not (R-HA-066).
		items := map[string]string{"api": "tls.crt->tls.crt,tls.key->tls.key", "packages": "tls.crt->tls.crt,tls.key->tls.key,ca.crt->ca.crt", "worker": "tls.crt->tls.crt,tls.key->tls.key,ca.crt->ca.crt"}
		for _, role := range []string{"api", "packages", "worker"} {
			pod := at(find(docs, "Deployment", "quader-"+role), "spec", "template", "spec")
			c := at(pod, "containers", "0")
			for _, probe := range []string{"startupProbe", "livenessProbe", "readinessProbe"} {
				if got := str(c, probe, "httpGet", "scheme"); got != "HTTPS" {
					t.Errorf("quader-%s %s scheme = %q, want HTTPS", role, probe, got)
				}
				if got := str(c, probe, "httpGet", "port"); got != "ops" {
					t.Errorf("quader-%s %s port = %q, want ops", role, probe, got)
				}
			}
			if got := kindSecrets(pod); len(got) != 1 || got[0] != "quader-tls-"+role {
				t.Fatalf("quader-%s projects the pairs %v, want its own kind's only", role, got)
			}
			vol := secretVolume(pod, "quader-tls-"+role)
			m := mountOf(pod, vol)
			if m == nil || str(m, "mountPath") != tlsMountPath+"" || at(m, "readOnly") != true {
				t.Errorf("quader-%s tls mount = %v, want a read-only mount at %s", role, m, tlsMountPath)
			}
			if at(m, "subPath") != nil {
				t.Errorf("quader-%s tls mount uses subPath, the kubelet's symlink swap would not reach the pod on rotation", role)
			}
			var keys []string
			for _, it := range list(vol, "secret", "items") {
				keys = append(keys, str(it, "key")+"->"+str(it, "path"))
			}
			if strings.Join(keys, ",") != items[role] {
				t.Errorf("quader-%s projects %v, want %s", role, keys, items[role])
			}
		}

		cfg := configOf(t, docs)
		want := map[string]string{
			"cert_file": tlsMountPath + "/tls.crt",
			"key_file":  tlsMountPath + "/tls.key",
		}
		for k, v := range want {
			if got := str(cfg, "server", "tls", "certificates", "0", k); got != v {
				t.Errorf("config server.tls.certificates[0].%s = %q, want %q", k, got, v)
			}
		}
		if got := str(cfg, "server", "tls", "min_version"); got != "1.2" {
			t.Errorf("server.tls.min_version = %q, want 1.2", got)
		}
		if got := str(cfg, "server", "tls", "reload_interval"); got != "10s" {
			t.Errorf("server.tls.reload_interval = %q, want 10s", got)
		}
		for _, absent := range [][]string{{"server", "tls", "client_ca_file"}, {"server", "tls", "client_auth"}, {"server", "ops_tls"}} {
			if at(cfg, absent...) != nil {
				t.Errorf("config %s is set without tls.clientCA", strings.Join(absent, "."))
			}
		}

		for _, name := range []string{"quader", "quader-ops"} {
			svc := find(docs, "Service", name)
			for _, p := range list(svc, "spec", "ports") {
				if got := str(p, "appProtocol"); got != "https" {
					t.Errorf("Service %s port %s appProtocol = %q, want https", name, str(p, "name"), got)
				}
			}
		}
	})

	t.Run("R-HA-063 Q-HA-346 the chart issues a Certificate per kind with its name, the ops name and the usages of the kind", func(t *testing.T) {
		_, docs := render(t, required())
		certs := ofKind(docs, "Certificate")
		if len(certs) != 6 {
			t.Fatalf("%d Certificates, want one per kind (api, packages, worker, vuln-scanner-trivy, malware-scanner-clamav, malware-updater-freshclam)", len(certs))
		}
		for i, kind := range []string{"api", "packages", "worker", "vuln-scanner-trivy", "malware-scanner-clamav", "malware-updater-freshclam"} {
			c := certs[i]
			name := "quader-tls-" + kind
			if str(c, "metadata", "name") != name || str(c, "spec", "secretName") != name {
				t.Errorf("Certificate %d = %s into %s, want %s", i, str(c, "metadata", "name"), str(c, "spec", "secretName"), name)
			}
			var dns []string
			for _, d := range list(c, "spec", "dnsNames") {
				dns = append(dns, d.(string))
			}
			if want := "quader-" + kind + ".default.svc,quader-ops.default.svc"; strings.Join(dns, ",") != want {
				t.Errorf("%s dnsNames = %v, want %s", name, dns, want)
			}
			var usages []string
			for _, u := range list(c, "spec", "usages") {
				usages = append(usages, u.(string))
			}
			want := "digital signature,server auth"
			if kind != "api" && kind != "packages" {
				want += ",client auth"
			}
			if strings.Join(usages, ",") != want {
				t.Errorf("%s usages = %v, want %s", name, usages, want)
			}
			if str(c, "spec", "issuerRef", "name") != "cluster-ca" || str(c, "spec", "issuerRef", "kind") != "ClusterIssuer" ||
				str(c, "spec", "issuerRef", "group") != "cert-manager.io" || str(c, "spec", "privateKey", "algorithm") != "ECDSA" ||
				str(c, "spec", "privateKey", "rotationPolicy") != "Always" || str(c, "spec", "duration") != "2160h" || str(c, "spec", "renewBefore") != "720h" {
				t.Errorf("%s spec = %v", name, at(c, "spec"))
			}
			if at(c, "spec", "ipAddresses") != nil {
				t.Errorf("%s carries IP addresses without extraIpAddresses", name)
			}
		}
		_, rsa := render(t, required(), "tls.certManager.privateKey.algorithm=RSA", "tls.certManager.privateKey.size=3072",
			"tls.certManager.extraDnsNames[0]=localhost", "tls.certManager.extraIpAddresses[0]=127.0.0.1", "config.vulnerabilities.images.enabled=false",
			"config.malware.enabled=false")
		c := ofKind(rsa, "Certificate")
		if len(c) != 3 {
			t.Fatalf("%d Certificates with both scannings off, want three: no scanner", len(c))
		}
		if toStr(at(c[0], "spec", "usages")) != "- digital signature\n- key encipherment\n- server auth\n" ||
			toStr(at(c[0], "spec", "dnsNames")) != "- quader-api.default.svc\n- quader-ops.default.svc\n- localhost\n" ||
			toStr(at(c[0], "spec", "ipAddresses")) != "- 127.0.0.1\n" {
			t.Errorf("an RSA pair with extra names = %v", at(c[0], "spec"))
		}
		renderFails(t, required(), []string{"tls.certManager.privateKey.algorithm=DSA"}, "algorithm")
	})

	t.Run("R-HA-063 Q-HA-347 without an issuer every kind names a Secret of its own; neither, both or one Secret for two kinds fail", func(t *testing.T) {
		renderFails(t, required(), []string{noIssuer}, "tls.certManager.issuerRef.name", "tls.existingSecrets", "tls.enabled: false")
		named := []string{
			noIssuer, "tls.existingSecrets.api=a", "tls.existingSecrets.packages=p", "tls.existingSecrets.worker=w", "tls.existingSecrets.vuln-scanner-trivy=s",
			"tls.existingSecrets.malware-scanner-clamav=m", "tls.existingSecrets.malware-updater-freshclam=u",
		}
		_, docs := render(t, required(), named...)
		if n := len(ofKind(docs, "Certificate")); n != 0 {
			t.Errorf("%d Certificates rendered while tls.existingSecrets names the pairs", n)
		}
		for role, secret := range map[string]string{"api": "a", "packages": "p", "worker": "w"} {
			if secretVolume(at(find(docs, "Deployment", "quader-"+role), "spec", "template", "spec"), secret) == nil {
				t.Errorf("quader-%s does not mount %s", role, secret)
			}
		}
		renderFails(t, required(), append(named[:4:4], "tls.existingSecrets.vuln-scanner-trivy=", "tls.existingSecrets.malware-scanner-clamav=m"), "tls.existingSecrets.vuln-scanner-trivy")
		renderFails(t, required(), append(named[:4:4], "tls.existingSecrets.vuln-scanner-trivy=w", "tls.existingSecrets.malware-scanner-clamav=m"), "tls.existingSecrets.worker", "tls.existingSecrets.vuln-scanner-trivy", "the same Secret w")
		renderFails(t, required(), []string{"tls.existingSecrets.api=a"}, "tls.certManager.issuerRef.name", "tls.existingSecrets.api")
		// Without the scanners, three Secrets are enough.
		render(t, required(), append(named[:4:4], "config.vulnerabilities.images.enabled=false", "config.malware.enabled=false")...)
	})

	t.Run("R-HA-063 tls.enabled=false renders HTTP probes, plain Service ports and no TLS wiring", func(t *testing.T) {
		out, docs := render(t, required(), "tls.enabled=false", noIssuer)
		for _, role := range []string{"api", "packages", "worker"} {
			pod := at(find(docs, "Deployment", "quader-"+role), "spec", "template", "spec")
			c := at(pod, "containers", "0")
			for _, probe := range []string{"startupProbe", "livenessProbe", "readinessProbe"} {
				if got := at(c, probe, "httpGet", "scheme"); got != nil {
					t.Errorf("quader-%s %s has scheme %v with tls.enabled false", role, probe, got)
				}
			}
			for _, v := range list(pod, "volumes") {
				if strings.Contains(str(v, "name"), "tls") {
					t.Errorf("quader-%s mounts %s with tls.enabled false", role, str(v, "name"))
				}
			}
		}
		cfg := configOf(t, docs)
		if at(cfg, "server", "tls") != nil || at(cfg, "server", "ops_tls") != nil {
			t.Errorf("config carries server.tls or ops_tls with tls.enabled false")
		}
		if strings.Contains(out, "appProtocol") || strings.Contains(out, "scheme: HTTPS") || strings.Contains(out, "BackendTLSPolicy") || len(ofKind(docs, "Certificate")) != 0 {
			t.Error("TLS wiring rendered with tls.enabled false")
		}
		t.Run("TLS options without tls.enabled fail", func(t *testing.T) {
			renderFails(t, required(), []string{"tls.enabled=false", noIssuer, "tls.clientCA.secretName=edge"}, "tls.enabled")
			render(t, required(), "tls.enabled=false") // the issuer value may stay: without TLS it renders nothing
		})
	})

	t.Run("R-HA-030 the migration Job serves no listener and mounts no TLS material; each kind mounts its own pair", func(t *testing.T) {
		_, docs := render(t, haIngress())
		job := at(find(docs, "Job", "quader-migrate"), "spec", "template", "spec")
		if job == nil {
			t.Fatal("no migration Job rendered")
		}
		for _, v := range list(job, "volumes") {
			if strings.Contains(str(v, "name"), "tls") || strings.Contains(str(v, "secret", "secretName"), "tls") {
				t.Errorf("the migration Job mounts %s", str(v, "name"))
			}
		}
		for _, m := range list(job, "containers", "0", "volumeMounts") {
			if strings.Contains(str(m, "mountPath"), "/tls") {
				t.Errorf("the migration Job mounts %s", str(m, "mountPath"))
			}
		}
		// The packages pods hold their own pair and the client CA of :8080; the worker its own pair only (SA5, H-57).
		web := at(find(docs, "Deployment", "quader-packages"), "spec", "template", "spec")
		for _, s := range []string{"quader-tls-packages", "quader-edge-ca"} {
			if secretVolume(web, s) == nil {
				t.Errorf("quader-packages does not mount %s", s)
			}
		}
		worker := at(find(docs, "Deployment", "quader-worker"), "spec", "template", "spec")
		if got := kindSecrets(worker); len(got) != 1 || got[0] != "quader-tls-worker" {
			t.Errorf("quader-worker mounts the pairs %v, want quader-tls-worker only", got)
		}
		if secretVolume(worker, "quader-edge-ca") != nil {
			t.Error("quader-worker mounts quader-edge-ca although it serves no listener that loads it")
		}
	})

	t.Run("R-HA-063 a client CA renders client_ca_file and client_auth and keeps the operations listener free of client certificates", func(t *testing.T) {
		_, docs := render(t, required(), "tls.clientCA.secretName=quader-edge-ca")
		cfg := configOf(t, docs)
		if got := str(cfg, "server", "tls", "client_ca_file"); got != "/var/run/secrets/quader/tls-client-ca/ca.crt" {
			t.Errorf("client_ca_file = %q", got)
		}
		if got := str(cfg, "server", "tls", "client_auth"); got != "require" {
			t.Errorf("client_auth = %q, want require", got)
		}
		// Probes and scrapers present no client certificate: :8081 must not inherit the requirement.
		if at(cfg, "server", "ops_tls", "inherit") != false {
			t.Errorf("ops_tls.inherit = %v, want false so the client CA does not reach :8081", at(cfg, "server", "ops_tls", "inherit"))
		}
		if got := str(cfg, "server", "ops_tls", "certificates", "0", "cert_file"); got != tlsMountPath+"/tls.crt" {
			t.Errorf("ops_tls certificate = %q, want the same pair as :8080", got)
		}
		if at(cfg, "server", "ops_tls", "client_ca_file") != nil || at(cfg, "server", "ops_tls", "client_auth") != nil {
			t.Error("ops_tls carries a client CA")
		}
		pod := at(find(docs, "Deployment", "quader-packages"), "spec", "template", "spec")
		vol := secretVolume(pod, "quader-edge-ca")
		if vol == nil || str(mountOf(pod, vol), "mountPath") != "/var/run/secrets/quader/tls-client-ca" {
			t.Errorf("client CA volume = %v", vol)
		}
		if at(mountOf(pod, vol), "subPath") != nil {
			t.Error("client CA mount uses subPath")
		}
		t.Run("verify_if_given", func(t *testing.T) {
			_, docs := render(t, required(), "tls.clientCA.secretName=quader-edge-ca", "tls.clientCA.auth=verify_if_given")
			if got := str(configOf(t, docs), "server", "tls", "client_auth"); got != "verify_if_given" {
				t.Errorf("client_auth = %q", got)
			}
		})
		t.Run("an unknown mode fails schema validation", func(t *testing.T) {
			renderFails(t, required(), []string{"tls.clientCA.secretName=x", "tls.clientCA.auth=maybe"}, "auth")
		})
	})

	t.Run("R-HA-063 the values of the one pair are gone", func(t *testing.T) {
		for set, key := range map[string]string{
			"tls.secretName=quader-tls": "'secretName'", "tls.ops.secretName=quader-ops-tls": "'ops'", "tls.certKey=tls.crt": "'certKey'",
			"vulnScannerTrivy.tls.secretName=s": "'tls'", "tls.backendTLSPolicy.hostname=quader.default.svc": "'hostname'", "ingress.backendTLS.serversTransport=t": "'serversTransport'",
		} {
			renderFails(t, required(), []string{set}, key)
		}
	})

	t.Run("R-HA-063 min version and reload interval reach the config", func(t *testing.T) {
		_, docs := render(t, required(), "tls.minVersion=1.3", "tls.reloadInterval=30s")
		cfg := configOf(t, docs)
		if str(cfg, "server", "tls", "min_version") != "1.3" || str(cfg, "server", "tls", "reload_interval") != "30s" {
			t.Errorf("server.tls = %v", at(cfg, "server", "tls"))
		}
		renderFails(t, required(), []string{"tls.minVersion=1.1"}, "minVersion")
	})

	t.Run("R-HA-063 the chart owns server.tls and server.ops_tls while TLS is on", func(t *testing.T) {
		renderFails(t, required(), []string{"config.server.tls.min_version=1.3"}, "config.server.tls")
		renderFails(t, required(), []string{"config.server.ops_tls.inherit=false"}, "config.server.ops_tls")
		// Opted out, the operator may wire their own material (extraVolumes) through config.
		render(t, required(), "tls.enabled=false", noIssuer, "config.server.tls.min_version=1.3")
	})

	t.Run("R-HA-063 rotating the Secret does not roll the pods, a changed setting does", func(t *testing.T) {
		_, a := render(t, required())
		_, b := render(t, required(), "tls.minVersion=1.3")
		sum := func(docs []doc) string {
			return str(find(docs, "Deployment", "quader-packages"), "spec", "template", "metadata", "annotations", "checksum/config")
		}
		if sum(a) == sum(b) {
			t.Error("checksum/config does not follow server.tls")
		}
	})

	t.Run("R-HA-063 Q-HA-348 the edge verifies each backend under its kind's name, where the controller reads it", func(t *testing.T) {
		t.Run("HAProxy Ingress is the default: secure-backends on the Ingress, the kind's name and CA on each Service", func(t *testing.T) {
			_, docs := render(t, haIngress())
			ann := at(ofKind(docs, "Ingress")[0], "metadata", "annotations")
			for k, v := range map[string]string{"haproxy-ingress.github.io/secure-backends": "true", "haproxy-ingress.github.io/secure-crt-secret": "quader-edge-client"} {
				if got := str(ann, k); got != v {
					t.Errorf("Ingress annotation %s = %q, want %q", k, got, v)
				}
			}
			if at(ann, "haproxy-ingress.github.io/secure-sni") != nil || at(ann, "haproxy-ingress.github.io/secure-verify-ca-secret") != nil {
				t.Errorf("the Ingress carries a name or a CA for every backend: %v", ann)
			}
			for svc, kind := range map[string]string{"quader-api": "api", "quader-packages": "packages"} {
				sa := at(find(docs, "Service", svc), "metadata", "annotations")
				if str(sa, "haproxy-ingress.github.io/secure-sni") != "quader-"+kind+".default.svc" ||
					str(sa, "haproxy-ingress.github.io/secure-verify-ca-secret") != "quader-tls-"+kind {
					t.Errorf("Service %s annotations = %v, want the name and the Secret of %s", svc, sa, kind)
				}
			}
			if s := toStr(ann); strings.Contains(s, "nginx.ingress") || strings.Contains(s, "traefik") {
				t.Errorf("another controller's setting rendered: %s", s)
			}
		})
		t.Run("a named CA wins; tls.clientCA needs a client certificate; traefik needs a transport per kind", func(t *testing.T) {
			_, docs := render(t, required(), "ingress.enabled=true", "ingress.backendTLS.caSecret=corp-ca")
			if got := str(find(docs, "Service", "quader-packages"), "metadata", "annotations", "haproxy-ingress.github.io/secure-verify-ca-secret"); got != "corp-ca" {
				t.Errorf("secure-verify-ca-secret = %q, want corp-ca", got)
			}
			renderFails(t, required(), []string{"ingress.enabled=true", "tls.clientCA.secretName=edge"}, "ingress.backendTLS.clientCertSecret")
			renderFails(t, required(), []string{"ingress.enabled=true", "ingress.backendTLS.controller=traefik", "ingress.backendTLS.serversTransports.api=a"}, "ingress.backendTLS.serversTransports.api and .packages")
		})
		t.Run("verify=false renders an encrypted but unverified connection", func(t *testing.T) {
			_, docs := render(t, required(), "ingress.enabled=true", "ingress.backendTLS.verify=false")
			ann := at(ofKind(docs, "Ingress")[0], "metadata", "annotations")
			if str(ann, "haproxy-ingress.github.io/secure-backends") != "true" {
				t.Errorf("annotations = %v", ann)
			}
			if sa := at(find(docs, "Service", "quader-api"), "metadata", "annotations"); sa != nil && strings.Contains(toStr(sa), "secure-") {
				t.Errorf("Service quader-api annotations = %v", sa)
			}
		})
		t.Run("tls.enabled=false needs no controller", func(t *testing.T) {
			_, docs := render(t, required(), "tls.enabled=false", noIssuer, "ingress.enabled=true")
			ing := ofKind(docs, "Ingress")[0]
			if at(ing, "metadata", "annotations") != nil {
				t.Errorf("annotations = %v", at(ing, "metadata", "annotations"))
			}
		})
		t.Run("ingress-nginx gets an Ingress per kind, each with its kind's name; the first holds the TLS", func(t *testing.T) {
			_, docs := render(t, required(), "ingress.enabled=true", "ingress.backendTLS.controller=ingress-nginx", "ingress.tls[0].secretName=edge", "ingress.tls[0].hosts[0]=quader.example.com")
			ings := ofKind(docs, "Ingress")
			if len(ings) != 2 || str(ings[0], "metadata", "name") != "quader" || str(ings[1], "metadata", "name") != "quader-packages" {
				t.Fatalf("Ingress objects = %d, want quader and quader-packages", len(ings))
			}
			for i, kind := range []string{"api", "packages"} {
				ann := at(ings[i], "metadata", "annotations")
				want := map[string]string{
					"nginx.ingress.kubernetes.io/backend-protocol":      "HTTPS",
					"nginx.ingress.kubernetes.io/proxy-ssl-verify":      "on",
					"nginx.ingress.kubernetes.io/proxy-ssl-secret":      "default/quader-tls-" + kind,
					"nginx.ingress.kubernetes.io/proxy-ssl-name":        "quader-" + kind + ".default.svc",
					"nginx.ingress.kubernetes.io/proxy-ssl-server-name": "on",
				}
				for k, v := range want {
					if got := str(ann, k); got != v {
						t.Errorf("Ingress %d annotation %s = %q, want %q", i, k, got, v)
					}
				}
				for _, p := range list(ings[i], "spec", "rules", "0", "http", "paths") {
					if svc := str(p, "backend", "service", "name"); !strings.HasPrefix(svc, "quader-"+kind) {
						t.Errorf("the Ingress of %s routes %s to %s", kind, str(p, "path"), svc)
					}
				}
				if strings.Contains(strings.ToLower(toStr(ann)), "traefik") {
					t.Error("a Traefik setting rendered for ingress-nginx")
				}
			}
			if at(ings[0], "spec", "tls") == nil || at(ings[1], "spec", "tls") != nil {
				t.Errorf("spec.tls = %v, %v; want it on the first Ingress only", at(ings[0], "spec", "tls"), at(ings[1], "spec", "tls"))
			}
		})
		t.Run("traefik sets the scheme and the kind's transport on the Service", func(t *testing.T) {
			_, docs := render(t, required(), "ingress.enabled=true", "ingress.backendTLS.controller=traefik",
				"ingress.backendTLS.serversTransports.api=quader-quader-api@kubernetescrd", "ingress.backendTLS.serversTransports.packages=quader-quader-packages@kubernetescrd")
			for svc, kind := range map[string]string{"quader-api": "api", "quader-packages": "packages"} {
				ann := at(find(docs, "Service", svc), "metadata", "annotations")
				if str(ann, "traefik.ingress.kubernetes.io/service.serversscheme") != "https" ||
					str(ann, "traefik.ingress.kubernetes.io/service.serverstransport") != "quader-quader-"+kind+"@kubernetescrd" {
					t.Errorf("Service %s annotations = %v", svc, ann)
				}
			}
			if a := at(ofKind(docs, "Ingress")[0], "metadata", "annotations"); a != nil && strings.Contains(toStr(a), "nginx") {
				t.Errorf("an ingress-nginx setting rendered for Traefik: %v", a)
			}
		})
		t.Run("manual renders no vendor setting", func(t *testing.T) {
			out, _ := render(t, required(), "ingress.enabled=true", "ingress.backendTLS.controller=manual")
			if strings.Contains(out, "nginx.ingress") || strings.Contains(out, "traefik.ingress") || strings.Contains(out, "haproxy-ingress") {
				t.Error("a vendor annotation rendered for manual")
			}
		})
	})

	t.Run("R-HA-063 the BackendTLSPolicy for Gateway API is optional and verifies each Service's kind against a CA ConfigMap", func(t *testing.T) {
		_, docs := render(t, required())
		if len(ofKind(docs, "BackendTLSPolicy")) != 0 {
			t.Error("a BackendTLSPolicy rendered by default")
		}
		renderFails(t, required(), []string{"tls.backendTLSPolicy.enabled=true"}, "tls.backendTLSPolicy.caConfigMapName")
		_, docs = render(t, required(), "tls.backendTLSPolicy.enabled=true", "tls.backendTLSPolicy.caConfigMapName=quader-tls-ca")
		p := ofKind(docs, "BackendTLSPolicy")
		// one policy per Service the default endpoint's edge names (R-HA-038), each verifying the name of its Service's kind
		if len(p) != 2 {
			t.Fatalf("%d BackendTLSPolicy objects, want one per role Service", len(p))
		}
		for i, kind := range []string{"api", "packages"} {
			if str(p[i], "spec", "targetRefs", "0", "name") != "quader-"+kind || str(p[i], "spec", "targetRefs", "0", "kind") != "Service" ||
				str(p[i], "metadata", "name") != "quader-"+kind {
				t.Errorf("targetRefs = %v", at(p[i], "spec", "targetRefs"))
			}
			if str(p[i], "spec", "validation", "hostname") != "quader-"+kind+".default.svc" ||
				str(p[i], "spec", "validation", "caCertificateRefs", "0", "name") != "quader-tls-ca" ||
				str(p[i], "spec", "validation", "caCertificateRefs", "0", "kind") != "ConfigMap" {
				t.Errorf("validation = %v", at(p[i], "spec", "validation"))
			}
		}
	})

	t.Run("R-HA-063 every value set either issues or names its pairs or opts out explicitly", func(t *testing.T) {
		eachSet(t, func(t *testing.T, _ string, _ string, docs []doc) {
			cfg := configOf(t, docs)
			on := at(cfg, "server", "tls") != nil
			pod := at(find(docs, "Deployment", "quader-packages"), "spec", "template", "spec")
			probe := str(pod, "containers", "0", "readinessProbe", "httpGet", "scheme")
			if on != (probe == "HTTPS") {
				t.Errorf("server.tls set = %v but the readiness probe scheme is %q", on, probe)
			}
		})
	})
}

// toStr renders a decoded document for substring checks.
func toStr(v any) string {
	b, _ := yaml.Marshal(v)
	return string(b)
}

func TestTLSMountsFollowTheListeners(t *testing.T) {
	t.Run("R-HA-030 R-HA-063 every runtime pod mounts its own kind's pair and no other, also with the scannings off", func(t *testing.T) {
		for _, sets := range [][]string{nil, {"config.vulnerabilities.images.enabled=false", "config.malware.enabled=false"}} {
			_, docs := render(t, required(), sets...)
			for _, role := range []string{"api", "packages", "worker"} {
				if got := kindSecrets(at(find(docs, "Deployment", "quader-"+role), "spec", "template", "spec")); len(got) != 1 || got[0] != "quader-tls-"+role {
					t.Errorf("%v: quader-%s mounts the pairs %v, want quader-tls-%s only", sets, role, got, role)
				}
			}
		}
		_, off := render(t, required(), "config.vulnerabilities.images.enabled=false", "config.malware.enabled=false")
		for _, it := range list(secretVolume(at(find(off, "Deployment", "quader-worker"), "spec", "template", "spec"), "quader-tls-worker"), "secret", "items") {
			if str(it, "key") == "ca.crt" {
				t.Error("quader-worker projects ca.crt with both scannings off, although no link verifies with it")
			}
		}
	})

	t.Run("R-HA-030 an endpoint pool mounts its kind's pair, and its endpoint's own pair beside it", func(t *testing.T) {
		pool := []string{"endpoints.c.port=8443", "endpoints.c.baseUrl=https://p.example.com", "endpoints.c.pool.enabled=true"}
		podOf := func(docs []doc) any {
			return at(find(docs, "Deployment", "quader-packages-ep-c"), "spec", "template", "spec")
		}
		_, docs := render(t, required(), pool...)
		if got := kindSecrets(podOf(docs)); len(got) != 1 || got[0] != "quader-tls-packages" {
			t.Errorf("the pool mounts the pairs %v, want its kind's", got)
		}
		_, docs = render(t, required(), append(pool, "endpoints.c.tls.existingSecret=c-tls")...)
		if secretVolume(podOf(docs), "c-tls") == nil || secretVolume(podOf(docs), "quader-tls-packages") == nil {
			t.Error("the pool does not mount its endpoint's pair beside its kind's")
		}
	})

	t.Run("R-HA-030 the client CA of :8080 reaches the main web pods only", func(t *testing.T) {
		_, docs := render(t, required(), "tls.clientCA.secretName=quader-edge-ca", "endpoints.c.port=8443", "endpoints.c.baseUrl=https://p.example.com", "endpoints.c.pool.enabled=true")
		for name, want := range map[string]bool{"quader-packages": true, "quader-worker": false, "quader-packages-ep-c": false} {
			if got := secretVolume(at(find(docs, "Deployment", name), "spec", "template", "spec"), "quader-edge-ca") != nil; got != want {
				t.Errorf("%s mounts the client CA: %v, want %v", name, got, want)
			}
		}
	})
}
