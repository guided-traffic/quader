package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/api/v1/apitest"
	"github.com/guided-traffic/quader/internal/api/v1/gen"
)

type fakeScanners struct{ list gen.ScannerList }

func (f fakeScanners) Scanners(context.Context) (gen.ScannerList, error) { return f.list, nil }

// R-OPS-048, Q-OPS-223: GET /api/v1/system/scanners answers the kinds and the replicas in the document's shape, a ClamAV replica with
// its database and a replica that is not ready with its reason, and 503 on a replica that has no lister. settings:read is the
// permission of the document, the same as the replica registry's.
func TestR_OPS_048_TheScannerReplicasMatchTheDocument(t *testing.T) {
	at := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	tool, reason, node, version := "1.5.5", "database", "pool-a-7x2k", "main-62.daily-27841.bytecode-339"
	ready := gen.ScannerReplica{
		Kind: gen.ScannerReplicaKindClamav, InstanceUid: "8a0b5f8e-3c1d-4f2a-9b7e-1c2d3e4f5a6b", Name: "quader-malware-scanner-clamav-6d9c7b5f4-x2v4q",
		Node: &node, Version: "0.42.0", Revision: "abc", State: gen.Ready, StateSince: at, StartedAt: at, HeartbeatAt: at, ToolVersion: &tool,
		Database: &gen.ScannerDatabase{Version: version, BuiltAt: &at},
	}
	ready.Slots.InUse, ready.Slots.Total = 1, 2
	waiting := ready
	waiting.InstanceUid, waiting.State, waiting.Reason, waiting.Database, waiting.Node = "9b1c6f9f-4d2e-4a3b-8c8f-2d3e4f5a6b7c", gen.Starting, &reason, nil, nil
	list := gen.ScannerList{
		Kinds: []gen.ScannerKind{{Kind: gen.ScannerKindKindClamav, Deployment: "malware-scanner-clamav", Purpose: gen.Malware, Tool: "ClamAV", Replicas: gen.ScannerKindReplicasReporting}},
		Items: []gen.ScannerReplica{ready, waiting},
	}
	s := newSetup(t, func(o *Options) { o.Scanners = fakeScanners{list: list} }, authed)
	h := apitest.New(t, EmbeddedYAML()).Handler(t, s.handler)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/system/scanners", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var got gen.ScannerList
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || len(got.Items) != 2 || got.Items[1].Reason == nil || *got.Items[1].Reason != "database" {
		t.Fatalf("%v %s", err, w.Body.String())
	}
	if r := requirementOf(t, "listScanners"); r.Permission != "settings:read" {
		t.Errorf("the permission: %+v", r)
	}

	none := newSetup(t, nil, authed)
	if w := none.get("/api/v1/system/scanners"); w.Code != http.StatusServiceUnavailable {
		t.Errorf("without a lister: %d %s", w.Code, w.Body.String())
	}
}
