package report

import (
	"testing"

	"github.com/guided-traffic/quader/internal/vuln"
	"github.com/guided-traffic/quader/internal/vuln/advisory"
	"github.com/guided-traffic/quader/internal/vuln/store"
)

// R-VULN-001, R-VULN-020: a version that is not checked names what it is missing whether or not its repository blocks it, so that the
// version page says why it is not checked: a package its advisory data, an image the state of its scan, nothing for a version that is
// checked, internal or not covered. Unit, microseconds.
func TestR_VULN_020_AVersionThatIsNotCheckedNamesWhatItIsMissing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		image bool
		e     vuln.Evaluation
		cause vuln.MissingCause
		want  vuln.MissingCause
	}{
		{"a package waiting for its data", false, vuln.EvaluationPendingData, "", vuln.MissingAdvisoryData},
		{"a checked package", false, vuln.EvaluationEvaluated, "", ""},
		{"an internal package", false, vuln.EvaluationInternal, "", ""},
		{"a package not covered", false, vuln.EvaluationNotCovered, "", ""},
		{"an image without a database", true, vuln.EvaluationPendingData, vuln.MissingImageDatabase, vuln.MissingImageDatabase},
		{"an image not scanned yet", true, vuln.EvaluationPendingScan, vuln.MissingImageNotScanned, vuln.MissingImageNotScanned},
		{"an image over the limits", true, vuln.EvaluationScanFailed, vuln.MissingImageOverLimits, vuln.MissingImageOverLimits},
		{"an image being scanned again", true, vuln.EvaluationPendingScan, "", ""},
		{"an image not covered", true, vuln.EvaluationNotCovered, vuln.MissingImageNotScanned, ""},
	} {
		if got := notChecked(tc.image, tc.e, tc.cause); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// R-VULN-021: the coverage counts a repository waiting for its data by what it waits for: a repository of images for the database for
// images, every other for its advisory data. Unit, microseconds.
func TestR_VULN_021_TheCoverageCountsARepositoryByWhatItWaitsFor(t *testing.T) {
	var c Coverage
	c.add(vuln.EvaluationPendingData, "npm")
	c.add(vuln.EvaluationPendingData, advisory.EcosystemOCI)
	c.add(vuln.EvaluationEvaluated, advisory.EcosystemOCI)
	c.add(vuln.EvaluationInternal, "npm")
	c.add(vuln.EvaluationNotCovered, "")
	if want := (Coverage{Evaluated: 1, PendingData: 1, PendingDatabase: 1, Internal: 1, NotCovered: 1}); c != want {
		t.Errorf("%+v, want %+v", c, want)
	}
}

// R-VULN-015, R-VULN-020: the version view holds an image as the gate of its pull does, while a platform image's detection has never
// finished, and names the cause an operator must act on first. Unit, microseconds.
func TestR_VULN_020_AnImageVersionNamesWhatItsPullIsMissing(t *testing.T) {
	scanned := ImageState{State: store.ImageScanScanned, Detected: true}
	rescanning := ImageState{State: store.ImageScanPending, Detected: true}
	pending := ImageState{State: store.ImageScanPending}
	retried := ImageState{State: store.ImageScanFailed, ErrorCode: "timeout"}
	unreadable := ImageState{State: store.ImageScanFailed, ErrorCode: "layer_format", Judged: vuln.JudgedReleasePrefix + "0.90.0"}
	for _, tc := range []struct {
		name     string
		images   []ImageState
		database bool
		want     vuln.MissingCause
	}{
		{"every platform detected", []ImageState{scanned, rescanning}, true, ""},
		{"no platform image stored yet", nil, true, vuln.MissingImageNotScanned},
		{"one not scanned yet", []ImageState{scanned, pending}, true, vuln.MissingImageNotScanned},
		{"a retry before a pending one", []ImageState{pending, retried}, true, vuln.MissingImageScanRetried},
		{"a terminal failure first", []ImageState{retried, unreadable, pending}, true, vuln.MissingImageUnreadable},
		{"no database", []ImageState{pending, unreadable}, false, vuln.MissingImageDatabase},
	} {
		if got := imageMissing(tc.images, tc.database); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
