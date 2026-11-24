package report

import (
	"testing"

	"github.com/guided-traffic/quader/internal/vuln"
	"github.com/guided-traffic/quader/internal/vuln/store"
)

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
