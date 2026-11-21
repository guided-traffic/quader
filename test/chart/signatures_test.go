//go:build chart

package chart_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// The signatures on artifacts in the chart (signatures.md R-SIG-018, R-SIG-039): the trust anchors of signatures.anchors rendered into the
// configuration file, their ConfigMap material mounted into the migration Job only, and the three alerts. ci/ha-ingress.yaml declares a
// CA anchor and a Sigstore anchor with their material from ConfigMaps; TestRenderedConfigurationPassesTheBinary runs the binary's
// validator over that file.

// signaturesOf is the section signatures of the configuration file a ConfigMap holds, nil without one.
func signaturesOf(t *testing.T, docs []doc, configMap string) map[string]any {
	t.Helper()
	cm := find(docs, "ConfigMap", configMap)
	if cm == nil {
		t.Fatalf("no ConfigMap %s", configMap)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(str(cm, "data", "quader.yaml")), &cfg); err != nil {
		t.Fatal(err)
	}
	s, _ := cfg["signatures"].(map[string]any)
	return s
}

// TestR_SIG_018_DeclaredAnchorsReachTheMigrationJob: a declared CA anchor whose certificates come from a configMapKeyRef is rendered into
// both configuration files as the path of its file, the migration Job mounts exactly that key of the ConfigMap read-only at that path,
// and no Deployment mounts it; the defaults render no anchor and no mount; a duplicate key, config.signatures.anchors and a Secret as the
// source are refused.
func TestR_SIG_018_DeclaredAnchorsReachTheMigrationJob(t *testing.T) {
	t.Run("the default values render no anchor, no signatures section and no mount", func(t *testing.T) {
		_, docs := render(t, required())
		for _, cm := range []string{"quader-config", "quader-migrate"} {
			if s := signaturesOf(t, docs, cm); s != nil {
				t.Errorf("%s: signatures = %v", cm, s)
			}
		}
		for _, v := range list(at(find(docs, "Job", "quader-migrate"), "spec", "template", "spec"), "volumes") {
			if str(v, "name") == "signature-anchors" {
				t.Error("the migration Job mounts signature-anchors without a declared anchor")
			}
		}
	})

	_, docs := render(t, haIngress())
	t.Run("both configuration files carry the anchors, the material as the path of its mounted file", func(t *testing.T) {
		want := map[string]any{
			"gitlab-ci-signing": map[string]any{
				"title": "GitLab CI signing CA", "kind": "ca",
				"ca": map[string]any{
					"certificates_file": "/etc/quader/signature-anchors/gitlab-ci-signing/certificates.pem",
					"subject":           map[string]any{"O": "Example GmbH"},
				},
			},
			"company-sigstore": map[string]any{
				"title": "Company Sigstore", "kind": "sigstore",
				"sigstore": map[string]any{
					"issuer":            "https://gitlab.example.com",
					"identity_pattern":  `^https://gitlab\.example\.com/platform/.+$`,
					"trusted_root_file": "/etc/quader/signature-anchors/company-sigstore/trusted_root.json",
				},
			},
		}
		for _, cm := range []string{"quader-config", "quader-migrate"} {
			if got := signaturesOf(t, docs, cm)["anchors"]; !reflect.DeepEqual(got, want) {
				t.Errorf("%s: signatures.anchors = %#v", cm, got)
			}
		}
	})

	t.Run("the migration Job mounts the ConfigMap keys read-only, one file per material", func(t *testing.T) {
		pod := at(find(docs, "Job", "quader-migrate"), "spec", "template", "spec")
		var vol any
		for _, v := range list(pod, "volumes") {
			if str(v, "name") == "signature-anchors" {
				vol = v
			}
		}
		if vol == nil {
			t.Fatal("no volume signature-anchors in the migration Job")
		}
		var got []string
		for _, src := range list(vol, "projected", "sources") {
			for _, it := range list(src, "configMap", "items") {
				got = append(got, str(src, "configMap", "name")+":"+str(it, "key")+">"+str(it, "path"))
			}
			if at(src, "secret") != nil {
				t.Errorf("a Secret is projected: %v", src)
			}
		}
		want := []string{"company-ca:ca.crt>gitlab-ci-signing/certificates.pem", "company-sigstore-root:trusted_root.json>company-sigstore/trusted_root.json"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("sources = %v, want %v", got, want)
		}
		if !slices.Contains(mounts(container(pod, "migrate")), "signature-anchors:/etc/quader/signature-anchors") {
			t.Errorf("mounts = %v", mounts(container(pod, "migrate")))
		}
		for _, m := range list(container(pod, "migrate"), "volumeMounts") {
			if str(m, "name") == "signature-anchors" && (at(m, "readOnly") != true || str(m, "subPath") != "") {
				t.Errorf("mount %v is not read-only without subPath", m)
			}
		}
	})

	t.Run("no Deployment mounts the anchors' material", func(t *testing.T) {
		for _, d := range ofKind(docs, "Deployment") {
			for _, v := range list(at(d, "spec", "template", "spec"), "volumes") {
				if str(v, "name") == "signature-anchors" {
					t.Errorf("%s mounts signature-anchors", str(d, "metadata", "name"))
				}
			}
		}
	})

	t.Run("a duplicate key, config.signatures.anchors and a Secret as the source are refused", func(t *testing.T) {
		anchor := func(i int, key string) []string {
			p := "signatures.anchors[" + string(rune('0'+i)) + "]."
			return []string{p + "key=" + key, p + "title=t", p + "kind=openpgp", p + "openpgp.keys=k"}
		}
		renderFails(t, required(), append(anchor(0, "release"), anchor(1, "release")...), `signatures.anchors[1].key "release" is also the key of signatures.anchors[0]`)
		renderFails(t, required(), []string{"config.signatures.anchors.a.title=t"}, "config.signatures.anchors is rendered by the chart from signatures.anchors")
		renderFails(t, required(), []string{
			"signatures.anchors[0].key=a", "signatures.anchors[0].title=t", "signatures.anchors[0].kind=ca",
			"signatures.anchors[0].ca.certificatesFrom.secretKeyRef.name=s", "signatures.anchors[0].ca.certificatesFrom.secretKeyRef.key=k",
		}, "secretKeyRef")
		renderFails(t, required(), []string{
			"signatures.anchors[0].key=a", "signatures.anchors[0].title=t", "signatures.anchors[0].kind=openpgp",
			"signatures.anchors[0].ca.certificates=c",
		}, "missing property 'openpgp'")
	})
}

