package v1

import (
	"testing"

	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// TestR_REPO_070_ScannerTrivyIsInUseWhileTheImageScanningReadsThroughIt: the item of scanner-trivy answers inUse from the injected
// function of the replica that answers, false without one; a preset repository answers its state and no inUse (R-REPO-073, R-UI-187).
// The image scanning's package supplies the function (vulnerability-scanning.md R-VULN-044); until then the API reads "not in use".
func TestR_REPO_070_ScannerTrivyIsInUseWhileTheImageScanningReadsThroughIt(t *testing.T) {
	system := repository.UpstreamItem{Key: wellknown.ScannerTrivyKey, Kind: wellknown.KindSystem, Fixed: []string{"state", "visibility"}}
	preset := repository.UpstreamItem{Key: "npmjs", Kind: wellknown.KindPreset, State: wellknown.StateNotSwitchedOn}
	for _, tc := range []struct {
		name  string
		inUse ScannerInUse
		want  bool
	}{
		{"without the scanning's function", nil, false},
		{"while the scanning reads through it", func() bool { return true }, true},
		{"while it does not", func() bool { return false }, false},
	} {
		t.Run("R-REPO-070 "+tc.name, func(t *testing.T) {
			s := &Server{o: Options{Upstream: UpstreamServices{ScannerTrivyInUse: tc.inUse}}}
			got := s.toUpstream(system)
			if got.InUse == nil || *got.InUse != tc.want || got.State != nil || len(got.Fixed) != 2 {
				t.Fatalf("inUse %v, state %v, fixed %v", got.InUse, got.State, got.Fixed)
			}
			p := s.toUpstream(preset)
			if p.InUse != nil || p.State == nil || string(*p.State) != "not-switched-on" || p.Name != "upstream/npmjs" {
				t.Fatalf("preset %+v", p)
			}
		})
	}
}

// TestR_MAL_011_ScannerClamavIsInUseWhileTheMalwareScanKeepsItsDatabaseThere: the item of scanner-clamav answers inUse from the malware
// scanning's function, never from the image scanning's, and names its address and account as fixed (R-REPO-075, Q-REPO-412).
func TestR_MAL_011_ScannerClamavIsInUseWhileTheMalwareScanKeepsItsDatabaseThere(t *testing.T) {
	clam := repository.UpstreamItem{Key: wellknown.ScannerClamavKey, Kind: wellknown.KindSystem, Fixed: []string{"state", "visibility", "address", "account"}}
	yes, no := func() bool { return true }, func() bool { return false }
	for _, tc := range []struct {
		name string
		u    UpstreamServices
		want bool
	}{
		{"without the scanning's function", UpstreamServices{ScannerTrivyInUse: yes}, false},
		{"while the malware scan keeps its database there", UpstreamServices{ScannerTrivyInUse: no, ScannerClamavInUse: yes}, true},
		{"while it does not", UpstreamServices{ScannerTrivyInUse: yes, ScannerClamavInUse: no}, false},
	} {
		t.Run("R-REPO-075 "+tc.name, func(t *testing.T) {
			got := (&Server{o: Options{Upstream: tc.u}}).toUpstream(clam)
			if got.InUse == nil || *got.InUse != tc.want || got.State != nil || len(got.Fixed) != 4 || got.Fixed[2] != "address" {
				t.Fatalf("inUse %v, state %v, fixed %v", got.InUse, got.State, got.Fixed)
			}
		})
	}
}
