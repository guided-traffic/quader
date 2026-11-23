package summary

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/guided-traffic/quader/internal/meta/postgres/q"
)

// Unit tests of the storage summary's figures (storage.md R-STOR-028), the states of a connection (R-STOR-051), the order and the join
// of the API's list (R-STOR-061) and the summary's gauges (R-OPS-008, R-STOR-060). The statements are the integration test's
// (test/integration/blob/summary_test.go).

func TestR_STOR_028_TheSummaryFigures(t *testing.T) {
	in := Input{
		Totals: q.StorageSummaryTotalsRow{Blobs: 4, BlobBytes: 3000, TrashItems: 2, TrashBytes: 70, PendingBlobs: 1, PendingBytes: 9, DamagedMissing: 1},
		Repositories: []q.StorageSummaryRepositoriesRow{
			{Project: "web", Key: "npm-local", Class: "local", Format: "npm", Files: 3, Folders: 1, LogicalBytes: 4000, Packages: 2, Versions: 3},
			{Project: "web", Key: "npm-all", Class: "virtual", Format: "npm"},
			{Project: "web", Key: "generic-local", Class: "local", Format: "generic", Files: 1, LogicalBytes: 2000},
			{Project: "web", Key: "deb-all", Class: "virtual", Format: "deb", Files: 2, Folders: 1, LogicalBytes: 0},
		},
		Connections: []q.StorageSummaryConnectionsRow{
			{ID: "default", PresentCopies: 4, PresentBytes: 3000, PresentBlobBytes: 3000, VerifiedWithinPeriod: 3, SoleCopies: 2},
			{ID: "secondary", PresentCopies: 2, PresentBytes: 1100, PresentBlobBytes: 1000, MissingCopies: 1, ScrubOverdue: 1},
		},
		KeyCopies: []q.StorageSummaryKeyCopiesRow{{ConnectionID: "secondary", StorageKeyID: "sk-secondary-1-0a1b2c3d", Copies: 2}},
	}
	s := Assemble(in)

	t.Run("R-STOR-028 the logical content sums the listed repositories and a virtual one without items is left out", func(t *testing.T) {
		if len(s.Repositories) != 3 || s.Repositories[1].Key != "generic-local" || s.Repositories[2].Key != "deb-all" {
			t.Fatalf("repositories = %+v", s.Repositories)
		}
		if s.Logical != (Logical{Files: 6, Folders: 2, Bytes: 6000}) {
			t.Errorf("logical = %+v", s.Logical)
		}
	})
	t.Run("R-STOR-028 the ratios are rounded to four decimals and the shares sum to 1", func(t *testing.T) {
		if s.DeduplicationRatio != 0.5 {
			t.Errorf("deduplicationRatio = %v", s.DeduplicationRatio)
		}
		if s.Repositories[0].ShareOfLogicalBytes != 0.6667 || s.Repositories[1].ShareOfLogicalBytes != 0.3333 || s.Repositories[2].ShareOfLogicalBytes != 0 {
			t.Errorf("shares = %+v", s.Repositories)
		}
		empty := Assemble(Input{})
		if empty.DeduplicationRatio != 0 || len(empty.Connections) != 0 || empty.Repositories == nil {
			t.Errorf("empty = %+v", empty)
		}
	})
	t.Run("R-STOR-061 a connection's backlog is the live blobs it lacks and its fill the share it holds", func(t *testing.T) {
		d, sec := s.Connections[0], s.Connections[1]
		if d.BacklogBlobs != 0 || d.BacklogBytes != 0 || d.FillRatio != 1 || d.SoleCopies != 2 || d.KeyCopies != nil {
			t.Errorf("default = %+v", d)
		}
		if sec.BacklogBlobs != 2 || sec.BacklogBytes != 2000 || sec.FillRatio != 0.5 || sec.PresentBytes != 1100 || sec.KeyCopies["sk-secondary-1-0a1b2c3d"] != 2 {
			t.Errorf("secondary = %+v", sec)
		}
		none := Assemble(Input{Connections: []q.StorageSummaryConnectionsRow{{ID: "default"}}})
		if none.Connections[0].FillRatio != 1 || none.Connections[0].BacklogBlobs != 0 {
			t.Errorf("no blob: %+v", none.Connections[0])
		}
	})
	t.Run("R-STOR-028 the snapshot names its members as the specification does and carries no quota", func(t *testing.T) {
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range []string{`"pendingCollection":{"blobs":1,"bytes":9}`, `"damaged":{"missing":1,"corrupt":0}`, `"shareOfLogicalBytes":0.6667`, `"soleCopies":2`} {
			if !strings.Contains(string(b), m) {
				t.Errorf("%s lacks %s", b, m)
			}
		}
		if strings.Contains(string(b), "quota") {
			t.Errorf("a quota member before R-STOR-029 is built: %s", b)
		}
	})
}

