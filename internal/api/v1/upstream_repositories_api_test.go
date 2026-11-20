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
