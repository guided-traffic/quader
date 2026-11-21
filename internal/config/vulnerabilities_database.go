package config

import (
	"regexp"
	"time"

	"github.com/guided-traffic/quader/internal/qname"
)

// VulnerabilityImageDatabase names where the analyser's vulnerability database comes from (vulnerability-scanning.md R-VULN-044,
// Q-VULN-171): a local or remote OCI repository of the installation and the image in it. The tag is no setting: it is the schema
// version of the database the release reads (imagescan.DatabaseTag), so no setting can point the analyser at a schema it cannot read.
// What the repository must be (it exists, is of the format oci, is local or remote, is not disabled) is checked at every run of the
// task vuln-sync-trivy-db, because the configuration is read before the repository layer exists.
type VulnerabilityImageDatabase struct {
	Repository     string        `yaml:"repository" default:"upstream/scanner-trivy" doc:"The qualified name <projectKey>/<repoKey> of the OCI repository the database comes from: the system repository upstream/scanner-trivy, a remote of mirror.gcr.io that is always private and only for the scanner, or a local or remote OCI repository of an own project, for an installation without the internet (R-VULN-044). Whoever may push to such a repository decides what the images are compared with."`
	Image          string        `yaml:"image" default:"aquasec/trivy-db" doc:"The image of the database in that repository (R-OCI-002). Only aquasec/trivy-db is accepted while repository is upstream/scanner-trivy, whose include pattern admits that name only; another name is for a repository of an own project, for a registry that publishes the database under another name (ghcr.io: aquasecurity/trivy-db)."`
	MaxArchiveSize ByteSize      `yaml:"max_archive_size" default:"2GiB" validate:"min=1MiB" doc:"Largest database layer accepted: its declared size is checked before it is fetched or read, and a larger one fails the run with archive_too_large. The worker's spool must hold the unpacked database plus headroom."`
	Timeout        time.Duration `yaml:"timeout" default:"15m" validate:"min=10s,max=24h" doc:"Time limit of one run's fetch and check of the database's layer, and of one load of it by the loader of a scanner pod (R-VULN-049)."`
	// The copy of the database in each scanner pod (R-VULN-049, Q-VULN-196): written by the loader of the kind trivy-db, read-only to the
	// scanner of the kind trivy.
	Dir       string   `yaml:"dir" default:"/var/lib/quader/trivy-db" doc:"The directory of the scanner pod's copy of the database: the loader of the kind trivy-db writes each database into <dir>/<64 hex digits of its layer's digest>, renamed into place only when it is complete, and keeps the newest and the one before it; the scanner of the kind trivy mounts it read-only and detects with the database a call names (R-VULN-049). The chart mounts the volume trivy-db there. Read by the role scanner only; the worker reads none of it."`
	DirBudget ByteSize `yaml:"dir_budget" default:"5GiB" validate:"min=1MiB" doc:"The room of the databases' files in dir: the loader holds the sizes of a database's two files from the archive's headers before it writes a byte, and a database that does not fit beside the newest replaces it (detections answer database_loading meanwhile). The chart sets it to the volume's size, vulnScannerTrivy.database.sizeLimit (Q-HA-380, Q-VULN-209). Read by the loader of the kind trivy-db only."`
}

// ScannerTrivyRepository and ScannerTrivyImage are the defaults of the setting: wellknown.ScannerTrivy and wellknown.ScannerTrivyImage,
// spelled here because the configuration is in the Bastion's closure and the well-known repositories are not (internal/archtest);
// TestR_VULN_044_TheDefaultsAreTheWellKnownRepositorys in internal/wellknown holds them equal.
const (
	ScannerTrivyRepository = "upstream/scanner-trivy"
	ScannerTrivyImage      = "aquasec/trivy-db"
)

// imageNameRE is the image name grammar of formats/oci.md R-OCI-002, the one of internal/formats/oci (nameRE): components of lower-case
// alphanumeric runs separated by '.', '_', '__' or runs of '-', joined by '/'. The configuration is a leaf and cannot import the format.
var imageNameRE = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*)*$`)

// Validate refuses a repository that is no qualified name, an image that is no image name, and an image other than the one the
// well-known repository's include pattern admits while the repository is that one (R-VULN-044, Q-VULN-171).
func (d *VulnerabilityImageDatabase) Validate(r *Report) {
	name, err := qname.ParseRepo(d.Repository)
	if err != nil {
		r.Add("repository", "%q is not a repository's qualified name <projectKey>/<repoKey> (%v)", d.Repository, err)
	}
	if !imageNameRE.MatchString(d.Image) {
		r.Add("image", "%q is not an image name: lower-case components of letters and digits separated by '.', '_', '__' or '-', joined by '/' (R-OCI-002)", d.Image)
		return
	}
	if err == nil && name.String() == ScannerTrivyRepository && d.Image != ScannerTrivyImage {
		r.Add("image", "is %q, but repository is %s, whose include pattern admits %s only: name a remote OCI repository of an own project for a registry that publishes the database under another name (R-VULN-044)",
			d.Image, ScannerTrivyRepository, ScannerTrivyImage)
	}
}

// ReadsThroughScannerTrivy reports whether the image scanning reads its database through upstream/scanner-trivy: the scanning is on
// and the setting names that repository (R-REPO-070, Q-REPO-380). The admin page of the well-known repositories shows it as in use.
func (v Vulnerabilities) ReadsThroughScannerTrivy() bool {
	if !v.Enabled || !v.Images.Enabled {
		return false
	}
	name, err := qname.ParseRepo(v.Images.Database.Repository)
	return err == nil && name.String() == ScannerTrivyRepository
}
