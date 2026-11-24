package imagescan

import "fmt"

// What a terminal failure of a scan was judged under (R-VULN-034, Q-VULN-270). A code the same bytes cannot get past is not retried after a
// time: the same image under the same bounds and the same reading fails the same way, and a retry costs a pull. It is due again when what
// decided it changes, and at once then:
//
//	a code of a bound   image_too_large, too_many_layers, decompressed_size, too_many_entries: the bounds of vulnerabilities.images, which
//	                    an operator raises for the image that met them
//	a code of reading   layer_format, path_traversal, digest_mismatch, manifest_invalid, report_too_large: Quader's own unpacking in the
//	                    scanner and the analyser the release pins, which change with a release
//
// Neither class waits for a new vulnerability database: the analysis reads no database, so a new one changes nothing of either.

// Judges are the two judges of a worker: its bounds and its release.
type Judges struct {
	// Limits is the fingerprint of the bounds ([LimitsJudge]).
	Limits string
	// Release is the version of the release whose scanner reads the images (buildinfo).
	Release string
}

// LimitsJudge is the fingerprint of the bounds of the image scanning, as a terminal failure of a bound records it.
func LimitsJudge(l ContentLimits) string {
	return fmt.Sprintf("limits:max_layers=%d,max_image_size=%d,max_uncompressed_size=%d,max_entries=%d",
		l.MaxLayers, l.MaxImageBytes, l.MaxUncompressedBytes, l.MaxEntries)
}

// ReleaseJudge is the judge of a terminal failure of reading under a release.
func ReleaseJudge(version string) string { return "release:" + version }

// boundCode reports whether a terminal code is one of a bound, judged by the bounds rather than by the release.
func boundCode(code string) bool {
	switch code {
	case CodeImageTooLarge, CodeTooManyLayers, CodeDecompressedSize, CodeTooManyEntries:
		return true
	}
	return false
}

// judgedUnder is what a terminal failure of the code is judged under; never empty, so that the row is never read as a failure that may
// pass.
func (j Judges) judgedUnder(code string) string {
	if boundCode(code) {
		return j.limits()
	}
	return ReleaseJudge(j.Release)
}

// all are the judges a terminal row may have been judged under and still stand.
func (j Judges) all() []string { return []string{j.limits(), ReleaseJudge(j.Release)} }

// limits is the fingerprint of the bounds; without one, that of zero bounds.
func (j Judges) limits() string {
	if j.Limits == "" {
		return LimitsJudge(ContentLimits{})
	}
	return j.Limits
}
