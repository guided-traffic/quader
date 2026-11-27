package vuln

// CheckState says whether a stored version is checked for vulnerabilities and, when it is not, why and what comes (R-VULN-039,
// Q-VULN-274): the one state every list cell, the version page's card and the security screens read, so that "N/A" never stands without
// its cause. The server derives it; a screen only says it in the reader's language.
type CheckState string

// The states. Only CheckChecked has a grade other than N/A.
const (
	// CheckChecked: the version is evaluated; its grade says the rest.
	CheckChecked CheckState = "checked"

	// Waits, and is checked without anyone's act.

	// CheckQueued: an image waits for its scan; StateSince is when it was queued, when that is known.
	CheckQueued CheckState = "queued"
	// CheckScanning: a call of the scanner for the image runs now; StateSince is when it started.
	CheckScanning CheckState = "scanning"

	// Cannot be checked now: the installation lacks something, and the version is checked once it is there.

	// CheckNoScanner: no scanner of images is connected (none configured, none ready, or the last call reached none).
	CheckNoScanner CheckState = "no_scanner"
	// CheckNoDatabase: no vulnerability database for images has been imported yet.
	CheckNoDatabase CheckState = "no_database"
	// CheckNoAdvisoryData: a package; the advisory data of its ecosystem has not been imported completely.
	CheckNoAdvisoryData CheckState = "no_advisory_data"

	// The scan failed; StateCode is the code of R-VULN-034.

	// CheckScanFailed: for a reason that may pass; it is tried again.
	CheckScanFailed CheckState = "scan_failed"
	// CheckOverLimits: it stopped at a bound of the scanning; it waits for an operator to raise it.
	CheckOverLimits CheckState = "over_limits"
	// CheckUnreadable: its content could not be read; it waits for a release that reads it.
	CheckUnreadable CheckState = "unreadable"

	// Not checked by setting or by what it is.

	// CheckOffInstallation: the installation's scanning is off (vulnerabilities.enabled, or vulnerabilities.images.enabled for an image).
	CheckOffInstallation CheckState = "off_installation"
	// CheckOffRepository: the repository's scanning is off (its member vulnerabilities.enabled).
	CheckOffRepository CheckState = "off_repository"
	// CheckNotMatched: the repository's setting matchPublicAdvisories is never.
	CheckNotMatched CheckState = "not_matched"
	// CheckUnknownUpstream: a remote repository whose upstream is not a public registry of its format, with matchPublicAdvisories auto
	// (R-VULN-010, H-314).
	CheckUnknownUpstream CheckState = "unknown_upstream"
	// CheckInternal: a name of the organisation's own; public advisories do not apply (R-VULN-010).
	CheckInternal CheckState = "internal"
	// CheckFormatNotScanned: the format has no vulnerability adapter (Debian, Ansible Galaxy, generic files).
	CheckFormatNotScanned CheckState = "format_not_scanned"
	// CheckNotAnImage: an OCI artifact that is no container image (a chart, a signature, an attestation), which is never scanned.
	CheckNotAnImage CheckState = "not_an_image"

	// The two states an anonymous caller is told instead of the installation's causes (R-VULN-039, Q-VULN-280): never answered to a
	// caller with a credential.

	// CheckPending: not checked yet; it is checked once the installation can (a scan waits, runs or failed, the data or a scanner is missing).
	CheckPending CheckState = "pending"
	// CheckNotChecked: not checked, by a setting of the installation or of the repository, or because public advisories do not apply.
	CheckNotChecked CheckState = "not_checked"
)

// Waits reports whether the state passes on its own: the version is checked without anyone's act. A screen that shows such a version
// reads it again while it is seen (R-UI-213).
func (c CheckState) Waits() bool { return c == CheckQueued || c == CheckScanning }

// Public is the state as an anonymous caller is told it (R-VULN-039, Q-VULN-280): checked, not checked yet, not checked, or what the
// version is (a format that is not scanned, an artifact that is no image), never the installation's cause behind it. The owner's
// decision of 2026-11-26 opens the open findings of public repositories to everyone; what the installation lacks or how a repository is
// set stays with who signs in.
func (c CheckState) Public() CheckState {
	switch c {
	case CheckChecked, CheckFormatNotScanned, CheckNotAnImage, CheckPending, CheckNotChecked:
		return c
	case CheckQueued, CheckScanning, CheckNoScanner, CheckNoDatabase, CheckNoAdvisoryData, CheckScanFailed, CheckOverLimits, CheckUnreadable:
		return CheckPending
	}
	return CheckNotChecked
}

// PublicEvaluation is the evaluation state as an anonymous caller is told it (Q-VULN-280): an internal name is not covered, and a
// failed scan is a scan that has not finished.
func PublicEvaluation(e Evaluation) Evaluation {
	switch e {
	case EvaluationInternal:
		return EvaluationNotCovered
	case EvaluationScanFailed:
		return EvaluationPendingScan
	}
	return e
}

// NoScannerCode reports whether a pending row's code says that no scanner could take its call: the codes of R-VULN-034 that a row put
// off for a reason of the installation keeps when none answered, the scanner could not pull from the scan listener, or the scan
// listener refused its token.
func NoScannerCode(code string) bool {
	switch code {
	case "scanner_unavailable", "registry_unavailable", "pull_refused":
		return true
	}
	return false
}

// UnitScannerTrivy is the unit that scans an image for vulnerabilities, the name a grade names of who checked it (R-VULN-051): the scanner
// of the kind trivy with its analyser. A package is matched by the worker against the advisory data, which is no scanner unit.
const UnitScannerTrivy = "vuln-scanner-trivy"
