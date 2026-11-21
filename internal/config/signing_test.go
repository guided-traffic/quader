package config

import (
	"strings"
	"testing"
	"time"
)

// TestR_SIG_058_TheSigningKeysHaveTheirDefaults: every key of the signing section with its default, the queue signing with one slot,
// claimed by a process that names no queue, and no files named.
func TestR_SIG_058_TheSigningKeysHaveTheirDefaults(t *testing.T) {
	l, err := loadSignatures(t, "")
	if err != nil {
		t.Fatal(err)
	}
	s := l.Config.Signing
	if !s.Enabled || s.Concurrency != 4 || s.MaxAttempts != 5 {
		t.Errorf("signing %+v", s)
	}
	id := s.Identity
	if id.Files() || id.KeyFile != "" || id.CAFile != "" || id.ReloadInterval != time.Minute || id.AnnouncePeriod != 10*time.Minute {
		t.Errorf("identity %+v", id)
	}
	if l.Config.Jobs.Workers.Signing != 1 || !l.Config.Jobs.Claims("signing") {
		t.Errorf("jobs %+v", l.Config.Jobs.Workers)
	}
}

// TestR_SIG_058_EachRefusalOfTheSigningSectionNamesTheKey: the pair of files both or neither, the root only with them, clean absolute
// paths, and the bounds of R-SIG-058 (reload_interval 1 s to 1 h by Q-SIG-151, announce_period 0 s to 24 h, concurrency 1 to 64,
// max_attempts 1 to 20); the boundaries themselves are accepted.
func TestR_SIG_058_EachRefusalOfTheSigningSectionNamesTheKey(t *testing.T) {
	for file, key := range map[string]string{
		"signing:\n  identity:\n    cert_file: /s/tls.crt\n":                                           "signing.identity.cert_file",
		"signing:\n  identity:\n    key_file: /s/tls.key\n":                                            "signing.identity.cert_file",
		"signing:\n  identity:\n    ca_file: /s/ca.crt\n":                                              "signing.identity.ca_file",
		"signing:\n  identity:\n    cert_file: s/tls.crt\n    key_file: /s/tls.key\n":                  "signing.identity.cert_file",
		"signing:\n  identity:\n    cert_file: /s/tls.crt\n    key_file: /s/../tls.key\n":              "signing.identity.key_file",
		"signing:\n  identity:\n    reload_interval: 500ms\n":                                          "signing.identity.reload_interval",
		"signing:\n  identity:\n    reload_interval: 2h\n":                                             "signing.identity.reload_interval",
		"signing:\n  identity:\n    announce_period: 25h\n":                                            "signing.identity.announce_period",
		"signing:\n  concurrency: 0\n":                                                                 "signing.concurrency",
		"signing:\n  concurrency: 65\n":                                                                "signing.concurrency",
		"signing:\n  max_attempts: 21\n":                                                               "signing.max_attempts",
		"signing:\n  max_attempts: 0\n":                                                                "signing.max_attempts",
		"jobs:\n  queues: [signin]\n":                                                                  "jobs.queues",
		"signing:\n  identity:\n    cert_file: /s/tls.crt\n    key_file: /s/tls.key\n    ca_file: c\n": "signing.identity.ca_file",
	} {
		_, err := loadSignatures(t, file)
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%q: %v, want %s named", file, err, key)
		}
	}
	ok := "signing:\n  identity:\n    cert_file: /s/tls.crt\n    key_file: /s/tls.key\n    ca_file: /s/ca.crt\n" +
		"    reload_interval: 1s\n    announce_period: 0s\n  concurrency: 64\n  max_attempts: 20\njobs:\n  queues: [signing]\n"
	l, err := loadSignatures(t, ok)
	if err != nil {
		t.Fatalf("the boundaries: %v", err)
	}
	if id := l.Config.Signing.Identity; !id.Files() || id.ReloadInterval != time.Second || id.AnnouncePeriod != 0 {
		t.Errorf("%+v", id)
	}
	if _, err := loadSignatures(t, "signing:\n  identity:\n    reload_interval: 1h\n    announce_period: 24h\n"); err != nil {
		t.Errorf("the upper boundaries: %v", err)
	}
}
