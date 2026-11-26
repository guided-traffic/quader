// Package scanreport is a scanner replica's report of itself (operations-and-observability.md R-OPS-048, Q-OPS-223, ADR-224). A scanner
// holds no credential of PostgreSQL (ADR-212), so it cannot write the replica registry: every system.heartbeat_interval it sends its
// state, the version of the tool it runs, its slots and the database it works with (for ClamAV the signature database clamd scans with,
// for Trivy the newest database its pod holds, Q-OPS-225) to the scan listener of packages, which it reaches already with the
// certificate of its kind (R-HA-066), and packages writes the row. At its clean shutdown it signs off. The kind is the one the
// certificate names; the scan listener never reads it from the report.
//
//	PUT     /scan/v1/replicas/<instance uid>   the report, application/json, at most MaxBodyBytes
//	DELETE  /scan/v1/replicas/<instance uid>   the sign-off
//
// Nothing a scanner reports is trusted beyond its size and shape (Validate): bounded strings of a fixed character set, so no markup,
// and at most MaxRowsPerKind live rows per kind. What a taken-over scanner can still make an administrator read is H-1161.
package scanreport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/guided-traffic/quader/internal/config"
)

// PathPrefix is the route of the report on the scan listener, followed by the replica's instance UID.
const PathPrefix = "/scan/v1/replicas/"

// Path is the route of the report of one replica.
func Path(uid string) string { return PathPrefix + uid }

// MaxBodyBytes bounds the body of a report: a report is a few hundred bytes.
const MaxBodyBytes = 4 << 10

// MaxRowsPerKind bounds the live rows of one kind (Q-OPS-223): a report of a new replica beyond it is refused, so a scanner that
// invents replicas fills no table. A Deployment of a scanner runs a handful of replicas.
const MaxRowsPerKind = 64

// The states of a report: the replica states of the registry (R-OPS-003).
const (
	StateStarting = "starting"
	StateReady    = "ready"
	StateUnready  = "unready"
	StateDraining = "draining"
)

// Report is the body of a report.
type Report struct {
	// Name is the pod's name (QUADER_POD_NAME), else the host's name and the process ID; Node the Kubernetes node, empty outside it.
	Name string `json:"name"`
	Node string `json:"node,omitempty"`
	// Version and Revision are the scanner's build of Quader.
	Version  string `json:"version"`
	Revision string `json:"revision"`
	// State is one of the State constants, Reason the code of the readiness check that fails (database, clamd, analyser,
	// certificate, spool), empty while the replica is ready or drains.
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
	// ToolVersion is the version of the tool the scanner runs (Trivy's, clamd's), empty while it does not know it.
	ToolVersion string `json:"toolVersion,omitempty"`
	// Database is the signature database clamd scans with (the kind clamav), the set the updater's directory holds (the kind freshclam),
	// or the 64 hexadecimal digits of the digest of the newest database the image scanner's pod holds (the kind trivy, Q-OPS-225); empty
	// before the first load and while the directory holds no complete set.
	Database string `json:"database,omitempty"`
	// DatabaseBuiltAt is the build time of the daily of the updater's directory (the kind freshclam, R-OPS-048, Q-OPS-224), or of the
	// image scanner's database's metadata.json (the kind trivy, Q-OPS-225).
	DatabaseBuiltAt *time.Time `json:"databaseBuiltAt,omitempty"`
	// Slots are a scanner's; the updater runs no calls and reports none (zero).
	Slots Slots `json:"slots"`
}

// Slots are the calls the scanner runs now and the calls it runs at a time.
type Slots struct {
	InUse int `json:"inUse"`
	Total int `json:"total"`
}

