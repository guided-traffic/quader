package items

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/guided-traffic/quader/internal/audit"
	"github.com/guided-traffic/quader/internal/blob"
	"github.com/guided-traffic/quader/internal/meta"
	"github.com/guided-traffic/quader/internal/qname"
)

// recorded keeps the asynchronous events a test's store records.
type recorded struct{ events []audit.Event }

func (r *recorded) RecordTx(context.Context, meta.Tx, audit.Event) error { return nil }
func (r *recorded) Record(_ context.Context, ev audit.Event)             { r.events = append(r.events, ev) }

// R-OPS-054: a download is audited per item, and from 2026-11-26 on its event names the item's size and the unit the host named: the
// package, the version and the unit's key; an item of no package names none of the three.
func TestR_OPS_054_ADownloadEventNamesItsUnitAndSize(t *testing.T) {
	rec := &recorded{}
	s := &Store{rec: rec, download: true}
	var d blob.Digest
	d[0] = 0xab
	it := Item{RepoID: 1, Repository: qname.Repo{Project: "platform", Key: "images"}, Path: "acme/app/_blobs/sha256:ab", Size: 4821, Digest: d}
	s.Served(context.Background(), ServedEvent{Item: it, Unit: DownloadUnit{Package: "acme/app", Version: "1.4.0", Key: "acme/app"}})
	s.Served(context.Background(), ServedEvent{Item: it})
	if len(rec.events) != 2 {
		t.Fatalf("%d events, want one per item served", len(rec.events))
	}
	var named, none map[string]any
	_ = json.Unmarshal(rec.events[0].Details, &named)
	_ = json.Unmarshal(rec.events[1].Details, &none)
	if rec.events[0].Type != audit.EventArtifactDownloaded || named["path"] != it.Path || named["sha256"] != d.Hex() || named["size"] != float64(4821) ||
		named["package"] != "acme/app" || named["version"] != "1.4.0" || named["unit"] != "acme/app" {
		t.Errorf("the event of an item of a unit: %v", named)
	}
	if none["size"] != float64(4821) || none["package"] != nil || none["version"] != nil || none["unit"] != nil {
		t.Errorf("the event of an item of no package: %v", none)
	}
}
