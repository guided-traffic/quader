package config

import (
	"path/filepath"
	"strings"
)

// scannerSections are the parts of the tree the role scanner reads (R-ARCH-036, Q-ARCH-527): the process, its listeners and their
// TLS, the probes, the log, the metrics, the runtime, its spool, and its kind's section: the image scanning for a scanner of the kind
// trivy, the malware section for one of the kind clamav (Q-ARCH-560, Q-ARCH-561), the updater's link and the scan listener's for the
// updater of the kind freshclam, which has no spool (R-MAL-032, Q-ARCH-562), and the pull link and the database's section for the loader
// of the kind trivy-db, which has no spool either (R-VULN-049, Q-ARCH-564). Every other key keeps its value but is neither validated nor
// resolved in that role, so a file the chart does not mount into the scanner (the database's, Valkey's, the object store's, the key
// ring's) is no error there; the role builds nothing that would read one (internal/app).
func scannerSections(kind string) []string {
	switch kind {
	case ScannerKindTrivyDB:
		return []string{"process", "server", "health", "log", "metrics", "runtime", "vulnerabilities.images.registry", "vulnerabilities.images.database"}
	case ScannerKindClamav:
		return []string{"process", "server", "health", "log", "metrics", "runtime", "storage.spool", "malware"}
	case ScannerKindFreshclam:
		return []string{"process", "server", "health", "log", "metrics", "runtime", "malware.updater", "malware.scan_listener"}
	}
	return []string{"process", "server", "health", "log", "metrics", "runtime", "storage.spool", "vulnerabilities.images"}
}

// scannerReads reports whether the role scanner of the kind reads the key or the section at path.
func scannerReads(path, kind string) bool {
	if path == "vulnerabilities.enabled" && kind == ScannerKindTrivy {
		return true
	}
	if path == "system.heartbeat_interval" {
		return true // the period of the scanner's report of itself (R-OPS-048)
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
	c.validateTrivyLoader(r)
}

// validateTrivyLoader is the loader's part (R-VULN-049, Q-ARCH-564): the scan listener it reads the database from and the directory it
// writes. The chart sets the first and mounts the second at its default.
func (c *Config) validateTrivyLoader(r *Report) {
	if r.Role() != RoleScanner || c.Process.ScannerKind != ScannerKindTrivyDB {
		return
	}
	images := c.Vulnerabilities.Images
	if images.Registry.URL == "" {
		r.Add("vulnerabilities.images.registry.url", "is required in the loader of the image scanner's database: it names the scan listener of packages the loader reads the database from (R-VULN-049); the chart sets it")
	}
	if !filepath.IsAbs(images.Database.Dir) {
		r.Add("vulnerabilities.images.database.dir", "%q must be an absolute path: the loader writes the database there and the scanner reads it (R-VULN-049)", images.Database.Dir)
	}
}