var (
	// name is a pod's name, a node's name, or a host's name with a process ID: letters, digits, dots, hyphens and underscores,
	// starting and ending with a letter or a digit.
	name = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,251}[A-Za-z0-9])?$`)
	// version is a version or a revision of Quader or of a tool, or a signature database's version.
	version = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_~-]*$`)
	// reason is the code of a readiness check's reason, or the name of the check (vulnScannerTrivy) where it gave none.
	reason = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,31}$`)
	// trivyDatabase is an image scanner's database: the 64 lower-case hexadecimal digits of its layer's digest.
	trivyDatabase = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ErrInvalid is the error of a report that is not of the shape Validate admits.
var ErrInvalid = errors.New("the scanner's report is not valid")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// ValidUID reports whether s is an instance UID as a scanner sends it: a UUID in its canonical lower-case form.
func ValidUID(s string) bool {
	u, err := uuid.Parse(s)
	return err == nil && u.String() == s
}

// Decode reads a report strictly: one JSON object of at most MaxBodyBytes with no unknown member.
func Decode(body io.Reader) (Report, error) {
	var r Report
	b, err := io.ReadAll(io.LimitReader(body, MaxBodyBytes+1))
	if err != nil {
		return Report{}, invalid("%v", err)
	}
	if len(b) > MaxBodyBytes {
		return Report{}, invalid("the body is larger than %d bytes", MaxBodyBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return Report{}, invalid("%v", err)
	}
	if dec.More() {
		return Report{}, invalid("more than one JSON value")
	}
	return r, nil
}

// Validate checks a report of a scanner of kind (config.ScannerKindTrivy, ScannerKindClamav or ScannerKindFreshclam) against the shape
// the table holds (migrations 00090, 00095 and the detection's): bounded strings of a fixed character set, a known state, slots within the
// configuration's bounds and none from the updater, an image scanner's database as the 64 hexadecimal digits of its digest, and a build
// time only from the updater and the image scanner, with the database it belongs to.
func (r Report) Validate(kind string) error {
	if err := r.validateCommon(); err != nil {
		return err
	}
	updater, trivy := kind == config.ScannerKindFreshclam, kind == config.ScannerKindTrivy
	switch {
	case r.Database != "" && trivy && !trivyDatabase.MatchString(r.Database):
		return invalid("an image scanner's database must be the 64 lower-case hexadecimal digits of its layer's digest")
	case r.Database != "" && !trivy && (!bounded(r.Database, 128) || !version.MatchString(r.Database)):
		return invalid("database must be at most 128 letters, digits or . + _ ~ -")
	case r.DatabaseBuiltAt != nil && ((!updater && !trivy) || r.Database == ""):
		return invalid("only the updater and the image scanner name the build time of the database they hold, with that database")
	case updater && (r.Slots.Total != 0 || r.Slots.InUse != 0):
		return invalid("the updater runs no calls and reports no slots")
	case !updater && (r.Slots.Total < 1 || r.Slots.Total > 64 || r.Slots.InUse < 0 || r.Slots.InUse > r.Slots.Total):
		return invalid("slots must be 0 to total in use of 1 to 64")
	}
	return nil
}

// validateCommon checks what every kind reports alike.
func (r Report) validateCommon() error {
	switch {
	case !name.MatchString(r.Name):
		return invalid("name must be 1 to 253 letters, digits, dots, hyphens or underscores")
	case r.Node != "" && !name.MatchString(r.Node):
		return invalid("node must be 1 to 253 letters, digits, dots, hyphens or underscores")
	case !bounded(r.Version, 64) || !version.MatchString(r.Version):
		return invalid("version must be 1 to 64 letters, digits or . + _ ~ -")
	case r.Revision != "" && (!bounded(r.Revision, 64) || !version.MatchString(r.Revision)):
		return invalid("revision must be at most 64 letters, digits or . + _ ~ -")
	case r.State != StateStarting && r.State != StateReady && r.State != StateUnready && r.State != StateDraining:
		return invalid("state must be starting, ready, unready or draining")
	case r.Reason != "" && !reason.MatchString(r.Reason):
		return invalid("reason must be a code of at most 32 letters, digits and underscores")
	case r.ToolVersion != "" && (!bounded(r.ToolVersion, 64) || !version.MatchString(r.ToolVersion)):
		return invalid("toolVersion must be at most 64 letters, digits or . + _ ~ -")
	}
	return nil
}

func bounded(s string, n int) bool { return s != "" && len(s) <= n }
