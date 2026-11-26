package v1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/api/v1/gen"
	"github.com/guided-traffic/quader/internal/blob/summary"
)

// Tests of the storage overview's operation over an in-memory input (storage.md R-STOR-107): its shape against the document, the
// permission and the absence of anything secret. Every state of every tile is summary's unit test (TestR_STOR_107_TheStatesOfTheOverview);
// the reads behind the input are the integration test's.

func TestR_STOR_107_TheOverview(t *testing.T) {
	val := newValidator(t)
	sum, views := storageFixture()
	calls := 0
	services := StorageServices{
		Overview: func(context.Context) (summary.OverviewInput, error) {
			calls++
			return summary.OverviewInput{Views: views, Latest: &sum, Stored: summary.Blobs{Count: 5, Bytes: 5120}, MinCopies: 1, DownloadVerify: true}, nil
		},
	}
	s := newSetup(t, func(o *Options) { o.API.Config = apiConfigForTest(); o.Storage = services }, authed)
	h := val.Handler(t, s.handler)

	t.Run("R-STOR-107 the overview answers the states and figures of now, stored from the folded totals, in the document's shape", func(t *testing.T) {
		w := httpRecorder(h, http.MethodGet, "/api/v1/storage/overview")
		if w.Code != http.StatusOK {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		var out gen.StorageOverview
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out.Stored.Blobs != 5 || out.Stored.Bytes != 5120 || out.FiguresAt == nil || !out.FiguresAt.Equal(sum.ComputedAt) {
			t.Errorf("stored and figuresAt = %+v %v", out.Stored, out.FiguresAt)
		}
		if out.Connections.State != gen.StorageOverviewConnectionsStateDegraded || out.Connections.Configured != 2 || out.Connections.Reachable != 1 ||
			len(out.Connections.Unreachable) != 1 || out.Connections.Unreachable[0].Id != "secondary" {
			t.Errorf("connections = %+v", out.Connections)
		}
		if out.Availability.State != gen.AtRisk || out.Availability.Target != 2 || out.Availability.MinCopies != 1 || len(out.Availability.Loss) != 1 ||
			out.Availability.Loss[0].Connection != "default" || out.Availability.Loss[0].Bytes != 2048 {
			t.Errorf("availability = %+v", out.Availability)
		}
		if out.Encryption.State != gen.StorageOverviewEncryptionStatePartly || len(out.Encryption.Encrypted) != 1 || len(out.Encryption.ProviderConfigured) != 1 ||
			out.Encryption.ProviderConfigured[0] != "secondary" {
			t.Errorf("encryption = %+v", out.Encryption)
		}
		if out.Integrity.State != gen.Checking || !out.Integrity.DownloadVerify || len(out.Integrity.AuthenticatedReads) != 1 ||
			out.Integrity.Scrub.LastRunAt != nil || out.Integrity.Scrub.Running {
			t.Errorf("integrity = %+v", out.Integrity)
		}
	})

	t.Run("R-STOR-107 no field holds an address, a path or a secret", func(t *testing.T) {
		body := strings.ToLower(httpRecorder(h, http.MethodGet, "/api/v1/storage/overview").Body.String())
		for _, bad := range []string{"https://", "_file", "/var/run/secrets", "secret", "accesskey", "sealed", "quader-blobs", "arn:aws"} {
			if strings.Contains(body, bad) {
				t.Errorf("the overview holds %q: %s", bad, body)
			}
		}
	})

	t.Run("R-STOR-107 a caller without settings:read gets 403 and nothing is read; 503 without the service, 500 on a failing read", func(t *testing.T) {
		denied := newSetup(t, func(o *Options) { o.API.Config = apiConfigForTest(); o.Access = denyAccess{}; o.Storage = services }, authed)
		before := calls
		if w := denied.get("/api/v1/storage/overview"); w.Code != http.StatusForbidden || calls != before {
			t.Errorf("%d %s, reads %d", w.Code, w.Body.String(), calls-before)
		}
		if r := requirementOf(t, "getStorageOverview"); r.Permission != "settings:read" {
			t.Errorf("getStorageOverview: %+v", r)
		}
		bare := newSetup(t, func(o *Options) { o.API.Config = apiConfigForTest() }, authed)
		if w := bare.get("/api/v1/storage/overview"); w.Code != http.StatusServiceUnavailable {
			t.Errorf("without the service: %d", w.Code)
		}
		failing := newSetup(t, func(o *Options) {
			o.API.Config = apiConfigForTest()
			o.Storage = StorageServices{Overview: func(context.Context) (summary.OverviewInput, error) {
				return summary.OverviewInput{}, errors.New("down")
			}}
		}, authed)
		if w := failing.get("/api/v1/storage/overview"); w.Code != http.StatusInternalServerError {
			t.Errorf("failing read: %d", w.Code)
		}
	})
}
