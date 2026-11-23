package v1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/blob/summary"
)

// Tests of the connections' list and the storage summary over in-memory services (storage.md R-STOR-061, R-STOR-028); the statements
// behind summary.List and summary.Latest are the integration test's (test/integration/blob/summary_test.go).

func storageFixture() (summary.Summary, []summary.View) {
	at := time.Date(2026, 11, 22, 10, 17, 0, 0, time.UTC)
	filled := time.Date(2026, 11, 20, 8, 2, 11, 0, time.UTC)
	queued := at.Add(-4 * time.Minute)
	sum := summary.Summary{
		ComputedAt: at, DurationMs: 412, Blobs: summary.Blobs{Count: 4, Bytes: 4096},
		Connections: []summary.Connection{
			{ID: "default", PresentCopies: 4, PresentBytes: 4096, FillRatio: 1, VerifiedWithinPeriod: 4},
			{
				ID: "secondary", PresentCopies: 2, PresentBytes: 2048, BacklogBlobs: 2, BacklogBytes: 2048, FillRatio: 0.5,
				KeyCopies: map[string]int64{"sk-secondary-1-0a1b2c3d": 2},
			},
		},
		Repositories: []summary.Repository{{Project: "web", Key: "npm-local", Class: "local", Format: "npm", Bytes: 4096, ShareOfLogicalBytes: 1}},
	}
	settings := summary.Settings{PayloadChecksum: "sha256", RequestChecksumCalculation: "when_supported", ResponseChecksumValidation: "when_supported", BatchDelete: true, SSEMode: "none", ScrubBytesPerSecond: 50 << 20}
	views := summary.Views([]summary.Row{
		{
			ID: "default", Bucket: "quader-blobs", Prefix: "quader", Configured: 3, LiveReplicas: 3, Undrained: true, FilledAt: &filled, HoldsCopies: true,
			Capabilities: &blob.Capabilities{PayloadChecksumEnforced: true, BatchDelete: true, Metadata: true, ServerSideCopyFrom: map[string]bool{"secondary": true}},
		},
		{
			ID: "secondary", Bucket: "quader-copy", Prefix: "quader", Configured: 3, LiveReplicas: 3, CircuitOpen: 1, Undrained: true, Queued: 12,
			OldestQueuedAt: &queued, HoldsCopies: true,
			Keys: []summary.StorageKey{{ID: "sk-secondary-1-0a1b2c3d", Generation: 1, State: "active", CreatedAt: filled}},
		},
	}, map[string]summary.Local{
		"default":   {Position: 1, Endpoint: "https://object.storage.example", CredentialSource: "static", ScrubPeriod: 720 * time.Hour, Settings: settings},
		"secondary": {Position: 2, Endpoint: "https://s3.eu-central-1.amazonaws.com", CredentialSource: "webIdentity", ScrubPeriod: 720 * time.Hour, Settings: settings},
	}, &sum)
	return sum, views
}