// TestR_SIG_039_TheAlertsOfTheSignatures: the three alerts of R-SIG-039's table and the one of R-SIG-075's (TestR_SIG_075 below the
// name, stage 3's gate) render with their expression, duration and severity, scoped to the release's namespace, while
// config.signatures.enabled (the default), and not at all when it is false. Their promtool tests are in
// deploy/helm/quader/tests/rules_test.yaml (TestR_VULN_024_PromtoolRules).
func TestR_SIG_039_TheAlertsOfTheSignatures(t *testing.T) {
	want := map[string]struct{ expr, dur string }{
		"QuaderSignatureInvalid":        {`sum(increase(quader_signatures_checks_total{ namespace="default",result="invalid" }[1h])) > 0`, ""},
		"QuaderSignatureBacklog":        {`max(quader_signatures_oldest_pending_seconds{ namespace="default" }) > 3600`, "15m"},
		"QuaderSignatureTrustRootStale": {`max(quader_signatures_trust_root_age_seconds{ namespace="default" }) > 1209600`, "1h"},
		// R-SIG-075: clients wait and get 503 because the checks do not keep up.
		"QuaderSignatureGatePending": {`sum(increase(quader_signatures_download_wait_seconds_count{ namespace="default",outcome="pending" }[15m])) > 0`, "15m"},
	}
	got := map[string]any{}
	for _, r := range renderedRules(t) {
		if strings.HasPrefix(str(r, "alert"), "QuaderSignature") {
			got[str(r, "alert")] = r
		}
	}
	for name, w := range want {
		r := got[name]
		if r == nil {
			t.Errorf("%s is not rendered", name)
			continue
		}
		if str(r, "expr") != w.expr || str(r, "for") != w.dur || str(r, "labels", "severity") != "warning" {
			t.Errorf("%s: expr %q for %q severity %q", name, str(r, "expr"), str(r, "for"), str(r, "labels", "severity"))
		}
	}
	if len(got) != len(want) {
		t.Errorf("signature alerts %v", got)
	}
	for _, r := range renderedRules(t, "config.signatures.enabled=false") {
		if strings.HasPrefix(str(r, "alert"), "QuaderSignature") {
			t.Errorf("%s is rendered with config.signatures.enabled false", str(r, "alert"))
		}
	}
}
