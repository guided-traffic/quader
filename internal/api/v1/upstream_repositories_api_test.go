package v1

import (
	"net/http/httptest"
	"testing"

	"github.com/guided-traffic/quader/internal/httpserver"
	"github.com/guided-traffic/quader/internal/repository"
	"github.com/guided-traffic/quader/internal/wellknown"
)

// TestR_REPO_070_ScannerTrivyIsInUseWhileTheImageScanningReadsThroughIt: the item of scanner-trivy answers inUse from the injected
// function of the replica that answers, false without one; a preset repository answers its state and no inUse (R-REPO-073, R-UI-187).
// The image scanning's package supplies the function (vulnerability-scanning.md R-VULN-044); until then the API reads "not in use".
func TestR_REPO_070_ScannerTrivyIsInUseWhileTheImageScanningReadsThroughIt(t *testing.T) {
	system := repository.UpstreamItem{Key: wellknown.ScannerTrivyKey, Kind: wellknown.KindSystem, Fixed: []string{"state", "visibility"}}
	preset := repository.UpstreamItem{Key: "npmjs", Kind: wellknown.KindPreset, State: wellknown.StateNotSwitchedOn}
	r := httptest.NewRequest("GET", "/api/v1/upstream-repositories", nil)
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
			got := s.toUpstream(r, system)
			if got.InUse == nil || *got.InUse != tc.want || got.State != nil || len(got.Fixed) != 2 || got.Clients != nil {
				t.Fatalf("inUse %v, state %v, fixed %v, clients %v", got.InUse, got.State, got.Fixed, got.Clients)
			}
			p := s.toUpstream(r, preset)
			if p.InUse != nil || p.State == nil || string(*p.State) != "not-switched-on" || p.Name != "upstream/npmjs" || p.Clients != nil {
				t.Fatalf("preset %+v", p)
			}
		})
	}
}

// TestR_MAL_011_ClamavIsInUseWhileTheMalwareScanKeepsItsDatabaseThere: the item of clamav answers inUse from the malware scanning's
// function, never from the image scanning's, and names its address and account as fixed (R-REPO-075, Q-REPO-412).
func TestR_MAL_011_ClamavIsInUseWhileTheMalwareScanKeepsItsDatabaseThere(t *testing.T) {
	clam := repository.UpstreamItem{Key: wellknown.ClamavKey, Format: "clamav", Kind: wellknown.KindSystem, Fixed: []string{"state", "visibility", "address", "account"}}
	yes, no := func() bool { return true }, func() bool { return false }
	r := httptest.NewRequest("GET", "/api/v1/upstream-repositories", nil)
	for _, tc := range []struct {
		name string
		u    UpstreamServices
		want bool
	}{
		{"without the scanning's function", UpstreamServices{ScannerTrivyInUse: yes}, false},
		{"while the malware scan keeps its database there", UpstreamServices{ScannerTrivyInUse: no, ClamavInUse: yes}, true},
		{"while it does not", UpstreamServices{ScannerTrivyInUse: yes, ClamavInUse: no}, false},
	} {
		t.Run("R-REPO-075 "+tc.name, func(t *testing.T) {
			got := (&Server{o: Options{Upstream: tc.u}}).toUpstream(r, clam)
			if got.InUse == nil || *got.InUse != tc.want || got.State != nil || len(got.Fixed) != 4 || got.Fixed[2] != "address" {
				t.Fatalf("inUse %v, state %v, fixed %v", got.InUse, got.State, got.Fixed)
			}
		})
	}
}

// TestQ_API_382_TheItemOfClamavCarriesItsClientsAndTheirAddress: the member clients of the item of clamav says whether its clients are
// on and names the address a ClamAV installation puts into PrivateMirror, the protocol base on the base URL of the request without a
// trailing slash (R-REPO-073, R-CLAMAV-006).
func TestQ_API_382_TheItemOfClamavCarriesItsClientsAndTheirAddress(t *testing.T) {
	clam := repository.UpstreamItem{
		Key: wellknown.ClamavKey, Format: "clamav", Kind: wellknown.KindSystem, Fixed: []string{"state", "address", "account"},
		Clients: &repository.UpstreamClients{Enabled: true},
	}
	r := httptest.NewRequest("GET", "/api/v1/upstream-repositories/clamav", nil)
	r = r.WithContext(httpserver.WithResolved(r.Context(), httpserver.RequestInfo{BaseURL: "https://quader.example.com"}))
	got := (&Server{}).toUpstream(r, clam)
	if got.Clients == nil || !got.Clients.Enabled || got.Clients.Address != "https://quader.example.com/api/clamav/upstream/clamav" {
		t.Fatalf("clients %+v", got.Clients)
	}
}
