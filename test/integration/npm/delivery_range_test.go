//go:build integration

package npm

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/quader/internal/formats/npm/semver"
	"github.com/guided-traffic/quader/test/integration/testenv"
	"github.com/guided-traffic/quader/test/npmstub/corpus"
)

// TestR_CUR_026_ARangeOfAPackageDeliversItsVersionsOnly: in mode allExceptBlocked a package allowed from its second version below its
// newest lists the versions inside the range only, the newest version's tarball answers the rules' 403 with the range's reason and the
// stub records no request for it, and the check names the range that decided (remote-curation.md R-CUR-026, R-CUR-004). The range's
// decision itself is the unit table of internal/remotecuration/rules and the documents' filter the kit's check 23; this case adds the
// request path of a running replica and the stub's count. Data: one package of the corpus, a few kilobytes.
func TestR_CUR_026_ARangeOfAPackageDeliversItsVersionsOnly(t *testing.T) {
	r := newRemoteEnv(t, remoteOptions{})
	p := upstreamPackage(t, 3)
	vs := sortedVersions(t, p)
	lo, hi := vs[1], vs[len(vs)-1]
	r.st.DeliveryRule(rRemote, testenv.RangeRule(p.Name, lo, hi, false))
	rep := r.start(1)[0]

	t.Run("R-CUR-026 the package document lists the versions inside the range only", func(t *testing.T) {
		a := r.view(rep, rRemote, p.Name, r.tokA)
		if a.Status != http.StatusOK {
			t.Fatalf("%d %s", a.Status, a.Body)
		}
		got := versionsOf(t, a)
		slices.Sort(got)
		want := slices.Clone(vs[1 : len(vs)-1])
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("versions %v, want %v (from %s below %s)", got, want, lo, hi)
		}
	})
	t.Run("R-CUR-026 R-CUR-004 a version above the range is refused with the range's reason and the upstream is not asked", func(t *testing.T) {
		file := corpus.TarballFile(p.Name, hi)
		a := r.get(rep, rRemote, p.Name+"/-/"+file, r.tokA)
		refusal(t, a, rRemote, p.Name, hi)
		var b map[string]string
		_ = json.Unmarshal(a.Body, &b)
		if !strings.Contains(b["error"], "outside the versions a rule of the repository allows") {
			t.Errorf("the text %q, want the reason outsideRange", b["error"])
		}
		if n := r.stub.Counts().Tarball(p.Name, file); n != 0 {
			t.Errorf("the upstream was asked %d times for the tarball outside the range", n)
		}
	})
	t.Run("R-CUR-026 the check names the range that decided", func(t *testing.T) {
		a := r.st.Do(rep, http.MethodPost, testenv.ManagementURL(testenv.DefaultProject, rRemote)+"/delivery-rules/check", r.opsPAT,
			map[string]any{"name": p.Name, "version": vs[0]})
		if a.Status != http.StatusOK {
			t.Fatalf("%d %s", a.Status, a.Body)
		}
		var d struct {
			Delivered bool   `json:"delivered"`
			Reason    string `json:"reason"`
			DecidedBy struct {
				Kind  string `json:"kind"`
				Range *struct {
					FromVersion, ToVersion *string
					ToInclusive            bool
				} `json:"range"`
			} `json:"decidedBy"`
		}
		if err := json.Unmarshal(a.Body, &d); err != nil {
			t.Fatal(err)
		}
		rg := d.DecidedBy.Range
		if d.Delivered || d.Reason != "outsideRange" || d.DecidedBy.Kind != "package" || rg == nil || rg.FromVersion == nil || *rg.FromVersion != lo ||
			rg.ToVersion == nil || *rg.ToVersion != hi || rg.ToInclusive {
			t.Errorf("check of %s: %s", vs[0], a.Body)
		}
	})
}

// sortedVersions are the package's versions in npm's order, oldest first.
func sortedVersions(t testing.TB, p *corpus.Package) []string {
	t.Helper()
	out := make([]string, 0, len(p.Versions))
	for _, v := range p.Versions {
		out = append(out, v.Version)
	}
	slices.SortFunc(out, func(a, b string) int {
		x, errA := semver.Parse(a)
		y, errB := semver.Parse(b)
		if errA != nil || errB != nil {
			t.Fatalf("the corpus holds a version npm does not read: %q %q", a, b)
		}
		return semver.Compare(x, y)
	})
	return out
}
