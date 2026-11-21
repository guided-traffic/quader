package config

import "strings"

// scannerSections are the parts of the tree the role scanner reads (R-ARCH-036, Q-ARCH-527): the process, its listeners and their
// TLS, the probes, the log, the metrics, the runtime, its spool, and its kind's section: the image scanning for a scanner of the kind
// trivy, the malware section for one of the kind clamav (Q-ARCH-560, Q-ARCH-561). Every other key keeps its value but is neither validated nor
// resolved in that role, so a file the chart does not mount into the scanner (the database's, Valkey's, the object store's, the key
// ring's) is no error there; the role builds nothing that would read one (internal/app).
func scannerSections(kind string) []string {
	own := "vulnerabilities.images"
	if kind == ScannerKindClamav {
		own = "malware"
	}
	return []string{"process", "server", "health", "log", "metrics", "runtime", "storage.spool", own}
}

// scannerReads reports whether the role scanner of the kind reads the key or the section at path.
func scannerReads(path, kind string) bool {
	if path == "vulnerabilities.enabled" && kind != ScannerKindClamav {
		return true
	}
	for _, p := range scannerSections(kind) {
		if path == p || strings.HasPrefix(path, p+".") {
			return true
		}
	}
	return false
}

// ScannerReads is [scannerReads] for the tests of other packages and the documentation's list.
func ScannerReads(path, kind string) bool { return scannerReads(path, kind) }

// scannerValidator is a section that validates, in the role scanner, the part of its rules that bind the keys the role reads:
// the root's shutdown budget.
type scannerValidator interface {
	validateForScanner(r *Report)
}

// validateForScanner is the part of the root's cross-section rules that binds the role scanner: the shutdown budget against the
// pod's grace period (R-HA-011, R-HA-065) and the names of its links (R-ARCH-038). The rules that bind the database, the object
// store, Valkey or the login are not the role's.
func (c *Config) validateForScanner(r *Report) {
	c.validateShutdownBudget(r)
	c.validateLinkNames(r)
	c.validateMalwareScanner(r)
}
