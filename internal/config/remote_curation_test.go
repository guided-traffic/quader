package config

import (
	"strings"
	"testing"
	"time"
)

func loadCuration(t *testing.T, file string) (*Loaded, error) {
	t.Helper()
	return Load(Options{File: "/c.yaml", ReadFile: fakeFS(map[string]string{"/c.yaml": file}), Environ: minimalEnv(), Command: CommandMigrate})
}

// TestR_CUR_020_TheKeysHaveTheirDefaults: the installation's keys of the curation of a remote repository, the queue downloads and the
// two tasks carry the defaults of R-CUR-020.
func TestR_CUR_020_TheKeysHaveTheirDefaults(t *testing.T) {
	l, err := loadCuration(t, "")
	if err != nil {
		t.Fatal(err)
	}
	c := l.Config
	if d := c.Proxy.Delivery; d != (ProxyDelivery{
		MaxRules: 10000, OpenRequestsPerPerson: 20, ExamineVersionsPerRequest: 1, RequestRetention: 720 * time.Hour, MaxClientEntries: 10000,
	}) {
		t.Errorf("proxy.delivery %+v", d)
	}
	if k := c.Proxy.Catalogue; k != (ProxyCatalogue{
		RequestsPerMinute: 120, RequestsPerMinutePerPerson: 30, CacheBytes: 32 << 20, CacheTTL: 300 * time.Second,
		MaxDocumentBytes: 64 << 20, Timeout: 20 * time.Second,
	}) {
		t.Errorf("proxy.catalogue %+v", k)
	}
	if d := c.Proxy.Downloads; d != (ProxyDownloads{
		PerRepository: 4, MaxWaitingPerRepository: 10000, MaxVersionsPerBulk: 10000, MaxBytesPerBulk: 100 << 30, MaxAttempts: 5,
		DoneRetention: 168 * time.Hour, FailedRetention: 720 * time.Hour, ClientFailuresPerRepository: 1000,
	}) {
		t.Errorf("proxy.downloads %+v", d)
	}
	if n, ok := c.Jobs.Workers.Count("downloads"); !ok || n != 4 {
		t.Errorf("jobs.workers.downloads = %d, %v", n, ok)
	}
	for name, schedule := range map[string]string{"remote-downloads-dispatch": "* * * * *", "remote-examination-cleanup": "0 4 * * *"} {
		if task, ok := c.Maintenance.Tasks[name]; !ok || !task.Enabled || task.Schedule != schedule {
			t.Errorf("maintenance.tasks.%s = %+v, %v", name, task, ok)
		}
	}
}

// TestR_CUR_020_TheKeysAreBounded: a value outside a key's range is refused with the key named.
func TestR_CUR_020_TheKeysAreBounded(t *testing.T) {
	for file, key := range map[string]string{
		"proxy:\n  delivery:\n    max_rules: 99\n":             "proxy.delivery.max_rules",
		"proxy:\n  catalogue:\n    requests_per_minute: 0\n":   "proxy.catalogue.requests_per_minute",
		"proxy:\n  catalogue:\n    cache_ttl: 2h\n":            "proxy.catalogue.cache_ttl",
		"proxy:\n  catalogue:\n    timeout: 0s\n":              "proxy.catalogue.timeout",
		"proxy:\n  downloads:\n    per_repository: 0\n":        "proxy.downloads.per_repository",
		"proxy:\n  downloads:\n    max_attempts: 21\n":         "proxy.downloads.max_attempts",
		"proxy:\n  downloads:\n    max_bytes_per_bulk: 1KiB\n": "proxy.downloads.max_bytes_per_bulk",
		"jobs:\n  workers:\n    downloads: -1\n":               "jobs.workers.downloads",
	} {
		if _, err := loadCuration(t, file); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%s: %v", key, err)
		}
	}
}
