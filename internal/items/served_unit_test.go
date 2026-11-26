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

type touched struct{ repos []string }

func (a *touched) Touch(repoID string) { a.repos = append(a.repos, repoID) }

type forHeadKey struct{}

// R-BAS-059, Q-BAS-479: the GET a Bastion sends for a caller's HEAD serves the content but is no download: the store records it as
// it records a HEAD, with no statistics, no activity and no event, while a GET of the same item beside it is recorded whole.
func TestR_BAS_059_TheGetOfAHeadRecordsNoDownload(t *testing.T) {
	rec, act := &recorded{}, &touched{}
	s := &Store{
		rec: rec, download: true, activity: act, stats: NewStats(nil, 0, nil),
		noDl: func(ctx context.Context) bool { return ctx.Value(forHeadKey{}) != nil },
	}
	it := Item{ID: 7, RepoID: 1, Repository: qname.Repo{Project: "web", Key: "docker-local"}, Path: "app/_manifests/1", Size: 512}
	s.Served(context.WithValue(context.Background(), forHeadKey{}, true), ServedEvent{Item: it, CountStats: true})
	if len(rec.events) != 0 || len(act.repos) != 0 || len(s.stats.pending) != 0 {
		t.Fatalf("the GET of a HEAD recorded %d events, %d activities, %d statistics", len(rec.events), len(act.repos), len(s.stats.pending))
	}
	s.Served(context.Background(), ServedEvent{Item: it, CountStats: true})
	if len(rec.events) != 1 || len(act.repos) != 1 || s.stats.pending[7] == nil || s.stats.pending[7].n != 1 {
		t.Fatalf("a GET recorded %d events, %d activities, statistics %+v", len(rec.events), len(act.repos), s.stats.pending[7])
	}
}

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
