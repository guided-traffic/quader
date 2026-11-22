//go:build chart

package chart_test

import "testing"

// TestR_BAS_061_TheCacheKeyIsMountedAndNamedOnlyWithItsSecret (R-BAS-061, R-BAS-046, Q-BAS-214): secrets.cacheKey, empty by
// default, is mounted into the Bastion's pods as a directory and named by bastion.cache.encryption_key_file only when
// existingSecret is set; the key is chart-managed and refused under config; with the volume backend the fills then go to the
// pod's tmp volume, which the chart sizes like the bucket backend's.
func TestR_BAS_061_TheCacheKeyIsMountedAndNamedOnlyWithItsSecret(t *testing.T) {
	const keySet = "secrets.cacheKey.existingSecret=quader-bastion-cache-key"
	pod := func(t *testing.T, docs []doc) any {
		t.Helper()
		return at(bstWorkload(t, docs), "spec", "template", "spec")
	}
	t.Run("without the Secret nothing is mounted and the cache stays plain", func(t *testing.T) {
		_, docs := bstRender(t, bstRequired())
		if got := at(bstConfig(t, docs), "bastion", "cache", "encryption_key_file"); got != nil {
			t.Errorf("bastion.cache.encryption_key_file = %v without secrets.cacheKey", got)
		}
		if volumeNamed(pod(t, docs), "secret-cache-key") != nil {
			t.Error("a cache key volume is rendered without secrets.cacheKey")
		}
	})
	t.Run("with the Secret the key is mounted as a directory and the file is named", func(t *testing.T) {
		for name, files := range map[string][]string{"volume": bstRequired(), "bucket": {bastionSet("bucket-valkey")}} {
			_, docs := bstRender(t, files, keySet, "secrets.cacheKey.key=cache.key")
			if got := at(bstConfig(t, docs), "bastion", "cache", "encryption_key_file"); got != "/var/run/secrets/quader/cache-key/key" {
				t.Errorf("%s: bastion.cache.encryption_key_file = %v", name, got)
			}
			p := pod(t, docs)
			v := volumeNamed(p, "secret-cache-key")
			if str(v, "secret", "secretName") != "quader-bastion-cache-key" || str(v, "secret", "items", "0", "key") != "cache.key" ||
				str(v, "secret", "items", "0", "path") != "key" {
				t.Errorf("%s: cache key volume = %v", name, v)
			}
			var mounted bool
			for _, m := range list(p, "containers", "0", "volumeMounts") {
				if str(m, "name") == "secret-cache-key" {
					mounted = str(m, "mountPath") == "/var/run/secrets/quader/cache-key" && at(m, "readOnly") == true && str(m, "subPath") == ""
				}
			}
			if !mounted {
				t.Errorf("%s: the cache key is not mounted read-only at /var/run/secrets/quader/cache-key without subPath", name)
			}
			for _, e := range list(p, "containers", "0", "env") {
				if at(e, "valueFrom", "secretKeyRef") != nil {
					t.Errorf("%s: env %s reads a Secret", name, str(e, "name"))
				}
			}
		}
	})
	t.Run("the volume backend with the key spools its fills in tmp, sized for them", func(t *testing.T) {
		tmp := func(t *testing.T, sets ...string) string {
			t.Helper()
			_, docs := bstRender(t, bstRequired(), sets...)
			return str(volumeNamed(pod(t, docs), "tmp"), "emptyDir", "sizeLimit")
		}
		if got := tmp(t, keySet); got != "2304Mi" {
			t.Errorf("tmp sizeLimit with the key = %q, want 2304Mi (2GiB spool + 256Mi)", got)
		}
		if got := tmp(t); got != "1Gi" {
			t.Errorf("tmp sizeLimit without the key = %q, want 1Gi", got)
		}
		bstFails(t, bstRequired(), []string{keySet, "tmp.sizeLimit=1Gi"}, "tmp.sizeLimit", "secrets.cacheKey", "evicted")
	})
	t.Run("config.bastion.cache.encryption_key_file is managed by the chart", func(t *testing.T) {
		bstFails(t, bstRequired(), []string{"config.bastion.cache.encryption_key_file=/x"}, "config.bastion.cache.encryption_key_file", "managed by the chart")
	})
}
