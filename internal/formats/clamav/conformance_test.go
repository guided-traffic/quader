package clamav

import (
	"context"
	"net/http"
	"testing"

	"github.com/guided-traffic/quader/internal/formats"
	"github.com/guided-traffic/quader/internal/formats/formattest"
)

// conformance is the fixture of the kit (R-CLAMAV-008): the two routes serve the version in use of the in-memory twin of the data
// port, the hooks are driven by the commits the database task makes, and the checks the format has nothing to run over are skipped
// with their reasons.
func conformance() formattest.Fixture {
	do := func(fn func(h *formattest.Host) error) func(context.Context, *formattest.Host) error {
		return func(_ context.Context, h *formattest.Host) error { return fn(h) }
	}
	return formattest.Fixture{
		Host:    func() formattest.HostOptions { return formattest.HostOptions{Data: newMem()} },
		Rejects: []string{"daily.cld", "safebrowsing.cvd", "daily-0.cvd.sign", "daily/1/daily.cvd", "../daily.cvd", "daily.cvd%00"},
		Paths: []formattest.PathCase{
			{Path: "daily/28190/daily.cvd", Want: formats.PathInfo{Kind: formats.Content, Package: "daily", Version: "28190"}},
			{Path: "main/63/main.cud", Want: formats.PathInfo{Kind: formats.Content, Package: "main", Version: "63"}},
			{Path: "daily/28190/daily-28190.cvd.sign", Want: formats.PathInfo{Kind: formats.Content, Package: "daily", Version: "28190"}},
			{Path: "daily/28190/main.cvd", Want: formats.PathInfo{Kind: formats.Content}},
			{Path: "daily/", Want: formats.PathInfo{Kind: formats.PathComputed, Package: "daily"}},
			{Path: "daily/28190/", Want: formats.PathInfo{Kind: formats.PathComputed, Package: "daily", Version: "28190"}},
			{Path: "", Want: formats.PathInfo{Kind: formats.PathComputed}},
			{Path: "daily.cvd", Want: formats.PathInfo{Kind: formats.PathMetadata}},
			{Path: "elsewhere.txt", Want: formats.PathInfo{Kind: formats.Content}},
		},
		// The routes address the version in use, which no path names: they serve a stored item of the layout and are no stored path.
		Canonical:   func(string) (string, bool) { return "", false },
		NoDocument:  "a database has no document of its own: the container route serves the version in use (R-CLAMAV-007)",
		Names:       []formattest.NameCase{{Spellings: []string{"daily"}}, {Spellings: []string{"main"}}, {Spellings: []string{"bytecode"}}},
		OptionNames: []string{"QUADER_TOKEN"},

		// The database beside every content case's own, for check 21 (R-BAS-086): bytecode in use with its signature, which no case reads.
		OtherPackage: &formattest.OtherPackage{Name: "bytecode", Seed: seedInUse("bytecode", true, 335)},

		Content: []formattest.ContentCase{
			{Name: "the container in use", Path: "daily.cvd", Seed: seedInUse("daily", true, 28189, 28190)},
			{Name: "a container without a set in use", Path: "main.cvd", Status: http.StatusNotFound},
			{Name: "the detached signature of a stored version", Path: "daily-28190.cvd.sign", Seed: seedInUse("daily", true, 28190)},
			{Name: "the detached signature of a version not stored", Path: "daily-1.cvd.sign", Status: http.StatusNotFound},
		},
		Changes: []formattest.ChangeCase{
			{Name: "a container and its signature (OnCommit)", Package: "daily", Do: do(func(h *formattest.Host) error {
				return commitVersion(context.Background(), h, "daily", 28190, true)
			})},
			{Name: "a newer container (OnCommit)", Package: "daily", Seed: seedInUse("daily", false, 28190), Do: do(func(h *formattest.Host) error {
				return commitVersion(context.Background(), h, "daily", 28191, false)
			})},
			{
				Name: "the cleanup removes a version (OnItemsRemoved)", Package: "daily", Seed: seedInUse("daily", true, 28189, 28190),
				Do: func(ctx context.Context, h *formattest.Host) error {
					rq, err := h.NewRequest(ctx, http.MethodGet, "daily.cvd", nil)
					if err != nil {
						return err
					}
					return h.Remove(ctx, rq, "delete", ContainerPath("daily", 28189, "daily.cvd"), SignaturePath("daily", 28189))
				},
			},
		},
		NoRevision: map[string]string{},
		View: &formattest.ViewFixture{
			Versions: []string{"9", "28189", "28190"}, Package: "daily",
			Seed: func(h *formattest.Host) error { return seedInUse("daily", false, 9, 28189, 28190)(h) },
		},
		Skip: map[formattest.Check]string{
			formattest.CheckRewrite:   "local class only: no remote adapter (R-CLAMAV-008)",
			formattest.CheckMerge:     "local class only: no virtual adapter (R-CLAMAV-008)",
			formattest.CheckDelivery:  "local class only: no remote class, so no delivery rules (R-CLAMAV-008, remote-curation.md R-CUR-023)",
			formattest.CheckInspect:   "no upload route: only the database task writes the repository, in process (R-CLAMAV-008)",
			formattest.CheckWiki:      "no wiki content: a definition file has no documentation (R-CLAMAV-008)",
			formattest.CheckAdmission: "no Download route: both routes are of the kind Metadata (Q-CLAMAV-2, R-CLAMAV-008)",
			formattest.CheckMalware:   "no Download route, and the gate never asks about an item of a system repository (R-MAL-013, R-CLAMAV-008)",
		},
	}
}

// TestR_ARCH_010_Conformance runs the conformance kit over the format (R-CLAMAV-008): it passes with exactly the skips of the fixture,
// each with its reason. Milliseconds, containers of hundreds of bytes.
func TestR_ARCH_010_Conformance(t *testing.T) {
	formattest.Run(t, Format{}, conformance())
}

// TestR_CLAMAV_008_TheKitSkipsExactlyWhatTheFormatLeavesOut: the seven skipped checks are the ones the format has nothing to run over, and
// every other check runs and passes.
func TestR_CLAMAV_008_TheKitSkipsExactlyWhatTheFormatLeavesOut(t *testing.T) {
	rep := formattest.Evaluate(Format{}, conformance())
	skipped := map[formattest.Check]bool{
		formattest.CheckRewrite: true, formattest.CheckMerge: true, formattest.CheckInspect: true, formattest.CheckWiki: true,
		formattest.CheckAdmission: true, formattest.CheckMalware: true, formattest.CheckDelivery: true,
	}
	for _, res := range rep {
		switch {
		case skipped[res.Check] && (res.Skipped == "" || len(res.Failures) > 0):
			t.Errorf("%v: %+v", res.Check, res)
		case !skipped[res.Check] && !res.Passed():
			t.Errorf("%v: %+v", res.Check, res)
		}
	}
}

// FuzzR_ARCH_028_Match: Match never resolves a path that is not clean and never panics.
func FuzzR_ARCH_028_Match(f *testing.F) { formattest.FuzzMatch(f, Format{}) }