func TestR_STOR_051_TheStateOfAConnection(t *testing.T) {
	at := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name string
		row  Row
		want string
	}{
		{"configured without drain and never filled", Row{Configured: 3, Undrained: true}, StateFilling},
		{"filled", Row{Configured: 3, Undrained: true, FilledAt: &at}, StateActive},
		{"configured on part of the replicas with drain, on the others without (a rolling update)", Row{Configured: 3, Undrained: true, FilledAt: &at}, StateActive},
		{"every replica that configures it drains it", Row{Configured: 2, HoldsCopies: true, FilledAt: &at}, StateDraining},
		{"drained", Row{Configured: 2, DrainedAt: &at}, StateDrained},
		{"drained and removed from every configuration", Row{DrainedAt: &at}, StateDrained},
		{"holds copies and no live replica configures it", Row{HoldsCopies: true, FilledAt: &at}, StateDetached},
		{"retired", Row{RetiredAt: &at, DrainedAt: &at}, StateRetired},
	} {
		if got := c.row.State(); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestR_STOR_061_TheListJoinsTheRowsTheReplicaAndTheSummary(t *testing.T) {
	at := time.Date(2026, 11, 22, 10, 17, 0, 0, time.UTC)
	rows := []Row{
		{ID: "archive", Bucket: "old", HoldsCopies: true},
		{ID: "default", Bucket: "quader-blobs", Configured: 2, Undrained: true, FilledAt: &at},
		{ID: "secondary", Bucket: "quader-copy", Configured: 2, Undrained: true},
	}
	local := map[string]Local{"secondary": {Position: 1, CredentialSource: "static"}, "default": {Position: 2}}
	latest := &Summary{ComputedAt: at, Connections: []Connection{{ID: "default", PresentCopies: 4}, {ID: "secondary", PresentCopies: 2}}}
	views := Views(rows, local, latest)
	if len(views) != 3 || views[0].ID != "secondary" || views[1].ID != "default" || views[2].ID != "archive" {
		t.Fatalf("order = %v %v %v", views[0].ID, views[1].ID, views[2].ID)
	}
	if views[0].State != StateFilling || views[0].Local == nil || views[0].Local.CredentialSource != "static" || views[0].Figures.PresentCopies != 2 ||
		!views[0].ComputedAt.Equal(at) {
		t.Errorf("secondary = %+v", views[0])
	}
	if views[2].State != StateDetached || views[2].Local != nil || views[2].Figures != nil || !views[2].ComputedAt.IsZero() {
		t.Errorf("archive = %+v", views[2])
	}
	if v := Views(rows, nil, nil); v[0].ID != "archive" || v[0].Figures != nil {
		t.Errorf("without a replica's configuration and a summary the list is by id: %+v", v)
	}
}

func TestR_STOR_060_TheGaugesOfTheSummary(t *testing.T) {
	at := time.Date(2026, 11, 22, 10, 17, 0, 0, time.UTC)
	s := Summary{ComputedAt: at, Damaged: Damaged{Corrupt: 1}, Connections: []Connection{{ID: "secondary", PresentCopies: 2, MissingCopies: 1, BacklogBlobs: 2, BacklogBytes: 9, FillRatio: 0.5}}}
	got := map[string]float64{}
	for _, m := range NewGauges().Metrics(s, at.Add(90*time.Second)) {
		var d dto.Metric
		if err := m.Write(&d); err != nil {
			t.Fatal(err)
		}
		got[name(m)+labels(&d)] = d.GetGauge().GetValue()
	}
	want := map[string]float64{
		MetricSummaryAge: 90, MetricBlobsDamaged + "{missing}": 0, MetricBlobsDamaged + "{corrupt}": 1,
		MetricCopies + "{secondary,present}": 2, MetricCopies + "{secondary,missing}": 1, MetricCopies + "{secondary,corrupt}": 0,
		MetricBacklogBlobs + "{secondary}": 2, MetricBacklogBytes + "{secondary}": 9, MetricFillRatio + "{secondary}": 0.5,
	}
	if len(got) != len(want) {
		t.Errorf("series = %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v (all: %v)", k, got[k], v, got)
		}
	}
}

func name(m prometheus.Metric) string {
	d := m.Desc().String()
	i := strings.Index(d, `fqName: "`) + len(`fqName: "`)
	return d[i : i+strings.Index(d[i:], `"`)]
}

func labels(d *dto.Metric) string {
	if len(d.GetLabel()) == 0 {
		return ""
	}
	byName := map[string]string{}
	for _, l := range d.GetLabel() {
		byName[l.GetName()] = l.GetValue()
	}
	var parts []string
	for _, n := range []string{"connection", "status", "state"} {
		if v, ok := byName[n]; ok {
			parts = append(parts, v)
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}
