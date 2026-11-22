package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/meta"
)

type eventsRecorder struct{ events []audit.Event }

func (r *eventsRecorder) RecordTx(context.Context, meta.Tx, audit.Event) error { return nil }
func (r *eventsRecorder) Record(_ context.Context, ev audit.Event)             { r.events = append(r.events, ev) }

// TestR_STOR_027_ADetectionWritesTheAuditEventOfTheBlob: the store's damage hook writes system.storage.blob_damaged with the
// digest and the state, as a warning of the system, an event of a registered type.
func TestR_STOR_027_ADetectionWritesTheAuditEventOfTheBlob(t *testing.T) {
	if blobDamaged(nil) != nil {
		t.Fatal("a hook without a recorder")
	}
	var rec eventsRecorder
	d := blob.Digest{0xab}
	blobDamaged(&rec)(context.Background(), d, blob.StatusMissing)
	if len(rec.events) != 1 {
		t.Fatalf("events = %+v", rec.events)
	}
	ev := rec.events[0]
	var details map[string]string
	if err := json.Unmarshal(ev.Details, &details); err != nil {
		t.Fatal(err)
	}
	if ev.Type != audit.EventStorageBlobDamaged || ev.Outcome != audit.Warning || details["sha256"] != d.Hex() || details["status"] != "missing" {
		t.Errorf("event = %+v, details %v", ev, details)
	}
	if et, ok := audit.Lookup(audit.EventStorageBlobDamaged); !ok || et.Mode != audit.Async {
		t.Errorf("the event type is not registered as asynchronous: %+v %v", et, ok)
	}
}
