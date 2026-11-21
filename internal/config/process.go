package config

import "slices"

// The roles a replica runs in (R-ARCH-001, Q-ARCH-294, Q-ARCH-295).
const (
	// RoleAPI serves the route families ui, api and ci_exchange and holds the sessions and the login secrets.
	RoleAPI = "api"
	// RolePackages serves the route families packages and files, with tokens only.
	RolePackages = "packages"
	// RoleWorker claims jobs and serves no client listener.
	RoleWorker = "worker"
	// RoleWeb is api and packages in one process without the job loops: a development role the chart never renders, the
	// default replica of the integration tests (Q-ARCH-295, Q-HA-315).
	RoleWeb = "web"
	// RoleAll is every role but the Bastion in one process, for local development.
	RoleAll = "all"
	// RoleScanner analyses container images for the worker and holds no secret of the database, the object store or Valkey
	// (R-ARCH-036, ADR-212): it serves the scanner's listener and the ops listener and nothing else.
	RoleScanner = "scanner"
)

// ValidRole reports whether role is one of the values a replica registers with, the value of --role, of the replica
// registry, of quader_build_info{role} and of the `role` of every log record (R-OPS-003, R-OPS-006, Q-OPS-161): one
// vocabulary for all of them.
func ValidRole(role string) bool {
	switch role {
	case RoleAPI, RolePackages, RoleWorker, RoleWeb, RoleAll, RoleScanner:
		return true
	}
	return false
}

// Process selects what one replica runs (R-ARCH-001).
type Process struct {
	// Role has no default: the chart sets it per Deployment and `quader serve
	// --role` overrides it.
	Role string `yaml:"role" validate:"oneof=api|packages|worker|scanner|web|all" doc:"Role of this replica: api, packages, worker, scanner (a scanning tool's wrapper, the tool chosen by process.scanner_kind, holding no secret of the installation, R-ARCH-036), web (api and packages in one process, development and tests only) or all (local development only). No default."`
	// ScannerKind chooses the tool a process of the role scanner wraps (R-ARCH-036, Q-ARCH-560, Q-ARCH-561).
	ScannerKind string `yaml:"scanner_kind" default:"trivy" validate:"oneof=trivy|trivy-db|clamav|freshclam" doc:"The tool a process of the role scanner wraps: trivy, the analysis of container images and the detection of their stored analyses with Trivy (R-VULN-041, R-VULN-048); trivy-db, the loader of Trivy's database, the second container of a pod of vuln-scanner-trivy, which reads the database in use through the scan listener into the directory the scanner mounts read-only (R-VULN-049, Q-ARCH-564); clamav, the malware scanner with clamd beside it (malware-scanning.md R-MAL-005 to R-MAL-007); or freshclam, the malware scanning's updater beside ClamAV's freshclam, which seeds, lists and serves freshclam's directory to the worker (R-MAL-032, Q-ARCH-562). The chart runs trivy and trivy-db in the Deployment <fullname>-vuln-scanner-trivy, sets clamav on <fullname>-malware-scanner-clamav and freshclam on <fullname>-malware-updater-freshclam. A scanner of the kind trivy-db reads vulnerabilities.images.registry and vulnerabilities.images.database, one of the kind clamav the section malware instead of vulnerabilities.images, one of the kind freshclam malware.updater and malware.scan_listener. Read by the role scanner only."`
}

// The kinds of a scanner, each named after the tool it wraps (Q-ARCH-560, Q-ARCH-561, Q-ARCH-562).
const (
	ScannerKindTrivy     = "trivy"
	ScannerKindTrivyDB   = "trivy-db"
	ScannerKindClamav    = "clamav"
	ScannerKindFreshclam = "freshclam"
)

// RoleError is the message `quader serve` prints when no valid role is set.
const RoleError = "process.role must be api, packages, worker, scanner, web or all"

// ServesClients reports whether a role opens client listeners: every role but worker.
func ServesClients(role string) bool {
	switch role {
	case RoleAPI, RolePackages, RoleWeb, RoleAll:
		return true
	}
	return false
}

// ValidateFor requires a role where one is run.
func (p *Process) ValidateFor(cmd Command, r *Report) {
	if cmd != CommandServe {
		return
	}
	switch p.Role {
	case RoleAPI, RolePackages, RoleWorker, RoleScanner, RoleWeb, RoleAll:
	default:
		r.Raw(RoleError)
	}
}

// RoleFamilies returns the route families a role serves (R-EP-005, Q-ARCH-298), in the fixed order of [Surfaces]: api the
// families ui, api and ci_exchange, packages the families packages and files, web and all every family, worker none. A
// process registers on each endpoint the families of its surface that its role serves.
func RoleFamilies(role string) []string {
	switch role {
	case RoleAPI:
		return []string{SurfaceUI, SurfaceAPI, SurfaceCIExchange}
	case RolePackages:
		return []string{SurfacePackages, SurfaceFiles}
	case RoleWeb, RoleAll:
		return Surfaces()
	}
	return nil
}

// IntersectFamilies returns the families of surface that a role's families hold, in the order of surface: the effective
// surface of an endpoint on a process (Q-ARCH-298). An empty result means the process opens no listener for the endpoint.
func IntersectFamilies(surface, role []string) []string {
	var out []string
	for _, f := range surface {
		if slices.Contains(role, f) {
			out = append(out, f)
		}
	}
	return out
}