func TestR_STOR_061_TheConnectionsInTheAPI(t *testing.T) {
	val := newValidator(t)
	sum, views := storageFixture()
	calls := 0
	services := StorageServices{
		Connections: func(context.Context) ([]summary.View, error) { calls++; return views, nil },
		Summary:     func(context.Context) (summary.Summary, error) { return sum, nil },
	}
	s := newSetup(t, func(o *Options) { o.API.Config = apiConfigForTest(); o.Storage = services }, authed)
	h := val.Handler(t, s.handler)

	t.Run("R-STOR-061 two connections, one filling with half the blobs, are listed in order with their states and the summary's figures", func(t *testing.T) {
		w := httpRecorder(h, http.MethodGet, "/api/v1/system/storage-connections")
		if w.Code != http.StatusOK {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		var out gen.StorageConnectionList
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Connections) != 2 || out.Connections[0].Id != "default" || out.Connections[1].Id != "secondary" {
			t.Fatalf("connections = %s", w.Body.String())
		}
		d, sec := out.Connections[0], out.Connections[1]
		if d.State != summary.StateActive || *d.Position != 1 || d.Summary == nil || d.Summary.FillRatio != 1 || d.Capabilities == nil ||
			!d.Capabilities.ServerSideCopyFrom["secondary"] || *d.ScrubPeriodSeconds != 2592000 || d.Settings == nil || d.Settings.PayloadChecksum != "sha256" {
			t.Errorf("default = %+v", d)
		}
		if sec.State != summary.StateFilling || sec.Summary.FillRatio != 0.5 || sec.Summary.BacklogBlobs != 2 || sec.Queue.Blobs != 12 || sec.Queue.OldestQueuedAt == nil ||
			sec.Replicas.CircuitOpen != 1 || sec.Replicas.Configured != 3 || sec.Capabilities != nil || len(sec.StorageKeys) != 1 || *sec.StorageKeys[0].Copies != 2 ||
			!sec.Summary.ComputedAt.Equal(sum.ComputedAt) {
			t.Errorf("secondary = %+v", sec)
		}
	})

	t.Run("R-STOR-061 no field holds a file path or a secret", func(t *testing.T) {
		body := httpRecorder(h, http.MethodGet, "/api/v1/system/storage-connections").Body.String()
		for _, bad := range []string{"_file", "/var/run/secrets", "secret", "accessKey", "sealed", "/etc/quader"} {
			if strings.Contains(strings.ToLower(body), strings.ToLower(bad)) {
				t.Errorf("the list holds %q: %s", bad, body)
			}
		}
	})

	t.Run("R-STOR-061 one connection by id, 404 for an id never seen and 400 for one that is no id", func(t *testing.T) {
		w := httpRecorder(h, http.MethodGet, "/api/v1/system/storage-connections/secondary")
		var one gen.StorageConnection
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &one) != nil || one.Id != "secondary" {
			t.Errorf("%d %s", w.Code, w.Body.String())
		}
		if w := httpRecorder(h, http.MethodGet, "/api/v1/system/storage-connections/archive"); w.Code != http.StatusNotFound {
			t.Errorf("unknown: %d %s", w.Code, w.Body.String())
		}
		if w := httpRecorder(h, http.MethodGet, "/api/v1/system/storage-connections/Archive"); w.Code != http.StatusBadRequest {
			t.Errorf("malformed: %d %s", w.Code, w.Body.String())
		}
	})

	t.Run("R-STOR-061 a caller without settings:read gets 403 and the list is not read", func(t *testing.T) {
		denied := newSetup(t, func(o *Options) { o.API.Config = apiConfigForTest(); o.Access = denyAccess{}; o.Storage = services }, authed)
		before := calls
		for _, p := range []string{"/api/v1/system/storage-connections", "/api/v1/system/storage-connections/default", "/api/v1/storage/summary"} {
			if w := denied.get(p); w.Code != http.StatusForbidden {
				t.Errorf("%s: %d %s", p, w.Code, w.Body.String())
			}
		}
		if calls != before {
			t.Errorf("the list was read for a refused caller")
		}
		for _, op := range []string{"listStorageConnections", "getStorageConnection", "getStorageSummary"} {
			if r := requirementOf(t, op); r.Permission != "settings:read" {
				t.Errorf("%s: %+v", op, r)
			}
		}
	})

	t.Run("R-STOR-028 the summary answers the latest snapshot, 404 before the first, and 503 without the services", func(t *testing.T) {
		w := httpRecorder(h, http.MethodGet, "/api/v1/storage/summary")
		var out gen.StorageSummary
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		if out.DurationMs != 412 || len(out.Connections) != 2 || out.Connections[1].FillRatio != 0.5 || out.Repositories[0].ShareOfLogicalBytes != 1 ||
			strings.Contains(w.Body.String(), "keyCopies") {
			t.Errorf("summary = %s", w.Body.String())
		}
		none := newSetup(t, func(o *Options) {
			o.API.Config = apiConfigForTest()
			o.Storage = StorageServices{Summary: func(context.Context) (summary.Summary, error) { return summary.Summary{}, summary.ErrNone }}
		}, authed)
		if w := none.get("/api/v1/storage/summary"); w.Code != http.StatusNotFound {
			t.Errorf("before the first: %d %s", w.Code, w.Body.String())
		}
		bare := newSetup(t, func(o *Options) { o.API.Config = apiConfigForTest() }, authed)
		for _, p := range []string{"/api/v1/storage/summary", "/api/v1/system/storage-connections", "/api/v1/system/storage-connections/default"} {
			if w := bare.get(p); w.Code != http.StatusServiceUnavailable {
				t.Errorf("%s: %d", p, w.Code)
			}
		}
		failing := newSetup(t, func(o *Options) {
			o.API.Config = apiConfigForTest()
			o.Storage = StorageServices{Connections: func(context.Context) ([]summary.View, error) { return nil, errors.New("down") }}
		}, authed)
		if w := failing.get("/api/v1/system/storage-connections"); w.Code != http.StatusInternalServerError {
			t.Errorf("failing read: %d", w.Code)
		}
	})
}
