// Package scanreport is a scanner replica's report of itself (operations-and-observability.md R-OPS-048, Q-OPS-223, ADR-224). A scanner
// holds no credential of PostgreSQL (ADR-212), so it cannot write the replica registry: every system.heartbeat_interval it sends its
// state, the version of the tool it runs, its slots and, for ClamAV, the signature database it scans with to the scan listener of
// packages, which it reaches already with the certificate of its kind (R-HA-066), and packages writes the row. At its clean shutdown it
// signs off. The kind is the one the certificate names; the scan listener never reads it from the report.
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
	"strings"

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
	// Database is the signature database clamd scans with; a scanner of the kind clamav only, empty before its first load.
	Database string `json:"database,omitempty"`
	Slots    Slots  `json:"slots"`
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

// Validate checks a report of a scanner of kind (config.ScannerKindTrivy or config.ScannerKindClamav) against the shape the table
// holds (migration 00090): bounded strings of a fixed character set, a known state, slots within the configuration's bounds, and a
// signature database only from the kind that has one.
func (r Report) Validate(kind string) error {
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
	case r.Database != "" && kind != config.ScannerKindClamav:
		return invalid("only a scanner of the kind clamav names a signature database")
	case r.Database != "" && (!bounded(r.Database, 128) || !version.MatchString(r.Database)):
		return invalid("database must be at most 128 letters, digits or . + _ ~ -")
	case r.Slots.Total < 1 || r.Slots.Total > 64 || r.Slots.InUse < 0 || r.Slots.InUse > r.Slots.Total:
		return invalid("slots must be 0 to total in use of 1 to 64")
	}
	return nil
}

func bounded(s string, n int) bool { return s != "" && len(s) <= n }

// ToolVersionOfClamd reads the version of ClamAV from clamd's answer to VERSION, "ClamAV <version>/<daily>/<date>"; "" for an answer of
// another form.
func ToolVersionOfClamd(line string) string {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), "ClamAV ")
	if !ok {
		return ""
	}
	v, _, _ := strings.Cut(rest, "/")
	if !bounded(v, 64) || !version.MatchString(v) {
		return ""
	}
	return v
}
