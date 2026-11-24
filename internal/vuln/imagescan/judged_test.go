package imagescan

import (
	"slices"
	"testing"
	"time"

	"github.com/guided-traffic/quader/internal/vuln"
	"github.com/guided-traffic/quader/internal/vuln/store"
)

// R-VULN-034, Q-VULN-270: a terminal failure waits for no time. A code of reading (layer_format) names the release, a code of a bound
// (image_too_large) names the bounds, neither has a back-off; a failure that may pass (timeout) keeps its doubling back-off and names no
// judge. The run asks for the rows of the current judges. Unit, milliseconds.
func TestR_VULN_034_ATerminalFailureNamesWhatJudgedItAndWaitsForNoTime(t *testing.T) {
	judges := Judges{Limits: LimitsJudge(ContentLimits{MaxLayers: 256, MaxImageBytes: 3 << 30, MaxUncompressedBytes: 32 << 30, MaxEntries: 5_000_000}), Release: "0.123.0"}
	for _, tc := range []struct {
		err    *ScanError
		judged string
		wait   time.Duration
	}{
		{&ScanError{Code: CodeLayerFormat, Detail: "a layer is not a tar stream", Terminal: true}, "release:0.123.0", 0},
		{&ScanError{Code: CodeImageTooLarge, Detail: "too large", Terminal: true}, "limits:max_layers=256,max_image_size=3221225472,max_uncompressed_size=34359738368,max_entries=5000000", 0},
		{&ScanError{Code: CodeTimeout, Detail: "the analyser did not finish"}, "", 2 * retryBackoff},
	} {
		t.Run(tc.err.Code, func(t *testing.T) {
			r := newJobRig(t, "")
			r.svc.o.Judges = judges
			r.rows.due[0].Attempts = 1
			r.scanner.analyse = func() (Analysis, error) { return Analysis{}, tc.err }
			if err := r.svc.RunScan(t.Context(), &fakeRun{}); err != nil {
				t.Fatal(err)
			}
			if len(r.rows.failures) != 1 {
				t.Fatalf("failures %+v", r.rows.failures)
			}
			if f := r.rows.failures[0]; f.Code != tc.err.Code || f.Judged != tc.judged || f.Backoff != tc.wait {
				t.Fatalf("recorded %+v, want judged %q and a wait of %v", f, tc.judged, tc.wait)
			}
		})
	}
	want := store.Retry{MaxAttempts: 5, Judges: []string{judges.Limits, "release:0.123.0"}}
	if got := (&Service{o: Options{MaxAttempts: 5, Judges: judges}}).retry(); got.MaxAttempts != want.MaxAttempts || !slices.Equal(got.Judges, want.Judges) {
		t.Fatalf("retry %+v, want %+v", got, want)
	}
	// Without the bounds wired a terminal failure still names a judge: an empty one would read as a failure that may pass, with no wait.
	if got := (Judges{}).judgedUnder(CodeTooManyEntries); got != LimitsJudge(ContentLimits{}) {
		t.Fatalf("judged %q without bounds", got)
	}
}

// R-VULN-015, R-VULN-034: the gate of a pull reads the class of a terminal failure from what it was judged under, so every code a judge is
// written for reads back as its class: a code of a bound as over the limits, a code of reading as unreadable, a failure that may pass as
// tried again. Unit, microseconds.
func TestR_VULN_015_TheGateReadsTheClassOfAFailureFromItsJudge(t *testing.T) {
	judges := Judges{Limits: LimitsJudge(ContentLimits{MaxLayers: 256}), Release: "0.123.0"}
	for code, want := range map[string]vuln.MissingCause{
		CodeImageTooLarge: vuln.MissingImageOverLimits, CodeTooManyLayers: vuln.MissingImageOverLimits,
		CodeDecompressedSize: vuln.MissingImageOverLimits, CodeTooManyEntries: vuln.MissingImageOverLimits,
		CodeLayerFormat: vuln.MissingImageUnreadable, CodePathTraversal: vuln.MissingImageUnreadable, CodeDigestMismatch: vuln.MissingImageUnreadable,
		CodeManifestInvalid: vuln.MissingImageUnreadable, CodeReportTooLarge: vuln.MissingImageUnreadable,
	} {
		if got := vuln.ImageMissingCause(true, true, judges.judgedUnder(code)); got != want || !got.Terminal() {
			t.Errorf("%s: %s, want %s", code, got, want)
		}
	}
	if got := vuln.ImageMissingCause(true, true, ""); got != vuln.MissingImageScanRetried || got.Terminal() {
		t.Errorf("a failure that may pass: %s", got)
	}
}
