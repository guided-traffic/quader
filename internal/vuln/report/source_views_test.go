package report

import (
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/vuln/store"
)

// TestR_VULN_021_OneViewOfTheSourceState (A6, Q-VULN-70): the sources operation and the system information share this
// function, so a row of an ecosystem the configuration no longer names has the mode off in both, an unconfigured source
// shows state empty, and a zero stale_after never marks a source stale.
func TestR_VULN_021_OneViewOfTheSourceState(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	old := now.Add(-72 * time.Hour)
	rows := []store.SourceRow{
		{Source: "osv", Ecosystem: "npm", State: "complete", SyncedAt: &old},
		{Source: "osv", Ecosystem: "PyPI", State: "complete", SyncedAt: &old},
	}
	cfg := SourceConfig{Enabled: true, OSVMode: "online", KEVMode: "repository", Ecosystems: []string{"npm", "NuGet"}, StaleAfter: 24 * time.Hour}
	by := map[string]SourceView{}
	var order []string
	for _, v := range SourceViews(cfg, rows, now) {
		by[v.Source+"/"+v.Ecosystem] = v
		order = append(order, v.Source+"/"+v.Ecosystem)
	}
	if want := []string{"kev/", "osv/NuGet", "osv/PyPI", "osv/npm"}; len(order) != len(want) || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] || order[3] != want[3] {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if v := by["osv/PyPI"]; v.Mode != "off" || !v.Stale {
		t.Errorf("a stored row of an ecosystem the configuration does not name: %+v", v)
	}
	if v := by["osv/NuGet"]; v.Mode != "online" || v.State != "empty" || v.Stale {
		t.Errorf("a configured ecosystem without a row: %+v", v)
	}
	if v := by["kev/"]; v.Mode != "repository" {
		t.Errorf("kev: %+v", v)
	}
	cfg.StaleAfter = 0
	for _, v := range SourceViews(cfg, rows, now) {
		if v.Stale {
			t.Errorf("stale_after 0 marks %s/%s stale", v.Source, v.Ecosystem)
		}
	}
	cfg.Enabled = false
	for _, v := range SourceViews(cfg, rows, now) {
		if v.Mode != "off" {
			t.Errorf("scanning disabled: %s/%s has mode %s", v.Source, v.Ecosystem, v.Mode)
		}
	}
}

// R-VULN-045: the image scanning's database is a source of the view exactly while the image scanning is on, and then its mode is
// repository: it comes from a repository of the installation (R-VULN-044); a stored row of it with the image scanning off is off.
func TestR_VULN_045_TheDatabaseIsInModeRepositoryWhileTheImageScanningIsOn(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	cfg := SourceConfig{Enabled: true, OSVMode: "online", KEVMode: "online", Ecosystems: []string{"npm"}, StaleAfter: 24 * time.Hour, Images: true}
	find := func(views []SourceView) (SourceView, bool) {
		for _, v := range views {
			if v.Source == SourceTrivy && v.Ecosystem == "OCI" {
				return v, true
			}
		}
		return SourceView{}, false
	}
	if v, ok := find(SourceViews(cfg, nil, now)); !ok || v.Mode != "repository" || v.State != "empty" {
		t.Fatalf("the database without a row: %+v (%v)", v, ok)
	}
	cfg.Images = false
	if _, ok := find(SourceViews(cfg, nil, now)); ok {
		t.Fatal("the database is listed with the image scanning off and no row")
	}
	synced := now.Add(-time.Hour)
	if v, ok := find(SourceViews(cfg, []store.SourceRow{{Source: "trivy", Ecosystem: "OCI", State: "complete", SyncedAt: &synced}}, now)); !ok || v.Mode != "off" {
		t.Fatalf("a stored row with the image scanning off: %+v (%v)", v, ok)
	}
}
