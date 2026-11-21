//go:build chart

package chart_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Quader's signing identity in the chart (R-HA-071, signatures.md R-SIG-046, Q-HA-385): an existing Secret or a cert-manager Certificate,
// at most one, mounted into the worker pods alone with its three files in the configuration; the four alerts are TestR_HA_045's and
// TestR_VULN_024_PromtoolRules'.

// signingVolume is the volume of the identity's Secret in a pod spec, nil without one.
func signingVolume(pod any) any {
	for _, v := range list(pod, "volumes") {
		if str(v, "name") == "secret-signing" {
			return v
		}
	}
	return nil
}

func TestR_HA_071_TheSigningIdentityReachesTheWorkerPodsAlone(t *testing.T) {
	pool := []string{"worker.pools[0].name=malware", "worker.pools[0].queues[0]=malware", "worker.pools[0].replicaCount=2"}

	t.Run("with existingSecret the worker and its pools mount the Secret at the path, the configuration names the three files, nothing else mounts it", func(t *testing.T) {
		_, docs := render(t, required(), append([]string{"signing.identity.existingSecret=quader-signing"}, pool...)...)
		workers := 0
		for _, d := range append(ofKind(docs, "Deployment"), append(ofKind(docs, "Job"), ofKind(docs, "CronJob")...)...) {
			name := str(d, "metadata", "name")
			pod := at(d, "spec", "template", "spec")
			if str(d, "kind") == "CronJob" {
				pod = at(d, "spec", "jobTemplate", "spec", "template", "spec")
			}
			vol := signingVolume(pod)
			isWorker := str(d, "kind") == "Deployment" && strings.HasPrefix(name, "quader-worker")
			if !isWorker {
				if vol != nil {
					t.Errorf("%s %s mounts the signing identity", str(d, "kind"), name)
				}
				continue
			}
			workers++
			if vol == nil {
				t.Fatalf("%s does not mount the signing identity", name)
			}
			var items []string
			for _, it := range list(vol, "secret", "items") {
				items = append(items, str(it, "key")+">"+str(it, "path"))
			}
			if str(vol, "secret", "secretName") != "quader-signing" || at(vol, "secret", "defaultMode") != 288 ||
				!reflect.DeepEqual(items, []string{"tls.crt>tls.crt", "tls.key>tls.key", "ca.crt>ca.crt"}) {
				t.Errorf("%s: the volume %v", name, vol)
			}
			if !slices.Contains(mounts(container(pod, "quader")), "secret-signing:/var/run/secrets/quader/signing") {
				t.Errorf("%s: mounts %v", name, mounts(container(pod, "quader")))
			}
			for _, m := range list(container(pod, "quader"), "volumeMounts") {
				if str(m, "name") == "secret-signing" && (at(m, "readOnly") != true || str(m, "subPath") != "") {
					t.Errorf("%s: the mount %v is not read-only without subPath", name, m)
				}
			}
		}
		if workers != 2 {
			t.Errorf("%d worker Deployments mount it, want the main one and its pool", workers)
		}
		id, _ := at(renderedConfig(t, docs), "signing", "identity").(map[string]any)
		want := map[string]any{
			"cert_file": "/var/run/secrets/quader/signing/tls.crt", "key_file": "/var/run/secrets/quader/signing/tls.key",
			"ca_file": "/var/run/secrets/quader/signing/ca.crt",
		}
		if !reflect.DeepEqual(id, want) {
			t.Errorf("config.signing.identity = %v", id)
		}
		if find(docs, "Certificate", "quader-signing") != nil {
			t.Error("an existing Secret renders a Certificate")
		}
	})

	t.Run("with certManager the Certificate renders ECDSA 256, rotationPolicy Always, the two usages and the base URL first", func(t *testing.T) {
		_, docs := render(t, required(), "signing.identity.certManager.issuerRef.name=company-code-signing",
			"signing.identity.certManager.subject.organizations[0]=Example GmbH", "signing.identity.certManager.uris[0]=https://quader.example.com/signing")
		c := find(docs, "Certificate", "quader-signing")
		if c == nil {
			t.Fatal("no Certificate quader-signing")
		}
		base := str(renderedConfig(t, docs), "server", "base_url")
		switch {
		case str(c, "spec", "secretName") != "quader-signing" || str(c, "spec", "privateKey", "algorithm") != "ECDSA" ||
			at(c, "spec", "privateKey", "size") != 256 || str(c, "spec", "privateKey", "rotationPolicy") != "Always":
			t.Errorf("the key %v", at(c, "spec", "privateKey"))
		case !reflect.DeepEqual(list(c, "spec", "usages"), []any{"digital signature", "code signing"}):
			t.Errorf("usages %v", list(c, "spec", "usages"))
		case !reflect.DeepEqual(list(c, "spec", "uris"), []any{base, "https://quader.example.com/signing"}):
			t.Errorf("uris %v (base %s)", list(c, "spec", "uris"), base)
		case str(c, "spec", "commonName") != "Quader signing" || str(c, "spec", "duration") != "2160h" || str(c, "spec", "renewBefore") != "720h":
			t.Errorf("spec %v", at(c, "spec"))
		case str(c, "spec", "issuerRef", "name") != "company-code-signing" || str(c, "spec", "issuerRef", "kind") != "ClusterIssuer":
			t.Errorf("issuerRef %v", at(c, "spec", "issuerRef"))
		case !reflect.DeepEqual(list(c, "spec", "subject", "organizations"), []any{"Example GmbH"}):
			t.Errorf("subject %v", at(c, "spec", "subject"))
		}
		w := at(find(docs, "Deployment", "quader-worker"), "spec", "template", "spec")
		if str(signingVolume(w), "secret", "secretName") != "quader-signing" {
			t.Errorf("the worker mounts %v", signingVolume(w))
		}
	})

	t.Run("with both sources the render fails naming both keys; the three files under config are refused", func(t *testing.T) {
		renderFails(t, required(), []string{"signing.identity.existingSecret=s", "signing.identity.certManager.issuerRef.name=i"},
			"signing.identity.existingSecret", "signing.identity.certManager.issuerRef.name")
		for _, key := range []string{"cert_file", "key_file", "ca_file"} {
			renderFails(t, required(), []string{"config.signing.identity." + key + "=/x"}, "config.signing.identity."+key+" is managed by the chart")
		}
	})

	t.Run("with neither nothing of it renders", func(t *testing.T) {
		_, docs := render(t, required())
		if find(docs, "Certificate", "quader-signing") != nil {
			t.Error("a Certificate without a source")
		}
		for _, d := range ofKind(docs, "Deployment") {
			if signingVolume(at(d, "spec", "template", "spec")) != nil {
				t.Errorf("%s mounts the signing identity", str(d, "metadata", "name"))
			}
		}
		if at(renderedConfig(t, docs), "signing") != nil {
			t.Errorf("config.signing = %v", at(renderedConfig(t, docs), "signing"))
		}
	})
}
