package vuln

import (
	"strings"

	"github.com/guided-traffic/quader/internal/vuln/advisory"
)

// MissingCause says what the missing-data rule of R-VULN-015 is missing when it holds a version: the refusal's text, the audit event
// vulnerability.download_blocked and the version report name the block by it, so that an image that is not scanned yet is not told it
// lacks advisory data.
type MissingCause string

// The causes of a missing-data block.
const (
	// MissingAdvisoryData: a package; the advisory data of its ecosystem has not been imported completely.
	MissingAdvisoryData MissingCause = "advisory_data"
	// MissingImageDatabase: an image; no vulnerability database for images has been imported yet, so no scan can finish.
	MissingImageDatabase MissingCause = "image_database"
	// MissingImageNotScanned: an image whose scan has not finished yet: it waits for its turn or is under way.
	MissingImageNotScanned MissingCause = "image_not_scanned"
	// MissingImageScanRetried: an image whose scan failed for a reason that may pass (a timeout, a layer still on its way); it is tried
	// again with a back-off.
	MissingImageScanRetried MissingCause = "image_scan_retried"
	// MissingImageOverLimits: an image whose scan stopped at a bound of the scanning (too large, too many layers or files); it is not tried
	// again until an operator raises the bound.
	MissingImageOverLimits MissingCause = "image_over_limits"
	// MissingImageUnreadable: an image whose content the scan could not read (a layer format it does not know, an invalid manifest); it is
	// not tried again until a release reads it differently.
	MissingImageUnreadable MissingCause = "image_unreadable"
)

// The prefixes of what a terminal failure of an image scan was judged under (vuln_image_scan.judged, R-VULN-034, Q-VULN-270): a code of a
// bound names the bounds, a code of reading the release. The image scanning writes them and [ImageMissingCause] reads the class from them.
const (
	JudgedLimitsPrefix  = "limits:"
	JudgedReleasePrefix = "release:"
)

// Terminal reports whether the cause needs an operator: the scan will not be tried again on its own.
func (c MissingCause) Terminal() bool {
	return c == MissingImageOverLimits || c == MissingImageUnreadable
}

// ImageMissingCause is the cause of an image whose detection has never finished (R-VULN-015, R-VULN-034): no database while none has been
// imported; else the state of its scan row: a failure judged under the bounds or the release is terminal, another failure is tried again,
// and a pending row or none is not scanned yet.
func ImageMissingCause(databaseImported, failed bool, judged string) MissingCause {
	switch {
	case !databaseImported:
		return MissingImageDatabase
	case !failed:
		return MissingImageNotScanned
	case strings.HasPrefix(judged, JudgedLimitsPrefix):
		return MissingImageOverLimits
	case judged != "":
		return MissingImageUnreadable
	}
	return MissingImageScanRetried
}

// dataCause is the cause of a missing-data block decided by the state of the ecosystem's data alone: for an image the database, for
// every other format its advisory data.
func dataCause(ecosystem string) MissingCause {
	if ecosystem == advisory.EcosystemOCI {
		return MissingImageDatabase
	}
	return MissingAdvisoryData
}
