package config

import (
	"strings"
	"testing"
	"time"
)

func loadStorage(t *testing.T, file, role string, cmd Command) error {
	t.Helper()
	env := minimalEnv("QUADER_STORAGE_CONNECTIONS=[{\"bucket\":\"blobs\"}]")
	if role != "" {
		env = append(env, "QUADER_PROCESS_ROLE="+role)
	}
	_, err := Load(Options{File: "/c.yaml", ReadFile: fakeFS(map[string]string{"/c.yaml": file}), Environ: env, Command: cmd})
	return err
}

func hasIssue(err error, key string) bool {
	return err != nil && strings.Contains(err.Error(), key)
}

func TestStorageSpoolRule(t *testing.T) {
	small := "storage:\n  spool:\n    max_bytes: 1GiB\n"
	t.Run("R-STOR-004 the maximum upload must fit the spool of web and all", func(t *testing.T) {
		for _, role := range []string{"web", "all"} {
			if err := loadStorage(t, small, role, CommandServe); !hasIssue(err, "storage.upload.max_size") {
				t.Errorf("role %s: want an issue naming storage.upload.max_size, got %v", role, err)
			}
		}
	})
	t.Run("R-HA-029 an api replica spools the UI's uploads only: a spool below upload.max_size passes when it holds the UI's limit", func(t *testing.T) {
		const ui = "ui:\n  deploy:\n    max_upload_bytes: 512MiB\n"
		if err := loadStorage(t, small+ui, "api", CommandServe); hasIssue(err, "storage.upload.max_size") || hasIssue(err, "ui.deploy.max_upload_bytes") {
			t.Fatalf("api with a UI limit below its spool: %v", err)
		}
	})
	t.Run("R-HA-029 an api replica refuses a UI limit above its spool, and 0 means upload.max_size", func(t *testing.T) {
		if err := loadStorage(t, small+"ui:\n  deploy:\n    max_upload_bytes: 2GiB\n", "api", CommandServe); !hasIssue(err, "ui.deploy.max_upload_bytes") {
			t.Errorf("a UI limit of 2GiB over a spool of 1GiB: want an issue naming ui.deploy.max_upload_bytes, got %v", err)
		}
		if err := loadStorage(t, small+"ui:\n  deploy:\n    max_upload_bytes: 0\n", "api", CommandServe); !hasIssue(err, "ui.deploy.max_upload_bytes") {
			t.Errorf("an unlimited UI over a spool below upload.max_size: want an issue naming ui.deploy.max_upload_bytes, got %v", err)
		}
	})
	t.Run("R-HA-029 a worker accepts no uploads and its spool need not hold the maximum", func(t *testing.T) {
		if err := loadStorage(t, small, "worker", CommandServe); hasIssue(err, "storage.upload.max_size") {
			t.Fatalf("worker: %v", err)
		}
	})
	t.Run("R-STOR-004 config validate without a role cannot know the role and checks no spool rule", func(t *testing.T) {
		if err := loadStorage(t, small, "", CommandValidate); hasIssue(err, "storage.upload.max_size") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("R-STOR-004 a spool that holds the maximum passes on every role", func(t *testing.T) {
		ok := "storage:\n  spool:\n    max_bytes: 12GiB\n"
		for _, role := range []string{"web", "worker", "all"} {
			if err := loadStorage(t, ok, role, CommandServe); hasIssue(err, "storage.upload.max_size") {
				t.Errorf("role %s: %v", role, err)
			}
		}
	})
}

func TestStorageHoldBackAndSettle(t *testing.T) {
	t.Run("R-STOR-013 a download hold-back below 1 KiB is refused", func(t *testing.T) {
		for _, v := range []string{"0", "1023B"} {
			if err := loadStorage(t, "storage:\n  download:\n    hold_back: "+v+"\n", "web", CommandServe); !hasIssue(err, "storage.download.hold_back") {
				t.Errorf("hold_back %s: %v", v, err)
			}
		}
		if err := loadStorage(t, "storage:\n  download:\n    hold_back: 1KiB\n", "web", CommandServe); hasIssue(err, "storage.download.hold_back") {
			t.Errorf("1KiB: %v", err)
		}
	})
	t.Run("R-STOR-024 delete_settle below one second is refused", func(t *testing.T) {
		if err := loadStorage(t, "storage:\n  gc:\n    delete_settle: 0s\n", "worker", CommandServe); !hasIssue(err, "storage.gc.delete_settle") {
			t.Errorf("0s: %v", err)
		}
	})
}

func TestStorageCoupling(t *testing.T) {
	check := func(lease, settle, handover time.Duration) []string {
		var c Config
		c.Jobs.LeaseDuration = lease
		c.Storage.GC.DeleteSettle, c.Storage.GC.HandoverDelay = settle, handover
		var issues []Issue
		c.validateStorageCoupling(&Report{issues: &issues})
		out := make([]string, len(issues))
		for i, is := range issues {
			out[i] = is.Message
		}
		return out
	}
	t.Run("R-STOR-024 the shipped defaults are accepted", func(t *testing.T) {
		if got := check(60*time.Second, 30*time.Second, 10*time.Minute); len(got) != 0 {
			t.Fatalf("issues: %v", got)
		}
	})
	t.Run("R-STOR-024 handover_delay not above lease_duration plus twice delete_settle is refused naming the keys", func(t *testing.T) {
		for _, handover := range []time.Duration{2 * time.Minute, 90 * time.Second} { // equal to and below 60 s + 2 x 30 s ... 120 s
			got := check(60*time.Second, 30*time.Second, handover)
			if len(got) != 1 || !strings.Contains(got[0], "storage.gc.handover_delay") || !strings.Contains(got[0], "jobs.lease_duration") {
				t.Errorf("handover %s: issues = %v", handover, got)
			}
		}
		if got := check(60*time.Second, 30*time.Second, 121*time.Second); len(got) != 0 {
			t.Errorf("one second above the bound: %v", got)
		}
	})
}
